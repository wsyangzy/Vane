package app

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/wsyangzy/Vane/internal/platform/clientconfig"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"github.com/wsyangzy/Vane/internal/routing"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

func (a *App) managePoolConnection(input SubscriptionConnectionCommand, result SubscriptionConnectionResult, current clientconfig.Snapshot) (SubscriptionConnectionResult, error) {
	ctx, cancel := routeCtx()
	defer cancel()
	runtime, err := a.refreshPoolRuntime(ctx, input.Provider)
	if err != nil {
		return result, err
	}
	var pool *routing.Pool
	for _, p := range runtime.Config().Pools {
		if p.ID == input.PoolID {
			copy := p
			pool = &copy
			break
		}
	}
	if pool == nil {
		return result, errors.New("账号池不存在")
	}
	result.Managed = true
	result.OfficialEnabled = true
	result.BindingVersion = pool.Revision
	for _, member := range pool.Members {
		account, ok := runtime.Account(member.ChannelID, member.GroupID)
		if !ok || !account.Enabled {
			continue
		}
		if input.Provider == "claude" {
			eps, e := a.endpointStore.ListByCredentialGroupID(ctx, member.GroupID)
			if e != nil {
				return result, e
			}
			for _, ep := range eps {
				result.EndpointReady = result.EndpointReady || ep.FailoverEnabled
			}
		} else {
			eps, e := a.codexEndpointStore.ListByChannelID(ctx, member.ChannelID)
			if e != nil {
				return result, e
			}
			for _, ep := range eps {
				if ep.CredentialGroupID == member.GroupID {
					result.EndpointReady = result.EndpointReady || ep.FailoverEnabled
				}
			}
		}
	}
	if input.Action == "status" {
		return result, nil
	}
	if input.Action == "prepare_endpoint" {
		return result, errors.New("请在渠道的账号详情中准备端点")
	}
	if !result.EndpointReady {
		return result, errors.New("账号池没有启用且配置了端点的账号")
	}
	key := strings.TrimSpace(input.LocalKey)
	hash := sha256.Sum256([]byte(key))
	stored, err := hex.DecodeString(pool.KeyHash)
	if err != nil || len(stored) != 32 || subtle.ConstantTimeCompare(stored, hash[:]) != 1 {
		return result, errors.New("密钥与所选账号池不匹配，请核对或重新生成")
	}
	var updated, snippet []byte
	if input.Provider == "claude" {
		updated, snippet, result.Command, err = claudeservice.RenderSubscriptionClient(current.Data, result.BaseURL, key, true)
	} else {
		updated, snippet, result.Command, err = codexservice.RenderSubscriptionClient(current.Data, result.BaseURL, key, true)
	}
	if err != nil {
		return result, err
	}
	defer clear(updated)
	defer clear(snippet)
	result.Snippet = string(snippet)
	result.ContentRevision = (clientconfig.Snapshot{Data: updated, Exists: true}).Revision()
	if input.Action == "apply" {
		if input.ContentRevision == "" || input.ContentRevision != result.ContentRevision || input.BindingVersion != result.BindingVersion {
			return SubscriptionConnectionResult{}, errors.New("池配置或接入内容已变化，请重新预览")
		}
		if !result.Running {
			return result, errors.New("代理尚未运行，请启动代理后重新预览")
		}
		if err = clientconfig.Apply(result.ConfigPath, input.Revision, updated, accounts.UserVault{}); err != nil {
			return result, err
		}
		result.BackupAvailable = true
		result.Revision = (clientconfig.Snapshot{Data: updated, Exists: true}).Revision()
	}
	return result, nil
}
