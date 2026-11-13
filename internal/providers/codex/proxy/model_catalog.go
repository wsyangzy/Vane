package codexproxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/codex/clientconfig"
	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
)

func adaptCodexModelCatalog(r *http.Request, resp *http.Response) error {
	if r.Method != http.MethodGet || subscription.Path(r) != "/models" || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	// Codex appends client_version to catalog URLs. Ordinary OpenAI clients
	// keep their original /v1/models format.
	if !r.URL.Query().Has("client_version") && !strings.HasPrefix(r.URL.Path, "/backend-api/codex/") {
		return nil
	}
	if err := decodeCodexSubscriptionResponse(resp); err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, clientconfig.CatalogLimit+1))
	resp.Body.Close()
	if err != nil || len(raw) > clientconfig.CatalogLimit {
		return fmt.Errorf("codex_model_catalog_unavailable")
	}
	updated, err := clientconfig.AdaptModelCatalog(raw, clientconfig.CachedModelCatalog())
	if err != nil {
		return fmt.Errorf("codex_model_catalog_invalid: %w", err)
	}
	if len(updated) > clientconfig.CatalogLimit {
		return fmt.Errorf("codex_model_catalog_too_large")
	}
	resp.Body = io.NopCloser(bytes.NewReader(updated))
	resp.ContentLength = int64(len(updated))
	resp.Header.Set("Content-Length", strconv.Itoa(len(updated)))
	resp.Header.Set("Content-Type", "application/json")
	if !bytes.Equal(raw, updated) {
		for _, name := range []string{"ETag", "Last-Modified", "Content-MD5", "Digest"} {
			resp.Header.Del(name)
		}
		resp.Header.Set("Cache-Control", "no-store")
	}
	return nil
}
