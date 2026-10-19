package accounts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type testAdapter struct {
	refresh func(context.Context, Credential) (Credential, error)
}

func (testAdapter) Parse(raw []byte) (Credential, error) {
	var c Credential
	e := json.Unmarshal(raw, &c)
	return c, e
}
func (a testAdapter) Refresh(ctx context.Context, c Credential) (Credential, error) {
	return a.refresh(ctx, c)
}
func (testAdapter) Login() LoginSpec { return LoginSpec{} }

type testVault struct{}

func (testVault) Seal(data []byte) ([]byte, error) {
	result := append([]byte("sealed:"), data...)
	for i := 7; i < len(result); i++ {
		result[i] ^= 0x5a
	}
	return result, nil
}
func (testVault) Open(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte("sealed:")) {
		return nil, errors.New("invalid")
	}
	result := append([]byte(nil), data[7:]...)
	for i := range result {
		result[i] ^= 0x5a
	}
	return result, nil
}

func newAccountFixture(t *testing.T, adapter Adapter) (*Manager, *sql.DB) {
	t.Helper()
	db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "accounts.db"))
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	m, e := New(db, testVault{}, map[string]Adapter{"claude": adapter, "codex": adapter})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	return m, db
}
func importTestAccount(t *testing.T, m *Manager, provider string, expired bool) Info {
	t.Helper()
	expiry := time.Now().Add(time.Hour)
	if expired {
		expiry = time.Now().Add(-time.Minute)
	}
	raw, _ := json.Marshal(Credential{AccessToken: "access-secret-sentinel", RefreshToken: "refresh-secret-sentinel", Subject: "user-id", AccountID: "workspace-id", Email: "tester@example.invalid", Plan: "test", ExpiresAt: expiry})
	i, e := m.Import(context.Background(), provider, "test account", raw)
	if e != nil {
		t.Fatal(e)
	}
	return i
}

func TestAccountLifecycleEncryptsSecretsAndKeepsProvidersIndependent(t *testing.T) {
	m, db := newAccountFixture(t, testAdapter{})
	ctx := context.Background()
	a := importTestAccount(t, m, "claude", false)
	b := importTestAccount(t, m, "codex", false)
	if a.ID == b.ID {
		t.Fatal("provider identities merged")
	}
	for _, provider := range []string{"claude", "codex"} {
		list, e := m.List(ctx, provider)
		if e != nil || len(list) != 1 {
			t.Fatalf("list %s: %v %v", provider, list, e)
		}
		out, _ := json.Marshal(list)
		if strings.Contains(string(out), "secret-sentinel") {
			t.Fatal("secret returned in metadata")
		}
	}
	var ciphertext []byte
	if e := db.QueryRow(`SELECT credential FROM subscription_accounts WHERE id=?`, a.ID).Scan(&ciphertext); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(ciphertext, []byte("secret-sentinel")) {
		t.Fatal("plaintext credential persisted")
	}
	if _, e := m.Update(ctx, "codex", a.ID, "wrong provider", true); e == nil {
		t.Fatal("cross-provider update accepted")
	}
	if _, e := m.Update(ctx, "claude", a.ID, "renamed", false); e != nil {
		t.Fatal(e)
	}
	if e := m.Validate(ctx, "claude", a.ID); e == nil {
		t.Fatal("disabled account accepted")
	}
	if _, e := m.Resolve(ctx, "claude", a.ID); e == nil {
		t.Fatal("disabled credential resolved")
	}
	// Relogin/import replaces that identity without silently enabling a disabled account.
	reimported := importTestAccount(t, m, "claude", false)
	if reimported.ID != a.ID || reimported.Enabled {
		t.Fatal("reimport changed identity/enablement")
	}
	if e := m.Delete(ctx, "claude", a.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Resolve(ctx, "claude", a.ID); e == nil {
		t.Fatal("deleted account resolved")
	}
	if e := m.Validate(ctx, "codex", b.ID); e != nil {
		t.Fatal("other provider deleted")
	}
}

func TestConcurrentAccountRefreshRotatesOnceAndPersistsAcrossRestart(t *testing.T) {
	var calls atomic.Int32
	a := testAdapter{refresh: func(_ context.Context, c Credential) (Credential, error) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		c.AccessToken = "fresh-access"
		c.RefreshToken = "rotated-refresh"
		c.ExpiresAt = time.Now().Add(time.Hour)
		return c, nil
	}}
	m, db := newAccountFixture(t, a)
	i := importTestAccount(t, m, "codex", true)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, e := m.Resolve(context.Background(), "codex", i.ID)
			if e != nil {
				errs <- e
			} else if c.AccessToken != "fresh-access" || c.RefreshToken != "rotated-refresh" {
				errs <- errors.New("old token returned")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatalf("refreshes=%d", calls.Load())
	}
	restarted, e := New(db, testVault{}, map[string]Adapter{"codex": a})
	if e != nil {
		t.Fatal(e)
	}
	c, e := restarted.Resolve(context.Background(), "codex", i.ID)
	if e != nil || c.RefreshToken != "rotated-refresh" {
		t.Fatal("refresh rotation lost on restart")
	}
}

func TestRefreshSurvivesCancellationButDisableAndDeleteWinAfterExchange(t *testing.T) {
	for _, action := range []string{"disable", "delete"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			m, _ := newAccountFixture(t, testAdapter{refresh: func(ctx context.Context, c Credential) (Credential, error) {
				close(started)
				<-release
				if ctx.Err() != nil {
					return c, ctx.Err()
				}
				c.AccessToken = "new-token"
				c.RefreshToken = "rotated-token"
				c.ExpiresAt = time.Now().Add(time.Hour)
				return c, nil
			}})
			i := importTestAccount(t, m, "codex", true)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { _, e := m.Resolve(ctx, "codex", i.ID); done <- e }()
			<-started
			cancel()
			changed := make(chan error, 1)
			go func() {
				if action == "delete" {
					changed <- m.Delete(context.Background(), "codex", i.ID)
				} else {
					_, e := m.Update(context.Background(), "codex", i.ID, "disabled", false)
					changed <- e
				}
			}()
			close(release)
			if e := <-done; e != nil {
				t.Fatalf("cancel discarded refresh: %v", e)
			}
			if e := <-changed; e != nil {
				t.Fatal(e)
			}
			if e := m.Validate(context.Background(), "codex", i.ID); e == nil {
				t.Fatal("late refresh undid account mutation")
			}
		})
	}
}

func TestRefreshRefusalAndTransientFailureHaveDifferentAccountStates(t *testing.T) {
	for _, permanent := range []bool{false, true} {
		t.Run(fmt.Sprint(permanent), func(t *testing.T) {
			var calls atomic.Int32
			m, _ := newAccountFixture(t, testAdapter{refresh: func(_ context.Context, c Credential) (Credential, error) {
				calls.Add(1)
				if permanent {
					return c, ErrSignInRequired
				}
				return c, errors.New("subscription_refresh_transport_failed")
			}})
			i := importTestAccount(t, m, "claude", true)
			_, e := m.Resolve(context.Background(), "claude", i.ID)
			if e == nil {
				t.Fatal("refresh unexpectedly succeeded")
			}
			list, e := m.List(context.Background(), "claude")
			if e != nil {
				t.Fatal(e)
			}
			if (list[0].Status == "sign_in_required") != permanent {
				t.Fatal("incorrect account state")
			}
			_, _ = m.Resolve(context.Background(), "claude", i.ID)
			want := int32(2)
			if permanent {
				want = 1
			}
			if calls.Load() != want {
				t.Fatalf("refreshes=%d", calls.Load())
			}
		})
	}
}

func TestLateUnauthorizedCannotInvalidateRefreshedAccessToken(t *testing.T) {
	var calls atomic.Int32
	m, _ := newAccountFixture(t, testAdapter{refresh: func(_ context.Context, c Credential) (Credential, error) {
		calls.Add(1)
		c.AccessToken = "fresh-access"
		c.ExpiresAt = time.Now().Add(time.Hour)
		return c, nil
	}})
	i := importTestAccount(t, m, "codex", false)
	m.RejectAccess(context.Background(), "codex", i.ID, "access-secret-sentinel")
	if _, e := m.Resolve(context.Background(), "codex", i.ID); e != nil {
		t.Fatal(e)
	}
	m.RejectAccess(context.Background(), "codex", i.ID, "access-secret-sentinel")
	if _, e := m.Resolve(context.Background(), "codex", i.ID); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 1 {
		t.Fatal("old 401 expired new credential")
	}
}
