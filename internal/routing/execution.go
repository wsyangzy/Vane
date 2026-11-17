package routing

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

// Candidate identifies an execution choice, not just its network endpoint.
// Credential is an internal config identity; it must never contain a secret.
type Candidate struct {
	Provider     string `json:"provider"`
	Protocol     string `json:"protocol"`
	Channel      string `json:"channel"`
	Credential   string `json:"credential"`
	Endpoint     string `json:"endpoint"`
	Model        string `json:"model"`
	StateVersion uint64 `json:"state_version,omitempty"`
}

type FailureScope string

const (
	ScopeCandidate  FailureScope = "candidate"
	ScopeModel      FailureScope = "model"
	ScopeCredential FailureScope = "credential"
	ScopeEndpoint   FailureScope = "endpoint"
	ScopeRequest    FailureScope = "request"
)

// Failure is interpreted by the provider adapter. The shared ledger only
// applies the resulting scope; it does not interpret HTTP or provider codes.
type Failure struct {
	Scope    FailureScope `json:"scope"`
	Reason   string       `json:"reason"`
	Terminal bool         `json:"terminal,omitempty"`
}

type ExecutionEvent struct {
	Member    *PoolStage   `json:"member,omitempty"`
	Candidate Candidate    `json:"candidate"`
	Action    string       `json:"action"`
	Reason    string       `json:"reason,omitempty"`
	Scope     FailureScope `json:"failure_scope,omitempty"`
}

type Ledger struct {
	Member   *PoolStage `json:"-"`
	tried    map[Candidate]bool
	failures map[failureKey]Failure
	Events   []ExecutionEvent `json:"events,omitempty"`
	Dropped  int              `json:"dropped_events,omitempty"`
}

type failureKey struct {
	candidate Candidate
	scope     FailureScope
}

func (l *Ledger) event(event ExecutionEvent) {
	if l.Member != nil {
		member := *l.Member
		event.Member = &member
	}
	if len(l.Events) < 128 {
		l.Events = append(l.Events, event)
	} else {
		l.Dropped++
	}
}

func (l *Ledger) Allowed(c Candidate) (bool, string) {
	if l.tried[c] {
		return false, "candidate_already_tried"
	}
	for key, failure := range l.failures {
		failed := key.candidate
		if failed.Provider != c.Provider || failed.Protocol != c.Protocol {
			continue
		}
		match := false
		switch failure.Scope {
		case ScopeRequest:
			match = true
		case ScopeEndpoint:
			match = failed.Endpoint == c.Endpoint
		case ScopeCredential:
			match = failed.Credential == c.Credential
		case ScopeModel:
			match = failed.Credential == c.Credential && failed.Model == c.Model
		case ScopeCandidate:
			match = failed == c
		}
		if match {
			return false, failure.Reason
		}
	}
	return true, ""
}

func (l *Ledger) Begin(c Candidate) {
	if l.tried == nil {
		l.tried = make(map[Candidate]bool)
	}
	l.tried[c] = true
	l.event(ExecutionEvent{Candidate: c, Action: "attempt"})
}

func (l *Ledger) Fail(c Candidate, failure Failure) {
	if l.failures == nil {
		l.failures = make(map[failureKey]Failure)
	}
	l.failures[failureKey{candidate: c, scope: failure.Scope}] = failure
	l.event(ExecutionEvent{Candidate: c, Action: "failed", Reason: failure.Reason, Scope: failure.Scope})
}

func (l *Ledger) Skip(c Candidate, reason string) {
	l.event(ExecutionEvent{Candidate: c, Action: "skipped", Reason: reason})
}

func (l *Ledger) Wait(reason string) { l.event(ExecutionEvent{Action: "wait", Reason: reason}) }

func (l *Ledger) LastSkipReason() string {
	for i := len(l.Events) - 1; i >= 0; i-- {
		if l.Events[i].Action == "skipped" {
			return l.Events[i].Reason
		}
	}
	return ""
}

var ErrBudgetExhausted = errors.New("retry_budget_exhausted")

const (
	StopRetryAttemptsExhausted = "retry_attempts_exhausted"
	StopRetryDeadlineExceeded  = "retry_deadline_exceeded"
	StopRecoveryOutsideBudget  = "recovery_outside_budget"
)

// BudgetError keeps the legacy sentinel available to callers while preserving
// the condition that stopped this request's execution.
type BudgetError struct{ Reason string }

func (e *BudgetError) Error() string { return e.Reason }
func (e *BudgetError) Unwrap() error { return ErrBudgetExhausted }

func BudgetStopReason(err error) string {
	var stop *BudgetError
	if errors.As(err, &stop) {
		return stop.Reason
	}
	if errors.Is(err, ErrBudgetExhausted) {
		return ErrBudgetExhausted.Error()
	}
	return ""
}

func IsBudgetStopReason(reason string) bool {
	switch reason {
	case "retry_budget_exhausted", StopRetryAttemptsExhausted, StopRetryDeadlineExceeded, StopRecoveryOutsideBudget:
		return true
	}
	return false
}

// Budget limits new sends and waits. It deliberately does not install a
// deadline on an accepted response: a healthy long-running stream may finish.
type Budget struct {
	mu           sync.Mutex
	start        time.Time
	maxAttempts  int
	maxDuration  time.Duration
	attempts     int
	waited       time.Duration
	stopped      bool
	stopReason   string
	nextRecovery time.Time
}

func NewBudget(maxAttempts int, maxDuration time.Duration) *Budget {
	return &Budget{start: time.Now(), maxAttempts: maxAttempts, maxDuration: maxDuration}
}

// Limit combines profile and application ceilings without resetting elapsed
// time or attempts when execution moves to another stage or provider adapter.
func (b *Budget) Limit(maxAttempts int, maxDuration time.Duration) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if maxAttempts > 0 && (b.maxAttempts <= 0 || maxAttempts < b.maxAttempts) {
		b.maxAttempts = maxAttempts
	}
	if maxDuration > 0 && (b.maxDuration <= 0 || maxDuration < b.maxDuration) {
		b.maxDuration = maxDuration
	}
}

func (b *Budget) Check() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.checkLocked()
}

func (b *Budget) checkLocked() error {
	if b.stopped {
		return &BudgetError{Reason: b.stopReason}
	}
	if b.maxAttempts > 0 && b.attempts >= b.maxAttempts {
		return b.stopLocked(StopRetryAttemptsExhausted)
	}
	if b.maxDuration > 0 && time.Since(b.start) >= b.maxDuration {
		return b.stopLocked(StopRetryDeadlineExceeded)
	}
	return nil
}

func (b *Budget) Begin(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.checkLocked(); err != nil {
		b.stopped = true
		return err
	}
	b.attempts++
	return nil
}

func (b *Budget) Wait(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	limited := false
	if b != nil {
		b.mu.Lock()
		err := b.checkLocked()
		if b.maxDuration > 0 {
			remaining := b.maxDuration - time.Since(b.start)
			if delay >= remaining {
				delay = remaining
				limited = true
			}
		}
		b.mu.Unlock()
		if err != nil {
			b.Stop()
			return err
		}
	}
	if delay <= 0 {
		if limited {
			b.Stop(StopRetryDeadlineExceeded)
			return &BudgetError{Reason: StopRetryDeadlineExceeded}
		}
		return nil
	}
	start := time.Now()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	var waitErr error
	select {
	case <-ctx.Done():
		waitErr = ctx.Err()
	case <-timer.C:
	}
	if b != nil {
		b.mu.Lock()
		b.waited += time.Since(start)
		b.mu.Unlock()
	}
	if waitErr != nil {
		return waitErr
	}
	if limited {
		b.Stop(StopRetryDeadlineExceeded)
		return &BudgetError{Reason: StopRetryDeadlineExceeded}
	}
	if err := b.Check(); err != nil {
		b.Stop()
		return err
	}
	return nil
}

// WaitRecovery returns immediately when the endpoint recovery is beyond this
// request's remaining budget, instead of idling until the budget expires.
func (b *Budget) WaitRecovery(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b != nil {
		b.mu.Lock()
		b.nextRecovery = time.Now().Add(delay)
		if err := b.checkLocked(); err != nil {
			b.stopped = true
			b.mu.Unlock()
			return err
		}
		if b.maxDuration > 0 && delay >= b.maxDuration-time.Since(b.start) {
			err := b.stopLocked(StopRecoveryOutsideBudget)
			b.mu.Unlock()
			return err
		}
		b.mu.Unlock()
	}
	return b.Wait(ctx, delay)
}

func (b *Budget) stopLocked(reason string) error {
	b.stopped, b.stopReason = true, reason
	return &BudgetError{Reason: reason}
}

func (b *Budget) Stop(reasons ...string) {
	if b != nil {
		b.mu.Lock()
		if !b.stopped {
			if len(reasons) > 0 && IsBudgetStopReason(reasons[0]) {
				b.stopLocked(reasons[0])
			} else if b.checkLocked() == nil {
				b.stopLocked(ErrBudgetExhausted.Error())
			}
		}
		b.mu.Unlock()
	}
}

func (b *Budget) Reason() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopReason
}

// Limits and NextRecoveryAt expose public scheduling evidence only.
func (b *Budget) Limits() (int, int64) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxAttempts, b.maxDuration.Milliseconds()
}

func (b *Budget) NextRecoveryAt() *time.Time {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.nextRecovery.IsZero() {
		return nil
	}
	at := b.nextRecovery
	return &at
}

func (b *Budget) Stopped() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopped
}

func (b *Budget) Stats() (int, int64) {
	if b == nil {
		return 0, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attempts, b.waited.Milliseconds()
}

type budgetContextKey struct{}

func WithBudget(ctx context.Context, b *Budget) context.Context {
	return context.WithValue(ctx, budgetContextKey{}, b)
}
func BudgetFromContext(ctx context.Context) *Budget {
	b, _ := ctx.Value(budgetContextKey{}).(*Budget)
	return b
}

// ErrorIdentity extracts only structured identifiers. Message text is not a
// source of HTTP status or an authorization/credential identity.
func ErrorIdentity(body []byte) (code, kind, param string) {
	var envelope struct {
		Code  string `json:"code"`
		Type  string `json:"type"`
		Param string `json:"param"`
		Error *struct {
			Code  string `json:"code"`
			Type  string `json:"type"`
			Param string `json:"param"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return "", "", ""
	}
	if envelope.Error != nil {
		return envelope.Error.Code, envelope.Error.Type, envelope.Error.Param
	}
	return envelope.Code, envelope.Type, envelope.Param
}

func (l *Ledger) RetryRecovered() { l.tried = nil; l.failures = nil }
