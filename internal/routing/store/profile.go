package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

var (
	ErrProfileConflict   = errors.New("routing_profile_revision_conflict")
	ErrProfileReferenced = errors.New("routing_profile_referenced")
)

// ProfileStore persists the new contract independently of legacy pool resources.
// Its mutex serializes writes through this instance; revision predicates also
// protect callers using another store instance or an outdated UI revision.
type ProfileStore struct {
	db          *sql.DB
	mu          sync.Mutex
	environment string // Diagnostic traces follow request-log dev/release isolation.
}

func NewProfileStore(db *sql.DB, environments ...string) *ProfileStore {
	environment := "release"
	if len(environments) > 0 && strings.EqualFold(strings.TrimSpace(environments[0]), "dev") {
		environment = "dev"
	}
	return &ProfileStore{db: db, environment: environment}
}

func (s *ProfileStore) EnsureSchema(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS routing_profiles (
 id INTEGER PRIMARY KEY AUTOINCREMENT, provider TEXT NOT NULL CHECK(provider IN ('claude','codex')),
 name TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, is_default INTEGER NOT NULL DEFAULT 0,
 candidate_mode TEXT NOT NULL DEFAULT 'ordered_channels', strategy TEXT NOT NULL DEFAULT 'smart',
 affinity_mode TEXT NOT NULL DEFAULT 'turn', max_attempts INTEGER NOT NULL DEFAULT 10,
 max_duration_ms INTEGER NOT NULL DEFAULT 60000, wait_for_recovery INTEGER NOT NULL DEFAULT 0,
 revision INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(provider,name));
CREATE UNIQUE INDEX IF NOT EXISTS routing_profiles_default ON routing_profiles(provider) WHERE is_default=1;
CREATE TABLE IF NOT EXISTS routing_profile_members (
 profile_id INTEGER NOT NULL REFERENCES routing_profiles(id) ON DELETE CASCADE,
 position INTEGER NOT NULL, channel_id INTEGER NOT NULL, group_id INTEGER NOT NULL DEFAULT 0,
 weight INTEGER NOT NULL DEFAULT 1, enabled INTEGER NOT NULL DEFAULT 1, upstream_model TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(profile_id,position), UNIQUE(profile_id,channel_id,group_id));
CREATE TABLE IF NOT EXISTS routing_profile_rules (
 id INTEGER NOT NULL, profile_id INTEGER NOT NULL REFERENCES routing_profiles(id) ON DELETE CASCADE,
 kind TEXT NOT NULL, name TEXT NOT NULL, priority INTEGER NOT NULL DEFAULT 0, enabled INTEGER NOT NULL DEFAULT 1,
 match_model TEXT NOT NULL DEFAULT '', require_stream INTEGER, intent_id TEXT NOT NULL DEFAULT '',
 mode TEXT NOT NULL DEFAULT 'prefer', targets_json TEXT NOT NULL DEFAULT '[]', fallback_to_default INTEGER,
 revision INTEGER NOT NULL DEFAULT 1, PRIMARY KEY(profile_id,id));
CREATE TABLE IF NOT EXISTS routing_profile_intents (
 id TEXT NOT NULL, profile_id INTEGER NOT NULL REFERENCES routing_profiles(id) ON DELETE CASCADE,
 name TEXT NOT NULL, description TEXT NOT NULL, priority INTEGER NOT NULL DEFAULT 0,
 enabled INTEGER NOT NULL DEFAULT 1, targets_json TEXT NOT NULL DEFAULT '[]', mode TEXT NOT NULL DEFAULT 'prefer',
 fallback_intent TEXT NOT NULL DEFAULT '', fallback_to_default INTEGER, PRIMARY KEY(profile_id,id));
CREATE TABLE IF NOT EXISTS routing_profile_classifier_config (
 profile_id INTEGER PRIMARY KEY REFERENCES routing_profiles(id) ON DELETE CASCADE, payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS routing_quota_snapshots (
 provider TEXT NOT NULL CHECK(provider IN ('claude','codex')), channel_id INTEGER NOT NULL, group_id INTEGER NOT NULL,
 model_scope TEXT NOT NULL, window_name TEXT NOT NULL, payload TEXT NOT NULL, observed_at TEXT NOT NULL,
 PRIMARY KEY(provider,channel_id,group_id,model_scope,window_name));
CREATE TABLE IF NOT EXISTS routing_affinity (
 provider TEXT NOT NULL, profile_id INTEGER NOT NULL REFERENCES routing_profiles(id) ON DELETE CASCADE,
 affinity_key TEXT NOT NULL, payload TEXT NOT NULL, expires_at TEXT NOT NULL,
 PRIMARY KEY(provider,profile_id,affinity_key));
CREATE INDEX IF NOT EXISTS routing_affinity_expiry ON routing_affinity(expires_at);
CREATE TABLE IF NOT EXISTS routing_runtime_state (
 provider TEXT PRIMARY KEY CHECK(provider IN ('claude','codex')), mode TEXT NOT NULL DEFAULT 'unified'
 CHECK(mode IN ('legacy','unified')), updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS routing_dispatch_traces (
 id INTEGER PRIMARY KEY AUTOINCREMENT, provider TEXT NOT NULL, profile_id INTEGER NOT NULL,
 request_id TEXT NOT NULL, payload TEXT NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 environment TEXT NOT NULL DEFAULT 'unknown');
CREATE INDEX IF NOT EXISTS routing_dispatch_traces_provider ON routing_dispatch_traces(provider,id);
CREATE TABLE IF NOT EXISTS routing_profile_migrations (
 provider TEXT PRIMARY KEY, state TEXT NOT NULL, started_at TEXT NOT NULL,
 completed_at TEXT, details TEXT NOT NULL DEFAULT '');`)
	if err != nil {
		return err
	}
	if err = ensureDispatchTraceEnvironment(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, manualBalanceSchema); err != nil {
		return err
	}
	return tx.Commit()
}

func validProfileProvider(provider string) bool { return provider == "claude" || provider == "codex" }

const profileColumns = `id,provider,name,enabled,is_default,candidate_mode,strategy,affinity_mode,max_attempts,max_duration_ms,wait_for_recovery,revision`

func (s *ProfileStore) List(ctx context.Context, provider string) ([]routing.RoutingProfile, error) {
	if !validProfileProvider(provider) {
		return nil, errors.New("provider_invalid")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+profileColumns+` FROM routing_profiles WHERE provider=? ORDER BY id`, provider)
	if err != nil {
		return nil, err
	}
	profiles := []routing.RoutingProfile{}
	for rows.Next() {
		p, scanErr := scanProfile(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		profiles = append(profiles, *p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range profiles {
		if err := loadProfileChildren(ctx, tx, &profiles[i]); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return profiles, nil
}

func (s *ProfileStore) Get(ctx context.Context, provider string, id int64) (*routing.RoutingProfile, error) {
	if !validProfileProvider(provider) {
		return nil, errors.New("provider_invalid")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p, err := scanProfile(tx.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM routing_profiles WHERE provider=? AND id=?`, provider, id))
	if err != nil {
		return nil, err
	}
	if err = loadProfileChildren(ctx, tx, p); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

type profileScanner interface{ Scan(...any) error }

func scanProfile(row profileScanner) (*routing.RoutingProfile, error) {
	p := &routing.RoutingProfile{Members: []routing.RoutingMember{}, Rules: []routing.RoutingRule{}, Intents: []routing.IntentRule{}}
	var durationMS int64
	err := row.Scan(&p.ID, &p.Provider, &p.Name, &p.Enabled, &p.IsDefault, &p.CandidateMode, &p.Strategy, &p.AffinityMode, &p.MaxAttempts, &durationMS, &p.WaitForRecovery, &p.Revision)
	p.MaxDuration = time.Duration(durationMS) * time.Millisecond
	return p, err
}

func loadProfileChildren(ctx context.Context, tx *sql.Tx, p *routing.RoutingProfile) error {
	rows, err := tx.QueryContext(ctx, `SELECT profile_id,channel_id,group_id,position,weight,enabled,upstream_model FROM routing_profile_members WHERE profile_id=? ORDER BY position`, p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m routing.RoutingMember
		if err = rows.Scan(&m.ProfileID, &m.ChannelID, &m.GroupID, &m.Position, &m.Weight, &m.Enabled, &m.UpstreamModel); err != nil {
			rows.Close()
			return err
		}
		p.Members = append(p.Members, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,profile_id,kind,name,priority,enabled,match_model,require_stream,intent_id,mode,targets_json,fallback_to_default FROM routing_profile_rules WHERE profile_id=? ORDER BY priority,id`, p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r routing.RoutingRule
		var targets string
		if err = rows.Scan(&r.ID, &r.ProfileID, &r.Kind, &r.Name, &r.Priority, &r.Enabled, &r.MatchModel, &r.RequireStream, &r.IntentID, &r.Mode, &targets, &r.FallbackToDefault); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(targets), &r.Targets); err != nil {
			rows.Close()
			return err
		}
		p.Rules = append(p.Rules, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,profile_id,name,description,priority,enabled,targets_json,mode,fallback_intent,fallback_to_default FROM routing_profile_intents WHERE profile_id=? ORDER BY priority,id`, p.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var in routing.IntentRule
		var targets string
		if err = rows.Scan(&in.ID, &in.ProfileID, &in.Name, &in.Description, &in.Priority, &in.Enabled, &targets, &in.Mode, &in.FallbackIntent, &in.FallbackToDefault); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(targets), &in.Targets); err != nil {
			rows.Close()
			return err
		}
		p.Intents = append(p.Intents, in)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var config string
	err = tx.QueryRowContext(ctx, `SELECT payload FROM routing_profile_classifier_config WHERE profile_id=?`, p.ID).Scan(&config)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(config), &p.Classifier)
}

// Save updates the caller only after commit, including defaults and child IDs.
func (s *ProfileStore) Save(ctx context.Context, profile *routing.RoutingProfile) error {
	if profile == nil {
		return errors.New("profile_missing")
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	var p routing.RoutingProfile
	if err = json.Unmarshal(data, &p); err != nil {
		return err
	}
	if err = routing.ValidateProfile(&p); err != nil {
		return err
	}
	// Configuration budgets are persisted in milliseconds, matching the UI and
	// documented schema. Return that same value after a successful commit.
	p.MaxDuration = p.MaxDuration.Truncate(time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if p.ID == 0 {
		if p.Revision != 0 {
			return ErrProfileConflict
		}
	} else {
		var revision int64
		var wasDefault bool
		if err = tx.QueryRowContext(ctx, `SELECT revision,is_default FROM routing_profiles WHERE provider=? AND id=?`, p.Provider, p.ID).Scan(&revision, &wasDefault); err != nil {
			return err
		}
		if revision != p.Revision {
			return ErrProfileConflict
		}
		if wasDefault && !p.IsDefault {
			return errors.New("unified_default_profile_required")
		}
	}
	if p.IsDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE routing_profiles SET is_default=0,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE provider=? AND is_default=1 AND id<>?`, p.Provider, p.ID); err != nil {
			return err
		}
	}
	values := []any{p.Name, p.Enabled, p.IsDefault, p.CandidateMode, p.Strategy, p.AffinityMode, p.MaxAttempts, p.MaxDuration.Milliseconds(), p.WaitForRecovery}
	if p.ID == 0 {
		values = append(values, p.Provider)
		result, execErr := tx.ExecContext(ctx, `INSERT INTO routing_profiles(name,enabled,is_default,candidate_mode,strategy,affinity_mode,max_attempts,max_duration_ms,wait_for_recovery,provider) VALUES(?,?,?,?,?,?,?,?,?,?)`, values...)
		if execErr != nil {
			return execErr
		}
		if p.ID, err = result.LastInsertId(); err != nil {
			return err
		}
		p.Revision = 1
	} else {
		values = append(values, p.Provider, p.ID, p.Revision)
		result, execErr := tx.ExecContext(ctx, `UPDATE routing_profiles SET name=?,enabled=?,is_default=?,candidate_mode=?,strategy=?,affinity_mode=?,max_attempts=?,max_duration_ms=?,wait_for_recovery=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE provider=? AND id=? AND revision=?`, values...)
		if execErr != nil {
			return execErr
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return ErrProfileConflict
		}
		p.Revision++
	}
	if err = validateProfileTargets(ctx, tx, &p); err != nil {
		return err
	}
	if err = deleteProfileChildren(ctx, tx, p.ID); err != nil {
		return err
	}
	for i := range p.Members {
		m := &p.Members[i]
		m.ProfileID = p.ID
		if _, err = tx.ExecContext(ctx, `INSERT INTO routing_profile_members(profile_id,position,channel_id,group_id,weight,enabled,upstream_model) VALUES(?,?,?,?,?,?,?)`, p.ID, m.Position, m.ChannelID, m.GroupID, m.Weight, m.Enabled, m.UpstreamModel); err != nil {
			return err
		}
	}
	for i := range p.Rules {
		r := &p.Rules[i]
		r.ProfileID = p.ID
		targets, marshalErr := json.Marshal(r.Targets)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO routing_profile_rules(id,profile_id,kind,name,priority,enabled,match_model,require_stream,intent_id,mode,targets_json,fallback_to_default,revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, p.ID, r.Kind, r.Name, r.Priority, r.Enabled, r.MatchModel, r.RequireStream, r.IntentID, r.Mode, string(targets), r.FallbackToDefault, p.Revision); err != nil {
			return err
		}
	}
	for i := range p.Intents {
		in := &p.Intents[i]
		in.ProfileID = p.ID
		targets, marshalErr := json.Marshal(in.Targets)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO routing_profile_intents(id,profile_id,name,description,priority,enabled,targets_json,mode,fallback_intent,fallback_to_default) VALUES(?,?,?,?,?,?,?,?,?,?)`, in.ID, p.ID, in.Name, in.Description, in.Priority, in.Enabled, string(targets), in.Mode, in.FallbackIntent, in.FallbackToDefault); err != nil {
			return err
		}
	}
	if p.Classifier != nil {
		config, marshalErr := json.Marshal(p.Classifier)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO routing_profile_classifier_config(profile_id,payload) VALUES(?,?)`, p.ID, string(config)); err != nil {
			return err
		}
	}
	if err = ensureRuntimeDefault(ctx, tx, p.Provider); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	*profile = p
	return nil
}

func validateProfileTargets(ctx context.Context, tx *sql.Tx, p *routing.RoutingProfile) error {
	check := func(targets []routing.RouteTarget) error {
		for _, target := range targets {
			if target.ProfileID == 0 {
				continue
			}
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM routing_profiles WHERE provider=? AND id=?`, p.Provider, target.ProfileID).Scan(&exists); err != nil {
				return err
			}
			if exists != 1 {
				return errors.New("route_target_profile_missing_or_provider_mismatch")
			}
		}
		return nil
	}
	for _, r := range p.Rules {
		if err := check(r.Targets); err != nil {
			return err
		}
	}
	for _, in := range p.Intents {
		if err := check(in.Targets); err != nil {
			return err
		}
	}
	return nil
}

func deleteProfileChildren(ctx context.Context, tx *sql.Tx, id int64) error {
	for _, table := range []string{"routing_profile_members", "routing_profile_rules", "routing_profile_intents", "routing_profile_classifier_config"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE profile_id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *ProfileStore) Delete(ctx context.Context, provider string, id int64) error {
	if !validProfileProvider(provider) {
		return errors.New("provider_invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists, isDefault int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(is_default),0) FROM routing_profiles WHERE provider=? AND id=?`, provider, id).Scan(&exists, &isDefault); err != nil {
		return err
	}
	if exists == 0 {
		return sql.ErrNoRows
	}
	if isDefault != 0 {
		return errors.New("unified_default_profile_required")
	}
	// Targets contain no recursive policy: references are checked solely to avoid
	// silently orphaning configured member expansion when a profile is deleted.
	rows, err := tx.QueryContext(ctx, `SELECT targets_json FROM routing_profile_rules WHERE profile_id<>? UNION ALL SELECT targets_json FROM routing_profile_intents WHERE profile_id<>?`, id, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var targets []routing.RouteTarget
		if err = json.Unmarshal([]byte(raw), &targets); err != nil {
			rows.Close()
			return err
		}
		for _, target := range targets {
			if target.ProfileID == id {
				rows.Close()
				return ErrProfileReferenced
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = deleteProfileChildren(ctx, tx, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM routing_affinity WHERE profile_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM routing_profiles WHERE provider=? AND id=?`, provider, id); err != nil {
		return err
	}
	if err = ensureRuntimeDefault(ctx, tx, provider); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ProfileStore) SetDefault(ctx context.Context, provider string, id int64) error {
	if !validProfileProvider(provider) {
		return errors.New("provider_invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var enabled, isDefault bool
	if err = tx.QueryRowContext(ctx, `SELECT enabled,is_default FROM routing_profiles WHERE provider=? AND id=?`, provider, id).Scan(&enabled, &isDefault); err != nil {
		return err
	}
	if !enabled {
		return errors.New("default_profile_disabled")
	}
	if !isDefault {
		if _, err = tx.ExecContext(ctx, `UPDATE routing_profiles SET is_default=0,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE provider=? AND is_default=1`, provider); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE routing_profiles SET is_default=1,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE provider=? AND id=?`, provider, id); err != nil {
			return err
		}
	}
	if err = ensureRuntimeDefault(ctx, tx, provider); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureRuntimeDefault(ctx context.Context, tx *sql.Tx, provider string) error {
	var defaults int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM routing_profiles WHERE provider=? AND is_default=1`, provider).Scan(&defaults); err != nil {
		return err
	}
	if defaults == 0 {
		return nil
	}
	var valid int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM routing_profiles p WHERE p.provider=? AND p.enabled=1 AND p.is_default=1 AND EXISTS(SELECT 1 FROM routing_profile_members m WHERE m.profile_id=p.id AND m.enabled=1)`, provider).Scan(&valid)
	if err != nil {
		return err
	}
	if valid != 1 {
		return errors.New("unified_default_profile_required")
	}
	return nil
}

func (s *ProfileStore) SaveQuota(ctx context.Context, snapshots []routing.QuotaSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, snapshot := range snapshots {
		if !validProfileProvider(snapshot.Provider) || snapshot.ChannelID <= 0 || snapshot.GroupID < 0 || snapshot.WindowName == "" || snapshot.ObservedAt.IsZero() {
			return errors.New("quota_snapshot_invalid")
		}
		key := []any{snapshot.Provider, snapshot.ChannelID, snapshot.GroupID, snapshot.ModelScope, snapshot.WindowName}
		var previousPayload string
		lookupErr := tx.QueryRowContext(ctx, `SELECT payload FROM routing_quota_snapshots WHERE provider=? AND channel_id=? AND group_id=? AND model_scope=? AND window_name=?`, key...).Scan(&previousPayload)
		if lookupErr != nil && !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}
		if lookupErr == nil {
			var previous routing.QuotaSnapshot
			if err = json.Unmarshal([]byte(previousPayload), &previous); err != nil {
				return err
			}
			// A group may have been rebound to another account. Its quota must
			// never inherit the previous account's facts, even after a failed read.
			sameAccount := snapshot.AccountIDHash != "" && snapshot.AccountIDHash == previous.AccountIDHash
			if snapshot.ObservedAt.Before(previous.ObservedAt) {
				continue
			}
			if snapshot.Error != "" && sameAccount {
				previous.Error = snapshot.Error
				previous.Confidence = "stale"
				if !hasQuotaMeasurement(previous) {
					previous.Confidence = "unknown"
				}
				snapshot = previous
			} else if snapshot.Error != "" {
				clearFailedQuotaMeasurement(&snapshot)
			}
		} else if snapshot.Error != "" {
			clearFailedQuotaMeasurement(&snapshot)
		}
		payload, marshalErr := json.Marshal(snapshot)
		if marshalErr != nil {
			return fmt.Errorf("quota_snapshot_encode: %w", marshalErr)
		}
		values := append(key, string(payload), snapshot.ObservedAt.UTC().Format(time.RFC3339Nano))
		if _, err = tx.ExecContext(ctx, `INSERT INTO routing_quota_snapshots(provider,channel_id,group_id,model_scope,window_name,payload,observed_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(provider,channel_id,group_id,model_scope,window_name) DO UPDATE SET payload=excluded.payload,observed_at=excluded.observed_at`, values...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *ProfileStore) ListQuota(ctx context.Context, provider string) ([]routing.QuotaSnapshot, error) {
	if !validProfileProvider(provider) {
		return nil, errors.New("provider_invalid")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM routing_quota_snapshots WHERE provider=? ORDER BY channel_id,group_id,model_scope,window_name`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	snapshots := []routing.QuotaSnapshot{}
	now := time.Now()
	for rows.Next() {
		var payload string
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		var snapshot routing.QuotaSnapshot
		if err = json.Unmarshal([]byte(payload), &snapshot); err != nil {
			return nil, err
		}
		// Expiry is a read projection. Historical values and timestamps remain
		// available for display, but cannot appear as a fresh quota observation.
		if snapshot.Confidence != "unknown" && (snapshot.ExpiresAt.IsZero() || !now.Before(snapshot.ExpiresAt) || (snapshot.ResetAt != nil && !now.Before(*snapshot.ResetAt))) {
			snapshot.Confidence = "stale"
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, rows.Err()
}

func hasQuotaMeasurement(snapshot routing.QuotaSnapshot) bool {
	return snapshot.UsedPercent != nil || snapshot.Remaining != nil || snapshot.Limit != nil
}

func clearFailedQuotaMeasurement(snapshot *routing.QuotaSnapshot) {
	snapshot.UsedPercent = nil
	snapshot.Remaining = nil
	snapshot.Limit = nil
	snapshot.ResetAt = nil
	snapshot.Confidence = "unknown"
}
