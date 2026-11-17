// Package pathpolicy resolves endpoint paths independently of scheduling mode.
package pathpolicy

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	Legacy = "legacy"
	Base   = "base"
	V1     = "v1"
)

func Normalize(strategy string) string {
	if strategy == "" {
		return Legacy
	}
	return strategy
}

func Validate(strategy string) error {
	switch Normalize(strategy) {
	case Legacy, Base, V1:
		return nil
	default:
		return fmt.Errorf("不支持的端点路径策略: %s", strategy)
	}
}

// Relative removes only known client entry prefixes. Endpoint-specific prefixes
// belong to baseURL and are never inferred from a hostname.
func Relative(path string) string {
	if strings.HasPrefix(path, "/backend-api/codex/") {
		path = strings.TrimPrefix(path, "/backend-api/codex")
	} else if strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	// native/service select Vane's search adapter, not an upstream API path.
	if path == "/alpha/search/native" || path == "/alpha/search/service" {
		path = "/alpha/search"
	}
	return path
}

// Resolve preserves legacy callers exactly; explicit policies are identical
// across legacy forwarding, pools and unified profiles.
func Resolve(baseURL string, request *url.URL, strategy string, legacyRelative bool) string {
	base := strings.TrimRight(baseURL, "/")
	path := request.Path
	switch Normalize(strategy) {
	case Base:
		path = Relative(path)
	case V1:
		path = Relative(path)
		if !strings.HasSuffix(base, "/v1") {
			path = "/v1" + path
		}
	default:
		if legacyRelative {
			path = Relative(path)
		}
		if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1/") {
			path = path[3:]
		}
	}
	target := base + path
	if request.RawQuery != "" {
		target += "?" + request.RawQuery
	}
	return target
}
