package routing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLedgerSeparatesModelsCredentialsAndEndpoints(t *testing.T) {
	x := Candidate{Provider: "p", Protocol: "messages", Channel: "b", Credential: "g1", Endpoint: "e1", Model: "x"}
	y := x
	y.Model = "y"
	l := &Ledger{}
	l.Begin(x)
	l.Fail(x, Failure{Scope: ScopeModel, Reason: "model_not_found"})
	if ok, _ := l.Allowed(y); !ok {
		t.Fatal("model X failure rejected Y")
	}
	if ok, _ := l.Allowed(x); ok {
		t.Fatal("duplicate X allowed")
	}
	other := x
	other.Endpoint = "e2"
	if ok, _ := l.Allowed(other); ok {
		t.Fatal("same account and model escaped through another endpoint")
	}
	l.Fail(x, Failure{Scope: ScopeEndpoint, Reason: "network"})
	if ok, _ := l.Allowed(y); ok {
		t.Fatal("network failure must block endpoint for Y too")
	}
	other.Model = "y"
	if ok, _ := l.Allowed(other); !ok {
		t.Fatal("network failure leaked to independent endpoint")
	}
	l.Fail(other, Failure{Scope: ScopeCredential, Reason: "auth"})
	if ok, _ := l.Allowed(y); ok {
		t.Fatal("same credential escaped credential failure")
	}
	y.Credential = "g2"
	y.Endpoint = "e3"
	if ok, _ := l.Allowed(y); !ok {
		t.Fatal("credential failure leaked to independent credential")
	}
}

func TestBudgetStopsSendsAndBoundsWait(t *testing.T) {
	b := NewBudget(2, time.Hour)
	for range 2 {
		if err := b.Begin(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if !errors.Is(b.Begin(context.Background()), ErrBudgetExhausted) {
		t.Fatal("third send allowed")
	}
	if !errors.Is(b.Wait(context.Background(), time.Hour), ErrBudgetExhausted) {
		t.Fatal("wait allowed after attempts exhausted")
	}
	b = NewBudget(10, time.Millisecond)
	if !errors.Is(b.Wait(context.Background(), time.Hour), ErrBudgetExhausted) {
		t.Fatal("wait ignored duration")
	}
	b = NewBudget(10, time.Millisecond)
	started := time.Now()
	if !errors.Is(b.WaitRecovery(context.Background(), time.Hour), ErrBudgetExhausted) || time.Since(started) > 50*time.Millisecond {
		t.Fatal("recovery wait consumed the full budget instead of stopping when recovery cannot fit")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b = NewBudget(10, time.Hour)
	if !errors.Is(b.Begin(ctx), context.Canceled) {
		t.Fatal("cancelled send allowed")
	}
	if !errors.Is(b.Wait(ctx, time.Hour), context.Canceled) {
		t.Fatal("cancelled wait allowed")
	}
	if n, _ := b.Stats(); n != 0 {
		t.Fatal("cancelled request consumed attempt")
	}
}

func TestBudgetLimitsNeverResetOrWeakenPriorCeilings(t *testing.T) {
	b := NewBudget(10, time.Minute)
	if err := b.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	b.Limit(1, time.Hour)
	if !errors.Is(b.Begin(context.Background()), ErrBudgetExhausted) {
		t.Fatal("global limit reset the attempt count")
	}
	b.Limit(100, 10*time.Minute)
	if !errors.Is(b.Check(), ErrBudgetExhausted) {
		t.Fatal("a later stage weakened the attempt ceiling")
	}
	b = NewBudget(10, time.Minute)
	b.start = time.Now().Add(-2 * time.Second)
	b.Limit(0, time.Second)
	if !errors.Is(b.Check(), ErrBudgetExhausted) {
		t.Fatal("duration limit reset elapsed time")
	}
}

func TestBudgetReportsStopConditionAndSchedulingEvidence(t *testing.T) {
	t.Run("attempt ceiling", func(t *testing.T) {
		budget := NewBudget(1, time.Minute)
		if err := budget.Begin(context.Background()); err != nil {
			t.Fatal(err)
		}
		err := budget.Begin(context.Background())
		if !errors.Is(err, ErrBudgetExhausted) || BudgetStopReason(err) != StopRetryAttemptsExhausted {
			t.Fatalf("send ceiling lost its identity: %v", err)
		}
		budget.Stop(StopRetryDeadlineExceeded)
		if attempts, wait := budget.Stats(); attempts != 1 || wait != 0 || budget.Reason() != StopRetryAttemptsExhausted {
			t.Fatalf("stop changed counters or the first cause: attempts=%d wait=%d reason=%s", attempts, wait, budget.Reason())
		}
	})
	t.Run("elapsed deadline", func(t *testing.T) {
		budget := NewBudget(10, time.Minute)
		budget.start = time.Now().Add(-2 * time.Minute)
		err := budget.Begin(context.Background())
		if !errors.Is(err, ErrBudgetExhausted) || BudgetStopReason(err) != StopRetryDeadlineExceeded {
			t.Fatalf("elapsed deadline lost its identity: %v", err)
		}
		if attempts, wait := budget.Stats(); attempts != 0 || wait != 0 {
			t.Fatalf("expired request consumed execution: attempts=%d wait=%d", attempts, wait)
		}
	})
	t.Run("recovery cannot fit", func(t *testing.T) {
		budget := NewBudget(10, time.Minute)
		before := time.Now().Add(time.Hour)
		err := budget.WaitRecovery(context.Background(), time.Hour)
		after := time.Now().Add(time.Hour)
		if !errors.Is(err, ErrBudgetExhausted) || BudgetStopReason(err) != StopRecoveryOutsideBudget {
			t.Fatalf("future recovery lost its identity: %v", err)
		}
		if attempts, wait := budget.Stats(); attempts != 0 || wait != 0 {
			t.Fatalf("recovery preflight fabricated sends or wait: attempts=%d wait=%d", attempts, wait)
		}
		if at := budget.NextRecoveryAt(); at == nil || at.Before(before) || at.After(after) {
			t.Fatalf("expected recovery time missing: %v", at)
		}
		if attempts, duration := budget.Limits(); attempts != 10 || duration != time.Minute.Milliseconds() {
			t.Fatalf("limits missing: attempts=%d duration=%d", attempts, duration)
		}
	})
	t.Run("historical sentinel", func(t *testing.T) {
		if BudgetStopReason(errors.Join(errors.New("wrapped"), ErrBudgetExhausted)) != "retry_budget_exhausted" || !IsBudgetStopReason("retry_budget_exhausted") {
			t.Fatal("legacy stop reason is no longer recognized")
		}
	})
}
