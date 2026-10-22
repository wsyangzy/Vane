package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/transport"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
	codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type SubscriptionAccountCommand struct {
	LoginMethod    string `json:"login_method"`
	Action         string `json:"action"`
	Provider       string `json:"provider"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	CredentialJSON string `json:"credential_json"`
	AccountEmail   string `json:"account_email"`
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
	proxyURL, proxyLabel, closeProxy, err := transport.LoginProxy(a.config)
	if err != nil {
		a.logger.Error("订阅登录代理初始化失败")
		return
	}
	m.ConfigureLoginProxy(proxyURL, proxyLabel, closeProxy)
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
		if len(raw) > 1<<20 {
			return result, errors.New("凭证不能超过 1 MiB")
		}
		if input.Provider == "claude" {
			prepared, e := claudeservice.PrepareSubscriptionImport(raw, input.AccountEmail)
			if e != nil {
				return result, e
			}
			raw = prepared
			defer clear(prepared)
		}
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
		}
		if err == nil {
			login, err = m.LoginStatus(input.ID)
			result.Login = &login
		}
	default:
		err = errors.New("未知的订阅账号操作")
	}
	if err == nil && result.Login != nil && result.Login.Account != nil {
		result.Account = result.Login.Account
	}
	if err == nil && result.Account != nil && (input.Action == "import" || input.Action == "refresh" || input.Action == "login_status" || input.Action == "login_complete") {
		a.poolMu.Lock()
		runtime := a.poolRuntimes[input.Provider]
		a.poolMu.Unlock()
		if runtime != nil {
			err = runtime.ClearRepair("account/" + result.Account.ID)
		}
	}
	return result, err
}
