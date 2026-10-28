package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
	_ "modernc.org/sqlite"
)

func newProfileTestStore(t *testing.T) (*ProfileStore, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "profiles.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	s := NewProfileStore(db)
	if err = s.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, db
}

func TestSessionAffinityPersistenceIsIsolatedAndReplacementRollsBack(t *testing.T) {
	s, db := newProfileTestStore(t)
	ctx := context.Background()
	p := testRoutingProfile("claude", "session")
	if err := s.Save(ctx, &p); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Round(0)
	entry := routing.SessionAffinity{Provider: "claude", ProfileID: p.ID, Key: strings.Repeat("a", 64), Candidate: routing.Candidate{Provider: "claude", Protocol: "messages", Channel: "1", Credential: "account:hash", Endpoint: "endpoint", Model: "claude-test"}, Model: "request-model", SuccessfulAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := s.SaveAffinity(ctx, "claude", []routing.SessionAffinity{entry}); err != nil {
		t.Fatal(err)
	}
	// A second store instance represents reopening the app's persistence layer.
	reopened := NewProfileStore(db)
	loaded, err := reopened.LoadAffinity(ctx, "claude")
	if err != nil || !reflect.DeepEqual(loaded, []routing.SessionAffinity{entry}) {
		t.Fatalf("session did not survive reopen: %+v %v", loaded, err)
	}
	if other, err := reopened.LoadAffinity(ctx, "codex"); err != nil || len(other) != 0 {
		t.Fatalf("session crossed providers: %+v %v", other, err)
	}
	duplicate := entry
	duplicate.Key = strings.Repeat("b", 64)
	if err := s.SaveAffinity(ctx, "claude", []routing.SessionAffinity{duplicate, duplicate}); err == nil {
		t.Fatal("duplicate session replacement unexpectedly succeeded")
	}
	loaded, err = reopened.LoadAffinity(ctx, "claude")
	if err != nil || !reflect.DeepEqual(loaded, []routing.SessionAffinity{entry}) {
		t.Fatalf("failed replacement lost old session: %+v %v", loaded, err)
	}
	entry.ExpiresAt = now.Add(-time.Hour)
	if err := s.SaveAffinity(ctx, "claude", []routing.SessionAffinity{entry}); err != nil {
		t.Fatal(err)
	}
	if loaded, err = reopened.LoadAffinity(ctx, "claude"); err != nil || len(loaded) != 0 {
		t.Fatalf("expired session restored: %+v %v", loaded, err)
	}
}

func TestDispatchTraceStoresLatestOutcomeWithoutMergingProviders(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	trace := routing.DispatchTrace{RequestID: "same-request", Provider: "claude", ResponseStarted: true, Attempts: 1}
	if err := s.SaveTrace(ctx, trace); err != nil {
		t.Fatal(err)
	}
	other := trace
	other.Provider, other.Successful = "codex", true
	if err := s.SaveTrace(ctx, other); err != nil {
		t.Fatal(err)
	}
	trace.StopReason = "response_incomplete"
	if err := s.SaveTrace(ctx, trace); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.ListTraces(ctx, "claude", 100)
	if err != nil || len(loaded) != 1 || loaded[0].StopReason != "response_incomplete" || loaded[0].Successful || !loaded[0].ResponseStarted {
		t.Fatalf("intermediate outcome replaced final trace: %+v %v", loaded, err)
	}
	loaded, err = s.ListTraces(ctx, "codex", 100)
	if err != nil || len(loaded) != 1 || !loaded[0].Successful {
		t.Fatalf("provider trace was overwritten: %+v %v", loaded, err)
	}
}

func TestDispatchTraceSeparatesBuildEnvironments(t *testing.T) {
	release, db := newProfileTestStore(t)
	dev := NewProfileStore(db, "dev")
	ctx := context.Background()
	for _, provider := range []string{"claude", "codex"} {
		trace := routing.DispatchTrace{RequestID: "same-request", Provider: provider, Attempts: 1}
		if err := release.SaveTrace(ctx, trace); err != nil {
			t.Fatal(err)
		}
		trace.Attempts = 2
		if err := dev.SaveTrace(ctx, trace); err != nil {
			t.Fatal(err)
		}
		trace.Successful = true
		if err := dev.SaveTrace(ctx, trace); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			store    *ProfileStore
			attempts int
			success  bool
		}{{release, 1, false}, {dev, 2, true}} {
			loaded, err := tc.store.ListTraces(ctx, provider, 100)
			if err != nil || len(loaded) != 1 || loaded[0].Attempts != tc.attempts || loaded[0].Successful != tc.success {
				t.Fatalf("trace crossed build or provider boundary: %+v %v", loaded, err)
			}
		}
	}
}

func TestDispatchTraceRetentionDoesNotPruneAnotherEnvironment(t *testing.T) {
	release, db := newProfileTestStore(t)
	ctx := context.Background()
	_, err := db.ExecContext(ctx, `WITH RECURSIVE sequence(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM sequence WHERE n<5001)
INSERT INTO routing_dispatch_traces(environment,provider,profile_id,request_id,payload)
SELECT 'release','codex',1,printf('release-%d',n),'{}' FROM sequence`)
	if err != nil {
		t.Fatal(err)
	}
	dev := NewProfileStore(db, "dev")
	if err := dev.SaveTrace(ctx, routing.DispatchTrace{RequestID: "dev-only", Provider: "codex"}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routing_dispatch_traces WHERE environment='release'`).Scan(&count); err != nil || count != 5001 {
		t.Fatalf("dev trace pruned release diagnostics: %d %v", count, err)
	}
	if err := release.SaveTrace(ctx, routing.DispatchTrace{RequestID: "release-latest", Provider: "codex"}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routing_dispatch_traces WHERE environment='release'`).Scan(&count); err != nil || count != 5000 {
		t.Fatalf("release retention was not bounded: %d %v", count, err)
	}
	if traces, err := dev.ListTraces(ctx, "codex", 100); err != nil || len(traces) != 1 || traces[0].RequestID != "dev-only" {
		t.Fatalf("release retention removed dev diagnostics: %+v %v", traces, err)
	}
}

func TestLegacyDispatchTraceEnvironmentUsesRequestProvenance(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE routing_dispatch_traces (
id INTEGER PRIMARY KEY AUTOINCREMENT, provider TEXT NOT NULL, profile_id INTEGER NOT NULL,
request_id TEXT NOT NULL, payload TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE request_logs (request_id TEXT PRIMARY KEY, provider TEXT, environment TEXT);
INSERT INTO request_logs VALUES ('req-fdf62f6d','codex','release'),('dev-request','codex','dev'),
('old-request','codex',NULL),('invalid-environment','codex','other')`)
	if err != nil {
		t.Fatal(err)
	}
	legacy := []routing.DispatchTrace{
		{RequestID: "req-fdf62f6d", Provider: "codex", Successful: true},
		{RequestID: "dev-request", Provider: "codex", Attempts: 2},
		{RequestID: "old-request", Provider: "codex"},
		{RequestID: "unlinked-request", Provider: "codex"},
		{RequestID: "req-fdf62f6d", Provider: "claude"},
		{RequestID: "invalid-environment", Provider: "codex"},
	}
	for _, trace := range legacy {
		payload, err := json.Marshal(trace)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO routing_dispatch_traces(provider,profile_id,request_id,payload) VALUES(?,0,?,?)`, trace.Provider, trace.RequestID, string(payload)); err != nil {
			t.Fatal(err)
		}
	}
	dev, release := NewProfileStore(db, "dev"), NewProfileStore(db, "release")
	for _, store := range []*ProfileStore{dev, release, dev} {
		if err := store.EnsureSchema(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	if traces, err := dev.ListTraces(ctx, "codex", 100); err != nil || len(traces) != 1 || !reflect.DeepEqual(traces[0], legacy[1]) {
		t.Fatalf("legacy dev provenance changed: %+v %v", traces, err)
	}
	if traces, err := release.ListTraces(ctx, "codex", 100); err != nil || len(traces) != 2 || !reflect.DeepEqual(traces[1], legacy[0]) {
		t.Fatalf("legacy release provenance or payload changed: %+v %v", traces, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM routing_dispatch_traces WHERE environment='unknown'`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("unlinked or invalid provenance was guessed: %d %v", count, err)
	}
	if _, err := db.Exec(`INSERT INTO request_logs VALUES('unlinked-request','codex','dev')`); err != nil {
		t.Fatal(err)
	}
	if err := dev.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if traces, err := dev.ListTraces(ctx, "codex", 100); err != nil || len(traces) != 2 || !reflect.DeepEqual(traces[0], legacy[3]) {
		t.Fatalf("late request persistence lost diagnostic provenance: %+v %v", traces, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM routing_dispatch_traces`).Scan(&count); err != nil || count != len(legacy) {
		t.Fatalf("migration deleted legacy traces: %d %v", count, err)
	}
}

func testRoutingProfile(provider, name string) routing.RoutingProfile {
	p := routing.DefaultProfile(provider)
	p.Name = name
	p.Members = []routing.RoutingMember{{ChannelID: 1, Enabled: true, Weight: 3}, {ChannelID: 2, GroupID: 4, Enabled: true, Weight: 7, UpstreamModel: "upstream-v1"}}
	return p
}

func TestProfileSchemaPreservesLegacyAndDoesNotInferProfiles(t *testing.T) {
	s, db := newProfileTestStore(t)
	ctx := context.Background()
	poolStore := NewPoolStore(db)
	if err := poolStore.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	pool := routing.Pool{Provider: "codex", Name: "legacy", Default: true, KeyHash: "old-pool-key", Strategy: routing.PoolSequential, Members: []routing.PoolMember{{ChannelID: 4, GroupID: 9, Tier: routing.TierPrimary}}}
	if err := poolStore.Save(ctx, &pool); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE existing_resources (id INTEGER PRIMARY KEY,payload TEXT); INSERT INTO existing_resources VALUES(1,'retain-channel-and-credential')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
	}
	pools, err := poolStore.List(ctx, "codex")
	if err != nil || len(pools) != 1 || pools[0].KeyHash != pool.KeyHash || !reflect.DeepEqual(pools[0].Members, pool.Members) {
		t.Fatalf("legacy pool changed: %+v %v", pools, err)
	}
	var resource string
	if err = db.QueryRow(`SELECT payload FROM existing_resources WHERE id=1`).Scan(&resource); err != nil || resource != "retain-channel-and-credential" {
		t.Fatalf("resource changed: %s %v", resource, err)
	}
	profiles, err := s.List(ctx, "codex")
	if err != nil || len(profiles) != 0 {
		t.Fatalf("legacy resources were inferred as profiles: %+v %v", profiles, err)
	}
}

func TestProfileRoundTripFallbackAndProviderIsolation(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	p := testRoutingProfile("claude", "main")
	yes, no := true, false
	p.IsDefault = true
	p.Rules = []routing.RoutingRule{{ID: 11, Enabled: true, Kind: "model", Name: "model-rule", MatchModel: "claude-*", Priority: 2, Targets: []routing.RouteTarget{{ChannelID: 2, GroupID: 4}}, FallbackToDefault: &yes}, {ID: 12, Enabled: true, Kind: "non_stream", Name: "nonstream", Priority: 3, RequireStream: &no, Targets: []routing.RouteTarget{{ChannelID: 1}}, FallbackToDefault: &no}}
	p.Intents = []routing.IntentRule{{ID: "coding", Name: "编程", Description: "代码任务", Enabled: true, Targets: []routing.RouteTarget{{ChannelID: 2}}, FallbackIntent: "general", FallbackToDefault: &yes}, {ID: "general", Name: "常规", Enabled: true}}
	p.Classifier = &routing.ClassifierConfig{Enabled: true, Provider: "codex", ChannelID: 3, GroupID: 5, Model: "gpt-test", FailureMode: "previous"}
	if err := s.Save(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 || p.Revision != 1 || p.Rules[0].ProfileID != p.ID || p.Classifier.Timeout != 8*time.Second {
		t.Fatalf("missing committed defaults: %+v", p)
	}
	got, err := s.Get(ctx, "claude", p.ID)
	if err != nil || !reflect.DeepEqual(*got, p) {
		t.Fatalf("round trip differs:\nwant %+v\ngot %+v\nerr %v", p, got, err)
	}
	if _, err = s.Get(ctx, "codex", p.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-provider read: %v", err)
	}
	other := testRoutingProfile("codex", "main")
	other.IsDefault = true
	if err = s.Save(ctx, &other); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, "codex", p.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-provider delete: %v", err)
	}
	if err = s.SetDefault(ctx, "codex", p.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-provider default: %v", err)
	}
	if listed, err := s.List(ctx, "claude"); err != nil || len(listed) != 1 || listed[0].ID != p.ID {
		t.Fatalf("provider list: %+v %v", listed, err)
	}
	replacement := testRoutingProfile("claude", "replacement")
	replacement.IsDefault = true
	if err := s.Save(ctx, &replacement); err != nil {
		t.Fatal(err)
	}
	previous, err := s.Get(ctx, "claude", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	p = *previous
	p.Classifier = nil
	p.Rules = nil
	p.Intents = nil
	p.Members = nil
	if err = s.Save(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(ctx, "claude", p.ID)
	if err != nil || got.Classifier != nil || len(got.Rules) != 0 || len(got.Intents) != 0 || len(got.Members) != 0 {
		t.Fatalf("children not removed: %+v %v", got, err)
	}
}

func TestProfileSaveRollbackDoesNotMutateCallerOrDefault(t *testing.T) {
	s, db := newProfileTestStore(t)
	ctx := context.Background()
	original := testRoutingProfile("codex", "original")
	original.IsDefault = true
	if err := s.Save(ctx, &original); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_new_member BEFORE INSERT ON routing_profile_members WHEN NEW.channel_id=99 BEGIN SELECT RAISE(ABORT,'injected child failure'); END`); err != nil {
		t.Fatal(err)
	}
	p := testRoutingProfile("codex", "  failed  ")
	p.IsDefault = true
	p.Strategy = ""
	p.Members[0].ChannelID = 99
	before := p
	before.Members = append([]routing.RoutingMember(nil), p.Members...)
	if err := s.Save(ctx, &p); err == nil {
		t.Fatal("expected transaction failure")
	}
	if !reflect.DeepEqual(p, before) {
		t.Fatalf("failed Save mutated caller: %+v", p)
	}
	got, err := s.Get(ctx, "codex", original.ID)
	if err != nil || !got.IsDefault || got.Revision != original.Revision {
		t.Fatalf("default demotion escaped rollback: %+v %v", got, err)
	}
	if listed, _ := s.List(ctx, "codex"); len(listed) != 1 {
		t.Fatalf("partial profile committed: %+v", listed)
	}
	stale := original
	original.Name = "updated"
	if err = s.Save(ctx, &original); err != nil {
		t.Fatal(err)
	}
	stale.Name = "overwrite"
	if err = s.Save(ctx, &stale); !errors.Is(err, ErrProfileConflict) {
		t.Fatalf("stale update=%v", err)
	}
	got, err = s.Get(ctx, "codex", original.ID)
	if err != nil || got.Name != "updated" || got.Revision != 2 {
		t.Fatalf("stale write changed record: %+v %v", got, err)
	}
}

func TestProfileDefaultSwitchAtomicAndInvalidTargetPreserved(t *testing.T) {
	s, db := newProfileTestStore(t)
	ctx := context.Background()
	a := testRoutingProfile("codex", "a")
	a.IsDefault = true
	b := testRoutingProfile("codex", "b")
	for _, p := range []*routing.RoutingProfile{&a, &b} {
		if err := s.Save(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_default BEFORE UPDATE OF is_default ON routing_profiles WHEN NEW.name='b' AND NEW.is_default=1 BEGIN SELECT RAISE(ABORT,'injected switch failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefault(ctx, "codex", b.ID); err == nil {
		t.Fatal("expected switch failure")
	}
	got, _ := s.Get(ctx, "codex", a.ID)
	if !got.IsDefault || got.Revision != a.Revision {
		t.Fatalf("switch rollback lost old default: %+v", got)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_default`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefault(ctx, "codex", b.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Get(ctx, "codex", a.ID)
	next, _ := s.Get(ctx, "codex", b.ID)
	if got.IsDefault || !next.IsDefault || got.Revision != 2 || next.Revision != 2 {
		t.Fatalf("switch state old=%+v new=%+v", got, next)
	}
	if err := s.Save(ctx, &a); !errors.Is(err, ErrProfileConflict) {
		t.Fatalf("default switch did not invalidate stale editor: %v", err)
	}
	if err := s.SetDefault(ctx, "codex", b.ID); err != nil {
		t.Fatal(err)
	}
	next, _ = s.Get(ctx, "codex", b.ID)
	if next.Revision != 2 {
		t.Fatalf("idempotent default advanced revision: %d", next.Revision)
	}
}

func TestProfileTargetsMustShareProviderAndReferencedDeleteBlocked(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	target := testRoutingProfile("codex", "target")
	if err := s.Save(ctx, &target); err != nil {
		t.Fatal(err)
	}
	wrong := testRoutingProfile("claude", "cross-provider")
	wrong.Intents = []routing.IntentRule{{ID: "coding", Enabled: true, Targets: []routing.RouteTarget{{ProfileID: target.ID}}}}
	if err := s.Save(ctx, &wrong); err == nil {
		t.Fatal("cross-provider profile target accepted")
	}
	if wrong.ID != 0 || wrong.Revision != 0 {
		t.Fatalf("failed insert leaked generated identity: %+v", wrong)
	}
	// Editing a disabled target remains valid configuration; candidate
	// eligibility, rather than persistence, decides whether it can be used.
	target.Enabled = false
	if err := s.Save(ctx, &target); err != nil {
		t.Fatal(err)
	}
	p := testRoutingProfile("codex", "source")
	p.Rules = []routing.RoutingRule{{Kind: "model", MatchModel: "gpt-*", Targets: []routing.RouteTarget{{ProfileID: target.ID}}}}
	if err := s.Save(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "codex", target.ID); !errors.Is(err, ErrProfileReferenced) {
		t.Fatalf("referenced delete=%v", err)
	}
	if err := s.Delete(ctx, "codex", p.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "codex", target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "codex", target.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted profile survived: %v", err)
	}
}

func TestDefaultProfileRemainsValidWithoutRuntimeMode(t *testing.T) {
	s, db := newProfileTestStore(t)
	ctx := context.Background()
	p := testRoutingProfile("codex", "default")
	p.IsDefault = true
	if err := s.Save(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO routing_runtime_state(provider,mode) VALUES('codex','legacy')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "codex", p.ID); err == nil {
		t.Fatal("deleted live default")
	}
	p.Members = nil
	if err := s.Save(ctx, &p); err == nil {
		t.Fatal("removed every live member")
	}
	got, err := s.Get(ctx, "codex", p.ID)
	if err != nil || len(got.Members) != 2 || got.Revision != 1 {
		t.Fatalf("default changed despite rejection: %+v %v", got, err)
	}
	got.IsDefault = false
	if err := s.Save(ctx, got); err == nil {
		t.Fatal("unset live default")
	}
	other := testRoutingProfile("codex", "replacement")
	if err := s.Save(ctx, &other); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefault(ctx, "codex", other.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "codex", p.ID); err != nil {
		t.Fatal(err)
	}
}

func TestProfileQuotaModelIsolationStalenessAndFailureRetention(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	usedA, usedB := 15.0, 75.0
	a := routing.QuotaSnapshot{Provider: "codex", ChannelID: 1, GroupID: 2, AccountIDHash: "account-hash", ModelScope: "gpt-a", WindowName: "5h", UsedPercent: &usedA, ObservedAt: now, ExpiresAt: now.Add(time.Minute), Source: "official", Confidence: "exact"}
	b := a
	b.ModelScope = "gpt-b"
	b.UsedPercent = &usedB
	other := a
	other.Provider = "claude"
	if err := s.SaveQuota(ctx, []routing.QuotaSnapshot{a, b, other}); err != nil {
		t.Fatal(err)
	}
	stale := a
	stale.ObservedAt = now.Add(-time.Minute)
	stale.UsedPercent = &usedB
	if err := s.SaveQuota(ctx, []routing.QuotaSnapshot{stale}); err != nil {
		t.Fatal(err)
	}
	failure := a
	failure.ObservedAt = now.Add(time.Minute)
	failure.Error = "refresh unavailable"
	failure.UsedPercent = nil
	if err := s.SaveQuota(ctx, []routing.QuotaSnapshot{failure}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListQuota(ctx, "codex")
	if err != nil || len(got) != 2 {
		t.Fatalf("snapshots=%+v err=%v", got, err)
	}
	if *got[0].UsedPercent != 15 || !got[0].ObservedAt.Equal(now) || !got[0].ExpiresAt.Equal(a.ExpiresAt) || got[0].Error != failure.Error || got[0].Confidence != "stale" || *got[1].UsedPercent != 75 {
		t.Fatalf("quota scope or previous data lost: %+v", got)
	}
	if values, _ := s.ListQuota(ctx, "claude"); len(values) != 1 || values[0].Error != "" {
		t.Fatalf("provider quota isolation: %+v", values)
	}
	changed := b
	changed.ObservedAt = now.Add(time.Hour)
	changed.UsedPercent = &usedA
	invalid := a
	invalid.ChannelID = 0
	if err := s.SaveQuota(ctx, []routing.QuotaSnapshot{changed, invalid}); err == nil {
		t.Fatal("expected invalid snapshot batch failure")
	}
	got, _ = s.ListQuota(ctx, "codex")
	if *got[1].UsedPercent != 75 {
		t.Fatal("partial quota batch committed")
	}
	// A later successful observation restores its own confidence and clears the
	// previous read error without retaining the failed refresh's timestamps.
	fresh := a
	fresh.ObservedAt = now.Add(2 * time.Minute)
	fresh.ExpiresAt = now.Add(3 * time.Minute)
	fresh.UsedPercent = &usedB
	if err := s.SaveQuota(ctx, []routing.QuotaSnapshot{fresh}); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListQuota(ctx, "codex")
	if err != nil || got[0].Confidence != "exact" || got[0].Error != "" || *got[0].UsedPercent != usedB || !got[0].ObservedAt.Equal(fresh.ObservedAt) {
		t.Fatalf("successful refresh did not recover quota: %+v %v", got, err)
	}
}

func TestProfileQuotaFailedRefreshCannotInheritAnotherAccount(t *testing.T) {
	for _, newHash := range []string{"account-b", ""} {
		t.Run(newHash, func(t *testing.T) {
			s, _ := newProfileTestStore(t)
			ctx := context.Background()
			now := time.Now().UTC()
			used, remaining, limit := 91.0, 9.0, 100.0
			reset := now.Add(time.Hour)
			a := routing.QuotaSnapshot{Provider: "codex", ChannelID: 1, GroupID: 2, AccountIDHash: "account-a", ModelScope: "gpt-a", WindowName: "5h", UsedPercent: &used, Remaining: &remaining, Limit: &limit, ResetAt: &reset, ObservedAt: now, ExpiresAt: now.Add(time.Minute), Confidence: "exact"}
			if err := s.SaveQuota(ctx, []routing.QuotaSnapshot{a}); err != nil {
				t.Fatal(err)
			}
			failure := a
			failure.AccountIDHash = newHash
			failure.ObservedAt = now.Add(time.Second)
			failure.Error = "refresh failed"
			if err := s.SaveQuota(ctx, []routing.QuotaSnapshot{failure}); err != nil {
				t.Fatal(err)
			}
			got, err := s.ListQuota(ctx, "codex")
			if err != nil || len(got) != 1 {
				t.Fatalf("snapshot=%+v err=%v", got, err)
			}
			b := got[0]
			if b.AccountIDHash != newHash || b.Confidence != "unknown" || b.Error != failure.Error || b.UsedPercent != nil || b.Remaining != nil || b.Limit != nil || b.ResetAt != nil || !b.ObservedAt.Equal(failure.ObservedAt) {
				t.Fatalf("failed new account inherited previous identity or quota: %+v", b)
			}
		})
	}
}

func TestProfileQuotaExpiredSuccessIsReadAsStaleWithoutRewritingSnapshot(t *testing.T) {
	s, db := newProfileTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	used := 25.0
	past := now.Add(-time.Hour)
	for _, tc := range []struct {
		name    string
		expires time.Time
		reset   *time.Time
	}{
		{"expired", past, nil}, {"no-expiry", time.Time{}, nil}, {"reset-passed", now.Add(time.Hour), &past},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := routing.QuotaSnapshot{Provider: "codex", ChannelID: 1, GroupID: 2, AccountIDHash: "account-a", ModelScope: tc.name, WindowName: "5h", UsedPercent: &used, ObservedAt: now.Add(-2 * time.Hour), ExpiresAt: tc.expires, ResetAt: tc.reset, Confidence: "exact"}
			if err := s.SaveQuota(ctx, []routing.QuotaSnapshot{q}); err != nil {
				t.Fatal(err)
			}
			values, err := s.ListQuota(ctx, "codex")
			if err != nil {
				t.Fatal(err)
			}
			var found *routing.QuotaSnapshot
			for i := range values {
				if values[i].ModelScope == q.ModelScope {
					found = &values[i]
					break
				}
			}
			if found == nil || found.Confidence != "stale" || found.UsedPercent == nil || *found.UsedPercent != used || !found.ObservedAt.Equal(q.ObservedAt) || !found.ExpiresAt.Equal(q.ExpiresAt) {
				t.Fatalf("expired observation changed facts or stayed fresh: %+v", found)
			}
			var confidence string
			if err := db.QueryRow(`SELECT json_extract(payload,'$.confidence') FROM routing_quota_snapshots WHERE model_scope=?`, tc.name).Scan(&confidence); err != nil {
				t.Fatal(err)
			}
			if confidence != "exact" {
				t.Fatalf("read rewrote stored success confidence: %s", confidence)
			}
		})
	}
}

func TestProfileQuotaLatePreviousAccountCannotReplaceNewerBinding(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	usedA, usedB := 90.0, 10.0
	a := routing.QuotaSnapshot{Provider: "codex", ChannelID: 1, GroupID: 2, AccountIDHash: "account-a", ModelScope: "gpt-a", WindowName: "5h", UsedPercent: &usedA, ObservedAt: now, ExpiresAt: now.Add(time.Hour), Confidence: "exact"}
	b := a
	b.AccountIDHash = "account-b"
	b.ObservedAt = now.Add(2 * time.Second)
	b.UsedPercent = &usedB
	lateA := a
	lateA.ObservedAt = now.Add(time.Second)
	for _, snapshot := range []routing.QuotaSnapshot{a, b, lateA} {
		if err := s.SaveQuota(ctx, []routing.QuotaSnapshot{snapshot}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListQuota(ctx, "codex")
	if err != nil || len(got) != 1 {
		t.Fatalf("snapshots=%+v err=%v", got, err)
	}
	if got[0].AccountIDHash != b.AccountIDHash || !got[0].ObservedAt.Equal(b.ObservedAt) || got[0].UsedPercent == nil || *got[0].UsedPercent != usedB {
		t.Fatalf("late previous account overwrote newer binding: %+v", got[0])
	}
}
