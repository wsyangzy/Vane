package host

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wsyangzy/Vane/frontend"
)

// The resource package moved away from main. Exercise the real Wails file
// server so that the embedded dist prefix cannot silently break desktop loading.
func TestEmbeddedFrontendServesEntryAndReferencedAssets(t *testing.T) {
	t.Setenv("FRONTEND_DEVSERVER_URL", "")
	handler := application.AssetFileServerFS(frontend.Assets)
	entry := httptest.NewRecorder()
	handler.ServeHTTP(entry, httptest.NewRequest(http.MethodGet, "/", nil))
	if entry.Code != http.StatusOK || !bytes.Contains(entry.Body.Bytes(), []byte(`id="root"`)) {
		t.Fatalf("desktop entry unavailable: status %d", entry.Code)
	}
	references := regexp.MustCompile(`(?:src|href)="(/assets/[^"\s]+)"`).FindAllSubmatch(entry.Body.Bytes(), -1)
	if len(references) == 0 {
		t.Fatal("embedded entry has no production asset references")
	}
	for _, reference := range references {
		path := string(reference[1])
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		expected, err := frontend.Assets.ReadFile("dist" + path)
		if err != nil {
			t.Errorf("referenced asset not embedded: %s: %v", path, err)
			continue
		}
		if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), expected) {
			t.Errorf("referenced asset not served correctly: %s, status %d", path, response.Code)
		}
	}
}
