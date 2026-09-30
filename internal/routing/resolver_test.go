package routing

import (
	"reflect"
	"testing"
)

func TestMatchModelWildcardIsCaseInsensitive(t *testing.T) {
	cases := []struct {
		pattern string
		model   string
		want    bool
	}{
		{"claude-haiku-*", "claude-haiku-4-5", true},
		{"CLAUDE-HAIKU-*", "claude-haiku-4-5", true},
		{"claude-haiku-*", "claude-sonnet-5", false},
		{"*", "anything", true},
		{"*-haiku-*", "claude-haiku-4", true},
		{"claude-*-5", "claude-opus-5", true},
		{"claude-*-5", "claude-opus-5-5", true},
		{"claude-*-5", "claude-opus-4", false},
		{"claude-opus-5", "Claude-Opus-5", true},
		{"claude-opus-5", "claude-opus-5-5", false},
		{"", "claude", false},
		{"claude", "", false},
	}
	for _, tc := range cases {
		if got := MatchModel(tc.pattern, tc.model); got != tc.want {
			t.Errorf("MatchModel(%q, %q) = %v, want %v", tc.pattern, tc.model, got, tc.want)
		}
	}
}

func TestMatchSupportedModelKeepsExactSemanticsWithoutWildcard(t *testing.T) {
	if !MatchSupportedModel("claude-haiku", "claude-haiku") {
		t.Fatal("exact entry must match itself")
	}
	if MatchSupportedModel("claude-haiku", "Claude-Haiku") {
		t.Fatal("entries without * must keep the previous case-sensitive exact match")
	}
	if !MatchSupportedModel("Claude-Haiku-*", "claude-haiku-4-5") {
		t.Fatal("wildcard entries are case-insensitive")
	}
}

func TestResolveModelRulePrefersSpecificity(t *testing.T) {
	target := []Target{{ChannelID: 1}}
	rules := []ModelRule{
		{ID: 1, Name: "all", Priority: 1, MatchModel: "*", Targets: target},
		{ID: 2, Name: "claude-star", Priority: 1, MatchModel: "claude-*", Targets: target},
		{ID: 3, Name: "haiku-star", Priority: 50, MatchModel: "claude-haiku-*", Targets: target},
		{ID: 4, Name: "exact", Priority: 100, MatchModel: "claude-haiku-4-5", Targets: target},
	}
	if got := ResolveModelRule(rules, "claude-haiku-4-5"); got == nil || got.ID != 4 {
		t.Fatalf("exact rule must win regardless of priority, got %+v", got)
	}
	if got := ResolveModelRule(rules, "claude-haiku-3"); got == nil || got.ID != 3 {
		t.Fatalf("longer literal prefix must win, got %+v", got)
	}
	if got := ResolveModelRule(rules, "claude-sonnet-5"); got == nil || got.ID != 2 {
		t.Fatalf("prefixed wildcard must beat bare *, got %+v", got)
	}
	if got := ResolveModelRule(rules, "gpt-5"); got == nil || got.ID != 1 {
		t.Fatalf("bare * is the last resort, got %+v", got)
	}
}

func TestResolveModelRuleTieBreaksOnPriorityThenID(t *testing.T) {
	target := []Target{{ChannelID: 1}}
	rules := []ModelRule{
		{ID: 9, Priority: 10, MatchModel: "claude-*", Targets: target},
		{ID: 5, Priority: 20, MatchModel: "claude-*", Targets: target},
		{ID: 7, Priority: 10, MatchModel: "claude-*", Targets: target},
	}
	if got := ResolveModelRule(rules, "claude-x"); got == nil || got.ID != 7 {
		t.Fatalf("expected lowest priority then lowest id, got %+v", got)
	}
}

func TestResolveModelRuleSkipsRulesWithoutTargets(t *testing.T) {
	rules := []ModelRule{{ID: 1, MatchModel: "claude-*"}}
	if got := ResolveModelRule(rules, "claude-x"); got != nil {
		t.Fatalf("rule without targets must not match, got %+v", got)
	}
}

func TestDecidePriorityOrder(t *testing.T) {
	rules := []ModelRule{{ID: 1, Name: "haiku", MatchModel: "claude-haiku-*", Mode: "prefer", Targets: []Target{{ChannelID: 3}}}}
	nonStream := &NonStreamRoute{Mode: ModePrefer, Targets: []Target{{ChannelID: 7}}}

	if d := Decide(rules, nonStream, "claude-haiku-4", false); d.Kind != KindModelRule || d.Rule.ID != 1 {
		t.Fatalf("model rule must win over non-stream route, got %+v", d)
	}
	if d := Decide(rules, nonStream, "claude-opus-5", false); d.Kind != KindNonStream || d.Mode != ModePrefer {
		t.Fatalf("non-stream request without a rule should use the channel non-stream route, got %+v", d)
	}
	if d := Decide(rules, nonStream, "claude-opus-5", true); d.Routed() {
		t.Fatalf("streaming requests must never trigger the non-stream route, got %+v", d)
	}
	if d := Decide(rules, nil, "claude-opus-5", false); d.Routed() {
		t.Fatalf("nothing configured must keep the default flow, got %+v", d)
	}
	if d := Decide(nil, &NonStreamRoute{Mode: "bogus", Targets: []Target{{ChannelID: 7}}}, "m", false); d.Routed() {
		t.Fatalf("invalid mode must not route, got %+v", d)
	}
}

func TestDecisionStages(t *testing.T) {
	targets := []Target{{ChannelID: 1}, {ChannelID: 2}}
	indexes := func(stages []Stage) []int {
		out := make([]int, 0, len(stages))
		for _, stage := range stages {
			if (stage.Index == 0) != (stage.Target == nil) {
				t.Fatalf("source stage must have nil target: %+v", stage)
			}
			out = append(out, stage.Index)
		}
		return out
	}

	cases := []struct {
		name string
		d    Decision
		want []int
	}{
		{"model rule prefer falls back to source", Decision{Kind: KindModelRule, Mode: ModePrefer, Targets: targets}, []int{1, 2, 0}},
		{"model rule fallback starts from source", Decision{Kind: KindModelRule, Mode: ModeFallback, Targets: targets}, []int{0, 1, 2}},
		{"non-stream prefer never returns to source", Decision{Kind: KindNonStream, Mode: ModePrefer, Targets: targets}, []int{1, 2}},
		{"non-stream fallback starts from source", Decision{Kind: KindNonStream, Mode: ModeFallback, Targets: targets}, []int{0, 1, 2}},
		{"default flow has no stages", Decision{}, []int{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := indexes(tc.d.Stages()); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("stages = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRulesBySourceIsolatesChannelsAndDropsLegacyGlobalRules(t *testing.T) {
	target := []Target{{ChannelID: 9}}
	grouped := RulesBySource([]ModelRule{
		{ID: 1, SourceChannelID: 1, MatchModel: "claude-haiku-*", Targets: target},
		{ID: 2, SourceChannelID: 2, MatchModel: "claude-haiku-*", Targets: target},
		{ID: 3, SourceChannelID: 0, MatchModel: "*", Targets: target}, // 旧版全局规则
	})
	if len(grouped) != 2 || len(grouped[1]) != 1 || grouped[1][0].ID != 1 || grouped[2][0].ID != 2 {
		t.Fatalf("unexpected grouping: %+v", grouped)
	}
	if _, ok := grouped[0]; ok {
		t.Fatal("legacy global rules must not take part in runtime matching")
	}
	// 渠道 1 激活时，渠道 2 的规则不生效
	if d := Decide(grouped[3], nil, "claude-haiku-4", true); d.Routed() {
		t.Fatalf("a channel without rules must keep the default flow, got %+v", d)
	}
}

func TestSnapshotJSON(t *testing.T) {
	rule := &ModelRule{ID: 3, Name: "haiku 改道"}
	got := NewSnapshot(Decision{Kind: KindModelRule, Mode: ModePrefer, Rule: rule}, "anyrouter").JSON(1)
	want := `{"kind":"model_rule","id":3,"name":"haiku 改道","channel":"anyrouter","mode":"prefer","target":1}`
	if got != want {
		t.Fatalf("model rule snapshot = %s, want %s", got, want)
	}
	got = NewSnapshot(Decision{Kind: KindNonStream, Mode: ModePrefer}, "anyrouter").JSON(2)
	want = `{"kind":"non_stream","channel":"anyrouter","mode":"prefer","target":2}`
	if got != want {
		t.Fatalf("non-stream snapshot = %s, want %s", got, want)
	}
}

func TestNonStreamRouteRoundTrip(t *testing.T) {
	route, err := ParseNonStreamRoute(`{"mode":"PREFER","targets":[{"channel_id":3,"group_id":0,"upstream_model":" m "},{"channel_id":3,"group_id":0,"upstream_model":"m"},{"channel_id":0}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if route == nil || route.Mode != ModePrefer || len(route.Targets) != 1 || route.Targets[0].UpstreamModel != "m" {
		t.Fatalf("unexpected parsed route: %+v", route)
	}
	encoded, err := EncodeNonStreamRoute(route)
	if err != nil {
		t.Fatal(err)
	}
	if encoded != `{"mode":"prefer","targets":[{"channel_id":3,"group_id":0,"upstream_model":"m"}]}` {
		t.Fatalf("unexpected encoded route: %s", encoded)
	}

	for _, raw := range []string{"", "null", `{"mode":"prefer","targets":[]}`} {
		if route, err := ParseNonStreamRoute(raw); err != nil || route != nil {
			t.Fatalf("%q must decode to a disabled route, got %+v err=%v", raw, route, err)
		}
	}
	if encoded, err := EncodeNonStreamRoute(nil); err != nil || encoded != "" {
		t.Fatalf("nil route must encode to empty (NULL), got %q err=%v", encoded, err)
	}
	if _, err := EncodeNonStreamRoute(&NonStreamRoute{Mode: "bogus", Targets: []Target{{ChannelID: 1}}}); err == nil {
		t.Fatal("invalid mode with targets must be rejected")
	}
}

func TestExhaustedReason(t *testing.T) {
	if KindModelRule.ExhaustedReason() != "route_exhausted" ||
		KindNonStream.ExhaustedReason() != "non_stream_route_exhausted" ||
		KindModelCapability.ExhaustedReason() != "endpoint_exhausted" {
		t.Fatal("unexpected exhausted reasons")
	}
}
