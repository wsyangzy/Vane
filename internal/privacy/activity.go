package privacy

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// Activity contains metadata only. Neither request bodies, matched strings nor
// reversible mappings are persisted. Keep at most 5,000 entries for 30 days.
type Activity struct {
	ID               int64              `json:"id"`
	RequestID        string             `json:"request_id"`
	Provider         string             `json:"provider"`
	Path             string             `json:"path"`
	CreatedAt        time.Time          `json:"created_at"`
	State            string             `json:"state"`
	HitCount         int                `json:"hit_count"`
	RuleHits         []RuleHit          `json:"rule_hits"`
	FieldHits        []ActivityFieldHit `json:"field_hits,omitempty"`
	FieldHitsLimited bool               `json:"field_hits_limited,omitempty"`
	ScanDurationMS   float64            `json:"scan_duration_ms"`
	SkippedReason    string             `json:"skipped_reason"`
	Truncated        bool               `json:"truncated"`
	RestoredCount    int                `json:"restored_count"`
	RestoreSkipped   string             `json:"restore_skipped"`
}

// ActivityFieldHit describes one rule in one sanitized field path.
// ReplacementMode is a strategy identifier, never the replacement text.
type ActivityFieldHit struct {
	Path            string `json:"path"`
	RuleID          int64  `json:"rule_id"`
	RuleName        string `json:"rule_name"`
	Kind            string `json:"kind,omitempty"`
	Action          string `json:"action"`
	ReplacementMode string `json:"replacement_mode,omitempty"`
	Count           int    `json:"count"`
}

const maxActivityFieldHits = 64

type activityFieldKey struct {
	path   string
	ruleID int64
}

func (r *ApplyResult) countFieldHit(index map[activityFieldKey]int, path string, hit *RuleHit, rule *CompiledRule) {
	key := activityFieldKey{path: path, ruleID: hit.RuleID}
	if i, ok := index[key]; ok {
		r.fieldHits[i].Count++
		return
	}
	if len(r.fieldHits) == maxActivityFieldHits {
		r.fieldHitsLimited = true
		return
	}
	field := ActivityFieldHit{Path: path, RuleID: hit.RuleID, RuleName: activityText(hit.RuleName, 96), Kind: hit.Kind, Action: hit.Action, Count: 1}
	if hit.Action == ActionRedact {
		field.ReplacementMode = rule.ReplacementMode
	}
	index[key] = len(r.fieldHits)
	r.fieldHits = append(r.fieldHits, field)
}

type activityHandle struct {
	mu    sync.Mutex
	store *Store
	entry Activity
}

func (s *Store) ensureActivitySchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS privacy_activity (id INTEGER PRIMARY KEY AUTOINCREMENT, created_at INTEGER NOT NULL, payload TEXT NOT NULL); CREATE INDEX IF NOT EXISTS idx_privacy_activity_time ON privacy_activity(created_at)`)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `DELETE FROM privacy_activity WHERE created_at < ? OR id < (SELECT id FROM privacy_activity ORDER BY id DESC LIMIT 1 OFFSET 4999)`, time.Now().Add(-30*24*time.Hour).Unix())
	}
	return err
}

func activityText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}
func (s *Service) recordActivity(ctx context.Context, req Request, result ApplyResult, applyErr error) *activityHandle {
	if s.store == nil || s.store.db == nil {
		return nil
	}
	state := "clean"
	switch {
	case applyErr != nil:
		state = "blocked"
	case result.Action == ModeDisabled:
		state = "disabled"
	case result.SkippedReason != "":
		state = "skipped"
	case result.HitCount > 0:
		state = "detected"
	}
	if result.Truncated && applyErr == nil {
		state = "truncated"
	}
	path := normalizeScanPath(req.Path)
	if !IsScannablePath(path) {
		path = "(未覆盖路径)"
	}
	reason := result.SkippedReason
	if len(reason) > 0 && reason != SkippedUnsupportedPath && reason != SkippedEmptyBody && reason != SkippedNoRules && reason != SkippedNoText && reason != SkippedScanTruncated && reason != SkippedScanLimit && reason != CodeScanFailed && reason != CodeRestoreCapacity && reason != "mapping_capacity" {
		reason = "scan_error"
	}
	h := &activityHandle{store: s.store, entry: Activity{RequestID: activityText(req.RequestID, 128), Provider: activityText(req.ProviderType, 16), Path: path, CreatedAt: s.now(), State: state, HitCount: result.HitCount, ScanDurationMS: float64(result.ScanDuration) / float64(time.Millisecond), SkippedReason: reason, Truncated: result.Truncated}}
	h.entry.FieldHits = result.fieldHits
	h.entry.FieldHitsLimited = result.fieldHitsLimited
	for _, hit := range result.RuleHits {
		if len(h.entry.RuleHits) >= 32 {
			break
		}
		hit.Matches = nil
		hit.RuleName = activityText(hit.RuleName, 96)
		h.entry.RuleHits = append(h.entry.RuleHits, hit)
	}
	writeCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	payload, _ := json.Marshal(h.entry)
	res, err := s.store.db.ExecContext(writeCtx, `INSERT INTO privacy_activity(created_at,payload) VALUES(?,?)`, h.entry.CreatedAt.Unix(), string(payload))
	if err != nil {
		return nil
	}
	h.entry.ID, _ = res.LastInsertId()
	_, _ = s.store.db.ExecContext(writeCtx, `DELETE FROM privacy_activity WHERE created_at < ? OR id < (SELECT id FROM privacy_activity ORDER BY id DESC LIMIT 1 OFFSET 4999)`, s.now().Add(-30*24*time.Hour).Unix())
	return h
}
func (h *activityHandle) update(redacted bool, restored int, skipped string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	changed := false
	if redacted && h.entry.State != "redacted" {
		h.entry.State = "redacted"
		changed = true
	}
	if restored > h.entry.RestoredCount {
		h.entry.RestoredCount = restored
		changed = true
	}
	if skipped != "" && skipped != h.entry.RestoreSkipped {
		h.entry.RestoreSkipped = skipped
		changed = true
	}
	if !changed {
		return
	}
	payload, _ := json.Marshal(h.entry)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, _ = h.store.db.ExecContext(ctx, `UPDATE privacy_activity SET payload=? WHERE id=?`, string(payload), h.entry.ID)
}
func (s *Store) listActivity(ctx context.Context) ([]Activity, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,payload FROM privacy_activity WHERE created_at>=? ORDER BY id DESC LIMIT 100`, time.Now().Add(-30*24*time.Hour).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var id int64
		var payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		var entry Activity
		if err := json.Unmarshal([]byte(payload), &entry); err != nil {
			return nil, err
		}
		entry.ID = id
		out = append(out, entry)
	}
	return out, rows.Err()
}

// ActivitiesForRequests returns the same metadata as the protection history,
// without limiting the lookup to its 100 most recent entries.
func (s *Store) ActivitiesForRequests(ctx context.Context, provider string, requestIDs []string) (map[string]Activity, error) {
	result := make(map[string]Activity)
	if len(requestIDs) == 0 {
		return result, nil
	}
	args := []any{time.Now().Add(-30 * 24 * time.Hour).Unix(), provider}
	for _, id := range requestIDs {
		args = append(args, id)
	}
	// This metadata table is bounded to 5,000 rows. Read its existing JSON
	// rather than migrating the user's database just for the detail view.
	rows, err := s.db.QueryContext(ctx, `SELECT id,payload FROM privacy_activity WHERE created_at>=? AND json_valid(payload) AND json_extract(payload,'$.provider')=? AND json_extract(payload,'$.request_id') IN (`+strings.TrimSuffix(strings.Repeat("?,", len(requestIDs)), ",")+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var payload string
		var activity Activity
		if err := rows.Scan(&id, &payload); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(payload), &activity) == nil {
			activity.ID = id
			result[activity.RequestID] = activity
		}
	}
	return result, rows.Err()
}
