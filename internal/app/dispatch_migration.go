package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	claudeendpoint "github.com/wsyangzy/Vane/internal/providers/claude/proxy/endpoint"
	claudesub "github.com/wsyangzy/Vane/internal/providers/claude/proxy/subscription"
	codexsub "github.com/wsyangzy/Vane/internal/providers/codex/proxy/subscription"
	"github.com/wsyangzy/Vane/internal/routing"
	routingstore "github.com/wsyangzy/Vane/internal/routing/store"
)

type RoutingConfigurationCheck struct {
	Code    string `json:"code"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}
type RoutingConfigurationStatus struct {
	Ready      bool                        `json:"ready"`
	Checks     []RoutingConfigurationCheck `json:"checks"`
	Channels   int                         `json:"channels"`
	Accounts   int                         `json:"accounts"`
	Candidates int                         `json:"candidates"`
}

func (a *App) CheckRoutingConfiguration(provider string) (RoutingConfigurationStatus, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	provider, err := normalizeProviderParam(provider)
	if err != nil {
		return RoutingConfigurationStatus{}, err
	}
	state, err := a.ListRoutingProfiles(provider)
	if err != nil {
		return RoutingConfigurationStatus{}, err
	}
	status := RoutingConfigurationStatus{Checks: []RoutingConfigurationCheck{}, Channels: len(state.Channels), Accounts: len(state.Accounts), Candidates: len(state.Candidates)}
	check := func(code string, passed bool, message string) {
		status.Checks = append(status.Checks, RoutingConfigurationCheck{Code: code, Passed: passed, Message: message})
	}
	var defaultID int64
	var defaultProfile routing.RoutingProfile
	for _, profile := range state.Profiles {
		if profile.IsDefault && profile.Enabled {
			defaultID = profile.ID
			defaultProfile = profile
		}
	}
	check("default_profile", defaultID > 0, "手动建立并启用默认调度方案")
	a.poolMu.Lock()
	runtime := a.profileRuntimes[provider]
	a.poolMu.Unlock()
	if defaultID > 0 && runtime != nil {
		plan := runtime.Plan(ctx, defaultID, "", true, DispatchInput{}.turn(), true)
		check("candidate_preview", plan.Error == "" && len(plan.EligibleCandidates) > 0, "默认方案模拟至少包含一个可用候选")
	} else {
		check("candidate_preview", false, "请先完成默认调度方案")
	}
	check("resources", len(state.Candidates) > 0, "渠道、凭证分组及端点资源可解析")
	credentialsValid, protocolSupported := false, false
	memberIssues := []string{}
	if defaultID > 0 && runtime != nil {
		candidates := runtime.ExpandTargets(defaultProfile, []routing.RouteTarget{{ProfileID: defaultID}}, "")
		credentialsValid = true
		for _, member := range defaultProfile.Members {
			if !member.Enabled {
				continue
			}
			resolved, hasEndpoint := false, false
			for _, candidate := range candidates {
				if candidate.ChannelID != member.ChannelID || member.GroupID != 0 && candidate.GroupID != member.GroupID {
					continue
				}
				hasEndpoint = true
				if a.routingCredentialConfigured(ctx, candidate) {
					if candidate.Enabled {
						resolved = true
						protocolSupported = protocolSupported || provider == "claude" && candidate.Protocol == "messages" || provider == "codex" && candidate.Protocol == "responses"
					}
				}
			}
			if !resolved {
				channelName := fmt.Sprintf("渠道 %d", member.ChannelID)
				for _, channel := range state.Channels {
					if channel.ID == member.ChannelID {
						channelName = channel.Name
						break
					}
				}
				groupName, groupExists := "全部凭证分组", false
				if member.GroupID != 0 {
					groupName = fmt.Sprintf("分组 %d", member.GroupID)
				}
				for _, account := range state.Accounts {
					if account.ChannelID == member.ChannelID && (member.GroupID == 0 || account.GroupID == member.GroupID) {
						groupExists = true
						if member.GroupID != 0 {
							groupName = account.GroupName
						}
					}
				}
				reason := "凭证未配置或托管账号绑定无效，请检查分组凭证"
				switch {
				case !groupExists:
					reason = "凭证分组不存在，请检查渠道资源"
				case !hasEndpoint:
					reason = "没有端点，请添加端点或在方案中停用此成员"
				}
				memberIssues = append(memberIssues, channelName+" / "+groupName+"："+reason)
			}
			credentialsValid = credentialsValid && resolved
		}
	}
	memberMessage := "每个方案成员均有端点及有效凭证配置；参与关系由方案决定"
	if len(memberIssues) > 0 {
		memberMessage = strings.Join(memberIssues, "；")
	}
	check("credential_modes", credentialsValid, memberMessage)
	check("provider_protocol", protocolSupported, "至少一个有效目标支持当前 provider 协议")
	a.mu.RLock()
	authValid := a.config != nil && (!a.config.Auth.Enabled || validRoutingAuthToken(a.config.Auth.Token))
	a.mu.RUnlock()
	check("application_auth", authValid, "已启用应用鉴权时必须配置有效令牌")
	status.Ready = true
	for _, item := range status.Checks {
		status.Ready = status.Ready && item.Passed
	}

	return status, nil
}

func validRoutingAuthToken(token string) bool {
	return token != "" && strings.TrimSpace(token) == token && !strings.ContainsAny(token, "\r\n\x00\t ")
}

// This check reads configuration only. It never refreshes an OAuth credential,
// calls an upstream, reserves an account, or changes a key selection cursor.
func (a *App) routingCredentialConfigured(ctx context.Context, candidate routing.DispatchCandidate) bool {
	if candidate.CredentialMode == "managed" || candidate.CredentialMode == "client_passthrough" {
		ref, err := a.quotaCredentialRef(ctx, candidate.Provider, candidate.ChannelID, candidate.GroupID)
		if err != nil {
			return false
		}
		if candidate.CredentialMode == "managed" && (ref.AccountID == "" || a.subscriptionAccounts == nil || a.subscriptionAccounts.Validate(ctx, candidate.Provider, ref.AccountID) != nil) {
			return false
		}
		if candidate.Provider == "claude" {
			if a.claudeSubscriptions == nil {
				return false
			}
			group, err := a.claudeCredentialGroupStore.GetByID(ctx, candidate.GroupID)
			if err != nil || group == nil || group.Subscription == nil || !group.Subscription.Enabled {
				return false
			}
		} else {
			if a.codexSubscriptions == nil {
				return false
			}
			group, err := a.codexCredentialGroupStore.GetByID(ctx, candidate.GroupID)
			if err != nil || group == nil || group.Subscription == nil || !group.Subscription.Enabled {
				return false
			}
		}
	}
	if candidate.Provider == "claude" && a.endpointManager != nil {
		for _, ep := range a.endpointManager.GetAllEndpoints() {
			if claudeendpoint.EndpointKeyForConfig(ep.Config) != candidate.EndpointID || ep.Config.CredentialGroupID != candidate.GroupID {
				continue
			}
			if candidate.CredentialMode == "managed" || candidate.CredentialMode == "client_passthrough" {
				return claudesub.ValidateEndpoint(ep.Config.URL, ep.Config.Headers) == nil
			}
			return candidate.CredentialMode == "api_key" && (a.endpointManager.GetTokenForEndpoint(ep) != "" || a.endpointManager.GetApiKeyForEndpoint(ep) != "")
		}
	}
	if candidate.Provider == "codex" && a.codexEndpointMgr != nil {
		for _, ep := range a.codexEndpointMgr.PoolEndpoints(candidate.ChannelID, candidate.GroupID) {
			if fmt.Sprint(ep.ID) != candidate.EndpointID {
				continue
			}
			if candidate.CredentialMode == "managed" || candidate.CredentialMode == "client_passthrough" {
				return codexsub.ValidateEndpoint(ep.BaseURL) == nil
			}
			return candidate.CredentialMode == "api_key" && validRoutingAuthToken(ep.APIKey)
		}
	}
	return false
}

// VACUUM INTO uses SQLite's snapshot machinery and includes committed WAL
// pages. Existing migration backups are never overwritten.
func (a *App) backupRoutingDatabase(ctx context.Context, provider string) (string, error) {
	a.mu.RLock()
	db := a.storeDB
	a.mu.RUnlock()
	if db == nil {
		return "", errors.New("routing_database_unavailable")
	}
	rows, err := db.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return "", err
	}
	var path string
	for rows.Next() {
		var seq int
		var name, file string
		if err = rows.Scan(&seq, &name, &file); err != nil {
			rows.Close()
			return "", err
		}
		if name == "main" {
			path = file
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", errors.New("routing_backup_requires_file_database")
	}
	backup := fmt.Sprintf("%s.before-unified-%s-%d.bak", path, provider, time.Now().UnixNano())
	if _, err = os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("routing_backup_path_not_fresh")
	}
	if _, err = db.ExecContext(ctx, `VACUUM INTO '`+strings.ReplaceAll(backup, "'", "''")+`'`); err != nil {
		return "", err
	}
	// Open the backup separately: checking the source alone would not prove the
	// snapshot is readable. The retained file is also the rollback artifact.
	snapshot, err := sql.Open("sqlite", backup+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer snapshot.Close()
	var integrity string
	if err = snapshot.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return "", err
	}
	if integrity != "ok" {
		return "", errors.New("routing_backup_integrity_failed")
	}
	return backup, nil
}

// The first refresh snapshots the database before retiring old entrypoints.
// The marker records the upgrade, never a choice of runtime.
func (a *App) prepareUnifiedRouting(ctx context.Context, provider string) error {
	s, err := a.profileStore(ctx)
	if err != nil {
		return err
	}
	record, err := s.Migration(ctx, provider)
	if err == nil && record.State == "unified_only" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	backup, err := a.backupRoutingDatabase(ctx, provider)
	if err != nil {
		return err
	}
	return s.SaveMigration(ctx, provider, routingstore.MigrationRecord{State: "unified_only", BackupPath: backup, UpdatedAt: time.Now()})
}
