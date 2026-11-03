package routing

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func runtimeTestConfig() RuntimeConfig {
	p := dispatchTestProfile(StrategyOrder)
	p.Name, p.IsDefault = "test profile", true
	p.Members = []RoutingMember{{ChannelID: 1, GroupID: 1, Enabled: true, Weight: 1}, {ChannelID: 1, GroupID: 2, Enabled: true, Weight: 1}}
	return RuntimeConfig{Provider: "codex", Profiles: []RoutingProfile{p}, Candidates: dispatchTestCandidates()}
}

func runtimeTestTurn(id string) TurnInput {
	return TurnInput{ConversationID: "conversation", TurnID: id, NewTurn: true, CurrentUserMessage: "PRIVATE_CURRENT_TEXT", PreviousUserMessage: "PRIVATE_PREVIOUS_TEXT", PreviousAssistantText: "PRIVATE_ASSISTANT_TEXT"}
}

func TestRuleTraceCapturesDefaultSourceWithoutSendingOrChangingSelection(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		for _, mapped := range []string{"", "mapped-model"} {
			t.Run(provider+"/"+mapped, func(t *testing.T) {
				cfg := runtimeTestConfig()
				cfg.Provider, cfg.Profiles[0].Provider = provider, provider
				cfg.Profiles[0].Strategy = StrategyRotate
				cfg.Profiles[0].Rules = []RoutingRule{{ID: 1, Kind: "model", Enabled: true, MatchModel: "gpt-test", Targets: []RouteTarget{{ChannelID: 2, GroupID: 3, UpstreamModel: mapped}}}}
				cfg.Catalog = Catalog{{ID: 99, Name: "unrelated active", Active: true}, {ID: 1, Name: "original channel", Groups: []CatalogGroup{{ID: 1, Name: "original group"}}}}
				for i := range cfg.Candidates {
					cfg.Candidates[i].Provider = provider
					cfg.Candidates[i].AccountIDHash = "PRIVATE_ACCOUNT_HASH"
				}
				r := NewProfileRuntime(cfg)
				cfg.Catalog[1].Name = "renamed channel"
				var plan DispatchPlan
				for range 3 {
					plan = r.Plan(context.Background(), 1, "gpt-test", false, TurnInput{}, false)
					source := plan.Trace().DefaultSource
					if source == nil || source.ChannelID != 1 || source.GroupID != 1 || source.ChannelName != "original channel" || source.GroupName != "original group" || source.Unavailable {
						t.Fatalf("missing default source or borrowed legacy active channel: %+v", source)
					}
					if plan.Trace().Attempts != 0 || len(plan.Trace().Candidates) != 0 || len(r.scheduler.loads) != 0 || len(r.scheduler.rotation) != 0 || len(r.affinity) != 0 {
						t.Fatal("capturing the source changed sends, rotation, loads or affinity")
					}
					source.ChannelName = "modified returned trace"
					if plan.Trace().DefaultSource.ChannelName != "original channel" {
						t.Fatal("a returned trace mutated the captured source")
					}
				}
				if actual := runtimeTestSend(t, r, &plan, 0); actual.ChannelID != 2 || actual.GroupID != 3 {
					t.Fatalf("recording the source changed the rule destination: %+v", actual)
				}
				data, err := json.Marshal(plan.Trace())
				var persisted DispatchTrace
				if err != nil || json.Unmarshal(data, &persisted) != nil || persisted.DefaultSource == nil || persisted.DefaultSource.ChannelName != "original channel" || persisted.RequestedModel != "gpt-test" || persisted.Attempts != 1 || len(persisted.Candidates) == 0 || persisted.Candidates[0].Candidate.Channel != "2" {
					t.Fatalf("source was lost or confused with an actual attempt: %s (err=%v)", data, err)
				}
				sourceData, _ := json.Marshal(persisted.DefaultSource)
				if strings.Contains(string(sourceData), "PRIVATE") {
					t.Fatal("default source contains a credential identity")
				}
				r.Replace(cfg)
				if plan.Trace().DefaultSource.ChannelName != "original channel" {
					t.Fatal("a runtime refresh changed a historical source")
				}
			})
		}
	}
}

func TestRuleDefaultSourceUsesCurrentEligibilityAndKeepsUnavailableExplicit(t *testing.T) {
	for _, reason := range []string{"healthy", "model", "cooldown", "membership", "quota"} {
		t.Run(reason, func(t *testing.T) {
			cfg := runtimeTestConfig()
			cfg.Profiles[0].Rules = []RoutingRule{{ID: 1, Kind: "model", Enabled: true, MatchModel: "gpt-test", Targets: []RouteTarget{{ChannelID: 2, GroupID: 3}}}}
			cfg.Supports = func(c DispatchCandidate, model string) bool {
				return reason != "model" || c.ChannelID != 1 || c.GroupID != 1
			}
			switch reason {
			case "cooldown":
				cfg.Candidates[0].Health.CoolingDown = true
			case "membership":
				cfg.Profiles[0].Members[0].Enabled = false
			case "quota":
				cfg.Candidates[0].Quota.Windows = []QuotaSnapshot{dispatchTestWindow(time.Now(), 100, time.Hour)}
			}
			r := NewProfileRuntime(cfg)
			plan := r.Plan(context.Background(), 1, "gpt-test", false, TurnInput{}, false)
			source := plan.Trace().DefaultSource
			wantGroup := int64(1)
			if reason != "healthy" {
				wantGroup = 2
			}
			if source == nil || source.ChannelID != 1 || source.GroupID != wantGroup || source.Unavailable {
				t.Fatalf("default source ignored eligibility: %+v", source)
			}
			cfg.Profiles[0].Members = nil
			r.Replace(cfg)
			unavailable := r.Plan(context.Background(), 1, "gpt-test", false, TurnInput{}, false)
			if source = unavailable.Trace().DefaultSource; source == nil || !source.Unavailable || source.ChannelID != 0 || unavailable.EligibleCandidates[0].ChannelID != 2 {
				t.Fatalf("unavailable default was confused with the direct target: %+v", source)
			}
			cfg.Profiles[0].Rules = nil
			r.Replace(cfg)
			plain := r.Plan(context.Background(), 1, "gpt-test", false, TurnInput{}, false)
			if plain.Trace().DefaultSource != nil {
				t.Fatal("default orchestration invented a rule source")
			}
		})
	}
}

func TestAutomaticHealthScopeFollowsOnlyDefaultMembers(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			cfg := RuntimeConfig{Provider: provider, Profiles: []RoutingProfile{
				{ID: 1, Provider: provider, Enabled: true, IsDefault: true, Members: []RoutingMember{
					{ChannelID: 1, GroupID: 11, Enabled: true},
					{ChannelID: 2, Enabled: true},
					{ChannelID: 3, Enabled: false},
				}},
				{ID: 2, Provider: provider, Enabled: true, Members: []RoutingMember{{ChannelID: 4, Enabled: true}}},
			}, Candidates: []DispatchCandidate{
				{Provider: provider, ChannelID: 1, GroupID: 11, EndpointID: "selected", Enabled: true, Health: HealthState{RepairRequired: true}},
				{Provider: provider, ChannelID: 1, GroupID: 12, EndpointID: "other-group", Enabled: true},
				{Provider: provider, ChannelID: 2, GroupID: 21, EndpointID: "wildcard-a", Enabled: true},
				{Provider: provider, ChannelID: 2, GroupID: 22, EndpointID: "wildcard-b", Enabled: true},
				{Provider: provider, ChannelID: 3, EndpointID: "disabled-member", Enabled: true},
				{Provider: provider, ChannelID: 4, EndpointID: "other-profile", Enabled: true},
				{Provider: provider, ChannelID: 2, EndpointID: "no-credential", Enabled: false},
				{Provider: "other", ChannelID: 2, EndpointID: "other-provider", Enabled: true},
			}}
			r := NewProfileRuntime(cfg)
			for _, candidate := range cfg.Candidates {
				want := candidate.EndpointID == "selected" || strings.HasPrefix(candidate.EndpointID, "wildcard-")
				if got := r.HealthCheckEnabled(candidate.EndpointID); got != want {
					t.Fatalf("scope(%s) = %v, want %v", candidate.EndpointID, got, want)
				}
			}
			cfg.Profiles[0].IsDefault, cfg.Profiles[1].IsDefault = false, true
			r.Replace(cfg)
			if r.HealthCheckEnabled("selected") || !r.HealthCheckEnabled("other-profile") {
				t.Fatal("default change retained old health scope")
			}
			cfg.Profiles[1].Members = nil
			r.Replace(cfg)
			if r.HealthCheckEnabled("other-profile") {
				t.Fatal("removed member retained health scope")
			}
			for _, profiles := range [][]RoutingProfile{
				nil,
				{{Provider: provider, Enabled: true}},
				{{Provider: provider, Enabled: false, IsDefault: true}},
				{{Provider: provider, Enabled: true, IsDefault: true}, {Provider: provider, Enabled: true, IsDefault: true}},
			} {
				cfg.Profiles = profiles
				r.Replace(cfg)
				for _, candidate := range cfg.Candidates {
					if r.HealthCheckEnabled(candidate.EndpointID) {
						t.Fatal("invalid default enabled automatic health checks")
					}
				}
			}
		})
	}
}

func TestProfileEditReevaluatesCurrentTurnWithoutLosingSession(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			cfg := runtimeTestConfig()
			cfg.Provider, cfg.Profiles[0].Provider = provider, provider
			for i := range cfg.Candidates {
				cfg.Candidates[i].Provider = provider
			}
			cfg.Profiles[0].Rules = []RoutingRule{{ID: 1, Kind: "model", Enabled: true, MatchModel: "gpt-test", Targets: []RouteTarget{{ChannelID: 1, GroupID: 1}}}}
			r := NewProfileRuntime(cfg)
			turn := runtimeTestTurn("same-turn")
			first := r.Plan(context.Background(), 1, "gpt-test", true, turn, false)
			runtimeTestSend(t, r, &first, 0)
			cfg.Profiles[0].Revision++
			cfg.Profiles[0].Rules[0].Targets = []RouteTarget{{ChannelID: 1, GroupID: 2}}
			r.Replace(cfg)
			turn.NewTurn = false
			next := r.Plan(context.Background(), 1, "gpt-test", true, turn, false)
			if next.Decision.ReusedTurn || runtimeTestSend(t, r, &next, 0).GroupID != 2 {
				t.Fatal("saved policy reused stale turn targets")
			}
		})
	}
}

func TestProfileRuntimeExplicitPreviewEligibilityAndNoSideEffects(t *testing.T) {
	for _, reason := range []string{"model_unsupported", "quota_exhausted"} {
		t.Run(reason, func(t *testing.T) {
			config := runtimeTestConfig()
			config.Profiles[0].Strategy = StrategyRotate
			var calls, traces atomic.Int32
			config.Classify = func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
				calls.Add(1)
				return IntentResult{}, nil
			}
			config.OnTrace = func(DispatchTrace) { traces.Add(1) }
			config.Supports = func(c DispatchCandidate, model string) bool {
				return reason != "model_unsupported" || c.GroupID != 1
			}
			if reason == "quota_exhausted" {
				config.Candidates[0].Quota.Windows = []QuotaSnapshot{dispatchTestWindow(time.Now(), 100, time.Hour)}
			}
			r := NewProfileRuntime(config)
			p := config.Profiles[0]
			targets := []RouteTarget{{ChannelID: 1}}
			for range 3 {
				evaluations := r.PreviewTargets(context.Background(), p, targets, "gpt-test")
				if len(evaluations) != 2 || evaluations[0].Candidate.GroupID != 2 || !evaluations[0].Eligible || evaluations[1].Reason != reason {
					t.Fatalf("explicit resource eligibility: %+v", evaluations)
				}
			}
			if calls.Load() != 0 || traces.Load() != 0 || len(r.affinity) != 0 || len(r.scheduler.loads) != 0 || len(r.scheduler.rotation) != 0 {
				t.Fatal("explicit previews changed classification, traces, affinity or scheduler state")
			}

			config.Supports = nil
			config.Candidates[0].Quota.Windows = nil
			r.Replace(config)
			first := r.Plan(context.Background(), p.ID, "gpt-test", true, runtimeTestTurn("first"), false)
			if got := runtimeTestSend(t, r, &first, 0); got.GroupID != 1 {
				t.Fatalf("preview advanced initial rotation: %+v", got)
			}
			for range 3 {
				r.PreviewTargets(context.Background(), p, targets, "gpt-test")
			}
			second := r.Plan(context.Background(), p.ID, "gpt-test", true, runtimeTestTurn("second"), false)
			if got := runtimeTestSend(t, r, &second, 0); got.GroupID != 2 {
				t.Fatalf("preview advanced existing rotation: %+v", got)
			}
		})
	}
}

func runtimeTestSend(t *testing.T, r *ProfileRuntime, plan *DispatchPlan, cached int64) DispatchCandidate {
	t.Helper()
	if plan.Error != "" || len(plan.EligibleCandidates) == 0 {
		t.Fatalf("plan unavailable: %+v", plan)
	}
	c := plan.EligibleCandidates[0]
	a, err := r.Reserve(context.Background(), plan, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.SetCachedTokens(cached)
	a.Finish(10, true, Failure{})
	return c
}

func TestProfileRuntimeRulePriority(t *testing.T) {
	p := runtimeTestConfig().Profiles[0]
	p.Classifier = &ClassifierConfig{Enabled: true}
	stream := true
	target := []RouteTarget{{ChannelID: 2, GroupID: 3}}
	p.Rules = []RoutingRule{
		{ID: 1, Enabled: true, Kind: "intent", IntentID: "coding", Priority: -100, Targets: target},
		{ID: 2, Enabled: true, Kind: "non_stream", Priority: -100, Targets: target},
		{ID: 3, Enabled: true, Kind: "composite", MatchModel: "gpt-test", RequireStream: &stream, Priority: -100, Targets: target},
		{ID: 4, Enabled: true, Kind: "model", MatchModel: "gpt-*", Priority: -100, Targets: target},
		{ID: 5, Enabled: true, Kind: "model", MatchModel: "gpt-test", Priority: 100, Targets: target},
	}
	for _, tc := range []struct {
		remove int
		want   int64
	}{{0, 5}, {1, 4}, {2, 3}} {
		p.Rules = p.Rules[:5-tc.remove]
		if got := ResolveDecision(p, "gpt-test", true, "coding"); got.RuleID != tc.want {
			t.Fatalf("priority got %+v want rule %d", got, tc.want)
		}
	}
	p.Rules = p.Rules[:2]
	if got := ResolveDecision(p, "gpt-test", false, "coding"); got.RuleID != 2 || !got.FallbackToDefault {
		t.Fatalf("non-stream priority/default fallback wrong: %+v", got)
	}
	if got := ResolveDecision(p, "gpt-test", true, "coding"); got.RuleID != 1 {
		t.Fatalf("intent rule did not apply: %+v", got)
	}
}

func TestProfileRuntimeRuleModesAndFallbackOption(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind string
		mode string
		flag *bool
		want []int64
	}{{"model default", "model", ModePrefer, nil, []int64{3, 1, 2}}, {"non-stream default", "non_stream", ModePrefer, nil, []int64{3, 1, 2}}, {"explicit disabled", "model", ModePrefer, boolPointer(false), []int64{3}}, {"non-stream explicit disabled", "non_stream", ModePrefer, boolPointer(false), []int64{3}}, {"non-stream explicit enabled", "non_stream", ModePrefer, boolPointer(true), []int64{3, 1, 2}}, {"fallback starts default", "model", ModeFallback, boolPointer(false), []int64{1, 2, 3}}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := runtimeTestConfig()
			cfg.Profiles[0].Rules = []RoutingRule{{ID: 1, Kind: tc.kind, Enabled: true, MatchModel: "gpt-test", Mode: tc.mode, Targets: []RouteTarget{{ChannelID: 2, GroupID: 3}}, FallbackToDefault: tc.flag}}
			plan := NewProfileRuntime(cfg).Preview(context.Background(), 1, "gpt-test", false, TurnInput{})
			got := []int64{}
			for _, c := range plan.EligibleCandidates {
				got = append(got, c.GroupID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("candidate stage order got %v want %v", got, tc.want)
			}
		})
	}
}

func boolPointer(value bool) *bool { return &value }

func TestProfileMembersNeverOverrideModels(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.Profiles[0].Members[0].UpstreamModel = "legacy-member-model"
	r := NewProfileRuntime(cfg)
	plan := r.Preview(context.Background(), 1, "requested-model", true, TurnInput{})
	if len(plan.EligibleCandidates) == 0 || plan.EligibleCandidates[0].Identity().Model != "requested-model" {
		t.Fatalf("hidden member model still affects default dispatch: %+v", plan)
	}
	for _, override := range []string{"", "explicit-target-model"} {
		candidates := r.ExpandTargets(cfg.Profiles[0], []RouteTarget{{ProfileID: 1, UpstreamModel: override}}, "requested-model")
		want := override
		if want == "" {
			want = "requested-model"
		}
		if len(candidates) == 0 || candidates[0].Identity().Model != want {
			t.Fatalf("referenced members changed an explicit target model: %+v", candidates)
		}
	}
}

func TestMappingUsesModelRoutingWithinSelectedResources(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			cfg := runtimeTestConfig()
			cfg.Provider, cfg.Profiles[0].Provider = provider, provider
			for i := range cfg.Candidates {
				cfg.Candidates[i].Provider = provider
			}
			cfg.Profiles[0].Rules = []RoutingRule{{ID: 1, Kind: "mapping", Enabled: true, MatchModel: "client-alias", Targets: []RouteTarget{{ChannelID: 1, GroupID: 2, UpstreamModel: "supported-model"}}, FallbackToDefault: boolPointer(false)}}
			if err := ValidateProfile(&cfg.Profiles[0]); err != nil {
				t.Fatal(err)
			}
			r := NewProfileRuntime(cfg)
			preview := r.Preview(context.Background(), 1, "client-alias", false, TurnInput{})
			if preview.Decision.Kind != "mapping" || len(preview.EligibleCandidates) != 1 || preview.EligibleCandidates[0].GroupID != 2 || preview.EligibleCandidates[0].Identity().Model != "supported-model" {
				t.Fatalf("mapping changed resource scope or model: %+v", preview)
			}
			actual := r.Plan(context.Background(), 1, "client-alias", false, runtimeTestTurn("mapping"), false)
			if got := runtimeTestSend(t, r, &actual, 0); got.Identity() != preview.EligibleCandidates[0].Identity() {
				t.Fatalf("mapping preview differs from actual dispatch: %+v", got)
			}
			data, err := json.Marshal(actual.Trace())
			var captured DispatchTrace
			if err != nil || json.Unmarshal(data, &captured) != nil || captured.RequestedModel != "client-alias" || len(captured.Candidates) == 0 || captured.Candidates[0].Candidate.Model != "supported-model" {
				t.Fatalf("trace lost the original or rewritten model: %s (err=%v)", data, err)
			}
			unmatched := r.Preview(context.Background(), 1, "other-model", true, TurnInput{})
			if unmatched.Decision.Kind != "default" || unmatched.EligibleCandidates[0].Identity().Model != "other-model" {
				t.Fatalf("mapping leaked to unmatched requests: %+v", unmatched)
			}
			cfg.Profiles[0].Members[1].Enabled = false
			r.Replace(cfg)
			removed := r.Preview(context.Background(), 1, "client-alias", false, TurnInput{})
			if len(removed.EligibleCandidates) != 0 {
				t.Fatalf("mapping bypassed removed membership: %+v", removed)
			}
		})
	}
}

func TestMappingRejectsTargetsOutsideOrchestration(t *testing.T) {
	for _, target := range []RouteTarget{
		{ChannelID: 2, GroupID: 3, UpstreamModel: "mapped"},
		{ChannelID: 1, GroupID: 0, UpstreamModel: "mapped"},
		{ChannelID: 1, GroupID: 3, UpstreamModel: "mapped"},
		{ProfileID: 2, UpstreamModel: "mapped"},
		{ChannelID: 1, GroupID: 1},
	} {
		p := runtimeTestConfig().Profiles[0]
		p.Rules = []RoutingRule{{Kind: "mapping", Enabled: true, MatchModel: "alias", Targets: []RouteTarget{target}}}
		if err := ValidateProfile(&p); err == nil {
			t.Fatalf("invalid mapping target was accepted: %+v", target)
		}
	}
}

func TestIntentMasterSwitchDisablesRememberedDecisions(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.Profiles[0].Classifier = &ClassifierConfig{Enabled: true, Model: "classifier", Confidence: .6}
	cfg.Profiles[0].Intents = []IntentRule{{ID: "coding", Enabled: true, Targets: []RouteTarget{{ChannelID: 2, GroupID: 3}}}}
	var calls atomic.Int32
	cfg.Classify = func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
		calls.Add(1)
		return IntentResult{IntentID: "coding", Confidence: 1}, nil
	}
	r := NewProfileRuntime(cfg)
	turn := runtimeTestTurn("same-turn")
	first := r.Plan(context.Background(), 1, "gpt-test", true, turn, false)
	if got := runtimeTestSend(t, r, &first, 0); got.ChannelID != 2 {
		t.Fatalf("enabled intent did not select target: %+v", got)
	}
	cfg.Profiles[0].Classifier.Enabled = false
	r.Replace(cfg)
	turn.NewTurn, turn.Continuation = false, true
	for _, preview := range []bool{false, true} {
		plan := r.Plan(context.Background(), 1, "gpt-test", true, turn, preview)
		if plan.Decision.Kind != "default" || plan.Intent.IntentID != "" || plan.EligibleCandidates[0].ChannelID != 1 || calls.Load() != 1 {
			t.Fatalf("disabled intent reused cached decision: %+v calls=%d", plan, calls.Load())
		}
	}
	p := cfg.Profiles[0]
	p.Rules = []RoutingRule{{Kind: "intent", Enabled: true, IntentID: "coding", Targets: []RouteTarget{{ChannelID: 2}}}}
	if got := ResolveDecision(p, "gpt-test", true, "coding"); got.Kind != "default" {
		t.Fatalf("disabled intent rule matched: %+v", got)
	}
}

func TestProfileRuntimeIntentFallbackChainAndTargetProfilesDoNotRecurse(t *testing.T) {
	cfg := runtimeTestConfig()
	p := cfg.Profiles[0]
	p.Classifier = &ClassifierConfig{Enabled: true, Provider: "codex", ChannelID: 1, GroupID: 1, Model: "router"}
	p.Intents = []IntentRule{
		{ID: "coding", Enabled: true, Targets: []RouteTarget{{ProfileID: 2, UpstreamModel: "coding-model"}}, FallbackIntent: "simple", FallbackToDefault: boolPointer(false)},
		{ID: "simple", Enabled: true, Targets: []RouteTarget{{ChannelID: 1, GroupID: 2, UpstreamModel: "small-model"}}, FallbackToDefault: boolPointer(false)},
	}
	other := p
	other.ID, other.IsDefault = 2, false
	other.Members = []RoutingMember{{ChannelID: 2, GroupID: 3, Enabled: true, UpstreamModel: "member-model"}}
	other.Rules = []RoutingRule{{ID: 999, Kind: "model", Enabled: true, MatchModel: "*", Targets: []RouteTarget{{ChannelID: 1, GroupID: 1}}}}
	cfg.Profiles = []RoutingProfile{p, other}
	var calls atomic.Int64
	cfg.Classify = func(_ context.Context, profile RoutingProfile, _ TurnInput) (IntentResult, error) {
		calls.Add(1)
		if profile.ID != 1 {
			t.Errorf("referenced profile classifier ran: %d", profile.ID)
		}
		return IntentResult{IntentID: "coding", Confidence: 1}, nil
	}
	r := NewProfileRuntime(cfg)
	plan := r.Plan(context.Background(), 1, "gpt-test", true, runtimeTestTurn("t1"), false)
	if calls.Load() != 1 || plan.Decision.IntentID != "coding" || len(plan.EligibleCandidates) != 2 {
		t.Fatalf("wrong classification/fallback expansion: %+v", plan)
	}
	if plan.EligibleCandidates[0].GroupID != 3 || plan.EligibleCandidates[0].UpstreamModel != "coding-model" || plan.EligibleCandidates[1].GroupID != 2 || plan.EligibleCandidates[1].UpstreamModel != "small-model" {
		t.Fatalf("target profiles recursed or lost overrides: %+v", plan.EligibleCandidates)
	}
}

func TestProfileRuntimePrepareSupportsAndCompatibilityApplyToPreviewAndActual(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.PrepareCandidate = func(c DispatchCandidate, model, override string) DispatchCandidate {
		c.Model, c.UpstreamModel = model, "effective-"+model
		return c
	}
	cfg.Supports = func(c DispatchCandidate, actual string) bool { return c.GroupID != 2 && actual == "effective-gpt-test" }
	r := NewProfileRuntime(cfg)
	preview := r.Preview(context.Background(), 1, "gpt-test", true, TurnInput{})
	actual := r.Plan(context.Background(), 1, "gpt-test", true, TurnInput{}, false)
	if !reflect.DeepEqual(preview.Evaluations, actual.Evaluations) || len(actual.EligibleCandidates) != 1 || actual.Evaluations[0].StateCompatibility != "unknown" {
		t.Fatal("preview and actual used different adapter decisions or guessed state compatibility")
	}
	cfg.Compatible = func(_ context.Context, c DispatchCandidate) (bool, string) {
		return false, "state_backend_incompatible"
	}
	r.Replace(cfg)
	plan := r.Preview(context.Background(), 1, "gpt-test", true, TurnInput{})
	if plan.Error != "route_unavailable" || len(plan.EligibleCandidates) != 0 || plan.Evaluations[0].StateCompatibility != "incompatible" {
		t.Fatalf("incompatible backend remained eligible: %+v", plan)
	}
}

func TestProfileRuntimePreviewDoesNotClassifyReserveOrRemember(t *testing.T) {
	cfg := runtimeTestConfig()
	p := &cfg.Profiles[0]
	p.Strategy = StrategyWeighted
	p.Classifier = &ClassifierConfig{Enabled: true, Provider: "codex", Model: "router", ChannelID: 1, GroupID: 1}
	p.Intents = []IntentRule{{ID: "coding", Enabled: true}}
	var calls atomic.Int64
	cfg.Classify = func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
		calls.Add(1)
		return IntentResult{IntentID: "coding", Confidence: 1}, nil
	}
	r := NewProfileRuntime(cfg)
	for i := 0; i < 5; i++ {
		plan := r.Preview(context.Background(), 1, "gpt-test", true, runtimeTestTurn("t1"))
		if _, err := r.Reserve(context.Background(), &plan, plan.EligibleCandidates[0]); err == nil {
			t.Fatal("preview could reserve")
		}
	}
	if calls.Load() != 0 || len(r.affinity) != 0 || len(r.scheduler.loads) != 0 || len(r.scheduler.rotation) != 0 {
		t.Fatal("preview changed runtime state")
	}
	if result := r.ClassifyIntent(context.Background(), 1, runtimeTestTurn("t1")); result.IntentID != "coding" || calls.Load() != 1 {
		t.Fatalf("explicit classify did not run: %+v", result)
	}
}

func TestProfileRuntimeToolsReuseDecisionWithoutClassificationOrRotation(t *testing.T) {
	cfg := runtimeTestConfig()
	p := &cfg.Profiles[0]
	p.Strategy = StrategyRotate
	p.Classifier = &ClassifierConfig{Enabled: true, Model: "router", Provider: "codex", ChannelID: 1, GroupID: 1}
	p.Intents = []IntentRule{{ID: "coding", Enabled: true, Targets: []RouteTarget{{ProfileID: 1}}}}
	var calls atomic.Int64
	cfg.Classify = func(context.Context, RoutingProfile, TurnInput) (IntentResult, error) {
		calls.Add(1)
		return IntentResult{IntentID: "coding", Confidence: 1}, nil
	}
	r := NewProfileRuntime(cfg)
	turn := runtimeTestTurn("t1")
	plan := r.Plan(context.Background(), 1, "gpt-test", true, turn, false)
	if c := runtimeTestSend(t, r, &plan, 0); c.GroupID != 1 {
		t.Fatal("first selection")
	}
	turn.NewTurn, turn.Continuation = false, true
	tool := r.Plan(context.Background(), 1, "changed-client-alias", true, turn, false)
	if tool.Trace().RequestedModel != "changed-client-alias" {
		t.Fatal("turn reuse overwrote the current request's original model")
	}
	if !tool.Decision.ReusedTurn || tool.Model != "gpt-test" || runtimeTestSend(t, r, &tool, 0).GroupID != 1 || calls.Load() != 1 {
		t.Fatalf("tool continuation reclassified/rerouted: %+v", tool)
	}
	next := r.Plan(context.Background(), 1, "gpt-test", true, runtimeTestTurn("t2"), false)
	if runtimeTestSend(t, r, &next, 0).GroupID != 2 || calls.Load() != 2 {
		t.Fatal("tool request advanced rotation")
	}
}

func TestProfileRuntimeAffinityModesAndInvalidation(t *testing.T) {
	for _, tc := range []struct {
		mode   AffinityMode
		cached int64
		want   int64
	}{{AffinityTurn, 20, 2}, {AffinitySession, 0, 1}, {AffinityAdaptive, 20, 1}, {AffinityAdaptive, 0, 2}, {AffinityNone, 20, 2}} {
		t.Run(fmtAffinity(tc.mode, tc.cached), func(t *testing.T) {
			cfg := runtimeTestConfig()
			cfg.Profiles[0].Strategy, cfg.Profiles[0].AffinityMode = StrategyRotate, tc.mode
			r := NewProfileRuntime(cfg)
			first := r.Plan(context.Background(), 1, "gpt-test", true, runtimeTestTurn("t1"), false)
			runtimeTestSend(t, r, &first, tc.cached)
			second := r.Plan(context.Background(), 1, "gpt-test", true, runtimeTestTurn("t2"), false)
			if second.EligibleCandidates[0].GroupID != tc.want {
				t.Fatalf("mode %s cached %d got %d want %d", tc.mode, tc.cached, second.EligibleCandidates[0].GroupID, tc.want)
			}
			cfg.Candidates[0].Health.RepairRequired = true
			r.Replace(cfg)
			tool := runtimeTestTurn("t1")
			tool.NewTurn = false
			third := r.Plan(context.Background(), 1, "gpt-test", true, tool, false)
			if third.EligibleCandidates[0].GroupID != 2 {
				t.Fatal("affinity bypassed current health")
			}
		})
	}
}

func fmtAffinity(mode AffinityMode, tokens int64) string {
	if tokens > 0 {
		return string(mode) + "_cached"
	}
	return string(mode)
}

func TestProfileRuntimeBudgetSpansRuleAndDefaultStagesAndCommitAvoidsDoubleCharge(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.Profiles[0].MaxAttempts = 2
	cfg.Profiles[0].Rules = []RoutingRule{{ID: 1, Kind: "model", Enabled: true, MatchModel: "gpt-test", Targets: []RouteTarget{{ChannelID: 2, GroupID: 3}}}}
	r := NewProfileRuntime(cfg)
	plan := r.Plan(context.Background(), 1, "gpt-test", true, runtimeTestTurn("t1"), false)
	ctx := WithProfileRequest(context.Background(), &plan)
	if ProfileRequestFromContext(ctx) != &plan || BudgetFromContext(ctx) != plan.Budget {
		t.Fatal("request context lost shared budget")
	}
	for _, candidate := range plan.EligibleCandidates[:2] {
		a, err := r.Reserve(ctx, &plan, candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := plan.Budget.Begin(ctx); err != nil {
			t.Fatal(err)
		}
		if err := a.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		a.Finish(0, false, Failure{Scope: ScopeCandidate, Reason: "candidate_failed"})
	}
	if _, err := r.Reserve(ctx, &plan, plan.EligibleCandidates[2]); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("cross-stage budget was reset: %v", err)
	}
	if trace := plan.Trace(); trace.Attempts != 2 || trace.StopReason != "retry_budget_exhausted" {
		t.Fatalf("wrong trace budget: %+v", trace)
	}
}

func TestProfileRuntimeStartedResponsePreventsReplayAndIsNotSuccess(t *testing.T) {
	cfg := runtimeTestConfig()
	var saved DispatchTrace
	cfg.OnTrace = func(trace DispatchTrace) { saved = trace }
	r := NewProfileRuntime(cfg)
	plan := r.Plan(context.Background(), 1, "gpt-test", true, runtimeTestTurn("t1"), false)
	a, err := r.Reserve(context.Background(), &plan, plan.EligibleCandidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.ResponseStarted()
	a.Finish(5, false, Failure{Scope: ScopeCandidate, Reason: "stream_interrupted"})
	if _, err := r.Reserve(context.Background(), &plan, plan.EligibleCandidates[1]); err == nil {
		t.Fatal("started stream replayed to another candidate")
	}
	if !saved.ResponseStarted || saved.Successful || saved.StopReason != "stream_interrupted" || len(r.affinity) != 0 {
		t.Fatalf("response start was confused with success: %+v", saved)
	}
}

func TestProfileRuntimeTraceExcludesRequestContentAndRawFailureText(t *testing.T) {
	cfg := runtimeTestConfig()
	cfg.Profiles[0].Intents = []IntentRule{{ID: "coding", Description: "PRIVATE_INTENT_DESCRIPTION"}}
	r := NewProfileRuntime(cfg)
	plan := r.Plan(context.Background(), 1, "gpt-test", true, runtimeTestTurn("PRIVATE_TURN_ID"), false)
	a, err := r.Reserve(context.Background(), &plan, plan.EligibleCandidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Begin(context.Background()); err != nil {
		t.Fatal(err)
	}
	a.Finish(0, false, Failure{Reason: "PRIVATE UPSTREAM AUTHORIZATION PAYLOAD", Scope: ScopeCandidate})
	data, err := json.Marshal(plan.Trace())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "PRIVATE") || !strings.Contains(string(data), "upstream_failure") {
		t.Fatalf("trace leaked private data: %s", data)
	}
	planData, _ := json.Marshal(plan)
	for _, text := range []string{"PRIVATE_CURRENT_TEXT", "PRIVATE_PREVIOUS_TEXT", "PRIVATE_ASSISTANT_TEXT", "PRIVATE_TURN_ID"} {
		if strings.Contains(string(planData), text) {
			t.Fatalf("plan serialization leaked %s", text)
		}
	}
}

func TestProfileRuntimeAffinityCacheIsBoundedAndCallerIsolated(t *testing.T) {
	cfg := runtimeTestConfig()
	r := NewProfileRuntime(cfg)
	base := time.Now()
	r.now = func() time.Time { return base }
	for i := 0; i < 4100; i++ {
		turn := runtimeTestTurn(string(rune(i + 1)))
		turn.ConversationID = turn.TurnID
		plan := r.Plan(context.Background(), 1, "gpt-test", true, turn, false)
		r.rememberSuccess(&plan, plan.EligibleCandidates[0], 0, base)
	}
	if len(r.affinity) > 4096 {
		t.Fatalf("affinity cache unbounded: %d", len(r.affinity))
	}
	turn := runtimeTestTurn("caller-test")
	ctxA := WithCallerScope(context.Background(), "a")
	plan := r.Plan(ctxA, 1, "gpt-test", true, turn, false)
	r.rememberSuccess(&plan, plan.EligibleCandidates[0], 0, base)
	ctxB := WithCallerScope(context.Background(), "b")
	other := r.Plan(ctxB, 1, "gpt-test", true, turn, false)
	if other.Decision.ReusedTurn {
		t.Fatal("caller inherited another caller's turn")
	}
	r.now = func() time.Time { return base.Add(2 * time.Hour) }
	if expired := r.Plan(ctxA, 1, "gpt-test", true, turn, false); expired.Decision.ReusedTurn {
		t.Fatal("expired turn was reused")
	}
}
