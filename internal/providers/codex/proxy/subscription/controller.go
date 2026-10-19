package subscription

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type scope struct {
	group, version int64
	model          string
}
type Controller struct {
	rejectAccess    func(context.Context, string, string)
	resolveAccount  func(context.Context, string) (string, string, error)
	validateAccount func(context.Context, string) error
	store           Store
	mu              sync.Mutex
	pauses          map[scope]Pause
	probing         map[scope]uint64
	sequence        uint64
	pending         map[scope]Pause
	onChange        func()
	now             func() time.Time
}

func NewController(store Store) *Controller {
	return &Controller{store: store, pauses: map[scope]Pause{}, probing: map[scope]uint64{}, pending: map[scope]Pause{}, now: time.Now}
}

func (c *Controller) SetOnChange(callback func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onChange = callback
}

func (c *Controller) SetAccountResolver(resolve func(context.Context, string) (string, string, error), validate func(context.Context, string) error) {
	c.resolveAccount = resolve
	c.validateAccount = validate
}

func (c *Controller) SetCredentialRejector(reject func(context.Context, string, string)) {
	c.rejectAccess = reject
}
func (c *Controller) RejectCredential(ctx context.Context) {
	r := FromContext(ctx)
	if r != nil && r.Binding.AccountID != "" && c.rejectAccess != nil {
		c.rejectAccess(ctx, r.Binding.AccountID, strings.TrimPrefix(r.authorization, "Bearer "))
	}
}

func (c *Controller) current(ctx context.Context, b Binding) bool {
	return c.valid(ctx, b, true)
}

func (c *Controller) Valid(ctx context.Context, b Binding) bool { return c.valid(ctx, b, false) }

func (c *Controller) valid(ctx context.Context, b Binding, requireEnabled bool) bool {
	if requireEnabled && b.AccountID != "" && (c.validateAccount == nil || c.validateAccount(ctx, b.AccountID) != nil) {
		return false
	}
	if requireEnabled {
		if r := FromContext(ctx); r != nil && r.credentialUnavailable {
			return false
		}
	}
	bindings, err := c.store.ListBindings(ctx)
	if err != nil {
		return false
	}
	for _, current := range bindings {
		if current.GroupID == b.GroupID && current.Version == b.Version {
			return !requireEnabled || current.Enabled
		}
	}
	return false
}

func (c *Controller) load(ctx context.Context) error {
	for key, p := range c.pending {
		if err := c.store.PutPause(ctx, p); err != nil {
			return err
		}
		delete(c.pending, key)
	}
	pauses, err := c.store.ListPauses(ctx)
	if err != nil {
		return err
	}
	// Persistent revisions make manual verification/deletion win over an old
	// response. There are no timers or background credential-bearing probes.
	next := map[scope]Pause{}
	for _, p := range pauses {
		next[scope{p.GroupID, p.Version, p.Model}] = p
	}
	c.pauses = next
	return nil
}

func (c *Controller) Eligible(ctx context.Context, b Binding, model string) (bool, string) {
	if !c.current(ctx, b) {
		return false, "subscription_manual_disabled_or_rebound"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.load(ctx) != nil {
		return false, "subscription_state_unavailable"
	}
	for _, m := range applicableScopes(model) {
		key := scope{b.GroupID, b.Version, m}
		if c.probing[key] != 0 {
			return false, "subscription_probing"
		}
		if pause, ok := c.pauses[key]; ok && c.now().Before(pause.ResumeNotBefore) {
			return false, "subscription_paused"
		}
	}
	return true, ""
}

type Lease struct {
	c     *Controller
	b     Binding
	model string
	keys  map[scope]uint64
	once  sync.Once
}

func (c *Controller) Acquire(ctx context.Context, b Binding, model string) (*Lease, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !c.current(ctx, b) {
		return nil, ErrBindingChanged
	}
	c.mu.Lock()
	changed := false
	defer func() {
		callback := c.onChange
		c.mu.Unlock()
		if changed && callback != nil {
			callback()
		}
	}()
	if err := c.load(ctx); err != nil {
		return nil, errors.New("subscription_state_unavailable")
	}
	lease := &Lease{c: c, b: b, model: model, keys: map[scope]uint64{}}
	for _, m := range applicableScopes(model) {
		key := scope{b.GroupID, b.Version, m}
		if c.probing[key] != 0 {
			return nil, ErrUnavailable
		}
		if p, ok := c.pauses[key]; ok {
			if c.now().Before(p.ResumeNotBefore) {
				return nil, ErrUnavailable
			}
			lease.keys[key] = p.Revision
		}
	}
	for key := range lease.keys {
		changed = true
		c.sequence++
		c.probing[key] = c.sequence
	}
	return lease, nil
}

// Finish success is protocol success after complete EOF, never just HTTP 200.
// Cancellations/incomplete streams release admission without declaring recovery.
func (l *Lease) Finish(status int, headers http.Header, body []byte, success bool) {
	if l == nil {
		return
	}
	l.once.Do(func() {
		c := l.c
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		current := c.current(ctx, l.b)
		c.mu.Lock()
		changed := len(l.keys) > 0
		defer func() {
			callback := c.onChange
			c.mu.Unlock()
			if changed && callback != nil {
				callback()
			}
		}()
		defer func() {
			for key := range l.keys {
				delete(c.probing, key)
			}
		}()
		if !current || c.load(ctx) != nil {
			return
		}
		for key, revision := range l.keys {
			p, exists := c.pauses[key]
			if !exists || p.Revision != revision {
				continue
			}
			if success {
				if err := c.store.DeletePause(ctx, p); err != nil {
					slog.Error("Codex subscription pause persistence failed", "operation", "delete")
					p.Revision++
					p.ResumeNotBefore = c.now().Add(30 * time.Second)
					p.ResetKnown = false
					p.Reason = "recovery_persistence_failed"
					c.persist(ctx, p)
				}
			} else {
				p.ResumeNotBefore = c.now().Add(30 * time.Second)
				p.ResetKnown = false
				p.Reason = "recovery_unconfirmed"
				p.Source = "bounded_recheck"
				p.Revision++
				c.persist(ctx, p)
			}
		}
		for _, p := range QuotaPauses(status, headers, body, l.b, l.model, c.now()) {
			changed = true
			// A failed recovery may already have advanced the same revision.
			// Reload before applying stronger quota evidence.
			if c.load(ctx) != nil {
				return
			}
			key := scope{p.GroupID, p.Version, p.Model}
			if prior, ok := c.pauses[key]; ok {
				p.Revision = prior.Revision + 1
				if prior.ResumeNotBefore.After(p.ResumeNotBefore) {
					p.ResumeNotBefore = prior.ResumeNotBefore
					p.ResetKnown = prior.ResetKnown
				}
			}
			c.persist(ctx, p)
		}
	})
}

func (c *Controller) Status(ctx context.Context, b Binding) []Pause {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.load(ctx) != nil {
		return []Pause{{State: "unknown", Reason: "subscription_state_unavailable"}}
	}
	var result []Pause
	for key, p := range c.pauses {
		if key.group != b.GroupID || key.version != b.Version {
			continue
		}
		p.State = "paused"
		if !b.Enabled {
			p.State = "manual_disabled"
		} else if c.probing[key] != 0 {
			p.State = "probing"
		} else if !c.now().Before(p.ResumeNotBefore) {
			p.State = "due_unverified"
		}
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Model < result[j].Model })
	return result
}

func applicableScopes(model string) []string { return []string{"*", model} }

func (c *Controller) persist(ctx context.Context, p Pause) {
	key := scope{p.GroupID, p.Version, p.Model}
	c.pending[key] = p
	if err := c.store.PutPause(ctx, p); err != nil {
		slog.Error("Codex subscription pause persistence failed", "operation", "write")
		return
	}
	delete(c.pending, key)
}

func IsLocalError(err error) bool {
	return err != nil && (errors.Is(err, ErrUnavailable) || errors.Is(err, ErrBindingChanged) || strings.HasPrefix(err.Error(), "subscription_"))
}
