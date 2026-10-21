package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type accountAPITestVault struct{}

func (accountAPITestVault) Seal(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil }
func (accountAPITestVault) Open(b []byte) ([]byte, error) { return append([]byte(nil), b...), nil }

func TestSubscriptionAccountAPIImportsExplicitIdentityWithoutExposingTokens(t *testing.T) {
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	m, e := accounts.New(db, accountAPITestVault{}, map[string]accounts.Adapter{"claude": claudeservice.SubscriptionAccountAdapter{}})
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	a := &App{subscriptionAccounts: m}
	email := "account" + "@" + "example.test"
	raw := `{"claudeAiOauth":{"accessToken":"access-private-sentinel","refreshToken":"refresh-private-sentinel"}}`
	if _, e = a.ManageSubscriptionAccounts(SubscriptionAccountCommand{Provider: "claude", Action: "import", CredentialJSON: raw}); e == nil {
		t.Fatal("missing identity accepted")
	}
	result, e := a.ManageSubscriptionAccounts(SubscriptionAccountCommand{Provider: "claude", Action: "import", CredentialJSON: raw, AccountEmail: email})
	if e != nil {
		t.Fatal(e)
	}
	if result.Account == nil || result.Account.Email != email {
		t.Fatal("explicit identity not mapped")
	}
	out, _ := json.Marshal(result)
	if strings.Contains(string(out), "private-sentinel") {
		t.Fatal("secret escaped DTO")
	}
	if _, e = m.Resolve(context.Background(), "codex", result.Account.ID); e == nil {
		t.Fatal("cross-provider credential access accepted")
	}
}
