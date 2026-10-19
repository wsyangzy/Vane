package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	claudeservice "ai-relay-forwarder/internal/providers/claude/service"
	codexservice "ai-relay-forwarder/internal/providers/codex/service"
	"ai-relay-forwarder/internal/subscriptions/accounts"
	"ai-relay-forwarder/internal/transport"
)

type SubscriptionAccountCommand struct {
	LoginMethod    string `json:"login_method"`
	Action         string `json:"action"`
	Provider       string `json:"provider"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	CredentialJSON string `json:"credential_json"`
	Code           string `json:"code"`
}
type SubscriptionAccountResult struct {
	Accounts []accounts.Info     `json:"accounts"`
	Account  *accounts.Info      `json:"account,omitempty"`
	Login    *accounts.LoginInfo `json:"login,omitempty"`
}

func (a *App) setupSubscriptionAccounts(db *sql.DB) {
	tr, err := transport.NewProvider(a.config).Default()
	if err != nil {
		a.logger.Error("订阅账号网络初始化失败")
		return
	}
	client := &http.Client{Transport: tr, Timeout: 25 * time.Second}
	m, err := accounts.New(db, accounts.UserVault{}, map[string]accounts.Adapter{"claude": claudeservice.SubscriptionAccountAdapter{Client: client}, "codex": codexservice.SubscriptionAccountAdapter{Client: client}})
	if err != nil {
		a.logger.Error("订阅账号存储初始化失败")
		return
	}
	a.subscriptionAccounts = m
}

// ManageSubscriptionAccounts returns metadata only. CredentialJSON and Code
// are local explicit inputs; neither is retained in an output/event or logged.
func (a *App) ManageSubscriptionAccounts(input SubscriptionAccountCommand) (SubscriptionAccountResult, error) {
	a.mu.RLock()
	m := a.subscriptionAccounts
	a.mu.RUnlock()
	var result SubscriptionAccountResult
	if m == nil {
		return result, errors.New("订阅账号服务尚未就绪")
	}
	if input.Provider != "claude" && input.Provider != "codex" {
		return result, errors.New("仅支持 Claude 和 Codex 订阅账号")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	var err error
	switch input.Action {
	case "list":
		result.Accounts, err = m.List(ctx, input.Provider)
		result.Login = m.PendingLogin(input.Provider)
	case "import":
		var info accounts.Info
		raw := []byte(input.CredentialJSON)
		defer clear(raw)
		info, err = m.Import(ctx, input.Provider, input.Name, raw)
		if err == nil {
			result.Account = &info
		}
	case "update":
		var info accounts.Info
		info, err = m.Update(ctx, input.Provider, input.ID, input.Name, input.Enabled)
		if err == nil {
			result.Account = &info
		}
	case "delete":
		err = m.Delete(ctx, input.Provider, input.ID)
	case "refresh":
		var info accounts.Info
		info, err = m.Refresh(ctx, input.Provider, input.ID)
		if err == nil {
			result.Account = &info
		}
	case "login_start":
		var login accounts.LoginInfo
		login, err = m.BeginLoginWithMethod(input.Provider, input.Name, input.LoginMethod)
		if err == nil {
			result.Login = &login
		}
	case "login_status", "login_complete", "login_cancel":
		var login accounts.LoginInfo
		login, err = m.LoginStatus(input.ID)
		if err != nil {
			break
		}
		if login.Provider != input.Provider {
			return result, errors.New("subscription_provider_mismatch")
		}
		if input.Action == "login_complete" {
			err = m.CompleteLogin(input.ID, input.Code)
		} else if input.Action == "login_cancel" {
			err = m.CancelLogin(input.ID)
		} else {
			result.Login = &login
		}
	default:
		err = errors.New("未知的订阅账号操作")
	}
	return result, err
}
