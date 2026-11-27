package codexproxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	codexstore "github.com/wsyangzy/Vane/internal/providers/codex/store"
	"github.com/wsyangzy/Vane/internal/routing"
)

const (
	maxCompletedReasoningEntries = 256
	maxCompletedReasoningBytes   = 8 << 20
	completedReasoningTTL        = time.Hour
)

// The process-local cache repairs exact added -> done transitions. Opt-in
// protected storage extends this with completed items and stable provenance.
// Opaque values never enter ordinary logs or request tracking records.
type completedReasoningCache struct {
	persistenceMu    sync.RWMutex
	mu               sync.Mutex
	entries          map[string]completedReasoningEntry
	bytes            int
	storageConfig    HistoryStorageConfig
	storage          *codexstore.RecoveryStore
	onStorageError   func()
	lastStorageError time.Time
}

type completedReasoningEntry struct {
	value   string
	expires time.Time
}

func newCompletedReasoningCache() *completedReasoningCache {
	return &completedReasoningCache{entries: make(map[string]completedReasoningEntry)}
}

func reasoningHash(parts ...string) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func completedReasoningScope(req, source *http.Request, ep *CodexEndpoint, body []byte) string {
	if source.Method != http.MethodPost || subscription.Path(source) != "/responses" || req.Header.Get("Content-Encoding") != "" {
		return ""
	}
	turn := routing.ResolveTurn(source.Context(), source.Header, body, "codex")
	if turn.ConversationID == "" {
		return "" // Never guess a conversation from message text or item IDs.
	}
	var payload struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	return reasoningHash(routing.CallerScope(source.Context()), turn.ConversationID, turn.BranchID,
		fmt.Sprintf("%d/%d/%d/%s/%s/%s", ep.ChannelID, ep.CredentialGroupID, ep.ID, ep.AuthMode, ep.BaseURL, ep.PathStrategy),
		req.URL.String(), req.Host, payload.Model, req.Header.Get("Authorization"), req.Header.Get("X-Api-Key"),
		req.Header.Get("X-Relay-Key"), req.Header.Get("Cookie"), req.Header.Get("ChatGPT-Account-ID"), req.Header.Get("OpenAI-Organization"), req.Header.Get("OpenAI-Project"))
}

func (c *completedReasoningCache) prune(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			c.bytes -= len(entry.value)
			delete(c.entries, key)
		}
	}
}

func (c *completedReasoningCache) remember(scope string, items map[string]*completedReasoningItem) {
	if c == nil || scope == "" {
		return
	}
	c.rememberComplete(scope, items)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.prune(now)
	for id, item := range items {
		if item.partial == "" || item.final == "" {
			continue
		}
		key := reasoningHash(scope, id, item.partial)
		if old, ok := c.entries[key]; ok {
			// Conflicting completions are ambiguous; disable this mapping until expiry.
			if old.value != item.final {
				c.bytes -= len(old.value)
				old.value = ""
				c.entries[key] = old
			}
			continue
		}
		if reasoningHash(item.final) == item.partial || len(c.entries) >= maxCompletedReasoningEntries || c.bytes+len(item.final) > maxCompletedReasoningBytes {
			continue
		}
		c.entries[key] = completedReasoningEntry{value: item.final, expires: now.Add(completedReasoningTTL)}
		c.bytes += len(item.final)
	}
}

type completedReasoningItem struct {
	partial, final string
	item           json.RawMessage
}
type completedReasoningObservation struct {
	items             map[string]*completedReasoningItem
	bytes             int
	completed, failed bool
}

// Official Responses streaming guidance: the encrypted_content on added can
// be incomplete; only output_item.done is a completed reasoning item. A bare
// [DONE], an incomplete response or a read error cannot commit repair state.
func (o *completedReasoningObservation) observe(event *codexSSEEvent) {
	if event == nil || o.failed {
		return
	}
	if event.err != nil {
		o.failed = true
		o.items = nil
		return
	}
	typ, _ := event.frame["type"].(string)
	if typ == "response.completed" {
		o.completed = true
	}
	if typ != "response.output_item.added" && typ != "response.output_item.done" {
		return
	}
	if o.completed {
		o.failed = true
		o.items = nil
		return
	}
	item, _ := event.frame["item"].(map[string]any)
	id, _ := item["id"].(string)
	value, _ := item["encrypted_content"].(string)
	if item["type"] != "reasoning" || id == "" || value == "" {
		return
	}
	id = reasoningHash(id)
	if o.items == nil {
		o.items = make(map[string]*completedReasoningItem)
	}
	if typ == "response.output_item.added" {
		if _, exists := o.items[id]; exists || len(o.items) >= maxCompletedReasoningEntries {
			o.failed = true
			o.items = nil
			return
		}
		o.items[id] = &completedReasoningItem{partial: reasoningHash(value)}
		return
	}
	previous := o.items[id]
	if previous == nil {
		if len(o.items) >= maxCompletedReasoningEntries {
			o.failed = true
			o.items = nil
			return
		}
		previous = &completedReasoningItem{}
		o.items[id] = previous
	}
	if status, _ := item["status"].(string); status != "" && status != "completed" {
		o.failed = true
		o.items = nil
		return
	}
	if previous.final != "" || o.bytes+len(value) > maxCompletedReasoningBytes {
		o.failed = true
		o.items = nil
		return
	}
	previous.final = value
	previous.item, _ = json.Marshal(item)
	o.bytes += len(value)
}

// Restore only ciphertext with evidence of its exact incomplete predecessor.
// Item order, message/tool content, JSON whitespace and all other fields retain
// their original bytes; unknown/corrupt values are never guessed or truncated.
func (c *completedReasoningCache) restore(scope string, body []byte, sessions ...string) ([]byte, int) {
	if c == nil || scope == "" {
		return body, 0
	}
	start, _, input, ok := encryptedHistoryInput(body)
	if !ok {
		return body, 0
	}
	session := ""
	if len(sessions) > 0 {
		session = sessions[0]
	}
	stored := c.storedItems(scope, input, session)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(time.Now())
	type edit struct {
		start, end int
		value      string
	}
	var edits []edit
	cursor := start + 1
	for _, raw := range input {
		var item struct {
			ID               string `json:"id"`
			Type             string `json:"type"`
			EncryptedContent string `json:"encrypted_content"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Type == "compaction" || item.Type == "context_compaction" {
			return body, 0
		}
		offset := bytes.Index(body[cursor:], raw)
		if offset < 0 {
			return body, 0
		}
		cursor += offset
		if item.Type == "reasoning" && item.ID != "" && item.EncryptedContent != "" {
			if record, found := stored[reasoningHash(item.ID)]; found && record.CipherHash == "" {
				cursor += len(raw)
				continue // Persisted conflicting completions also invalidate memory hints.
			}
			key := reasoningHash(scope, reasoningHash(item.ID), reasoningHash(item.EncryptedContent))
			value := ""
			if entry, found := c.entries[key]; found {
				value = entry.value
			}
			if value == "" {
				if record, found := stored[reasoningHash(item.ID)]; found && record.CipherHash != "" && (record.PartialHash == reasoningHash(item.EncryptedContent) || record.Fingerprint != "" && record.Fingerprint == reasoningFingerprint(raw)) {
					var complete struct {
						EncryptedContent string `json:"encrypted_content"`
					}
					if json.Unmarshal(record.Item, &complete) == nil && reasoningHash(complete.EncryptedContent) == record.CipherHash {
						value = complete.EncryptedContent
					}
				}
			}
			if value != "" && value != item.EncryptedContent {
				from, to, valid := reasoningCipherSpan(raw)
				if !valid {
					return body, 0
				}
				edits = append(edits, edit{cursor + from, cursor + to, value})
			}
		}
		cursor += len(raw)
	}
	if len(edits) == 0 {
		return body, 0
	}
	var out bytes.Buffer
	cursor = 0
	for _, change := range edits {
		out.Write(body[cursor:change.start])
		raw, _ := json.Marshal(change.value)
		out.Write(raw)
		cursor = change.end
	}
	out.Write(body[cursor:])
	return out.Bytes(), len(edits)
}

func reasoningCipherSpan(raw []byte) (start, end int, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if first, err := dec.Token(); err != nil || first != json.Delim('{') {
		return
	}
	seen := make(map[string]bool)
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return
		}
		name, _ := key.(string)
		if seen[name] {
			return 0, 0, false
		}
		seen[name] = true
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return
		}
		if name == "encrypted_content" {
			end = int(dec.InputOffset())
			start = end - len(value)
		}
	}
	return start, end, end > start
}
