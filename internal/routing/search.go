package routing

import (
	"errors"
	"net/url"
	"time"
)

// SearchPolicy is public profile configuration. Credentials are stored separately
// by the Codex adapter and never enter scheduling snapshots or diagnostic traces.
type SearchPolicy struct {
	Mode              string            `json:"mode"`
	Vendor            string            `json:"vendor"`
	Endpoint          string            `json:"endpoint"`
	Timeout           time.Duration     `json:"timeout"`
	MaxResults        int               `json:"max_results"`
	HasAPIKey         bool              `json:"has_api_key"`
	ServiceEndpoints  map[string]string `json:"service_endpoints,omitempty"`
	ConfiguredVendors []string          `json:"configured_vendors,omitempty"`
}

func DefaultSearchPolicy() SearchPolicy {
	return SearchPolicy{Mode: "auto", Vendor: "tavily", Endpoint: DefaultSearchEndpoint("tavily"), Timeout: 30 * time.Second, MaxResults: 5}
}

func DefaultSearchEndpoint(vendor string) string {
	switch vendor {
	case "tavily":
		return "https://api.tavily.com/search"
	case "exa":
		return "https://api.exa.ai/search"
	case "brave":
		return "https://api.search.brave.com/res/v1/web/search"
	}
	return ""
}

func IsSearchVendor(vendor string) bool {
	return vendor == "searxng" || DefaultSearchEndpoint(vendor) != ""
}

func validSearchEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func (p SearchPolicy) Validate() error {
	if p.Mode != "auto" && p.Mode != "native" && p.Mode != "service" {
		return errors.New("search_mode_invalid")
	}
	if !IsSearchVendor(p.Vendor) {
		return errors.New("search_vendor_invalid")
	}
	if !validSearchEndpoint(p.Endpoint) && !(p.Endpoint == "" && p.Vendor == "searxng" && p.Mode != "service") {
		return errors.New("search_endpoint_invalid")
	}
	for vendor, endpoint := range p.ServiceEndpoints {
		if !IsSearchVendor(vendor) || !validSearchEndpoint(endpoint) {
			return errors.New("search_endpoint_invalid")
		}
	}
	if p.Timeout < time.Second || p.Timeout > 2*time.Minute || p.MaxResults < 1 || p.MaxResults > 20 {
		return errors.New("search_limits_invalid")
	}
	return nil
}
