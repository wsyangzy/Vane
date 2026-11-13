package clientconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdaptModelCatalogPreservesNativeMetadataAndClientTemplates(t *testing.T) {
	native := []byte(`{"models":[{"slug":"known","display_name":"Known model","base_instructions":"Original model instructions","context_window":272000,"supported_in_api":false,"visibility":"hide","supported_reasoning_levels":[{"effort":"high","description":"High"}],"model_messages":{"instructions_template":"Original template"},"future_field":{"enabled":true}}],"future_wrapper":"keep"}`)
	got, err := AdaptModelCatalog(native, nil)
	if err != nil || string(got) != string(native) {
		t.Fatalf("native catalog changed: %s, %v", got, err)
	}
	got, err = AdaptModelCatalog([]byte(`{"object":"list","data":[{"id":"known"},{"id":"new"},{"id":"known"}]}`), native)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(got, &result); err != nil || len(result.Models) != 2 {
		t.Fatalf("bad model catalog: %s, %v", got, err)
	}
	if result.Models[0]["base_instructions"] != "Original model instructions" || result.Models[0]["context_window"] != float64(272000) || result.Models[0]["future_field"] == nil {
		t.Fatalf("model metadata was lost: %s", got)
	}
	if result.Models[0]["supported_in_api"] != true || result.Models[0]["visibility"] != "list" {
		t.Fatal("client cache incorrectly hid a model offered by this provider")
	}
	if result.Models[1]["slug"] != "new" || result.Models[1]["visibility"] != "list" || result.Models[1]["supported_in_api"] != true || result.Models[1]["truncation_policy"] == nil {
		t.Fatalf("missing documented gateway metadata: %s", got)
	}
	if _, invented := result.Models[1]["context_window"]; invented {
		t.Fatal("unknown model acquired an unverified context window")
	}
}

func TestAdaptModelCatalogRejectsInvalidLists(t *testing.T) {
	for _, raw := range []string{
		`<html>error</html>`, `null`, `{}`, `{"error":{"message":"denied"},"data":[]}`,
		`{"data":null}`, `{"data":[{"id":""}]}`, `{"models":null}`,
		`{"models":[{"slug":"a"}]}`,
		`{"models":[{"slug":"a","base_instructions":"x"},{"slug":"a","base_instructions":"x"}]}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := AdaptModelCatalog([]byte(raw), nil); err == nil {
				t.Fatal("invalid upstream response became a valid catalog")
			}
		})
	}
	got, err := AdaptModelCatalog([]byte(`{"data":[]}`), nil)
	if err != nil || string(got) != `{"models":[]}` {
		t.Fatalf("empty catalog: %s, %v", got, err)
	}
}

func TestReadModelCatalogResolvesHomeAndRejectsPublicIDList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".codex")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "codex-models.json")
	content := []byte(`{"models":[{"slug":"local","model_messages":{"instructions_template":"Local instructions"}}]}`)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	resolved, raw, err := ReadModelCatalog("~/.codex/codex-models.json")
	if err != nil || resolved != path || string(raw) != string(content) {
		t.Fatalf("home catalog: %s %s %v", resolved, raw, err)
	}
	if _, _, err := ReadModelCatalog("relative.json"); err == nil {
		t.Fatal("relative path accepted")
	}
	if err := os.WriteFile(path, []byte(`{"object":"list","data":[{"id":"not-codex"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadModelCatalog(path); err == nil || !strings.Contains(err.Error(), "models") {
		t.Fatalf("public list accepted as local Codex catalog: %v", err)
	}
}
