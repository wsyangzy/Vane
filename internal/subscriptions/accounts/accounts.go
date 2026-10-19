// Package accounts owns local account lifecycle. Provider authentication and
// refresh protocols are supplied by adapters, never selected in this package.
package accounts

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrSignInRequired = errors.New("subscription_sign_in_required")

// Credential never crosses an output DTO or logging boundary.
type Credential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	Subject      string    `json:"subject"`
	Email        string    `json:"email,omitempty"`
	Plan         string    `json:"plan,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scopes       []string  `json:"scopes,omitempty"`
}

type Info struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Plan      string    `json:"plan"`
	AccountID string    `json:"account_id"`
	ExpiresAt time.Time `json:"expires_at"`
	Enabled   bool      `json:"enabled"`
	Status    string    `json:"status"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Adapter interface {
	Parse([]byte) (Credential, error)
	Refresh(context.Context, Credential) (Credential, error)
	Login() LoginSpec
}

type Vault interface {
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}

type Manager struct {
	db       *sql.DB
	vault    Vault
	adapters map[string]Adapter
	locks    sync.Map
	loginMu  sync.Mutex
	logins   map[string]*loginFlow
}

func New(db *sql.DB, vault Vault, adapters map[string]Adapter) (*Manager, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS subscription_accounts (
	 id TEXT PRIMARY KEY, provider TEXT NOT NULL, identity_hash TEXT NOT NULL,
	 info TEXT NOT NULL, credential BLOB NOT NULL, revision INTEGER NOT NULL,
	 UNIQUE(provider,identity_hash));`)
	if err != nil {
		return nil, err
	}
	return &Manager{db: db, vault: vault, adapters: adapters, logins: map[string]*loginFlow{}}, nil
}

func (m *Manager) lock(id string) func() {
	v, _ := m.locks.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (m *Manager) List(ctx context.Context, provider string) ([]Info, error) {
	if _, ok := m.adapters[provider]; !ok {
		return nil, errors.New("unsupported_subscription_provider")
	}
	rows, err := m.db.QueryContext(ctx, `SELECT info FROM subscription_accounts WHERE provider=? ORDER BY id`, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Info{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var i Info
		if err = json.Unmarshal([]byte(raw), &i); err != nil {
			return nil, errors.New("invalid_account_metadata")
		}
		result = append(result, i)
	}
	return result, rows.Err()
}

func (m *Manager) read(ctx context.Context, id string) (Info, Credential, error) {
	var raw string
	var ciphertext []byte
	var info Info
	var cred Credential
	if err := m.db.QueryRowContext(ctx, `SELECT info,credential FROM subscription_accounts WHERE id=?`, id).Scan(&raw, &ciphertext); err != nil {
		return info, cred, errors.New("subscription_account_not_found")
	}
	if json.Unmarshal([]byte(raw), &info) != nil {
		return info, cred, errors.New("invalid_account_metadata")
	}
	data, err := m.vault.Open(ciphertext)
	if err != nil {
		return info, cred, errors.New("subscription_credential_decryption_failed")
	}
	defer clear(data)
	if json.Unmarshal(data, &cred) != nil {
		return info, cred, errors.New("invalid_subscription_credential")
	}
	return info, cred, nil
}

func (m *Manager) write(ctx context.Context, info Info, cred Credential, expected int64) error {
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	defer clear(data)
	ciphertext, err := m.vault.Seal(data)
	if err != nil {
		return errors.New("subscription_credential_encryption_failed")
	}
	info.Revision = expected + 1
	info.UpdatedAt = time.Now().UTC()
	raw, _ := json.Marshal(info)
	result, err := m.db.ExecContext(ctx, `UPDATE subscription_accounts SET info=?,credential=?,revision=? WHERE id=? AND revision=?`, string(raw), ciphertext, info.Revision, info.ID, expected)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return errors.New("subscription_account_changed")
	}
	return nil
}

// Import only consumes explicitly supplied official credential content. A repeat
// import replaces that identity, preserving enablement and group binding IDs.
func (m *Manager) Import(ctx context.Context, provider, name string, raw []byte) (Info, error) {
	a, ok := m.adapters[provider]
	if !ok {
		return Info{}, errors.New("unsupported_subscription_provider")
	}
	if len(raw) == 0 || len(raw) > 1<<20 {
		return Info{}, errors.New("invalid_credential_file_size")
	}
	cred, err := a.Parse(raw)
	if err != nil {
		return Info{}, err
	}
	if strings.TrimSpace(cred.Subject) == "" || !ValidToken(cred.AccessToken) {
		return Info{}, errors.New("invalid_subscription_identity")
	}
	sum := sha256.Sum256([]byte(cred.Subject + "\x00" + cred.AccountID))
	identity := hex.EncodeToString(sum[:])
	unlock := m.lock(provider + ":" + identity)
	defer unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		name = cred.Email
	}
	if name == "" {
		name = provider + " 订阅"
	}
	if len(name) > 256 {
		return Info{}, errors.New("account_name_too_long")
	}
	var id string
	err = m.db.QueryRowContext(ctx, `SELECT id FROM subscription_accounts WHERE provider=? AND identity_hash=?`, provider, identity).Scan(&id)
	if err == nil {
		unlockID := m.lock(id)
		defer unlockID()
		old, _, e := m.read(ctx, id)
		if e != nil {
			return Info{}, e
		}
		old.Name = name
		old.Email = cred.Email
		old.Plan = cred.Plan
		old.AccountID = cred.AccountID
		old.ExpiresAt = cred.ExpiresAt
		old.Status = "ready"
		if e = m.write(ctx, old, cred, old.Revision); e != nil {
			return Info{}, e
		}
		old.Revision++
		return old, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Info{}, err
	}
	info := Info{ID: uuid.NewString(), Provider: provider, Name: name, Email: cred.Email, Plan: cred.Plan, AccountID: cred.AccountID, ExpiresAt: cred.ExpiresAt, Enabled: true, Status: "ready", Revision: 1, UpdatedAt: time.Now().UTC()}
	data, _ := json.Marshal(cred)
	defer clear(data)
	sealed, err := m.vault.Seal(data)
	if err != nil {
		return Info{}, errors.New("subscription_credential_encryption_failed")
	}
	meta, _ := json.Marshal(info)
	_, err = m.db.ExecContext(ctx, `INSERT INTO subscription_accounts(id,provider,identity_hash,info,credential,revision) VALUES(?,?,?,?,?,1)`, info.ID, provider, identity, string(meta), sealed)
	return info, err
}

func (m *Manager) Update(ctx context.Context, provider, id, name string, enabled bool) (Info, error) {
	unlock := m.lock(id)
	defer unlock()
	i, c, err := m.read(ctx, id)
	if err != nil {
		return Info{}, err
	}
	if i.Provider != provider {
		return Info{}, errors.New("subscription_provider_mismatch")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 256 {
		return Info{}, errors.New("invalid_account_name")
	}
	i.Name = name
	i.Enabled = enabled
	err = m.write(ctx, i, c, i.Revision)
	i.Revision++
	return i, err
}

func (m *Manager) Delete(ctx context.Context, provider, id string) error {
	unlock := m.lock(id)
	defer unlock()
	_, err := m.db.ExecContext(ctx, `DELETE FROM subscription_accounts WHERE id=? AND provider=?`, id, provider)
	return err
}

func (m *Manager) Validate(ctx context.Context, provider, id string) error {
	var raw string
	if err := m.db.QueryRowContext(ctx, `SELECT info FROM subscription_accounts WHERE id=? AND provider=?`, id, provider).Scan(&raw); err != nil {
		return errors.New("subscription_account_not_found")
	}
	var i Info
	if json.Unmarshal([]byte(raw), &i) != nil || !i.Enabled {
		return errors.New("subscription_account_disabled")
	}
	if i.Status == "sign_in_required" {
		return ErrSignInRequired
	}
	return nil
}

// Refresh uses an account lock and preserves a rotating refresh token even when
// the caller cancels after the exchange started. No generation is replayed.
func (m *Manager) credential(ctx context.Context, provider, id string, force bool) (Info, Credential, error) {
	unlock := m.lock(id)
	defer unlock()
	i, c, err := m.read(ctx, id)
	if err != nil {
		return i, c, err
	}
	if i.Provider != provider {
		return i, Credential{}, errors.New("subscription_provider_mismatch")
	}
	if !i.Enabled {
		return i, Credential{}, errors.New("subscription_account_disabled")
	}
	if i.Status == "sign_in_required" {
		return i, Credential{}, ErrSignInRequired
	}
	if !force && (c.ExpiresAt.IsZero() || time.Until(c.ExpiresAt) > 2*time.Minute) {
		return i, c, nil
	}
	if ctx.Err() != nil {
		return i, Credential{}, ctx.Err()
	}
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	next, err := m.adapters[provider].Refresh(refreshCtx, c)
	if err != nil {
		if errors.Is(err, ErrSignInRequired) {
			i.Status = "sign_in_required"
			_ = m.write(refreshCtx, i, c, i.Revision)
		}
		return i, Credential{}, err
	}
	if next.Subject != c.Subject || next.AccountID != c.AccountID {
		return i, Credential{}, errors.New("subscription_refresh_identity_changed")
	}
	i.ExpiresAt = next.ExpiresAt
	i.Plan = next.Plan
	i.Status = "ready"
	if err = m.write(refreshCtx, i, next, i.Revision); err != nil {
		return i, Credential{}, err
	}
	i.Revision++
	return i, next, nil
}

func (m *Manager) Refresh(ctx context.Context, provider, id string) (Info, error) {
	i, _, e := m.credential(ctx, provider, id, true)
	return i, e
}
func (m *Manager) Resolve(ctx context.Context, provider, id string) (Credential, error) {
	_, c, e := m.credential(ctx, provider, id, false)
	return c, e
}

// RejectAccess schedules a refresh for the next request. A late 401 for an old
// token cannot expire a newer credential. It never replays a generation.
func (m *Manager) RejectAccess(ctx context.Context, provider, id, token string) {
	unlock := m.lock(id)
	defer unlock()
	i, c, err := m.read(ctx, id)
	if err != nil || i.Provider != provider || c.AccessToken != token {
		return
	}
	c.ExpiresAt = time.Now()
	i.ExpiresAt = c.ExpiresAt
	_ = m.write(ctx, i, c, i.Revision)
}

func ValidToken(s string) bool { return s != "" && !strings.ContainsAny(s, "\r\n\x00 ") }

func SafeRefreshError(status int, code string) error {
	switch code {
	case "invalid_grant", "invalid_refresh_token", "refresh_token_reused", "refresh_token_expired", "refresh_token_invalidated":
		return ErrSignInRequired
	}
	if status == 401 || status == 403 {
		return ErrSignInRequired
	}
	return fmt.Errorf("subscription_refresh_failed_http_%d", status)
}
