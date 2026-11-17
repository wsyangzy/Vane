package pathpolicy

import (
	"net/url"
	"testing"
)

func TestExplicitPathsDoNotDependOnClientEntryOrSchedulingMode(t *testing.T) {
	for _, entry := range []string{"", "/v1", "/backend-api/codex"} {
		for _, base := range []string{"https://example.invalid", "https://example.invalid/codex", "https://example.invalid/codex/v1/"} {
			for _, resource := range []string{"/responses", "/responses/compact", "/models", "/alpha/search", "/alpha/search/native", "/alpha/search/service"} {
				for _, relative := range []bool{false, true} {
					req := &url.URL{Path: entry + resource, RawQuery: "keep=a%2Fb"}
					basePath := base
					if basePath[len(basePath)-1] == '/' {
						basePath = basePath[:len(basePath)-1]
					}
					upstreamResource := resource
					if resource == "/alpha/search/native" || resource == "/alpha/search/service" {
						upstreamResource = "/alpha/search"
					}
					wantBase := basePath + upstreamResource + "?keep=a%2Fb"
					wantV1 := basePath + "/v1" + upstreamResource + "?keep=a%2Fb"
					if base == "https://example.invalid/codex/v1/" {
						wantV1 = wantBase
					}
					for strategy, want := range map[string]string{Base: wantBase, V1: wantV1} {
						if got := Resolve(base, req, strategy, relative); got != want {
							t.Fatalf("entry=%q base=%q resource=%q strategy=%q relative=%v: got %q want %q", entry, base, resource, strategy, relative, got, want)
						}
					}
				}
			}
		}
	}
}

func TestLegacyPathsRemainCompatible(t *testing.T) {
	for _, tc := range []struct {
		base, path string
		relative   bool
		want       string
	}{
		{"https://root.invalid", "/v1/responses", false, "https://root.invalid/v1/responses"},
		{"https://root.invalid", "/v1/responses", true, "https://root.invalid/responses"},
		{"https://versioned.invalid/v1/", "/v1/responses", false, "https://versioned.invalid/v1/responses"},
		{"https://versioned.invalid/v1/", "/v1/responses", true, "https://versioned.invalid/v1/responses"},
		{"https://prefixed.invalid/codex", "/backend-api/codex/responses", true, "https://prefixed.invalid/codex/responses"},
	} {
		for _, strategy := range []string{"", Legacy} {
			if got := Resolve(tc.base, &url.URL{Path: tc.path}, strategy, tc.relative); got != tc.want {
				t.Errorf("%+v strategy %q: %q", tc, strategy, got)
			}
		}
	}
	if got := Relative("/v10/responses"); got != "/v10/responses" {
		t.Fatalf("prefix boundary stripped: %q", got)
	}
}
