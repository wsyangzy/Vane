package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/search"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

type CodexRoutingProfileInput struct {
	Profile routing.RoutingProfile `json:"profile"`
	// nil retains the selected service's credential; empty explicitly removes it.
	SearchAPIKey *string `json:"search_api_key"`
}

// GetCodexSearchAPIKey reveals one credential only for an explicit editor action.
// Profile lists and routing snapshots continue to expose only HasAPIKey.
func (a *App) GetCodexSearchAPIKey(profileID int64, vendor string) (string, error) {
	if profileID <= 0 {
		return "", errors.New("请先保存调度方案")
	}
	switch vendor {
	case "tavily", "brave", "exa", "searxng":
	default:
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
	}
	if input.SearchAPIKey != nil && strings.ContainsAny(*input.SearchAPIKey, " \t\r\n\x00") {
		return routing.RoutingProfile{}, errors.New("搜索服务 API Key 不能包含空白或控制字符")
	}
	if p.Search.Mode == "service" && p.Search.Vendor != "searxng" {
		keyPresent := input.SearchAPIKey != nil && *input.SearchAPIKey != ""
		if input.SearchAPIKey == nil && p.ID > 0 {
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
		if input.SearchAPIKey == nil {
			return nil
		}
		return codexstore.PutSearchCredential(ctx, tx, id, p.Search.Vendor, *input.SearchAPIKey)
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
		present, err := codexstore.HasSearchCredential(ctx, a.searchStoreDB(), p.ID, p.Search.Vendor)
		if err != nil {
			return err
		}
		p.Search.HasAPIKey = present
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
	if id > 0 {
		key, err := codexstore.SearchCredential(ctx, a.searchStoreDB(), id, policy.Vendor)
		if err != nil {
			return cfg, err
		}
		cfg.APIKey = key
	}
	return cfg, nil
}
