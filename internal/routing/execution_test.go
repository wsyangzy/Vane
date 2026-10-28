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
