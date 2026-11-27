package codexproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const recoveryNativeSession = "019abcdef-0000-0000-0000-000000000000"
const validRecoveryNativeSession = "019a1234-0000-7000-8000-123456789abc"

func writeRecoveryNativeFixture(t *testing.T, root, session, item, extra string) string {
	t.Helper()
	dir := filepath.Join(root, "sessions", "2026", "11", "26")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-11-26-"+session+".jsonl")
	data := fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":%q}}\n{\"type\":\"response_item\",\"payload\":%s}\n%s", session, item, extra)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRecoveryPersistenceAcrossHandlersAndNativeLookup(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows user vault")
	}
	for _, large := range []bool{false, true} {
		for _, mode := range []string{"profile", "model-rule", "active"} {
			t.Run(fmt.Sprintf("%s/large=%v", mode, large), func(t *testing.T) {
				final := "final"
				if large {
					final = strings.Repeat("s", 300<<10)
				}
				item := fmt.Sprintf(`{"type":"reasoning","id":"rs-1","summary":[],"encrypted_content":%q}`, final)
				nativeRoot := t.TempDir()
				t.Setenv("CODEX_HOME", nativeRoot)
				file := writeRecoveryNativeFixture(t, nativeRoot, validRecoveryNativeSession, item, "")
				before, _ := os.ReadFile(file)
				body := `{"model":"original","input":[{"type":"reasoning","id":"rs-1","summary":[],"encrypted_content":"damaged"},{"role":"user","content":"continue"}]}`
				sends := 0
				h, _ := newEncryptedContentTestHandler(t, mode, func(w http.ResponseWriter, r *http.Request) {
					sends++
					raw, _ := io.ReadAll(r.Body)
					if sends == 1 {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"item\":%s}\n\n%s", item, diagnosticCompletedSSE)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					if strings.Contains(string(raw), `"encrypted_content":"`+final+`"`) {
						io.WriteString(w, encryptedContentTestSuccess)
						return
					}
					w.WriteHeader(400)
					io.WriteString(w, encryptedContentTestError)
				})
				cfg := HistoryStorageConfig{Enabled: true, ReadClientSessions: true, Directory: t.TempDir(), Namespace: "test-db", RetentionDays: 7, MaxMiB: 8}
				if err := h.completedHistory.configureStorage(context.Background(), cfg); err != nil {
					t.Fatal(err)
				}
				req := sameHistoryRequest(`{"model":"original","stream":true,"input":"start"}`)
				req.Header.Set("Session_id", validRecoveryNativeSession)
				h.ServeHTTP(httptest.NewRecorder(), req)
				// A new cache object must recover from disk, not process memory.
				h.completedHistory = newCompletedReasoningCache()
				if err := h.completedHistory.configureStorage(context.Background(), cfg); err != nil {
					t.Fatal(err)
				}
				req = sameHistoryRequest(body)
				req.Header.Set("Session_id", validRecoveryNativeSession)
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != 200 || sends != 4 {
					t.Fatalf("persistent recovery failed: status=%d sends=%d", rec.Code, sends)
				}
				after, _ := os.ReadFile(file)
				if string(before) != string(after) {
					t.Fatal("client session changed")
				}
				cfg.Enabled = false
				if err := h.completedHistory.configureStorage(context.Background(), cfg); err != nil {
					t.Fatal(err)
				}
				rec = httptest.NewRecorder()
				req = sameHistoryRequest(body)
				req.Header.Set("Session_id", validRecoveryNativeSession)
				h.ServeHTTP(rec, req)
				if rec.Code != 400 || sends != 6 {
					t.Fatal("disabled persistence still read disk")
				}
			})
		}
	}
}

func TestRecoveryPersistedConflictInvalidatesOlderMemoryHint(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows user vault")
	}
	c := newCompletedReasoningCache()
	cfg := HistoryStorageConfig{Enabled: true, Directory: t.TempDir(), RetentionDays: 7, MaxMiB: 8}
	if err := c.configureStorage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	item := json.RawMessage(`{"type":"reasoning","id":"rs","summary":[],"encrypted_content":"complete"}`)
	c.remember("scope", map[string]*completedReasoningItem{reasoningHash("rs"): {partial: reasoningHash("partial"), final: "complete", item: item}})
	body := []byte(`{"input":[{"type":"reasoning","id":"rs","summary":[],"encrypted_content":"partial"}]}`)
	if _, n := c.restore("scope", body); n != 1 {
		t.Fatal("initial completed item not restored")
	}
	// Simulate another handler seeing an incompatible completion for the same ID.
	other := newCompletedReasoningCache()
	if err := other.configureStorage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	conflict := json.RawMessage(`{"type":"reasoning","id":"rs","summary":[],"encrypted_content":"other"}`)
	other.remember("scope", map[string]*completedReasoningItem{reasoningHash("rs"): {final: "other", item: conflict}})
	if restored, n := c.restore("scope", body); n != 0 || string(restored) != string(body) {
		t.Fatal("persisted conflict was bypassed through stale process memory")
	}
}

func TestNativeRecoveryOnlyReturnsProvenSameSessionItems(t *testing.T) {
	item := `{"id":"rs","type":"reasoning","summary":[],"encrypted_content":"complete"}`
	needed := map[string]string{reasoningHash("rs"): reasoningHash("complete")}
	for _, ending := range []string{"", `{"type":"compacted","payload":{}}` + "\n", `{"type":"event_msg","payload":{"type":"thread_rolled_back"}}` + "\n", "{unfinished"} {
		t.Run(fmt.Sprint(len(ending)), func(t *testing.T) {
			root := t.TempDir()
			writeRecoveryNativeFixture(t, root, validRecoveryNativeSession, item, ending)
			got := readNativeReasoningAt(context.Background(), root, validRecoveryNativeSession, needed)
			if (len(got) == 1) != (ending == "") {
				t.Fatal("unsafe native history used")
			}
			if len(readNativeReasoningAt(context.Background(), root, recoveryNativeSession, needed)) != 0 {
				t.Fatal("invalid session accepted")
			}
			if len(readNativeReasoningAt(context.Background(), root, validRecoveryNativeSession, map[string]string{reasoningHash("rs"): "other"})) != 0 {
				t.Fatal("unproven ciphertext accepted")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if len(readNativeReasoningAt(ctx, root, validRecoveryNativeSession, needed)) != 0 {
				t.Fatal("cancelled lookup continued")
			}
		})
	}
	root := t.TempDir()
	path := writeRecoveryNativeFixture(t, root, validRecoveryNativeSession, item, "")
	copy, _ := os.ReadFile(path)
	dir := filepath.Join(root, "archived_sessions")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, filepath.Base(path)), copy, 0600)
	if len(readNativeReasoningAt(context.Background(), root, validRecoveryNativeSession, needed)) != 0 {
		t.Fatal("ambiguous duplicate session used")
	}
}

func TestRecoveryStorageDirectorySwitchAndFailedUpdate(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows user vault")
	}
	c := newCompletedReasoningCache()
	cfg := HistoryStorageConfig{Enabled: true, Directory: t.TempDir(), RetentionDays: 7, MaxMiB: 8}
	if err := c.configureStorage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	item := json.RawMessage(`{"type":"reasoning","id":"rs","summary":[],"encrypted_content":"complete"}`)
	c.remember("scope", map[string]*completedReasoningItem{reasoningHash("rs"): {final: "complete", item: item}})
	body := []byte(`{"input":[{"type":"reasoning","id":"rs","summary":[],"encrypted_content":"damaged"}]}`)
	if _, n := c.restore("scope", body); n != 1 {
		t.Fatal("missing stored restoration")
	}
	first := cfg.Directory
	cfg.Directory = t.TempDir()
	if err := c.configureStorage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, n := c.restore("scope", body); n != 0 {
		t.Fatal("directory switch merged old records")
	}
	bad := cfg
	bad.Directory = filepath.Join(t.TempDir(), "not-directory")
	os.WriteFile(bad.Directory, []byte("keep"), 0600)
	if err := c.configureStorage(context.Background(), bad); err == nil {
		t.Fatal("bad directory accepted")
	}
	if c.storageConfig != cfg {
		t.Fatal("failed configuration replaced working storage")
	}
	cfg.Directory = first
	if err := c.configureStorage(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if _, n := c.restore("scope", body); n != 1 {
		t.Fatal("original directory not preserved")
	}
}
