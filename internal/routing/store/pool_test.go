package store

import (
	"context"
	"database/sql"
	"github.com/wsyangzy/Vane/internal/routing"
	_ "modernc.org/sqlite"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPoolChannelSchemaUpgradePreservesExistingPool(t *testing.T) {
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
	p := routing.Pool{Provider: "codex", Name: "existing", Default: true, Strategy: routing.PoolSequential,
		KeyHash: "preserved-key-hash", Members: []routing.PoolMember{{1, 11, routing.TierPrimary}, {2, 22, routing.TierBackup}},
		Rules: []routing.ModelRule{{ID: 1, Name: "existing-rule", MatchModel: "test-*", Mode: "prefer", Targets: []routing.Target{{ChannelID: 2, GroupID: 22}}}}}
	if err = s.Save(ctx, &p); err != nil {
		t.Fatal(err)
	}
	// Recreate the previous schema using a real SQLite database, not a mocked row.
	if _, err = db.ExecContext(ctx, `ALTER TABLE routing_pools DROP COLUMN channels`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = s.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(ctx, "codex")
	if err != nil || len(got) != 1 {
		t.Fatalf("upgrade lost pool: %+v %v", got, err)
	}
	if got[0].ID != p.ID || got[0].KeyHash != p.KeyHash || !got[0].Default || !reflect.DeepEqual(got[0].Members, p.Members) || !reflect.DeepEqual(got[0].Rules, p.Rules) {
		t.Fatalf("upgrade changed existing configuration: %+v", got[0])
	}
	if !reflect.DeepEqual(got[0].Channels, []routing.PoolChannel{{1, routing.TierPrimary}, {2, routing.TierBackup}}) {
		t.Fatalf("old memberships were not interpreted as channels: %+v", got[0].Channels)
	}
	// A channel with no selected groups must survive reopening the pool.
	got[0].Channels = append(got[0].Channels, routing.PoolChannel{3, routing.TierPrimary})
	if err = s.Save(ctx, &got[0]); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.List(ctx, "codex")
	if err != nil || !reflect.DeepEqual(reloaded[0].Channels, got[0].Channels) || !reflect.DeepEqual(reloaded[0].Members, p.Members) {
		t.Fatalf("channel selection did not persist: %+v %v", reloaded, err)
	}
	got[0].Members = nil
	if err = s.Save(ctx, &got[0]); err != nil {
		t.Fatal(err)
	}
	reloaded, err = s.List(ctx, "codex")
	if err != nil || len(reloaded[0].Members) != 0 || !reflect.DeepEqual(reloaded[0].Channels, got[0].Channels) {
		t.Fatalf("disabling every group lost the selected channels: %+v %v", reloaded, err)
	}
}

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
