package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/routing"
)

func TestManualBalanceLifecycleAndIdempotency(t *testing.T) {
	s, db := newProfileTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	input := routing.ManualBalanceUpdate{Provider: "codex", ChannelID: 1, GroupID: 2, Action: "calibrate", AmountUSD: "20", StopWhenExhausted: true, OperationID: "initial-operation"}
	b, err := s.UpdateManualBalance(ctx, input, "account", now)
	require.NoError(t, err)
	require.Equal(t, "20", b.BalanceUSD)
	settlement := routing.AttemptSettlement{Candidate: routing.DispatchCandidate{Provider: "codex", ChannelID: 1, GroupID: 2, AccountIDHash: "account"}, ID: "request/attempt-1", StartedAt: now.Add(time.Second), Usage: routing.AttemptUsage{Known: true, Complete: true}}
	require.NoError(t, s.SettleManualBalance(ctx, settlement, 3.2, true, now.Add(2*time.Second)))
	require.NoError(t, s.SettleManualBalance(ctx, settlement, 3.2, true, now.Add(2*time.Second)))
	b, err = s.ManualBalance(ctx, "codex", 1, 2)
	require.NoError(t, err)
	require.Equal(t, "16.8", b.BalanceUSD)
	require.Equal(t, "3.2", b.SpentUSD)
	input.Action, input.AmountUSD, input.Revision, input.OperationID = "credit", "10", b.Revision, "credit-operation"
	b, err = s.UpdateManualBalance(ctx, input, "account", now.Add(3*time.Second))
	require.NoError(t, err)
	require.Equal(t, "26.8", b.BalanceUSD)
	// Replaying the same UI operation must not add another credit.
	b, err = s.UpdateManualBalance(ctx, input, "account", now.Add(4*time.Second))
	require.NoError(t, err)
	require.Equal(t, "26.8", b.BalanceUSD)
	changedInput := input
	changedInput.AmountUSD = "30"
	_, err = s.UpdateManualBalance(ctx, changedInput, "account", now)
	require.ErrorContains(t, err, "operation_conflict")
	reopened := NewProfileStore(db)
	b, err = reopened.ManualBalance(ctx, "codex", 1, 2)
	require.NoError(t, err)
	require.Equal(t, "26.8", b.BalanceUSD)
	entries, err := s.ManualBalanceEntries(ctx, "codex", 1, 2)
	require.NoError(t, err)
	require.Len(t, entries, 3)
	require.Equal(t, "-3.2", entries[1].AmountUSD)
	other, err := s.ManualBalance(ctx, "claude", 1, 2)
	require.NoError(t, err)
	require.Nil(t, other)
}

func TestManualBalanceCalibrationBoundaryCredentialChangesAndMissingUsage(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	input := routing.ManualBalanceUpdate{Provider: "claude", ChannelID: 1, GroupID: 2, Action: "calibrate", AmountUSD: "1", StopWhenExhausted: true, OperationID: "initial-operation"}
	b, err := s.UpdateManualBalance(ctx, input, "old-account", now)
	require.NoError(t, err)
	settlement := routing.AttemptSettlement{Candidate: routing.DispatchCandidate{Provider: "claude", ChannelID: 1, GroupID: 2, AccountIDHash: "old-account"}, ID: "old-attempt", StartedAt: now.Add(-time.Second), Usage: routing.AttemptUsage{Known: true, Complete: true}}
	require.NoError(t, s.SettleManualBalance(ctx, settlement, 5, true, now))
	settlement.ID, settlement.StartedAt = "unpriced", now.Add(time.Second)
	require.NoError(t, s.SettleManualBalance(ctx, settlement, 0, false, now))
	b, err = s.ManualBalance(ctx, "claude", 1, 2)
	require.NoError(t, err)
	require.Equal(t, "1", b.BalanceUSD)
	require.EqualValues(t, 1, b.IncompleteCount)
	settlement.ID = "priced"
	require.NoError(t, s.SettleManualBalance(ctx, settlement, 2, true, now))
	b, err = s.ManualBalance(ctx, "claude", 1, 2)
	require.NoError(t, err)
	require.Equal(t, "-1", b.BalanceUSD)
	require.True(t, b.Exhausted())
	input.Action, input.Revision, input.OperationID = "disable", b.Revision, "disable-operation"
	b, err = s.UpdateManualBalance(ctx, input, "old-account", now)
	require.NoError(t, err)
	require.False(t, b.Exhausted())
	settlement.ID = "disabled-attempt"
	require.NoError(t, s.SettleManualBalance(ctx, settlement, 2, true, now))
	input.Action, input.AmountUSD, input.Revision, input.OperationID = "calibrate", "8", b.Revision, "new-credentials"
	b, err = s.UpdateManualBalance(ctx, input, "new-account", now.Add(2*time.Second))
	require.NoError(t, err)
	require.Zero(t, b.IncompleteCount)
	require.Equal(t, "0", b.SpentUSD)
	settlement.ID, settlement.StartedAt = "old-credential-response", now.Add(3*time.Second)
	require.NoError(t, s.SettleManualBalance(ctx, settlement, 2, true, now))
	b, err = s.ManualBalance(ctx, "claude", 1, 2)
	require.NoError(t, err)
	require.Equal(t, "8", b.BalanceUSD)
	_, err = s.UpdateManualBalance(ctx, input, "new-account", now)
	require.NoError(t, err)
	input.OperationID = "stale-operation"
	_, err = s.UpdateManualBalance(ctx, input, "new-account", now)
	require.ErrorContains(t, err, "revision_conflict")
}

func TestManualBalanceConcurrentDebitsAndTinyCosts(t *testing.T) {
	s, _ := newProfileTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	_, err := s.UpdateManualBalance(ctx, routing.ManualBalanceUpdate{Provider: "codex", ChannelID: 1, GroupID: 2, Action: "calibrate", AmountUSD: "0.0000001", OperationID: "initial-operation"}, "account", now)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.SettleManualBalance(ctx, routing.AttemptSettlement{Candidate: routing.DispatchCandidate{Provider: "codex", ChannelID: 1, GroupID: 2, AccountIDHash: "account"}, ID: time.Unix(int64(i), 0).String(), StartedAt: now.Add(time.Second), Usage: routing.AttemptUsage{Complete: true}}, 0.000000001, true, now)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	b, err := s.ManualBalance(ctx, "codex", 1, 2)
	require.NoError(t, err)
	require.Equal(t, "0.00000008", b.BalanceUSD)
	require.Equal(t, "0.00000002", b.SpentUSD)
}

func TestManualBalanceLedgerFailureRollsBackBalance(t *testing.T) {
	s, db := newProfileTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	input := routing.ManualBalanceUpdate{Provider: "codex", ChannelID: 1, GroupID: 2, Action: "calibrate", AmountUSD: "20", OperationID: "initial-operation"}
	b, err := s.UpdateManualBalance(ctx, input, "account", now)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TRIGGER reject_balance_entries BEFORE INSERT ON routing_balance_entries BEGIN SELECT RAISE(ABORT,'test write failure'); END`)
	require.NoError(t, err)
	input.Action, input.AmountUSD, input.Revision, input.OperationID = "credit", "10", b.Revision, "rejected-credit"
	_, err = s.UpdateManualBalance(ctx, input, "account", now)
	require.Error(t, err)
	err = s.SettleManualBalance(ctx, routing.AttemptSettlement{Candidate: routing.DispatchCandidate{Provider: "codex", ChannelID: 1, GroupID: 2, AccountIDHash: "account"}, ID: "rejected-debit", StartedAt: now.Add(time.Second), Usage: routing.AttemptUsage{Known: true, Complete: true}}, 3, true, now)
	require.Error(t, err)
	b, err = s.ManualBalance(ctx, "codex", 1, 2)
	require.NoError(t, err)
	require.Equal(t, "20", b.BalanceUSD)
	require.Equal(t, "0", b.SpentUSD)
	entries, err := s.ManualBalanceEntries(ctx, "codex", 1, 2)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}
