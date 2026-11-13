package routing

import (
	"errors"
	"net/url"
	"time"
)

// SearchPolicy is public profile configuration. Credentials are stored separately
// by the Codex adapter and never enter scheduling snapshots or diagnostic traces.
type SearchPolicy struct {
	Mode       string        `json:"mode"`
	Vendor     string        `json:"vendor"`
	Endpoint   string        `json:"endpoint"`
	Timeout    time.Duration `json:"timeout"`
	MaxResults int           `json:"max_results"`
	HasAPIKey  bool          `json:"has_api_key"`
}

func DefaultSearchPolicy() SearchPolicy {
	return SearchPolicy{Mode: "auto", Vendor: "tavily", Endpoint: "https://api.tavily.com/search", Timeout: 30 * time.Second, MaxResults: 5}
}

func (p SearchPolicy) Validate() error {
	if p.Mode != "auto" && p.Mode != "native" && p.Mode != "service" {
		return errors.New("search_mode_invalid")
	}
	switch p.Vendor {
	case "tavily", "brave", "exa", "searxng":
	default:
		return errors.New("search_vendor_invalid")
	}
	u, err := url.Parse(p.Endpoint)
	if p.Endpoint == "" && p.Vendor == "searxng" && p.Mode != "service" {
		// A new automatic profile can be saved before an optional service is set.
	} else if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("search_endpoint_invalid")
	}
	if p.Timeout < time.Second || p.Timeout > 2*time.Minute || p.MaxResults < 1 || p.MaxResults > 20 {
		return errors.New("search_limits_invalid")
	}
	return nil
}
