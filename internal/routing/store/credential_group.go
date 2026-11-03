package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/wsyangzy/Vane/internal/routing"
)

// CheckCredentialGroupReferences runs inside the resource deletion transaction.
// Selected members in any profile and explicit targets retain their resources.
func CheckCredentialGroupReferences(ctx context.Context, tx *sql.Tx, provider string, channelID, groupID int64) error {
	profiles, err := loadCredentialGroupProfiles(ctx, tx)
	if err != nil {
		return err
	}
	matches := func(target routing.RouteTarget) bool {
		return target.ProfileID == 0 && (target.GroupID == groupID || (target.GroupID == 0 && channelID > 0 && target.ChannelID == channelID))
	}
	var references []string
	for _, profile := range profiles {
		referenced := false
		if profile.Provider == provider {
			for _, member := range profile.Members {
				referenced = referenced || (member.Enabled && matches(routing.RouteTarget{ChannelID: member.ChannelID, GroupID: member.GroupID}))
			}
			for _, rule := range profile.Rules {
				referenced = referenced || slices.ContainsFunc(rule.Targets, matches)
			}
			for _, intent := range profile.Intents {
				referenced = referenced || slices.ContainsFunc(intent.Targets, matches)
			}
		}
		if classifier := profile.Classifier; classifier != nil {
			classifierProvider := classifier.Provider
			if classifierProvider == "" {
				classifierProvider = profile.Provider
			}
			referenced = referenced || (classifierProvider == provider && classifier.GroupID == groupID)
		}
		if referenced {
			references = append(references, fmt.Sprintf("%s「%s」", profile.Provider, profile.Name))
		}
	}
	if len(references) > 0 {
		return fmt.Errorf("凭证分组仍被调度方案引用，不能删除：%s。请先在方案中移除该分组或更换目标", strings.Join(references, "、"))
	}
	return nil
}

// RemoveCredentialGroupReferences clears saved deselections after the reference
// check has ruled out selected members, rule targets and classifiers.
func RemoveCredentialGroupReferences(ctx context.Context, tx *sql.Tx, provider string, groupID int64) error {
	_, err := pruneCredentialGroupReferences(ctx, tx, provider, func(_, id int64) bool { return id == groupID && id > 0 })
	return err
}

// PruneMissingCredentialGroupReferences repairs references left by older builds.
// The caller supplies the complete provider catalog, including disabled groups.
func (s *ProfileStore) PruneMissingCredentialGroupReferences(ctx context.Context, provider string, catalog routing.Catalog) error {
	groups := map[[2]int64]bool{}
	for _, channel := range catalog {
		for _, group := range channel.Groups {
			groups[[2]int64{channel.ID, group.ID}] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := pruneCredentialGroupReferences(ctx, tx, provider, func(channelID, groupID int64) bool {
		return groupID > 0 && !groups[[2]int64{channelID, groupID}]
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func loadCredentialGroupProfiles(ctx context.Context, tx *sql.Tx) ([]*routing.RoutingProfile, error) {
	rows, err := tx.QueryContext(ctx, `SELECT `+profileColumns+` FROM routing_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var profiles []*routing.RoutingProfile
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if err := loadProfileChildren(ctx, tx, profile); err != nil {
			return nil, err
		}
	}
	return profiles, nil
}

func pruneCredentialGroupReferences(ctx context.Context, tx *sql.Tx, provider string, removed func(int64, int64) bool) (bool, error) {
	if !validProfileProvider(provider) {
		return false, sql.ErrNoRows
	}
	profiles, err := loadCredentialGroupProfiles(ctx, tx)
	if err != nil {
		return false, err
	}
	changed := false
	for _, profile := range profiles {
		if !pruneProfileCredentialGroups(profile, provider, removed) {
			continue
		}
		profile.Revision++
		if _, err := tx.ExecContext(ctx, `UPDATE routing_profiles SET revision=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, profile.Revision, profile.ID); err != nil {
			return false, err
		}
		if err := writeProfileChildren(ctx, tx, profile); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM routing_affinity WHERE profile_id=?`, profile.ID); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}

func pruneProfileCredentialGroups(profile *routing.RoutingProfile, provider string, removed func(int64, int64) bool) bool {
	changed := false
	pruneTargets := func(targets []routing.RouteTarget) []routing.RouteTarget {
		before := len(targets)
		targets = slices.DeleteFunc(targets, func(target routing.RouteTarget) bool {
			return target.ProfileID == 0 && removed(target.ChannelID, target.GroupID)
		})
		changed = changed || len(targets) != before
		return targets
	}
	if profile.Provider == provider {
		before := len(profile.Members)
		profile.Members = slices.DeleteFunc(profile.Members, func(member routing.RoutingMember) bool {
			return removed(member.ChannelID, member.GroupID)
		})
		changed = len(profile.Members) != before
		for i := range profile.Members {
			profile.Members[i].Position = i
		}
		for i := range profile.Rules {
			rule := &profile.Rules[i]
			before := len(rule.Targets)
			rule.Targets = pruneTargets(rule.Targets)
			if before > 0 && len(rule.Targets) == 0 && rule.Kind == "mapping" {
				rule.Enabled = false
			}
		}
		for i := range profile.Intents {
			profile.Intents[i].Targets = pruneTargets(profile.Intents[i].Targets)
		}
	}
	if classifier := profile.Classifier; classifier != nil {
		classifierProvider := classifier.Provider
		if classifierProvider == "" {
			classifierProvider = profile.Provider
		}
		if classifierProvider == provider && removed(classifier.ChannelID, classifier.GroupID) {
			classifier.Enabled = false
			classifier.ChannelID, classifier.GroupID = 0, 0
			changed = true
		}
	}
	return changed
}
