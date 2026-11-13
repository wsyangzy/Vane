package privacy

import (
	"strings"
	"testing"
)

func TestCodexSearchRedactsQueriesWithoutRewritingNativeState(t *testing.T) {
	const body = `{ "model":"SECRET-model", "input":[{"role":"user","content":"SECRET message"}], "commands":{"search_query":[{"q":"SECRET news","domains":["SECRET.example"]}],"image_query":[{"q":"SECRET image"}],"find":[{"ref_id":"SECRET-ref","pattern":"SECRET pattern"}],"open":[{"ref_id":"SECRET-ref"}]},"settings":{"filters":{"allowed_domains":["SECRET.example"]}},"encrypted_context":"SECRET-context" }`
	snapshot := newTestSnapshot(t, redactSettings(), literalRuleFor(1, "SECRET", "[X]"))
	for _, path := range []string{"/v1/alpha/search", "/alpha/search/native", "/backend-api/codex/alpha/search/service"} {
		result, err := snapshot.Apply(Request{Path: path}, []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		want := strings.ReplaceAll(body, `"q":"SECRET`, `"q":"[X]`)
		want = strings.ReplaceAll(want, `"pattern":"SECRET`, `"pattern":"[X]`)
		want = strings.ReplaceAll(want, `"content":"SECRET`, `"content":"[X]`)
		if string(result.Body) != want {
			t.Fatalf("%s: protocol state or query scanning changed: %s", path, result.Body)
		}
	}
}
