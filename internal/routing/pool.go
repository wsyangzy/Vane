package routing

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	KindPool       Kind = "pool"
	PoolSequential      = "sequential"
	PoolRoundRobin      = "round_robin"
	TierPrimary         = "primary"
	TierBackup          = "backup"
)

// Pool contains request-local routing configuration. Referenced pools contribute
// members only; their rules are never evaluated recursively.
type Pool struct {
	ID        int64           `json:"id"`
	Provider  string          `json:"provider"`
	Name      string          `json:"name"`
	Strategy  string          `json:"strategy"`
	Default   bool            `json:"is_default"`
	Channels  []PoolChannel   `json:"channels"`
	Members   []PoolMember    `json:"members"`
	Rules     []ModelRule     `json:"rules"`
	NonStream *NonStreamRoute `json:"non_stream,omitempty"`
	KeyHash   string          `json:"-"`
	HasKey    bool            `json:"has_key"`
	Revision  int64           `json:"revision"`
}

// PoolChannel is a channel selected into a pool. Members alone select which
// groups participate; a channel without selected groups contributes no stages.
type PoolChannel struct {
	ChannelID int64  `json:"channel_id"`
	Tier      string `json:"tier"`
}

// ChannelsFromMembers interprets the old flat membership without deleting it.
// A channel with any primary group belongs to the primary tier.
func ChannelsFromMembers(members []PoolMember) []PoolChannel {
	channels := []PoolChannel{}
	positions := map[int64]int{}
	for _, member := range members {
		if at, exists := positions[member.ChannelID]; exists {
			if member.Tier == TierPrimary {
				channels[at].Tier = TierPrimary
			}
			continue
		}
		positions[member.ChannelID] = len(channels)
		channels = append(channels, PoolChannel{member.ChannelID, member.Tier})
	}
	return channels
}

type PoolMember struct {
	ChannelID int64  `json:"channel_id"`
	GroupID   int64  `json:"group_id"`
	Tier      string `json:"tier"`
}

type PoolStage struct {
	Endpoint string `json:"endpoint,omitempty"`
	PoolID   int64  `json:"pool_id"`
	PoolName string `json:"pool_name"`
	PoolMember
	UpstreamModel string `json:"upstream_model,omitempty"`
	Index         int    `json:"index"`
}

type PoolPlan struct {
	Pool     Pool
	Decision Decision
	Stages   []PoolStage
}

// AccountState is shared by every pool and by direct routes. Identity is a
// configuration/account identifier, never a token or an authorization header.
type AccountState struct {
	ChannelID int64  `json:"channel_id"`
	GroupID   int64  `json:"group_id"`
	Identity  string `json:"-"`
	Enabled   bool   `json:"enabled"`
	Managed   bool   `json:"managed"`
}

type PoolConfig struct {
	Pools          []Pool
	Accounts       []AccountState
	CooldownPolicy CooldownPolicy
}

type poolCallerKey struct{}
type PoolCaller struct {
	Provider string
	PoolID   int64
	Revision int64
}

func WithPoolCaller(ctx context.Context, caller PoolCaller) context.Context {
	return WithCallerScope(context.WithValue(ctx, poolCallerKey{}, caller), fmt.Sprintf("pool:%s:%d:%d", caller.Provider, caller.PoolID, caller.Revision))
}
func PoolCallerFromContext(ctx context.Context) (PoolCaller, bool) {
	c, ok := ctx.Value(poolCallerKey{}).(PoolCaller)
	return c, ok
}

type Cooldown struct {
	Provider string    `json:"provider"`
	Identity string    `json:"identity"`
	Endpoint string    `json:"endpoint,omitempty"`
	Model    string    `json:"model,omitempty"`
	Reason   string    `json:"reason"`
	Until    time.Time `json:"until"`
	Failures int       `json:"failures"`
	Repair   bool      `json:"repair"`
	Probing  bool      `json:"probing"`
}

type cooldownKey struct{ identity, endpoint, model string }

// PoolRuntime serializes round-robin cursors and half-open admissions. Config
// replacement keeps cooldown and cursor state across UI edits.
type PoolRuntime struct {
	profileRuntime atomic.Pointer[ProfileRuntime]
	mu             sync.Mutex
	config         PoolConfig
	cursors        map[string]uint64
	cooldowns      map[cooldownKey]Cooldown
	leases         map[cooldownKey]uint64
	sequence       uint64
	persist        func([]Cooldown) error
	now            func() time.Time
}

// The application always attaches unified dispatch, even without a default profile.
func (r *PoolRuntime) SetProfileRuntime(profile *ProfileRuntime) { r.profileRuntime.Store(profile) }
func (r *PoolRuntime) ProfileRuntime() *ProfileRuntime {
	if r == nil {
		return nil
	}
	return r.profileRuntime.Load()
}

func NewPoolRuntime(cfg PoolConfig, saved []Cooldown, persist func([]Cooldown) error) *PoolRuntime {
	r := &PoolRuntime{config: cfg, cursors: map[string]uint64{}, cooldowns: map[cooldownKey]Cooldown{}, leases: map[cooldownKey]uint64{}, persist: persist, now: time.Now}
	for _, c := range saved {
		c.Probing = false
		r.cooldowns[cooldownKey{c.Identity, c.Endpoint, c.Model}] = c
	}
	return r
}
func (r *PoolRuntime) Replace(cfg PoolConfig) { r.mu.Lock(); defer r.mu.Unlock(); r.config = cfg }
func (r *PoolRuntime) Config() PoolConfig     { r.mu.Lock(); defer r.mu.Unlock(); return r.config }
func (r *PoolRuntime) pool(id int64) *Pool {
	for i := range r.config.Pools {
		p := &r.config.Pools[i]
		if p.ID == id || id == 0 && p.Default {
			return p
		}
	}
	return nil
}
func (r *PoolRuntime) Account(channel, group int64) (AccountState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.account(channel, group)
}
func (r *PoolRuntime) account(channel, group int64) (AccountState, bool) {
	for _, a := range r.config.Accounts {
		if a.ChannelID == channel && a.GroupID == group {
			return a, true
		}
	}
	return AccountState{}, false
}

func (r *PoolRuntime) ValidCaller(c PoolCaller) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.pool(c.PoolID)
	return p != nil && p.Provider == c.Provider && p.Revision == c.Revision && p.KeyHash != ""
}

func (r *PoolRuntime) Plan(ctx context.Context, provider, model string, streaming bool) PoolPlan {
	return r.plan(ctx, provider, model, streaming, true)
}

// PlanDefault excludes generation rules for protocol helper requests.
func (r *PoolRuntime) PlanDefault(ctx context.Context, provider string) PoolPlan {
	return r.plan(ctx, provider, "", true, false)
}

func (r *PoolRuntime) plan(ctx context.Context, provider, model string, streaming, rules bool) PoolPlan {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := int64(0)
	if caller, ok := PoolCallerFromContext(ctx); ok && caller.Provider == provider {
		p := r.pool(caller.PoolID)
		if p == nil || p.Revision != caller.Revision {
			return PoolPlan{}
		}
		id = caller.PoolID
	}
	p := r.pool(id)
	if p == nil {
		return PoolPlan{}
	}
	plan := PoolPlan{Pool: *p}
	if rules {
		plan.Decision = Decide(p.Rules, p.NonStream, model, streaming)
	}
	appendPool := func(target *Pool, upstream string, index int) {
		if target == nil {
			return
		}
		channels := target.Channels
		if len(channels) == 0 {
			channels = ChannelsFromMembers(target.Members)
		}
		groups := map[int64][]PoolMember{}
		for _, member := range target.Members {
			groups[member.ChannelID] = append(groups[member.ChannelID], member)
		}
		for _, tier := range []string{TierPrimary, TierBackup} {
			var candidates []PoolChannel
			for _, channel := range channels {
				if channel.Tier == tier && len(groups[channel.ChannelID]) > 0 {
					candidates = append(candidates, channel)
				}
			}
			if target.Strategy == PoolRoundRobin && len(candidates) > 0 {
				key := fmt.Sprintf("%d/%s", target.ID, tier)
				offset := int(r.cursors[key] % uint64(len(candidates)))
				r.cursors[key]++
				candidates = append(append([]PoolChannel{}, candidates[offset:]...), candidates[:offset]...)
			}
			for _, channel := range candidates {
				for _, member := range groups[channel.ChannelID] {
					member.Tier = tier
					plan.Stages = append(plan.Stages, PoolStage{PoolID: target.ID, PoolName: target.Name, PoolMember: member, UpstreamModel: upstream, Index: index})
				}
			}
		}
	}
	if !plan.Decision.Routed() {
		appendPool(p, "", 0)
		return plan
	}
	for _, stage := range plan.Decision.Stages() {
		if stage.Target == nil {
			appendPool(p, "", stage.Index)
			continue
		}
		t := stage.Target
		if t.PoolID > 0 {
			appendPool(r.pool(t.PoolID), t.UpstreamModel, stage.Index)
			continue
		}
		// Compatibility targets are bounded to the original configured account.
		for _, a := range r.config.Accounts {
			if a.ChannelID == t.ChannelID && (t.GroupID == 0 && !a.Managed || a.GroupID == t.GroupID) {
				plan.Stages = append(plan.Stages, PoolStage{PoolID: p.ID, PoolName: p.Name, PoolMember: PoolMember{a.ChannelID, a.GroupID, TierPrimary}, UpstreamModel: t.UpstreamModel, Index: stage.Index})
			}
		}
	}
	return plan
}

func (r *PoolRuntime) identity(channel, group int64) string {
	if a, ok := r.account(channel, group); ok && a.Identity != "" {
		return a.Identity
	}
	return fmt.Sprintf("%d/%d", channel, group)
}

func (r *PoolRuntime) Allowed(channel, group int64, endpoint, model string) (bool, string) {
	return r.allowed(channel, group, endpoint, model, true)
}

// HealthAllowed shares credential/endpoint recovery state with legacy routing,
// but profile membership, rather than the legacy account switch, grants access.
func (r *PoolRuntime) HealthAllowed(channel, group int64, endpoint, model string) (bool, string) {
	return r.allowed(channel, group, endpoint, model, false)
}

func (r *PoolRuntime) allowed(channel, group int64, endpoint, model string, legacy bool) (bool, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.account(channel, group)
	if !ok {
		return false, "account_missing"
	}
	if legacy && !a.Enabled {
		return false, "account_disabled"
	}
	for k, c := range r.cooldowns {
		if k.identity != r.identity(channel, group) || k.endpoint != "" && k.endpoint != endpoint || k.model != "" && k.model != model {
			continue
		}
		if c.Repair {
			return false, "account_repair_required"
		}
		if c.Probing {
			return false, "account_probe_in_progress"
		}
		if r.now().Before(c.Until) {
			return false, "account_cooling_down"
		}
	}
	return true, ""
}

// Acquire must run immediately before a send, after auth/state/model checks.
func (r *PoolRuntime) Acquire(channel, group int64, endpoint, model string) (func(bool), error) {
	return r.acquire(channel, group, endpoint, model, true)
}

// AcquireHealth retains the same single recovery probe and lease ownership as
// Acquire. Callers must first authorize the candidate through a profile plan.
func (r *PoolRuntime) AcquireHealth(channel, group int64, endpoint, model string) (func(bool), error) {
	return r.acquire(channel, group, endpoint, model, false)
}

func (r *PoolRuntime) acquire(channel, group int64, endpoint, model string, legacy bool) (func(bool), error) {
	r.mu.Lock()
	a, ok := r.account(channel, group)
	if !ok || legacy && !a.Enabled {
		r.mu.Unlock()
		return nil, errors.New("account_disabled")
	}
	identity := r.identity(channel, group)
	var keys []cooldownKey
	for k, c := range r.cooldowns {
		if k.identity != identity || k.endpoint != "" && k.endpoint != endpoint || k.model != "" && k.model != model {
			continue
		}
		if c.Repair || c.Probing || r.now().Before(c.Until) {
			r.mu.Unlock()
			return nil, errors.New("account_cooling_down")
		}
		keys = append(keys, k)
	}
	tokens := make(map[cooldownKey]uint64, len(keys))
	for _, k := range keys {
		c := r.cooldowns[k]
		c.Probing = true
		r.cooldowns[k] = c
		r.sequence++
		r.leases[k] = r.sequence
		tokens[k] = r.sequence
	}
	r.mu.Unlock()
	var once sync.Once
	return func(success bool) {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			for _, k := range keys {
				c, exists := r.cooldowns[k]
				if !exists || !c.Probing || r.leases[k] != tokens[k] {
					continue
				}
				delete(r.leases, k)
				if success {
					delete(r.cooldowns, k)
				} else {
					c.Probing = false
					if !r.now().Before(c.Until) {
						c.Until = r.now().Add(r.config.CooldownPolicy.delay(c.Failures))
					}
					r.cooldowns[k] = c
				}
			}
			_ = r.saveLocked()
		})
	}, nil
}

func (r *PoolRuntime) Fail(channel, group int64, endpoint, model string, f Failure, retryAt time.Time) error {
	if f.Terminal || f.Scope == ScopeRequest {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	k := cooldownKey{identity: r.identity(channel, group)}
	if f.Scope == ScopeEndpoint {
		k.endpoint = endpoint
	} else if f.Scope != ScopeCredential {
		k.model = model
		if f.Scope == ScopeCandidate {
			k.endpoint = endpoint
		}
	}
	c := r.cooldowns[k]
	c.Identity, c.Endpoint, c.Model = k.identity, k.endpoint, k.model
	c.Failures++
	c.Reason = f.Reason
	c.Probing = false
	delete(r.leases, k)
	c.Repair = f.Scope == ScopeCredential
	delay := r.config.CooldownPolicy.delay(c.Failures)
	c.Until = r.now().Add(delay)
	if retryAt.After(r.now()) {
		c.Until = retryAt
	}
	r.cooldowns[k] = c
	return r.saveLocked()
}
func (r *PoolRuntime) Cooldowns() []Cooldown {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cooldownsLocked()
}

// ClearRepair is invoked only after explicit successful credential repair.
func (r *PoolRuntime) ClearRepair(identity string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, state := range r.cooldowns {
		if key.identity == identity && state.Repair {
			delete(r.cooldowns, key)
			delete(r.leases, key)
		}
	}
	return r.saveLocked()
}
func (r *PoolRuntime) cooldownsLocked() []Cooldown {
	out := make([]Cooldown, 0, len(r.cooldowns))
	for _, c := range r.cooldowns {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Until.Before(out[j].Until) })
	return out
}
func (r *PoolRuntime) saveLocked() error {
	if r.persist != nil {
		return r.persist(r.cooldownsLocked())
	}
	return nil
}

// NextRecovery returns only configured, enabled accounts with a timed recovery.
// Missing/disabled/auth-repair accounts must never make a request wait.
func (r *PoolRuntime) NextRecovery(stages []PoolStage, model string) (time.Time, bool) {
	return r.NextRecoveryWith(stages, model, nil)
}

// External availability remains owned by the provider's quota controller.
func (r *PoolRuntime) NextRecoveryWith(stages []PoolStage, model string, external func(PoolStage) (time.Time, bool)) (time.Time, bool) {
	var next time.Time
	for _, stage := range stages {
		effective := model
		if stage.UpstreamModel != "" {
			effective = stage.UpstreamModel
		}
		ready, valid := r.ReadyAt(stage, effective)
		if !valid {
			continue
		}
		if external != nil {
			at, ok := external(stage)
			if !ok {
				continue
			}
			if at.After(ready) {
				ready = at
			}
		}
		if ready.After(r.now()) && (next.IsZero() || ready.Before(next)) {
			next = ready
		}
	}
	return next, !next.IsZero()
}

func (r *PoolRuntime) ReadyAt(s PoolStage, model string) (time.Time, bool) {
	return r.readyAt(s, model, true)
}

func (r *PoolRuntime) HealthReadyAt(channel, group int64, endpoint, model string) (time.Time, bool) {
	return r.readyAt(PoolStage{PoolMember: PoolMember{ChannelID: channel, GroupID: group}, Endpoint: endpoint}, model, false)
}

func (r *PoolRuntime) readyAt(s PoolStage, model string, legacy bool) (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.account(s.ChannelID, s.GroupID)
	if !ok || legacy && !a.Enabled {
		return time.Time{}, false
	}
	effective := model
	if s.UpstreamModel != "" {
		effective = s.UpstreamModel
	}
	blocked := false
	var ready time.Time
	for k, c := range r.cooldowns {
		if k.identity != r.identity(s.ChannelID, s.GroupID) || k.model != "" && k.model != effective || k.endpoint != "" && s.Endpoint != "" && k.endpoint != s.Endpoint {
			continue
		}
		if c.Repair {
			blocked = true
			break
		}
		if c.Probing {
			blocked = true
			break
		}
		if c.Until.After(ready) {
			ready = c.Until
		}
	}
	return ready, !blocked
}

func ValidatePool(p *Pool, catalog Catalog, pools []Pool) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return errors.New("账号池名称不能为空")
	}
	if len([]rune(p.Name)) > 100 {
		return errors.New("账号池名称不能超过 100 个字符")
	}
	if p.Strategy != "sequential" && p.Strategy != "round_robin" {
		return errors.New("账号池调度策略无效")
	}
	if p.Channels == nil {
		p.Channels = ChannelsFromMembers(p.Members)
	}
	channels := map[int64]PoolChannel{}
	for _, channel := range p.Channels {
		if catalog.Channel(channel.ChannelID) == nil || channel.Tier != TierPrimary && channel.Tier != TierBackup {
			return errors.New("入池渠道或主备层级无效")
		}
		if _, exists := channels[channel.ChannelID]; exists {
			return errors.New("同一渠道不能重复入池")
		}
		channels[channel.ChannelID] = channel
	}
	seen := map[PoolMember]bool{}
	for i, m := range p.Members {
		channel, exists := channels[m.ChannelID]
		if !exists {
			return errors.New("请先将分组所属渠道加入账号池")
		}
		if m.GroupID <= 0 || m.Tier != TierPrimary && m.Tier != TierBackup {
			return fmt.Errorf("第 %d 个成员的账号或层级无效", i+1)
		}
		key := m
		key.Tier = ""
		if seen[key] {
			return errors.New("同一账号不能重复入池")
		}
		seen[key] = true
		if err := catalog.Validate([]Target{{ChannelID: m.ChannelID, GroupID: m.GroupID}}); err != nil {
			return err
		}
		p.Members[i].Tier = channel.Tier
	}
	validateTargets := func(targets []Target) error {
		for _, t := range targets {
			if t.PoolID > 0 {
				found := false
				for _, v := range pools {
					if v.ID == t.PoolID {
						found = true
					}
				}
				if !found {
					return errors.New("目标账号池不存在")
				}
				continue
			}
			if err := catalog.Validate([]Target{t}); err != nil {
				return err
			}
		}
		return nil
	}
	ruleIDs := map[int64]bool{}
	for _, rule := range p.Rules {
		if rule.ID <= 0 || ruleIDs[rule.ID] {
			return errors.New("模型规则标识无效或重复")
		}
		ruleIDs[rule.ID] = true
		if strings.TrimSpace(rule.MatchModel) == "" || NormalizeMode(rule.Mode) == "" || len(rule.Targets) == 0 {
			return errors.New("模型规则需要模型、模式和目标")
		}
		if err := validateTargets(rule.Targets); err != nil {
			return err
		}
	}
	if p.NonStream != nil {
		if NormalizeMode(p.NonStream.Mode) == "" || len(p.NonStream.Targets) == 0 {
			return errors.New("非流改道需要模式和目标")
		}
		return validateTargets(p.NonStream.Targets)
	}
	return nil
}
