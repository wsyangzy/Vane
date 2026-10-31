package privacy

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func protectionService(t *testing.T) *Service {
	t.Helper()
	s := NewService(newTestStore(t))
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestProtectionOptionsAndActualDelivery(t *testing.T) {
	s := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.Words = []string{"Acme", "ACME"}
	settings.WordsAnyCase = false
	settings.RestoreResponses = true
	settings.Allow = []string{"EXEMPT@example.com"}
	settings.Actions = map[string]string{"phone": ActionBlock}
	migrateProtectionSettings(t, s, settings)
	if got, _ := s.GetSettings(ctx); hasLegacyMatchingOptions(got) || !got.RestoreResponses {
		t.Fatalf("options lost: %+v", got)
	}
	result, err := s.Apply(ctx, Request{RequestID: "request-1", SessionKey: "s", Path: "/v1/messages", ProviderType: "claude"}, []byte(`{"messages":[{"content":"Acme exempt@example.com"}]}`))
	if err != nil || result.Changed || result.HitCount != 1 {
		t.Fatalf("detect result: %+v %v", result, err)
	}
	result.SelectBody(result.Body)
	entries, err := s.store.listActivity(ctx)
	if err != nil || entries[0].State != "detected" {
		t.Fatalf("entries %+v %v", entries, err)
	}
	settings, _ = s.GetSettings(ctx)
	settings.Mode = ModeRedact
	if err := s.UpdateSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	result, err = s.Apply(ctx, Request{RequestID: "request-2", SessionKey: "s", Path: "/v1/messages", ProviderType: "claude"}, result.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, redacted := result.SelectBody(result.Body)
	if !redacted || strings.Contains(string(body), "Acme") || !strings.Contains(string(body), "exempt@example.com") {
		t.Fatalf("body: %s", body)
	}
	entries, _ = s.store.listActivity(ctx)
	encoded, _ := json.Marshal(entries)
	if entries[0].State != "redacted" || strings.Contains(string(encoded), "Acme") || strings.Contains(string(encoded), "exempt@example.com") {
		t.Fatalf("unsafe activity: %s", encoded)
	}
	before := len(s.vault.sessions)
	s.PreviewText("Acme")
	s.PreviewRequest("/v1/messages", []byte(`{"messages":[{"content":"Acme"}]}`))
	if len(s.vault.sessions) != before {
		t.Fatal("preview mutated vault")
	}
}
func TestToolArgumentsAndCompletePrivateKey(t *testing.T) {
	rules := []Rule{presetRule("完整私钥块", privateKeyPattern, "[私钥]", 10), presetRule("密钥", `SECRET`, "[密钥]", 20)}
	compiled, err := CompileRules(rules)
	if err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	s := Snapshot{Settings: settings, Rules: compiled}
	body := []byte(`{"model":"SECRET","tools":[{"name":"SECRET"}],"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"SECRET","signature":"SECRET"},{"type":"tool_use","id":"SECRET","name":"SECRET","input":{"id":"SECRET","name":"SECRET","url":"SECRET","args":{"data":"data:image/png;base64,SECRET"}}}]},{"role":"user","content":"-----BEGIN RSA PRIVATE KEY-----\nMATERIAL\n-----END RSA PRIVATE KEY-----"}]}`)
	res, err := s.Apply(Request{Path: "/v1/messages"}, body)
	if err != nil || res.HitCount != 4 {
		t.Fatalf("result %+v %v", res, err)
	}
	out := string(res.Body)
	for _, keep := range []string{`"model":"SECRET"`, `"thinking":"SECRET"`, `"signature":"SECRET"`, `"id":"SECRET","name":"SECRET","input"`, `data:image/png;base64,SECRET`} {
		if !strings.Contains(out, keep) {
			t.Fatalf("changed protocol/media: %s", out)
		}
	}
	if strings.Contains(out, "MATERIAL") || strings.Contains(out, "PRIVATE KEY") {
		t.Fatalf("PEM leaked: %s", out)
	}
}
func TestLegacyBlockBecomesOrdinaryRuleAndHonorsAllowance(t *testing.T) {
	first := presetRule("遮罩", `SECRET`, "[x]", 1)
	block := presetRule("拦截", `SECRET`, "[x]", 500)
	block.Action = ActionBlock
	block.ID = 2
	rules, _ := CompileRules([]Rule{first, block})
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	s := Snapshot{Settings: settings, Rules: rules}
	res, err := s.Apply(Request{Path: "/v1/responses"}, []byte(`{"input":"SECRET"}`))
	if err != nil || !res.Changed || res.HitCount != 1 || strings.Contains(string(res.Body), "SECRET") {
		t.Fatalf("legacy block did not follow redaction mode: %+v %v", res, err)
	}
	if preview := s.ApplyToText("SECRET"); !preview.Changed || preview.Action == ActionBlock {
		t.Fatal("preview still rejected legacy block rule")
	}
	allow, err := CompileRule(Rule{ID: 3, Name: "例外放行", Enabled: true, MatchType: MatchTypeLiteral, Pattern: "secret", Action: ActionAllow})
	if err != nil {
		t.Fatal(err)
	}
	s.Rules = append(s.Rules, allow)
	res, err = s.Apply(Request{Path: "/v1/responses"}, []byte(`{"input":"SECRET"}`))
	if err != nil || res.HitCount != 0 {
		t.Fatal("allow was not honored")
	}
}
func TestVaultIsolationExpiryAndCapacity(t *testing.T) {
	v := newRestorationVault()
	now := time.Now()
	v.now = func() time.Time { return now }
	r := v.request("session-a", "1")
	token := r.Replace("secret", "private-string-123", false)
	if token == "private-string-123" || token != v.request("session-a", "2").Replace("secret", "private-string-123", false) {
		t.Fatal("unstable replacement")
	}
	other := v.request("session-b", "3")
	other.enable()
	if restored, _ := other.restoreText(token, false, true); restored != token {
		t.Fatal("cross session restore")
	}
	if v.request("", "1").Replace("secret", "private-string-123", false) == v.request("", "1").Replace("secret", "private-string-123", false) {
		t.Fatal("missing session shared mappings")
	}
	now = now.Add(restorationTTL + time.Second)
	fresh := v.request("session-a", "4")
	if len(fresh.values) != 0 {
		t.Fatal("expired mapping retained")
	}
	for i := 0; i < maxRestorationSessions+4; i++ {
		v.request(string(rune(1000+i)), "")
	}
	if len(v.sessions) > maxRestorationSessions {
		t.Fatal("unbounded sessions")
	}
	for i := 0; i < maxRestorationValues+10; i++ {
		fresh.Replace("secret", strings.Repeat("x", 32)+string(rune(1000+i)), false)
	}
	if len(fresh.session.values) > maxRestorationValues || fresh.session.bytes > maxRestorationBytes {
		t.Fatal("unbounded mappings")
	}
}
func TestResponseRestoresJSONAndSplitSSEWithoutChangingSignatures(t *testing.T) {
	r := newRestorationVault().request("session", "r")
	raw := "quote\" and\nnewline"
	token := r.Replace("secret", raw, false)
	r.enable()
	result := ApplyResult{Restoration: r}
	rec := httptest.NewRecorder()
	writer, finish := result.WrapResponse(rec)
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Content-Length", "99")
	input := `{"id":"` + token + `","content":[{"type":"text","text":"` + token + `"},{"type":"thinking","thinking":"` + token + `","signature":"` + token + `"}],"arguments":"{\"value\":\"` + token + `\"}"}`
	writer.Write([]byte(input))
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	var obj struct {
		ID        string              `json:"id"`
		Content   []map[string]string `json:"content"`
		Arguments string              `json:"arguments"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &obj); err != nil {
		t.Fatal(err)
	}
	var args map[string]string
	err := json.Unmarshal([]byte(obj.Arguments), &args)
	if err != nil || args["value"] != raw || obj.Content[0]["text"] != raw || obj.ID != token || obj.Content[1]["thinking"] != token || obj.Content[1]["signature"] != token || rec.Header().Get("Content-Length") != "" {
		t.Fatalf("restore: %s err %v", rec.Body, err)
	}
	rec = httptest.NewRecorder()
	writer, finish = result.WrapResponse(rec)
	writer.Header().Set("Content-Type", "text/event-stream")
	for _, text := range []string{token[:9], token[9:17], token[17:]} {
		data, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
		event := append(append([]byte("data: "), data...), []byte("\n\n")...)
		for _, b := range event {
			if _, err := writer.Write([]byte{b}); err != nil {
				t.Fatal(err)
			}
		}
		writer.Write([]byte(": heartbeat\n\n"))
		writer.(http.Flusher).Flush()
	}
	writer.Write([]byte("data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	size := rec.Body.Len()
	finish()
	if rec.Body.Len() != size {
		t.Fatal("Finish is not idempotent")
	}
	var text strings.Builder
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			var obj struct {
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &obj)
			text.WriteString(obj.Delta.Text)
		}
	}
	if text.String() != raw || strings.Count(rec.Body.String(), ": heartbeat") != 3 {
		t.Fatalf("split: %q %s", text.String(), rec.Body)
	}
}
func TestResponseFlushesIncompleteSuffixBeforeStopAndSeparatesStreams(t *testing.T) {
	r := newRestorationVault().request("s", "r")
	token := r.Replace("secret", "secret-value", false)
	r.enable()
	rec := httptest.NewRecorder()
	w, finish := (ApplyResult{Restoration: r}).WrapResponse(rec)
	w.Header().Set("Content-Type", "text/event-stream")
	for _, item := range []struct {
		Index int
		Text  string
	}{{0, token[:8]}, {1, token[8:]}} {
		b, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": item.Index, "delta": map[string]string{"text": item.Text}})
		w.Write(append(append([]byte("data: "), b...), []byte("\n\n")...))
	}
	w.Write([]byte("data: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
	finish()
	out := rec.Body.String()
	if strings.Contains(out, "secret-value") || strings.Index(out, token[:8]) > strings.Index(out, "content_block_stop") {
		t.Fatalf("stream mixing/order: %s", out)
	}
}

func TestProtectionCategoryPrefixCaseAndKnownValueReuse(t *testing.T) {
	s := protectionService(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.Mode = ModeRedact
	settings.RestoreResponses = true
	settings.Words = []string{"Acme"}
	settings.WordsAnyCase = true
	settings.Categories = map[string]bool{"personal": false}
	migrateProtectionSettings(t, s, settings)
	if _, err := s.CreateRule(ctx, Rule{Name: "团队前缀", Enabled: true, MatchType: MatchTypePrefix, Pattern: "team-", Action: ActionRedact, Placeholder: "[密钥]", ReplacementMode: ReplacementPlaceholder}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Apply(ctx, Request{Path: "/v1/responses", SessionKey: "session"}, []byte(`{"input":"acme person@example.com team-ABCdef123456"}`))
	if err != nil || first.HitCount != 2 || !strings.Contains(string(first.Body), "person@example.com") {
		t.Fatalf("categories/prefix: %+v %v", first, err)
	}
	// A context rule matches a password once. Its value must still be protected
	// when the model/client repeats it later without the identifying field name.
	passwordRule := presetRule("密码字段", `password=(?P<redact>[a-z]{12})`, "[密码]", 10)
	passwordRule.ReplacementMode = ReplacementReversible
	_, err = s.CreateRule(ctx, passwordRule)
	if err != nil {
		t.Fatal(err)
	}
	first, err = s.Apply(ctx, Request{Path: "/v1/responses", SessionKey: "session"}, []byte(`{"input":"password=abcdefghijkl"}`))
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.Apply(ctx, Request{Path: "/v1/responses", SessionKey: "session"}, []byte(`{"input":"abcdefghijkl"}`))
	if err != nil || next.HitCount != 1 || strings.Contains(string(next.Body), "abcdefghijkl") {
		t.Fatalf("known reuse: %+v %v", next, err)
	}
	if !strings.Contains(string(first.Body), strings.Trim(string(next.Body), `{}"input:`)) {
		t.Fatal("known value changed its replacement")
	}
}

func TestVaultConcurrentStableAndShapedKeys(t *testing.T) {
	v := newRestorationVault()
	var wg sync.WaitGroup
	tokens := make(chan string, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := v.request("same-session", "")
			tokens <- r.Replace("api_key", "sk-abcABC1234567890", true)
		}()
	}
	wg.Wait()
	close(tokens)
	first := ""
	for token := range tokens {
		if first == "" {
			first = token
		}
		if token != first {
			t.Fatal("concurrent replacements differ")
		}
	}
	if first == "sk-abcABC1234567890" || len(first) != len("sk-abcABC1234567890") || !strings.HasPrefix(first, "sk-") {
		t.Fatalf("shape: %s", first)
	}
	r := v.request("same-session", "")
	r.enable()
	restored, _ := r.restoreText(first, false, true)
	if restored != "sk-abcABC1234567890" {
		t.Fatal("shaped value was not reversible")
	}
}

func TestExistingSettingsAndPresetUpgradePreserveUserRules(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE privacy_settings (id INTEGER PRIMARY KEY, mode TEXT, scan_max_bytes INTEGER, over_limit_action TEXT, on_error TEXT, unsupported_path_action TEXT, updated_at TEXT); INSERT INTO privacy_settings VALUES(1,'redact',4194304,'scan_prefix','fail_open','warn','')`)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	old := presetRule("私钥块头部", `-----BEGIN [A-Z ]*PRIVATE KEY-----`, "[私钥]", 190)
	preset, err := store.CreateRule(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	old.Source = SourceCustom
	custom, err := store.CreateRule(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	settings, err := store.LoadSettings(ctx)
	if err != nil || settings.Mode != ModeRedact || !settings.RestoreResponses {
		t.Fatalf("legacy config: %+v %v", settings, err)
	}
	upgraded, _ := store.GetRule(ctx, preset.ID)
	kept, _ := store.GetRule(ctx, custom.ID)
	if upgraded.Pattern != privateKeyPattern || kept.Pattern != old.Pattern {
		t.Fatal("preset upgrade changed user-authored pattern")
	}
}

type failedResponseWriter struct{ header http.Header }

func (w *failedResponseWriter) Header() http.Header { return w.header }
func (w *failedResponseWriter) WriteHeader(int)     {}
func (w *failedResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}
func TestResponsePreservesTransportErrorsAndEncodedBytes(t *testing.T) {
	r := newRestorationVault().request("s", "")
	token := r.Replace("secret", "real-value", false)
	r.enable()
	w, finish := (ApplyResult{Restoration: r}).WrapResponse(&failedResponseWriter{header: http.Header{}})
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"output_text":"` + token + `"}`)); err == nil {
		t.Fatal("transport write error swallowed")
	}
	if finish() == nil {
		t.Fatal("Finish lost transport error")
	}
	rec := httptest.NewRecorder()
	w, finish = (ApplyResult{Restoration: r}).WrapResponse(rec)
	w.Header().Set("Content-Encoding", "gzip")
	w.Write([]byte(token))
	finish()
	if rec.Body.String() != token || r.skipped != "encoded_response" {
		t.Fatal("encoded payload modified")
	}
}

func TestCodexToolArgumentDeltasRestoreEscapedValues(t *testing.T) {
	r := newRestorationVault().request("s", "")
	raw := "a\"b\nc"
	token := r.Replace("custom", raw, false)
	r.enable()
	rec := httptest.NewRecorder()
	w, finish := (ApplyResult{Restoration: r}).WrapResponse(rec)
	w.Header().Set("Content-Type", "text/event-stream")
	for _, text := range []string{`{"key":"` + token[:15], token[15:] + `"}`} {
		data, _ := json.Marshal(map[string]any{"type": "response.function_call_arguments.delta", "item_id": "call-1", "output_index": 0, "delta": text})
		w.Write(append(append([]byte("data: "), data...), []byte("\n\n")...))
	}
	w.Write([]byte("data: [DONE]\n\n"))
	finish()
	var arguments strings.Builder
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var root struct {
			Delta string `json:"delta"`
		}
		json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &root)
		arguments.WriteString(root.Delta)
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(arguments.String()), &args); err != nil || args["key"] != raw {
		t.Fatalf("arguments %q err %v", arguments.String(), err)
	}
}
