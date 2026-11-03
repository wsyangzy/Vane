package app

import (
	"context"
	"database/sql"
	"errors"

	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

// Callers hold routingWriteMu to serialize deletion with profile validation/save.
func (a *App) deleteCredentialGroup(ctx context.Context, provider string, groupID int64, deleteGroup func(*sql.Tx) error) error {
	if _, err := a.profileStore(ctx); err != nil {
		return err
	}
	a.mu.RLock()
	db := a.storeDB
	if db == nil && a.usageTracker != nil {
		db = a.usageTracker.GetDB()
	}
	a.mu.RUnlock()
	if db == nil {
		return errors.New("routing_database_unavailable")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	channelID, err := a.credentialGroupChannelID(ctx, tx, provider, groupID)
	if err != nil {
		return err
	}
	if err := routingstore.CheckCredentialGroupReferences(ctx, tx, provider, channelID, groupID); err != nil {
		return err
	}
	if err := deleteGroup(tx); err != nil {
		return err
	}
	if err := routingstore.RemoveCredentialGroupReferences(ctx, tx, provider, groupID); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) credentialGroupChannelID(ctx context.Context, tx *sql.Tx, provider string, groupID int64) (int64, error) {
	if provider == "codex" {
		group, err := a.codexCredentialGroupStore.WithTx(tx).GetByID(ctx, groupID)
		if err != nil {
			return 0, err
		}
		if group == nil {
			return 0, errors.New("Codex 凭证分组不存在")
		}
		return group.ChannelID, nil
	}
	group, err := a.claudeCredentialGroupStore.WithTx(tx).GetByID(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if group == nil {
		return 0, errors.New("Claude 凭证分组不存在")
	}
	channel, err := a.channelStore.WithTx(tx).Get(ctx, group.Channel)
	if err != nil || channel == nil {
		return 0, err
	}
	return channel.ID, nil
}

func (a *App) refreshRoutingAfterCredentialGroupDeletion(ctx context.Context) error {
	for _, provider := range []string{"claude", "codex"} {
		a.poolMu.Lock()
		loaded := a.profileRuntimes[provider] != nil
		a.poolMu.Unlock()
		if !loaded {
			continue
		}
		if _, err := a.refreshPoolRuntime(ctx, provider); err != nil {
			return err
		}
		a.publishPoolRuntime(provider)
	}
	return nil
}
