package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

func TestLegacySearchMigrationEncryptsCredentialsAndIsolatesProfiles(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UserVault uses Windows DPAPI")
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	profiles := routingstore.NewProfileStore(db)
	if err := profiles.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSearchSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON;CREATE TABLE settings(provider TEXT,category TEXT,key TEXT,value TEXT);INSERT INTO settings VALUES('global','search','mode','service'),('global','search','api_key','legacy-search-secret'),('global','search','max_results','9')`); err != nil {
		t.Fatal(err)
	}
	first := routing.DefaultProfile("codex")
	first.Name = "existing"
	second := routing.DefaultProfile("claude")
	second.Name = "claude"
	for _, p := range []*routing.RoutingProfile{&first, &second} {
		if err := profiles.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := MigrateLegacySearch(ctx, db); err != nil {
		t.Fatal(err)
	}
	p, err := profiles.Get(ctx, "codex", first.ID)
	if err != nil || p.Search == nil || p.Search.Mode != "service" || p.Search.MaxResults != 9 {
		t.Fatalf("lost legacy policy: %+v %v", p, err)
	}
	var secret []byte
	if err := db.QueryRow(`SELECT secret FROM codex_profile_search_credentials WHERE profile_id=?`, first.ID).Scan(&secret); err != nil || strings.Contains(string(secret), "legacy-search-secret") {
		t.Fatal("credential not encrypted", err)
	}
	key, err := SearchCredential(ctx, db, first.ID, "tavily")
	if err != nil || key != "legacy-search-secret" {
		t.Fatal("credential lost", err)
	}
	key, err = SearchCredential(ctx, db, second.ID, "tavily")
	if err != nil || key != "" {
		t.Fatal("credential crossed provider boundary", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM settings WHERE category='search'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("plaintext legacy config remains", err)
	}
	updated := *p
	updated.Search.Mode = "native"
	if err := profiles.Save(ctx, &updated); err != nil {
		t.Fatal(err)
	}
	if err := MigrateLegacySearch(ctx, db); err != nil {
		t.Fatal(err)
	}
	key, err = SearchCredential(ctx, db, first.ID, "tavily")
	if err != nil || key != "legacy-search-secret" {
		t.Fatal("metadata save dropped credential", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := PutSearchCredential(ctx, tx, first.ID, "tavily", ""); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if present, err := HasSearchCredential(ctx, db, first.ID, "tavily"); err != nil || present {
		t.Fatal("explicit credential deletion failed", err)
	}
	// Management connections historically do not guarantee foreign_keys=ON.
	// Provider-owned cleanup must also work without that per-connection pragma.
	_, err = db.Exec(`PRAGMA foreign_keys=OFF; INSERT INTO codex_profile_search_credentials(profile_id,vendor,secret) VALUES(?, 'brave', X'0102'); DELETE FROM routing_profiles WHERE id=?`, first.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM codex_profile_search_credentials WHERE profile_id=?`, first.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("profile deletion left orphaned credentials", err)
	}
}
