package codexproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type HistoryStorageConfig struct {
	Enabled, ReadClientSessions bool
	Directory, Namespace        string
	RetentionDays, MaxMiB       int
}

func (h *HTTPHandler) ConfigureHistoryStorage(ctx context.Context, cfg HistoryStorageConfig) error {
	if h.handler == nil {
		return errors.New("codex_handler_unavailable")
	}
	h.handler.completedHistory.mu.Lock()
	h.handler.completedHistory.onStorageError = func() {
		h.handler.logger.Warn("Codex 恢复状态存储暂不可用，本次仍按内存恢复和降级策略处理")
	}
	h.handler.completedHistory.mu.Unlock()
	return h.handler.completedHistory.configureStorage(ctx, cfg)
}

func (c *completedReasoningCache) configureStorage(ctx context.Context, cfg HistoryStorageConfig) error {
	c.persistenceMu.Lock()
	defer c.persistenceMu.Unlock()
	if cfg.Directory == "" {
		cfg.Directory = codexstore.DefaultRecoveryDirectory()
	}
	c.mu.Lock()
	if cfg == c.storageConfig {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	var storage *codexstore.RecoveryStore
	if cfg.Enabled {
		storage = &codexstore.RecoveryStore{Path: filepath.Join(cfg.Directory, "recovery.db"), Retention: time.Duration(cfg.RetentionDays) * 24 * time.Hour, MaxBytes: int64(cfg.MaxMiB) << 20, Vault: accounts.UserVault{}}
		if err := storage.Initialize(ctx); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.storageConfig, c.storage = cfg, storage
	c.mu.Unlock()
	return nil
}

func (c *completedReasoningCache) scopeNamespace() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.storageConfig.Namespace
}

func reasoningFingerprint(item []byte) string {
	if _, _, ok := reasoningCipherSpan(item); !ok {
		return ""
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(item, &fields) != nil {
		return ""
	}
	delete(fields, "encrypted_content")
	delete(fields, "status")
	// Normalize JSON layout without converting numbers to float64.
	var normalized any
	encoded, _ := json.Marshal(fields)
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if decoder.Decode(&normalized) != nil {
		return ""
	}
	raw, _ := json.Marshal(normalized)
	return reasoningHash(string(raw))
}

// Store completed reasoning only, never visible messages, tool arguments or
// credentials. Large items keep a protected digest as evidence for native-file
// lookup instead of multiplying their disk footprint.
func (c *completedReasoningCache) rememberComplete(scope string, items map[string]*completedReasoningItem) {
	c.persistenceMu.RLock()
	defer c.persistenceMu.RUnlock()
	c.mu.Lock()
	storage := c.storage
	c.mu.Unlock()
	if storage == nil {
		return
	}
	var records []codexstore.RecoveryRecord
	for id, item := range items {
		if item.final == "" || len(item.item) == 0 {
			continue
		}
		record := codexstore.RecoveryRecord{Scope: scope, ItemID: id, PartialHash: item.partial, CipherHash: reasoningHash(item.final), Fingerprint: reasoningFingerprint(item.item), CreatedAt: time.Now(), Item: item.item}
		if len(record.Item) > 256<<10 {
			record.Item = nil
		}
		records = append(records, record)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	if err := storage.Save(ctx, records); err != nil {
		c.reportStorageError()
	}
}

func (c *completedReasoningCache) reportStorageError() {
	c.mu.Lock()
	if time.Since(c.lastStorageError) < time.Minute {
		c.mu.Unlock()
		return
	}
	c.lastStorageError = time.Now()
	callback := c.onStorageError
	c.mu.Unlock()
	if callback != nil {
		callback()
	}
}

func (c *completedReasoningCache) storedItems(scope string, input []json.RawMessage, session string) map[string]codexstore.RecoveryRecord {
	c.persistenceMu.RLock()
	defer c.persistenceMu.RUnlock()
	c.mu.Lock()
	storage, native := c.storage, c.storageConfig.ReadClientSessions
	c.mu.Unlock()
	if storage == nil {
		return nil
	}
	ids := make([]string, 0, len(input))
	for _, raw := range input {
		var item struct{ Type, ID string }
		if json.Unmarshal(raw, &item) == nil && item.Type == "reasoning" && item.ID != "" {
			ids = append(ids, reasoningHash(item.ID))
		}
		if len(ids) > 256 {
			return nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
	defer cancel()
	records, err := storage.Load(ctx, scope, ids)
	if err != nil {
		c.reportStorageError()
		return nil
	}
	if native && session != "" {
		needed := make(map[string]string)
		for id, record := range records {
			if len(record.Item) == 0 && record.CipherHash != "" {
				needed[id] = record.CipherHash
			}
		}
		for id, item := range readNativeReasoning(ctx, session, needed) {
			record := records[id]
			record.Item = item
			records[id] = record
		}
	}
	return records
}

func (c *completedReasoningCache) rememberNonStreaming(scope string, body []byte) {
	if c == nil || scope == "" || len(body) > maxCodexStreamEventBytes {
		return
	}
	var frame map[string]any
	if json.Unmarshal(body, &frame) != nil || frame["status"] != "completed" || frame["error"] != nil {
		return
	}
	output, _ := frame["output"].([]any)
	var observation completedReasoningObservation
	for _, item := range output {
		observation.observe(&codexSSEEvent{frame: map[string]any{"type": "response.output_item.done", "item": item}})
	}
	if !observation.failed {
		c.remember(scope, observation.items)
	}
}
