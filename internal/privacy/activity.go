package privacy

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Activity contains metadata only. Neither request bodies, matched strings nor
// reversible mappings are persisted. Keep at most 5,000 entries for 30 days.
type Activity struct {
	ID             int64     `json:"id"`
	RequestID      string    `json:"request_id"`
	Provider       string    `json:"provider"`
	Path           string    `json:"path"`
	CreatedAt      time.Time `json:"created_at"`
	State          string    `json:"state"`
	HitCount       int       `json:"hit_count"`
	RuleHits       []RuleHit `json:"rule_hits"`
	ScanDurationMS float64   `json:"scan_duration_ms"`
	SkippedReason  string    `json:"skipped_reason"`
	Truncated      bool      `json:"truncated"`
	RestoredCount  int       `json:"restored_count"`
	RestoreSkipped string    `json:"restore_skipped"`
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
	if len(reason) > 0 && reason != SkippedUnsupportedPath && reason != SkippedEmptyBody && reason != SkippedNoRules && reason != SkippedNoText && reason != SkippedScanTruncated && reason != CodeScanFailed {
		reason = "scan_error"
	}
	h := &activityHandle{store: s.store, entry: Activity{RequestID: activityText(req.RequestID, 128), Provider: activityText(req.ProviderType, 16), Path: path, CreatedAt: s.now(), State: state, HitCount: result.HitCount, ScanDurationMS: float64(result.ScanDuration) / float64(time.Millisecond), SkippedReason: reason, Truncated: result.Truncated}}
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
