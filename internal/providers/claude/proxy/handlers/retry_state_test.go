package handlers

import (
	"errors"
	"testing"

	"ai-relay-forwarder/config"
	"ai-relay-forwarder/internal/providers/claude/proxy/endpoint"
)

func TestNextFailoverLoopStateKeepsStateForNewChannel(t *testing.T) {
	triedChannels := map[string]struct{}{"primary": {}}
	triedEndpoints := map[string]struct{}{
		endpoint.EndpointKey("primary", "ep-1"): {},
	}

	loopCount, alreadyTried, isSameChannel := nextFailoverLoopState(2, "primary", "backup", triedChannels, triedEndpoints)

	if loopCount != 2 {
		t.Fatalf("loopCount = %d, want 2", loopCount)
	}
	if alreadyTried {
		t.Fatal("alreadyTried = true, want false")
	}
	if isSameChannel {
		t.Fatal("isSameChannel = true, want false")
	}
	if len(triedChannels) != 1 || len(triedEndpoints) != 1 {
		t.Fatalf("state was cleared unexpectedly: channels=%v endpoints=%v", triedChannels, triedEndpoints)
	}
}

func TestNextFailoverLoopStateStartsNewLoopForAlreadyTriedChannel(t *testing.T) {
	triedChannels := map[string]struct{}{
		"primary": {},
		"backup":  {},
	}
	triedEndpoints := map[string]struct{}{
		endpoint.EndpointKey("primary", "ep-1"): {},
		endpoint.EndpointKey("backup", "ep-2"):  {},
	}

	loopCount, alreadyTried, isSameChannel := nextFailoverLoopState(2, "primary", "backup", triedChannels, triedEndpoints)

	if loopCount != 3 {
		t.Fatalf("loopCount = %d, want 3", loopCount)
	}
	if !alreadyTried {
		t.Fatal("alreadyTried = false, want true")
	}
	if isSameChannel {
		t.Fatal("isSameChannel = true, want false")
	}
	if len(triedChannels) != 0 || len(triedEndpoints) != 0 {
		t.Fatalf("state was not cleared: channels=%v endpoints=%v", triedChannels, triedEndpoints)
	}
}

func TestNextFailoverLoopStateStartsNewLoopForSameChannel(t *testing.T) {
	triedChannels := map[string]struct{}{"primary": {}}
	triedEndpoints := map[string]struct{}{
		endpoint.EndpointKey("primary", "ep-1"): {},
	}

	loopCount, alreadyTried, isSameChannel := nextFailoverLoopState(0, "primary", "primary", triedChannels, triedEndpoints)

	if loopCount != 1 {
		t.Fatalf("loopCount = %d, want 1", loopCount)
	}
	if !alreadyTried {
		t.Fatal("alreadyTried = false, want true because primary is in triedChannels")
	}
	if !isSameChannel {
		t.Fatal("isSameChannel = false, want true")
	}
	if len(triedChannels) != 0 || len(triedEndpoints) != 0 {
		t.Fatalf("state was not cleared: channels=%v endpoints=%v", triedChannels, triedEndpoints)
	}
}

func TestFilterStreamOnlyEndpoints(t *testing.T) {
	input := []*endpoint.Endpoint{
		{Config: config.EndpointConfig{Name: "regular-a", StreamOnly: false}},
		{Config: config.EndpointConfig{Name: "stream-only", StreamOnly: true}},
		{Config: config.EndpointConfig{Name: "regular-b", StreamOnly: false}},
	}

	filtered := filterStreamOnlyEndpoints(input)

	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(filtered))
	}
	if filtered[0].Config.Name != "regular-a" || filtered[1].Config.Name != "regular-b" {
		t.Fatalf("filtered endpoints = [%s %s], want [regular-a regular-b]", filtered[0].Config.Name, filtered[1].Config.Name)
	}
}

func TestEndpointKeysDeduplicatesAndSkipsInvalidEndpoints(t *testing.T) {
	endpoints := []*endpoint.Endpoint{
		nil,
		{Config: config.EndpointConfig{Name: "ep-a", Channel: "primary"}},
		{Config: config.EndpointConfig{Name: "ep-a", Channel: "primary"}},
		{Config: config.EndpointConfig{Name: "ep-b", Channel: "primary"}},
		{Config: config.EndpointConfig{}},
	}

	keys := endpointKeys(endpoints)

	want := []string{
		endpoint.EndpointKey("primary", "ep-a"),
		endpoint.EndpointKey("primary", "ep-b"),
	}
	if len(keys) != len(want) {
		t.Fatalf("len(keys) = %d, want %d: %v", len(keys), len(want), keys)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys[%d] = %q, want %q", i, keys[i], want[i])
		}
	}
}

func TestEndpointRouteNameFallsBackToName(t *testing.T) {
	if got := endpointRouteName(nil); got != "" {
		t.Fatalf("nil endpoint route = %q, want empty", got)
	}
	if got := endpointRouteName(&endpoint.Endpoint{Config: config.EndpointConfig{Name: "ep-a"}}); got != "ep-a" {
		t.Fatalf("route = %q, want ep-a", got)
	}
	if got := endpointRouteName(&endpoint.Endpoint{Config: config.EndpointConfig{Name: "ep-a", Channel: "primary"}}); got != "primary" {
		t.Fatalf("route = %q, want primary", got)
	}
}

func TestEndpointDisplayGroupNameFallsBackToConfigGroup(t *testing.T) {
	ep := &endpoint.Endpoint{Config: config.EndpointConfig{
		Name:    "ep-a",
		Channel: "primary",
		Group:   "默认分组",
	}}

	if got := endpointDisplayGroupName(ep); got != "默认分组" {
		t.Fatalf("display group = %q, want 默认分组", got)
	}
}

func TestFailoverTerminationClassifiesTerminalErrors(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantTerminate bool
		wantLoopLimit bool
	}{
		{
			name:          "nil",
			err:           nil,
			wantTerminate: false,
			wantLoopLimit: false,
		},
		{
			name:          "no channels",
			err:           errors.New("没有可用的故障转移渠道"),
			wantTerminate: true,
			wantLoopLimit: false,
		},
		{
			name:          "all tried",
			err:           errors.New("所有渠道均已尝试"),
			wantTerminate: true,
			wantLoopLimit: false,
		},
		{
			name:          "loop limit details",
			err:           errors.New("所有渠道均已尝试，共循环 3 轮，达到最大循环轮数"),
			wantTerminate: true,
			wantLoopLimit: true,
		},
		{
			name:          "non terminal",
			err:           errors.New("temporary failover error"),
			wantTerminate: false,
			wantLoopLimit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTerminate, gotLoopLimit, gotText := failoverTermination(tt.err)
			if gotTerminate != tt.wantTerminate {
				t.Fatalf("terminate = %v, want %v", gotTerminate, tt.wantTerminate)
			}
			if gotLoopLimit != tt.wantLoopLimit {
				t.Fatalf("loopLimit = %v, want %v", gotLoopLimit, tt.wantLoopLimit)
			}
			if tt.err != nil && gotText != tt.err.Error() {
				t.Fatalf("errText = %q, want %q", gotText, tt.err.Error())
			}
		})
	}
}
