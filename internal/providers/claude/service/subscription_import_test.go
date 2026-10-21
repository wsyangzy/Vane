package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
	_ "modernc.org/sqlite"
)

type importTestVault struct{}

func (importTestVault) Seal(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil }
func (importTestVault) Open(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil }

func TestClaudeImportKeepsIdentityAcrossBrowserFileAndTokenRotation(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	m, e := accounts.New(db, importTestVault{}, map[string]accounts.Adapter{"claude": SubscriptionAccountAdapter{}})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	native := []byte(`{"claudeAiOauth":{"accessToken":"access-sentinel","refreshToken":"refresh-sentinel","expiresAt":1800000000123},"account_uuid":"account-a","email":"one@example.test"}`)
	ctx := context.Background()
	first, e := m.Import(ctx, "claude", "browser", native)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Update(ctx, "claude", first.ID, "saved", false); e != nil {
		t.Fatal(e)
	}
	file := []byte(`{"claudeAiOauth":{"accessToken":"access-sentinel","refreshToken":"refresh-sentinel"}}`)
	if _, e = (SubscriptionAccountAdapter{}).Parse(file); e == nil {
		t.Fatal("missing identity accepted")
	}
	prepared, e := PrepareSubscriptionImport(file, "one@example.test")
	if e != nil {
		t.Fatal(e)
	}
	second, e := m.Import(ctx, "claude", "file", prepared)
	if e != nil {
		t.Fatal(e)
	}
	rotated := strings.ReplaceAll(strings.ReplaceAll(string(prepared), "access-sentinel", "access-rotated"), "refresh-sentinel", "refresh-rotated")
	third, e := m.Import(ctx, "claude", "rotated", []byte(rotated))
	if e != nil {
		t.Fatal(e)
	}
	list, e := m.List(ctx, "claude")
	if e != nil || len(list) != 1 || first.ID != second.ID || first.ID != third.ID || third.Enabled {
		t.Fatal("same account duplicated or enabled unexpectedly")
	}
	different := strings.ReplaceAll(string(native), "account-a", "account-b")
	if _, e = m.Import(ctx, "claude", "other", []byte(different)); e != nil {
		t.Fatal(e)
	}
	list, _ = m.List(ctx, "claude")
	if len(list) != 2 {
		t.Fatal("different stable accounts merged by email")
	}
}

func TestClaudeFlatImportAndExplicitIdentityValidation(t *testing.T) {
	raw := []byte(`{"type":"claude","access_token":"access","refresh_token":"refresh","email":"one@example.test","account_uuid":"account-a","scope":"user:inference user:profile","expired":"2026-11-01T00:00:00Z"}`)
	c, e := (SubscriptionAccountAdapter{}).Parse(raw)
	if e != nil || c.Subject != "account-a" || len(c.Scopes) != 2 || c.ExpiresAt.IsZero() {
		t.Fatal("flat credentials not imported", e)
	}
	for _, email := range []string{"other@example.test", "not-an-email"} {
		if _, e = PrepareSubscriptionImport(raw, email); e == nil {
			t.Fatal("invalid or conflicting identity accepted")
		}
	}
	var p map[string]any
	_ = json.Unmarshal(raw, &p)
	p["type"] = "codex"
	bad, _ := json.Marshal(p)
	if _, e = (SubscriptionAccountAdapter{}).Parse(bad); e == nil {
		t.Fatal("wrong provider accepted")
	}
	for _, code := range []string{"missing-state", "code#wrong"} {
		if validateClaudeCode(code, "https://claude.com/cai/oauth/authorize?state=expected") == nil {
			t.Fatal("foreign code accepted")
		}
	}
	if e = validateClaudeCode("code#expected", "https://claude.com/cai/oauth/authorize?state=expected"); e != nil {
		t.Fatal(e)
	}
}

func TestClaudeImportUpgradesEmailIdentityAndRejectsAmbiguousEmail(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	m, e := accounts.New(db, importTestVault{}, map[string]accounts.Adapter{"claude": SubscriptionAccountAdapter{}})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	ctx := context.Background()
	email := "account" + "@" + "example.test"
	raw := func(id, token string) []byte {
		data, _ := json.Marshal(map[string]any{"account_uuid": id, "email": email, "claudeAiOauth": map[string]string{"accessToken": token, "refreshToken": "refresh-" + token}})
		return data
	}
	first, e := m.Import(ctx, "claude", "file", raw("", "file-token"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Update(ctx, "claude", first.ID, "saved", false); e != nil {
		t.Fatal(e)
	}
	upgraded, e := m.Import(ctx, "claude", "browser", raw("uuid-a", "browser-token"))
	if e != nil || upgraded.ID != first.ID || upgraded.Enabled {
		t.Fatal("UUID login lost row or enablement", e)
	}
	var saved []byte
	if e = db.QueryRow(`SELECT credential FROM subscription_accounts WHERE id=?`, first.ID).Scan(&saved); e != nil {
		t.Fatal(e)
	}
	var cred accounts.Credential
	if json.Unmarshal(saved, &cred) != nil || cred.Subject != "uuid-a" {
		t.Fatal("stable identity not upgraded")
	}
	second, e := m.Import(ctx, "claude", "other", raw("uuid-b", "other-token"))
	if e != nil || second.ID == first.ID {
		t.Fatal("different UUID merged after email import", e)
	}
	if _, e = m.Import(ctx, "claude", "ambiguous", raw("", "rotated-file-token")); e == nil {
		t.Fatal("ambiguous email overwrote an arbitrary account")
	}
	repeated, e := m.Import(ctx, "claude", "repeat", raw("uuid-a", "new-token"))
	if e != nil || repeated.ID != first.ID || repeated.Enabled {
		t.Fatal("upgraded identity could not be reimported", e)
	}
	list, e := m.List(ctx, "claude")
	if e != nil || len(list) != 2 {
		t.Fatal("unexpected account count", e)
	}
}
