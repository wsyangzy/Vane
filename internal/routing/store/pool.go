package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/wsyangzy/Vane/internal/routing"
)

type PoolStore struct{ db *sql.DB }

func NewPoolStore(db *sql.DB) *PoolStore { return &PoolStore{db} }

func (s *PoolStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS routing_pools (
 id INTEGER PRIMARY KEY AUTOINCREMENT, provider TEXT NOT NULL CHECK(provider IN ('claude','codex')),
 name TEXT NOT NULL, strategy TEXT NOT NULL DEFAULT 'sequential', is_default INTEGER NOT NULL DEFAULT 0,
 rules TEXT NOT NULL DEFAULT '[]', non_stream TEXT, key_hash TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 1,
 UNIQUE(provider,name));
 CREATE UNIQUE INDEX IF NOT EXISTS routing_default_pool ON routing_pools(provider) WHERE is_default=1;
 CREATE TABLE IF NOT EXISTS routing_pool_members (
 pool_id INTEGER NOT NULL REFERENCES routing_pools(id) ON DELETE CASCADE, channel_id INTEGER NOT NULL,
 group_id INTEGER NOT NULL, tier TEXT NOT NULL CHECK(tier IN ('primary','backup')), position INTEGER NOT NULL,
 PRIMARY KEY(pool_id,channel_id,group_id));
 CREATE TABLE IF NOT EXISTS routing_pool_migrations (provider TEXT PRIMARY KEY, completed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
 CREATE TABLE IF NOT EXISTS routing_pool_cooldowns (provider TEXT PRIMARY KEY, state TEXT NOT NULL DEFAULT '[]');`)
	return err
}

func (s *PoolStore) List(ctx context.Context, provider string) ([]routing.Pool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider,name,strategy,is_default,rules,COALESCE(non_stream,''),key_hash,revision FROM routing_pools WHERE provider=? ORDER BY id`, provider)
	if err != nil {
		return nil, err
	}
	var pools []routing.Pool
	for rows.Next() {
		var p routing.Pool
		var rules, nonStream string
		if err = rows.Scan(&p.ID, &p.Provider, &p.Name, &p.Strategy, &p.Default, &rules, &nonStream, &p.KeyHash, &p.Revision); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(rules), &p.Rules); err != nil {
			rows.Close()
			return nil, err
		}
		if p.NonStream, err = routing.ParseNonStreamRoute(nonStream); err != nil {
			rows.Close()
			return nil, err
		}
		p.HasKey = p.KeyHash != ""
		p.Members = []routing.PoolMember{}
		pools = append(pools, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range pools {
		rows, err = s.db.QueryContext(ctx, `SELECT channel_id,group_id,tier FROM routing_pool_members WHERE pool_id=? ORDER BY position`, pools[i].ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m routing.PoolMember
			if err = rows.Scan(&m.ChannelID, &m.GroupID, &m.Tier); err != nil {
				rows.Close()
				return nil, err
			}
			pools[i].Members = append(pools[i].Members, m)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return pools, nil
}

func savePool(ctx context.Context, tx *sql.Tx, p *routing.Pool) error {
	rules, err := json.Marshal(p.Rules)
	if err != nil {
		return err
	}
	nonStream, err := routing.EncodeNonStreamRoute(p.NonStream)
	if err != nil {
		return err
	}
	if p.Default {
		if _, err = tx.ExecContext(ctx, `UPDATE routing_pools SET is_default=0 WHERE provider=?`, p.Provider); err != nil {
			return err
		}
	}
	if p.ID == 0 {
		result, e := tx.ExecContext(ctx, `INSERT INTO routing_pools(provider,name,strategy,is_default,rules,non_stream,key_hash) VALUES(?,?,?,?,?,?,?)`, p.Provider, p.Name, p.Strategy, p.Default, string(rules), nonStream, p.KeyHash)
		if e != nil {
			return e
		}
		p.ID, err = result.LastInsertId()
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE routing_pools SET name=?,strategy=?,is_default=?,rules=?,non_stream=?,revision=revision+1 WHERE id=? AND provider=?`, p.Name, p.Strategy, p.Default, string(rules), nonStream, p.ID, p.Provider)
		if err == nil {
			n, _ := result.RowsAffected()
			if n == 0 {
				return errors.New("账号池不存在")
			}
		}
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM routing_pool_members WHERE pool_id=?`, p.ID); err != nil {
		return err
	}
	for i, m := range p.Members {
		if _, err = tx.ExecContext(ctx, `INSERT INTO routing_pool_members(pool_id,channel_id,group_id,tier,position) VALUES(?,?,?,?,?)`, p.ID, m.ChannelID, m.GroupID, m.Tier, i); err != nil {
			return err
		}
	}
	return nil
}
func (s *PoolStore) Save(ctx context.Context, p *routing.Pool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = savePool(ctx, tx, p); err != nil {
		return err
	}
	var defaults int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM routing_pools WHERE provider=? AND is_default=1`, p.Provider).Scan(&defaults); err != nil {
		return err
	}
	if defaults != 1 {
		return errors.New("请先指定其他默认账号池")
	}
	return tx.Commit()
}
func (s *PoolStore) Delete(ctx context.Context, provider string, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var isDefault bool
	if err = tx.QueryRowContext(ctx, `SELECT is_default FROM routing_pools WHERE id=? AND provider=?`, id, provider).Scan(&isDefault); err != nil {
		return err
	}
	if isDefault {
		return errors.New("请先指定其他默认账号池")
	}
	pools, err := s.listReferences(ctx, tx, provider, id)
	if err != nil {
		return err
	}
	if pools {
		return errors.New("该账号池仍被路由规则引用")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM routing_pool_members WHERE pool_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM routing_pools WHERE id=? AND provider=?`, id, provider); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *PoolStore) listReferences(ctx context.Context, tx *sql.Tx, provider string, id int64) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT rules,COALESCE(non_stream,'') FROM routing_pools WHERE provider=? AND id<>?`, provider, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw, ns string
		if err = rows.Scan(&raw, &ns); err != nil {
			return false, err
		}
		var rules []routing.ModelRule
		if err = json.Unmarshal([]byte(raw), &rules); err != nil {
			return false, err
		}
		for _, rule := range rules {
			for _, target := range rule.Targets {
				if target.PoolID == id {
					return true, nil
				}
			}
		}
		route, err := routing.ParseNonStreamRoute(ns)
		if err != nil {
			return false, err
		}
		if route != nil {
			for _, target := range route.Targets {
				if target.PoolID == id {
					return true, nil
				}
			}
		}
	}
	return false, rows.Err()
}
func (s *PoolStore) RotateKey(ctx context.Context, provider string, id int64, hash string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE routing_pools SET key_hash=?,revision=revision+1 WHERE provider=? AND id=?`, hash, provider, id)
	if err == nil {
		n, _ := result.RowsAffected()
		if n == 0 {
			return errors.New("账号池不存在")
		}
	}
	return err
}
func (s *PoolStore) LoadCooldowns(ctx context.Context, provider string) ([]routing.Cooldown, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT state FROM routing_pool_cooldowns WHERE provider=?`, provider).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state []routing.Cooldown
	err = json.Unmarshal([]byte(raw), &state)
	return state, err
}
func (s *PoolStore) SaveCooldowns(ctx context.Context, provider string, state []routing.Cooldown) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO routing_pool_cooldowns(provider,state) VALUES(?,?) ON CONFLICT(provider) DO UPDATE SET state=excluded.state`, provider, string(raw))
	return err
}

// Migrate publishes all compatibility pools and the marker in one transaction.
// The caller takes a SQLite backup before invoking this for the first time.
func (s *PoolStore) NeedsMigration(ctx context.Context, provider string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM routing_pool_migrations WHERE provider=?`, provider).Scan(&n)
	return n == 0, err
}
func (s *PoolStore) Migrate(ctx context.Context, provider string, pools []routing.Pool, disabledGroups ...int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM routing_pool_migrations WHERE provider=?`, provider).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	for i := range pools {
		pool := pools[i]
		pool.ID = 0
		pool.Provider = provider
		if err = savePool(ctx, tx, &pool); err != nil {
			return fmt.Errorf("迁移账号池: %w", err)
		}
	}
	table := "claude_credential_groups"
	if provider == "codex" {
		table = "codex_credential_groups"
	}
	for _, id := range disabledGroups {
		if _, err = tx.ExecContext(ctx, `UPDATE `+table+` SET enabled=0 WHERE id=?`, id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO routing_pool_migrations(provider) VALUES(?)`, provider); err != nil {
		return err
	}
	return tx.Commit()
}
