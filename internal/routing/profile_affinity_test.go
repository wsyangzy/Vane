package routing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionAffinityRestoresOnlyResourcePreferenceAndChecksBinding(t *testing.T) {
	config := runtimeTestConfig()
	config.Profiles[0].AffinityMode = AffinitySession
	runtime := NewProfileRuntime(config)
	turn := runtimeTestTurn("one")
	plan := runtime.Plan(context.Background(), 0, "test", true, turn, false)
	selected := plan.EligibleCandidates[1]
	reservation, err := runtime.Reserve(context.Background(), &plan, selected)
	if err != nil {
		t.Fatal(err)
	}
	if err = reservation.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	reservation.Finish(2, true, Failure{})
	entries := runtime.ExportSessionAffinity()
	if len(entries) != 1 {
		t.Fatalf("sessions=%+v", entries)
	}
	raw, _ := json.Marshal(entries)
	for _, secret := range []string{"PRIVATE", "conversation", "intent_id", "decision", "current_user_message"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("session persistence leaked %s", secret)
		}
	}
	restarted := NewProfileRuntime(config)
	restarted.SeedSessionAffinity(entries)
	next := restarted.Plan(context.Background(), 0, "test", true, runtimeTestTurn("two"), false)
	if next.EligibleCandidates[0].Identity() != selected.Identity() || next.Decision.ReusedTurn || next.Intent.Cached {
		t.Fatalf("restart did not restore resource-only preference: %+v", next)
	}
	changed := config
	changed.Candidates = append([]DispatchCandidate(nil), config.Candidates...)
	for i := range changed.Candidates {
		if changed.Candidates[i].GroupID == selected.GroupID {
			changed.Candidates[i].AccountIDHash = "different_account"
		}
	}
	rebound := NewProfileRuntime(changed)
	rebound.SeedSessionAffinity(entries)
	if len(rebound.ExportSessionAffinity()) != 0 {
		t.Fatal("stale account affinity survived binding change")
	}
}
