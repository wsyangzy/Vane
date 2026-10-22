package store

import (
	"context"
	"database/sql"
	"github.com/wsyangzy/Vane/internal/routing"
	_ "modernc.org/sqlite"
	"path/filepath"
	"testing"
)

func TestPoolMigrationAtomicIdempotentAndProviderIsolated(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "pools.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := NewPoolStore(db)
	ctx := context.Background()
	if err = s.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	pools := []routing.Pool{{Name: "main", Default: true, Strategy: routing.PoolSequential, Members: []routing.PoolMember{{ChannelID: 1, GroupID: 2, Tier: routing.TierPrimary}}}}
	if err = s.Migrate(ctx, "codex", pools); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx, "codex", pools); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx, "claude", pools); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"claude", "codex"} {
		got, err := s.List(ctx, provider)
		if err != nil || len(got) != 1 || len(got[0].Members) != 1 {
			t.Fatalf("%s: %+v, %v", provider, got, err)
		}
	}
	bad := []routing.Pool{{Name: "new", Strategy: routing.PoolSequential}, {Name: "new", Strategy: routing.PoolSequential}}
	if _, err = db.Exec(`DELETE FROM routing_pool_migrations WHERE provider='codex'`); err != nil {
		t.Fatal(err)
	}
	if err = s.Migrate(ctx, "codex", bad); err == nil {
		t.Fatal("expected uniqueness error")
	}
	got, err := s.List(ctx, "codex")
	if err != nil || len(got) != 1 {
		t.Fatalf("partial migration committed: %+v, %v", got, err)
	}
}

func TestPoolDefaultAndKeyCannotLeakOrBeDeleted(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "pools.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := NewPoolStore(db)
	ctx := context.Background()
	if err = s.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	p := routing.Pool{Provider: "claude", Name: "main", Default: true, Strategy: routing.PoolSequential}
	if err = s.Save(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if err = s.RotateKey(ctx, "codex", p.ID, "hash"); err == nil {
		t.Fatal("cross provider mutation")
	}
	if err = s.Delete(ctx, "claude", p.ID); err == nil {
		t.Fatal("default deleted")
	}
	if err = s.RotateKey(ctx, "claude", p.ID, "hash"); err != nil {
		t.Fatal(err)
	}
	p.Name = "renamed"
	if err = s.Save(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ctx, "claude")
	if err != nil || got[0].KeyHash != "hash" {
		t.Fatalf("UI edit lost key: %+v, %v", got, err)
	}
}
