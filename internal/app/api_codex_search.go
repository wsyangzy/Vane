package app

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/search"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

type CodexRoutingProfileInput struct {
	Profile routing.RoutingProfile `json:"profile"`
	// nil retains the selected service's credential; empty explicitly removes it.
	SearchAPIKey  *string           `json:"search_api_key"`
	SearchAPIKeys map[string]string `json:"search_api_keys,omitempty"`
}

// GetCodexSearchAPIKey reveals one credential only for an explicit editor action.
// Profile lists and routing snapshots continue to expose only HasAPIKey.
func (a *App) GetCodexSearchAPIKey(profileID int64, vendor string) (string, error) {
	if profileID <= 0 {
		return "", errors.New("请先保存调度方案")
	}
	if !routing.IsSearchVendor(vendor) {
		return "", errors.New("search_vendor_invalid")
	}
	if _, err := a.GetRoutingProfile("codex", profileID); err != nil {
		return "", err
	}
	ctx, cancel := routeCtx()
	defer cancel()
	return codexstore.SearchCredential(ctx, a.searchStoreDB(), profileID, vendor)
}

func (a *App) migrateCodexSearch(ctx context.Context) error {
	if _, err := a.profileStore(ctx); err != nil {
		return err
	}
	return codexstore.MigrateLegacySearch(ctx, a.searchStoreDB())
}

func (a *App) SaveCodexRoutingProfile(input CodexRoutingProfileInput) (routing.RoutingProfile, error) {
	p := input.Profile
	if p.Provider != "codex" {
		return routing.RoutingProfile{}, errors.New("search_provider_invalid")
	}
	if p.Search == nil {
		defaults := routing.DefaultSearchPolicy()
		p.Search = &defaults
	} else {
		policy := *p.Search
		policy.ServiceEndpoints = maps.Clone(policy.ServiceEndpoints)
		p.Search = &policy
	}
	keys := maps.Clone(input.SearchAPIKeys)
	if keys == nil {
		keys = make(map[string]string)
	}
	if input.SearchAPIKey != nil {
		keys[p.Search.Vendor] = *input.SearchAPIKey
	}
	for vendor, key := range keys {
		if !routing.IsSearchVendor(vendor) || strings.ContainsAny(key, " \t\r\n\x00") {
			return routing.RoutingProfile{}, errors.New("搜索服务协议或 API Key 无效")
		}
	}
	// Keep each service's last endpoint when the preferred vendor changes.
	if p.ID > 0 {
		current, err := a.GetRoutingProfile("codex", p.ID)
		if err != nil {
			return routing.RoutingProfile{}, err
		}
		if current.Search != nil {
			endpoints := make(map[string]string)
			maps.Copy(endpoints, current.Search.ServiceEndpoints)
			if current.Search.Endpoint != "" {
				endpoints[current.Search.Vendor] = current.Search.Endpoint
			}
			maps.Copy(endpoints, p.Search.ServiceEndpoints)
			p.Search.ServiceEndpoints = endpoints
		}
	}
	delete(p.Search.ServiceEndpoints, p.Search.Vendor)
	for vendor, endpoint := range p.Search.ServiceEndpoints {
		if endpoint == "" && routing.IsSearchVendor(vendor) {
			delete(p.Search.ServiceEndpoints, vendor)
		}
	}
	if p.Search.Mode == "service" && p.Search.Vendor != "searxng" {
		key, edited := keys[p.Search.Vendor]
		keyPresent := key != ""
		if !edited && p.ID > 0 {
			ctx, cancel := routeCtx()
			defer cancel()
			err := a.migrateCodexSearch(ctx)
			if err != nil {
				return routing.RoutingProfile{}, err
			}
			keyPresent, err = codexstore.HasSearchCredential(ctx, a.searchStoreDB(), p.ID, p.Search.Vendor)
			if err != nil {
				return routing.RoutingProfile{}, err
			}
		}
		if !keyPresent {
			return routing.RoutingProfile{}, errors.New("搜索服务适配需要填写 API Key")
		}
	}
	p, err := a.saveRoutingProfile(p, func(ctx context.Context, tx *sql.Tx, id int64) error {
		for vendor, key := range keys {
			if err := codexstore.PutSearchCredential(ctx, tx, id, vendor, key); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return routing.RoutingProfile{}, err
	}
	views := []routing.RoutingProfile{p}
	if err := a.populateSearchPolicies(context.Background(), views); err != nil {
		return p, err
	}
	return views[0], nil
}

func (a *App) searchStoreDB() *sql.DB {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.storeDB != nil {
		return a.storeDB
	}
	if a.usageTracker != nil {
		return a.usageTracker.GetDB()
	}
	return nil
}

func (a *App) populateSearchPolicies(ctx context.Context, profiles []routing.RoutingProfile) error {
	for i := range profiles {
		p := &profiles[i]
		if p.Provider != "codex" {
			continue
		}
		if p.Search == nil {
			defaults := routing.DefaultSearchPolicy()
			p.Search = &defaults
		}
		copy := *p.Search
		p.Search = &copy
		vendors, err := codexstore.SearchCredentialVendors(ctx, a.searchStoreDB(), p.ID)
		if err != nil {
			return err
		}
		p.Search.ConfiguredVendors = vendors
		p.Search.HasAPIKey = slices.Contains(vendors, p.Search.Vendor)
	}
	return nil
}

func (a *App) codexSearchConfig(ctx context.Context) (search.Config, error) {
	policy := routing.DefaultSearchPolicy()
	a.poolMu.Lock()
	runtime := a.profileRuntimes["codex"]
	a.poolMu.Unlock()
	var id int64
	if runtime != nil {
		if p, ok := runtime.Profile(0); ok {
			id = p.ID
			if p.Search != nil {
				policy = *p.Search
			}
		}
	}
	cfg := search.Config{Mode: policy.Mode, Vendor: policy.Vendor, Endpoint: policy.Endpoint, Timeout: policy.Timeout, MaxResults: policy.MaxResults}
	if id <= 0 {
		return cfg, nil
	}
	db := a.searchStoreDB()
	key, err := codexstore.SearchCredential(ctx, db, id, policy.Vendor)
	cfg.APIKey = key
	if cfg.Mode != "auto" {
		return cfg, err
	}
	vendors, err := codexstore.SearchCredentialVendors(ctx, db, id)
	if err != nil {
		return cfg, nil
	}
	// SearXNG can be configured with an endpoint alone.
	if policy.ServiceEndpoints["searxng"] != "" && !slices.Contains(vendors, "searxng") {
		vendors = append(vendors, "searxng")
	}
	for _, vendor := range vendors {
		if vendor == cfg.Vendor || !routing.IsSearchVendor(vendor) {
			continue
		}
		endpoint := policy.ServiceEndpoints[vendor]
		if endpoint == "" {
			endpoint = routing.DefaultSearchEndpoint(vendor)
		}
		if endpoint == "" {
			continue
		}
		key, err := codexstore.SearchCredential(ctx, db, id, vendor)
		if err != nil {
			continue
		}
		fallback := search.Config{Mode: cfg.Mode, Vendor: vendor, Endpoint: endpoint, APIKey: key, Timeout: cfg.Timeout, MaxResults: cfg.MaxResults}
		if fallback.Validate() == nil {
			cfg.Fallbacks = append(cfg.Fallbacks, fallback)
		}
	}
	return cfg, nil
}
