package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/wsyangzy/Vane/internal/routing"
)

const manualBalanceSchema = `
CREATE TABLE IF NOT EXISTS routing_manual_balances (
 provider TEXT NOT NULL CHECK(provider IN ('claude','codex')), channel_id INTEGER NOT NULL, group_id INTEGER NOT NULL,
 account_hash TEXT NOT NULL, enabled INTEGER NOT NULL, stop_exhausted INTEGER NOT NULL,
 balance INTEGER NOT NULL, baseline INTEGER NOT NULL, credited INTEGER NOT NULL DEFAULT 0,
 spent INTEGER NOT NULL DEFAULT 0, incomplete INTEGER NOT NULL DEFAULT 0, revision INTEGER NOT NULL,
 calibrated_at TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(provider,channel_id,group_id));
CREATE TABLE IF NOT EXISTS routing_balance_entries (
 id INTEGER PRIMARY KEY AUTOINCREMENT, provider TEXT NOT NULL, channel_id INTEGER NOT NULL, group_id INTEGER NOT NULL,
 account_hash TEXT NOT NULL, operation_id TEXT NOT NULL, operation_fingerprint TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, amount INTEGER NOT NULL,
 balance INTEGER NOT NULL, estimated INTEGER NOT NULL, created_at TEXT NOT NULL,
 UNIQUE(provider,operation_id));
CREATE INDEX IF NOT EXISTS routing_balance_entries_group ON routing_balance_entries(provider,channel_id,group_id,id);`

const balanceColumns = `provider,channel_id,group_id,account_hash,enabled,stop_exhausted,balance,baseline,credited,spent,incomplete,revision,calibrated_at,updated_at`

func scanBalance(row profileScanner) (*routing.ManualBalance, error) {
	b := &routing.ManualBalance{Currency: "USD"}
	var amount, baseline, credited, spent int64
	var calibrated, updated string
	err := row.Scan(&b.Provider, &b.ChannelID, &b.GroupID, &b.AccountIDHash, &b.Enabled, &b.StopWhenExhausted, &amount, &baseline, &credited, &spent, &b.IncompleteCount, &b.Revision, &calibrated, &updated)
	if err != nil {
		return nil, err
	}
	b.BalanceUSD, b.BaselineUSD = routing.FormatUSD(amount), routing.FormatUSD(baseline)
	b.CreditedUSD, b.SpentUSD = routing.FormatUSD(credited), routing.FormatUSD(spent)
	b.CalibratedAt, err = time.Parse(time.RFC3339Nano, calibrated)
	if err != nil {
		return nil, err
	}
	b.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return b, err
}

func (s *ProfileStore) ManualBalance(ctx context.Context, provider string, channel, group int64) (*routing.ManualBalance, error) {
	b, err := scanBalance(s.db.QueryRowContext(ctx, `SELECT `+balanceColumns+` FROM routing_manual_balances WHERE provider=? AND channel_id=? AND group_id=?`, provider, channel, group))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return b, err
}

func (s *ProfileStore) UpdateManualBalance(ctx context.Context, input routing.ManualBalanceUpdate, accountHash string, now time.Time) (*routing.ManualBalance, error) {
	if !validProfileProvider(input.Provider) || input.ChannelID <= 0 || input.GroupID <= 0 || accountHash == "" || len(input.OperationID) < 8 || len(input.OperationID) > 100 {
		return nil, errors.New("balance_input_invalid")
	}
	var amount int64
	var err error
	if input.Action == "calibrate" || input.Action == "credit" {
		amount, err = routing.ParseUSD(input.AmountUSD)
		if err != nil {
			return nil, err
		}
		if input.Action == "credit" && amount == 0 {
			return nil, errors.New("balance_credit_invalid")
		}
	} else if input.Action != "disable" && input.Action != "settings" {
		return nil, errors.New("balance_action_invalid")
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s/%d/%t/%d/%s", input.Action, amount, input.StopWhenExhausted, input.Revision, accountHash))))
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	b, err := scanBalance(tx.QueryRowContext(ctx, `SELECT `+balanceColumns+` FROM routing_manual_balances WHERE provider=? AND channel_id=? AND group_id=?`, input.Provider, input.ChannelID, input.GroupID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var operationChannel, operationGroup int64
	var operationFingerprint string
	err = tx.QueryRowContext(ctx, `SELECT channel_id,group_id,operation_fingerprint FROM routing_balance_entries WHERE provider=? AND operation_id=?`, input.Provider, "manual/"+input.OperationID).Scan(&operationChannel, &operationGroup, &operationFingerprint)
	if err == nil {
		if operationChannel != input.ChannelID || operationGroup != input.GroupID || operationFingerprint != fingerprint {
			return nil, errors.New("balance_operation_conflict")
		}
		return b, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if b == nil {
		b = &routing.ManualBalance{Provider: input.Provider, ChannelID: input.ChannelID, GroupID: input.GroupID, Currency: "USD"}
	}
	if b.Revision != input.Revision {
		return nil, errors.New("balance_revision_conflict")
	}
	if input.Action != "calibrate" && (b.AccountIDHash != accountHash || !b.Enabled) {
		return nil, errors.New("balance_calibration_required")
	}
	var balance, baseline, credited, spent int64
	if b.Revision > 0 {
		// Stored strings were formatted by us and may include a negative balance.
		balance, baseline, credited, spent = signedUSD(b.BalanceUSD), signedUSD(b.BaselineUSD), signedUSD(b.CreditedUSD), signedUSD(b.SpentUSD)
	}
	entryAmount := int64(0)
	switch input.Action {
	case "calibrate":
		if balance < 0 && amount > math.MaxInt64+balance {
			return nil, errors.New("balance_amount_overflow")
		}
		entryAmount = amount - balance
		balance, baseline, credited, spent = amount, amount, 0, 0
		b.IncompleteCount, b.CalibratedAt, b.Enabled = 0, now, true
	case "credit":
		if balance > math.MaxInt64-amount || credited > math.MaxInt64-amount {
			return nil, errors.New("balance_amount_overflow")
		}
		balance, credited, entryAmount = balance+amount, credited+amount, amount
	case "disable":
		b.Enabled = false
	}
	b.AccountIDHash, b.StopWhenExhausted, b.UpdatedAt = accountHash, input.StopWhenExhausted, now
	b.Revision++
	_, err = tx.ExecContext(ctx, `INSERT INTO routing_manual_balances (`+balanceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(provider,channel_id,group_id) DO UPDATE SET account_hash=excluded.account_hash,enabled=excluded.enabled,stop_exhausted=excluded.stop_exhausted,balance=excluded.balance,baseline=excluded.baseline,credited=excluded.credited,spent=excluded.spent,incomplete=excluded.incomplete,revision=excluded.revision,calibrated_at=excluded.calibrated_at,updated_at=excluded.updated_at`, b.Provider, b.ChannelID, b.GroupID, b.AccountIDHash, b.Enabled, b.StopWhenExhausted, balance, baseline, credited, spent, b.IncompleteCount, b.Revision, b.CalibratedAt.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO routing_balance_entries(provider,channel_id,group_id,account_hash,operation_id,operation_fingerprint,kind,amount,balance,estimated,created_at) VALUES(?,?,?,?,?,?,?,?,?,0,?)`, b.Provider, b.ChannelID, b.GroupID, accountHash, "manual/"+input.OperationID, fingerprint, input.Action, entryAmount, balance, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	b.BalanceUSD, b.BaselineUSD, b.CreditedUSD, b.SpentUSD = routing.FormatUSD(balance), routing.FormatUSD(baseline), routing.FormatUSD(credited), routing.FormatUSD(spent)
	return b, nil
}

func signedUSD(value string) int64 {
	if value == "-9223372036.854775808" {
		return math.MinInt64
	}
	if len(value) > 0 && value[0] == '-' {
		n, _ := routing.ParseUSD(value[1:])
		return -n
	}
	n, _ := routing.ParseUSD(value)
	return n
}

// SettleManualBalance is independent of request-log retention or repricing.
// The unique attempt ID makes archive replay and repeated completion harmless.
func (s *ProfileStore) SettleManualBalance(ctx context.Context, settlement routing.AttemptSettlement, costUSD float64, priced bool, now time.Time) error {
	c := settlement.Candidate
	amount, err := routing.CostToUSD(costUSD)
	if err != nil {
		return err
	}
	if settlement.ID == "" || c.AccountIDHash == "" {
		return errors.New("balance_settlement_invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b, err := scanBalance(tx.QueryRowContext(ctx, `SELECT `+balanceColumns+` FROM routing_manual_balances WHERE provider=? AND channel_id=? AND group_id=?`, c.Provider, c.ChannelID, c.GroupID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !b.Enabled || b.AccountIDHash != c.AccountIDHash || settlement.StartedAt.Before(b.CalibratedAt) {
		return nil
	}
	if !priced {
		amount = 0
	}
	balance, spent := signedUSD(b.BalanceUSD), signedUSD(b.SpentUSD)
	if balance < math.MinInt64+amount || spent > math.MaxInt64-amount {
		return errors.New("balance_amount_overflow")
	}
	estimated := !priced || !settlement.Usage.Complete
	result, err := tx.ExecContext(ctx, `INSERT INTO routing_balance_entries(provider,channel_id,group_id,account_hash,operation_id,kind,amount,balance,estimated,created_at) VALUES(?,?,?,?,?,'debit',?,?,?,?) ON CONFLICT(provider,operation_id) DO NOTHING`, c.Provider, c.ChannelID, c.GroupID, c.AccountIDHash, "attempt/"+settlement.ID, -amount, balance-amount, estimated, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted == 0 {
		return nil
	}
	incomplete := 0
	if estimated {
		incomplete = 1
	}
	_, err = tx.ExecContext(ctx, `UPDATE routing_manual_balances SET balance=balance-?,spent=spent+?,incomplete=incomplete+?,revision=revision+1,updated_at=? WHERE provider=? AND channel_id=? AND group_id=?`, amount, amount, incomplete, now.UTC().Format(time.RFC3339Nano), c.Provider, c.ChannelID, c.GroupID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *ProfileStore) ManualBalanceEntries(ctx context.Context, provider string, channel, group int64) ([]routing.BalanceEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,kind,amount,balance,estimated,created_at FROM routing_balance_entries WHERE provider=? AND channel_id=? AND group_id=? ORDER BY id DESC LIMIT 100`, provider, channel, group)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []routing.BalanceEntry{}
	for rows.Next() {
		var e routing.BalanceEntry
		var amount, balance int64
		var created string
		if err = rows.Scan(&e.ID, &e.Kind, &amount, &balance, &e.Estimated, &created); err != nil {
			return nil, err
		}
		e.AmountUSD, e.BalanceUSD = routing.FormatUSD(amount), routing.FormatUSD(balance)
		if e.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
