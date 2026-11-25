package routing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDispatchReservationRetryAccountsForBothSendsWithoutAdvancingRotation(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.Profiles[0].Strategy = StrategyRotate
	var settlements []AttemptSettlement
	cfg.OnSettlement = func(value AttemptSettlement) { settlements = append(settlements, value) }
	runtime := NewProfileRuntime(cfg)
	now := time.Now()
	runtime.now = func() time.Time { return now }
	turn := runtimeTestTurn("first")
	turn.ConversationID = "first-conversation"
	plan := runtime.Plan(context.Background(), 1, "gpt-test", false, turn, false)
	candidate := plan.EligibleCandidates[0]
	attempt, err := runtime.Reserve(context.Background(), &plan, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := attempt.Retry(context.Background(), "invalid_encrypted_content"); err != nil {
		t.Fatal(err)
	}
	trace := plan.Trace()
	load := runtime.scheduler.Load(candidate, now)
	if trace.Attempts != 2 || len(trace.Candidates) != 3 || trace.Candidates[1].Action != "failed" || trace.Candidates[1].Reason != "invalid_encrypted_content" || trace.Candidates[2].Action != "attempt" || trace.Candidates[0].Candidate != trace.Candidates[2].Candidate || trace.StopReason != "" || load.Concurrent != 1 || load.DecayedRequests != 2 {
		t.Fatalf("retry changed routing or lost a send: trace=%+v load=%+v", trace, load)
	}
	attempt.SetUsage(AttemptUsage{Known: true, Complete: true, InputTokens: 2, OutputTokens: 3})
	attempt.Finish(5, true, Failure{})
	if len(settlements) != 2 || settlements[0].ID == settlements[1].ID || settlements[0].Candidate.Identity() != settlements[1].Candidate.Identity() || settlements[0].Usage.Known || settlements[1].Usage.OutputTokens != 3 {
		t.Fatalf("sends did not settle independently: %+v", settlements)
	}
	if load := runtime.scheduler.Load(candidate, now); load.Concurrent != 0 || load.DecayedTokens != 5 {
		t.Fatalf("reservation was not released exactly once: %+v", load)
	}
	if trace := plan.Trace(); !trace.Successful || trace.StopReason != "" || trace.Attempts != 2 {
		t.Fatalf("recovered request retained a terminal failure: %+v", trace)
	}
	turn = runtimeTestTurn("next")
	turn.ConversationID = "next-conversation"
	next := runtime.Plan(context.Background(), 1, "gpt-test", false, turn, false)
	if selected := runtimeTestSend(t, runtime, &next, 0); selected.GroupID == candidate.GroupID {
		t.Fatalf("retry advanced rotation and returned to the same group: %+v", selected)
	}
}

func TestDispatchReservationRetryRejectsOutputCancellationAndBudgetExhaustion(t *testing.T) {
	for _, condition := range []string{"not-begun", "response-started", "finished", "cancelled", "budget", "admission-changed"} {
		t.Run(condition, func(t *testing.T) {
			cfg := runtimeTestConfig()
			denied := false
			cfg.CheckAdmission = func(DispatchCandidate) string {
				if denied {
					return "candidate_configuration_changed"
				}
				return ""
			}
			settlements := 0
			cfg.OnSettlement = func(AttemptSettlement) { settlements++ }
			runtime := NewProfileRuntime(cfg)
			plan := runtime.Plan(context.Background(), 1, "gpt-test", true, runtimeTestTurn("first"), false)
			if condition == "budget" {
				plan.Budget.Limit(1, time.Second)
			}
			attempt, err := runtime.Reserve(context.Background(), &plan, plan.EligibleCandidates[0])
			if err != nil {
				t.Fatal(err)
			}
			if condition != "not-begun" {
				if err := attempt.Begin(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch condition {
			case "response-started":
				attempt.ResponseStarted()
			case "finished":
				attempt.Finish(0, false, Failure{Scope: ScopeRequest, Reason: "invalid_request"})
			case "cancelled":
				cancel()
			case "admission-changed":
				denied = true
			}
			before := plan.Trace()
			beforeSettlements := settlements
			err = attempt.Retry(ctx, "invalid_encrypted_content")
			if err == nil || condition == "cancelled" && !errors.Is(err, context.Canceled) || condition == "budget" && !errors.Is(err, ErrBudgetExhausted) {
				t.Fatalf("invalid retry accepted: condition=%s error=%v", condition, err)
			}
			if after := plan.Trace(); after.Attempts != before.Attempts || len(after.Candidates) != len(before.Candidates) || settlements != beforeSettlements {
				t.Fatalf("rejected retry created send evidence: before=%+v after=%+v settlements=%d", before, after, settlements)
			}
			attempt.Finish(0, false, Failure{Scope: ScopeRequest, Reason: "invalid_request"})
		})
	}
}

func TestDispatchReservationRetryTraceRedactsUnsafeReason(t *testing.T) {
	runtime := NewProfileRuntime(runtimeTestConfig())
	plan := runtime.Plan(context.Background(), 1, "gpt-test", false, runtimeTestTurn("first"), false)
	attempt, err := runtime.Reserve(context.Background(), &plan, plan.EligibleCandidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := attempt.Retry(context.Background(), "PRIVATE AUTHORIZATION PAYLOAD"); err != nil {
		t.Fatal(err)
	}
	for _, event := range plan.Trace().Candidates {
		if strings.Contains(event.Reason, "PRIVATE") {
			t.Fatalf("retry trace leaked an unsafe reason: %+v", event)
		}
	}
	attempt.Finish(0, true, Failure{})
}
