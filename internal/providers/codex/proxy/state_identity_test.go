package codexproxy

import (
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

func TestCodexReloadPreservesOnlyUnchangedBackendIdentity(t *testing.T) {
	for _, change := range []string{"unchanged", "policy", "url", "credential", "group", "auth"} {
		t.Run(change, func(t *testing.T) {
			prior := &CodexEndpoint{ID: 1, ChannelID: 2, CredentialGroupID: 3, BaseURL: "https://upstream.invalid", APIKey: "key"}
			version := routing.StateVersion(&prior.stateVersion)
			next := *prior
			next.stateVersion = 0
			switch change {
			case "policy":
				next.Name, next.Priority, next.Timeout, next.DailyCostLimitUSD = "renamed", 99, time.Minute, 20
			case "url":
				next.BaseURL = "https://another.invalid"
			case "credential":
				next.APIKey = "other-key"
			case "group":
				next.CredentialGroupID++
			case "auth":
				next.AuthMode = "client_passthrough"
			}
			m := &EndpointManager{endpointsByChannel: map[int64][]*CodexEndpoint{2: {prior}}}
			m.inheritStateVersionsLocked(map[int64][]*CodexEndpoint{2: {&next}})
			got := routing.StateVersion(&next.stateVersion)
			if (got == version) != (change == "unchanged" || change == "policy") {
				t.Fatalf("backend version inheritance incorrect: change=%s", change)
			}
		})
	}
}
