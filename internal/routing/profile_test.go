package routing

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestProfileDefaultsAndPersistentRuleIDs(t *testing.T) {
	p := RoutingProfile{Provider: "codex", Name: " 日常 ", Enabled: true, Rules: []RoutingRule{{ID: 2, Kind: "model", MatchModel: "gpt-*"}, {Kind: "non_stream"}}}
	if err := ValidateProfile(&p); err != nil {
		t.Fatal(err)
	}
	if p.Name != "日常" || p.CandidateMode != OrderedChannels || p.Strategy != StrategySmart || p.AffinityMode != AffinityTurn || p.MaxAttempts != 10 || p.MaxDuration != time.Minute {
		t.Fatalf("defaults: %+v", p)
	}
	if p.Rules[0].ID != 2 || p.Rules[1].ID != 3 {
		t.Fatalf("IDs collided after deleting an old rule: %+v", p.Rules)
	}
	p.Rules = append(p.Rules, RoutingRule{ID: 2, Kind: "non_stream"})
	if err := ValidateProfile(&p); err == nil {
		t.Fatal("duplicate persisted rule IDs allowed")
	}
}

func TestProfileRejectsCyclesAmbiguousTargetsAndInvalidClassifier(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*RoutingProfile)
	}{
		{"intent_cycle", func(p *RoutingProfile) {
			p.Intents = []IntentRule{{ID: "a", FallbackIntent: "b"}, {ID: "b", FallbackIntent: "a"}}
		}},
		{"missing_fallback", func(p *RoutingProfile) { p.Intents = []IntentRule{{ID: "a", FallbackIntent: "missing"}} }},
		{"ambiguous_target", func(p *RoutingProfile) {
			p.Rules = []RoutingRule{{Kind: "non_stream", Targets: []RouteTarget{{ProfileID: 2, ChannelID: 3}}}}
		}},
		{"budget", func(p *RoutingProfile) { p.MaxAttempts = 101 }},
		{"disabled_default", func(p *RoutingProfile) { p.Enabled = false; p.IsDefault = true }},
		{"duplicate_member", func(p *RoutingProfile) { p.Members = []RoutingMember{{ChannelID: 1}, {ChannelID: 1}} }},
		{"non_finite_confidence", func(p *RoutingProfile) { p.Classifier = &ClassifierConfig{Confidence: math.NaN()} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultProfile("claude")
			p.Name = "x"
			tc.change(&p)
			if err := ValidateProfile(&p); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestDispatchCandidateIdentityIncludesAllExecutionDimensions(t *testing.T) {
	base := DispatchCandidate{Provider: "codex", ChannelID: 1, GroupID: 2, EndpointID: "3", Model: "model", Protocol: "responses"}
	seen := map[Candidate]bool{base.Identity(): true}
	for _, change := range []func(*DispatchCandidate){func(c *DispatchCandidate) { c.Provider = "claude" }, func(c *DispatchCandidate) { c.ChannelID++ }, func(c *DispatchCandidate) { c.GroupID++ }, func(c *DispatchCandidate) { c.EndpointID = "4" }, func(c *DispatchCandidate) { c.Model = "other" }, func(c *DispatchCandidate) { c.Protocol = "messages" }, func(c *DispatchCandidate) { c.UpstreamModel = "rewrite" }} {
		c := base
		change(&c)
		if seen[c.Identity()] {
			t.Fatalf("candidate collided: %+v", c)
		}
		seen[c.Identity()] = true
	}
}

func TestProfileJSONContractContainsNoPoolAuthentication(t *testing.T) {
	p := DefaultProfile("codex")
	p.Name = "x"
	p.Rules = []RoutingRule{{Kind: "non_stream", FallbackToDefault: new(bool)}}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"candidate_mode"`, `"affinity_mode"`, `"max_duration"`, `"fallback_to_default":false`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("missing %s in %s", key, data)
		}
	}
	for _, key := range []string{"key_hash", "api_key", "pool_id", "authorization"} {
		if strings.Contains(string(data), key) {
			t.Fatalf("legacy credential field in profile: %s", data)
		}
	}
}
