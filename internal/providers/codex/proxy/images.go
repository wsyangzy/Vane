package codexproxy

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
)

func isCodexImageRequest(r *http.Request) bool {
	if r == nil || r.Method != http.MethodPost {
		return false
	}
	path := subscription.Path(r)
	return path == "/images/generations" || path == "/images/edits"
}

// Read only the model field; image bytes and prompts never enter diagnostics.
func (h *Handler) requestModel(r *http.Request, body []byte) string {
	if model := h.tokenParser.ParseModelFromBody(body); model != "" {
		return model
	}
	contentType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !isCodexImageRequest(r) || contentType != "multipart/form-data" || params["boundary"] == "" {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err != nil {
			return ""
		}
		if part.FormName() == "model" && part.FileName() == "" {
			value, err := io.ReadAll(io.LimitReader(part, 4097))
			if err == nil && len(value) <= 4096 {
				return strings.TrimSpace(string(value))
			}
			return ""
		}
		part.Close()
	}
}

func codexImageProtocolSuccess(frame map[string]any) bool {
	images, ok := frame["data"].([]any)
	if !ok || len(images) == 0 {
		return false
	}
	for _, item := range images {
		image, ok := item.(map[string]any)
		if !ok {
			return false
		}
		encoded, _ := image["b64_json"].(string)
		link, _ := image["url"].(string)
		if encoded == "" && link == "" {
			return false
		}
	}
	return true
}

func codexRequestFailure(r *http.Request, status int, body []byte, err error) routing.Failure {
	if isCodexSearchRequest(r) && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		return routing.Failure{Scope: routing.ScopeCandidate, Reason: "auxiliary_endpoint_unsupported"}
	}
	if isCodexImageRequest(r) && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		return routing.Failure{Scope: routing.ScopeCandidate, Reason: "image_endpoint_unsupported"}
	}
	return codexRouteFailure(status, body, err)
}
