package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/wsyangzy/Vane/internal/routing"
)

func TestProfileSearchPersistenceAndProviderCommitRollBackTogether(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	p := testRoutingProfile("codex", "search")
	policy := routing.DefaultSearchPolicy()
	policy.Vendor, policy.Endpoint, policy.HasAPIKey = "brave", "https://api.search.brave.com/res/v1/web/search", true
	policy.APIKeyLengths = map[string]int{"brave": 32}
	p.Search = &policy
	if err := s.Save(ctx, &p); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Get(ctx, "codex", p.ID)
	if err != nil || loaded.Search == nil || loaded.Search.Vendor != "brave" || loaded.Search.HasAPIKey || len(loaded.Search.APIKeyLengths) != 0 {
		t.Fatalf("metadata or credential marker leaked: %+v %v", loaded, err)
	}
	revision := loaded.Revision
	loaded.Search.Mode = "native"
	sentinel := errors.New("provider credential write failed")
	err = s.SaveWith(ctx, loaded, func(context.Context, *sql.Tx, int64) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	unchanged, err := s.Get(ctx, "codex", p.ID)
	if err != nil || unchanged.Revision != revision || unchanged.Search.Mode != "auto" {
		t.Fatalf("partial save committed: %+v %v", unchanged, err)
	}
	claude := testRoutingProfile("claude", "no-search")
	claude.Search = &policy
	if err := s.Save(ctx, &claude); err == nil {
		t.Fatal("Codex-only policy accepted for Claude")
	}
}
