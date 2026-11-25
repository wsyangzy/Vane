package app

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/wsyangzy/Vane/internal/platform/appdir"
	"github.com/wsyangzy/Vane/internal/platform/clientconfig"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
	"github.com/wsyangzy/Vane/internal/subscriptions/accounts"
)

type ClientConfigBackupCommand struct {
	Action   string `json:"action"`
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Revision string `json:"revision"`
	Name     string `json:"name"`
}

type ClientConfigBackupRecord struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Directory  string `json:"directory"`
	CreatedAt  string `json:"created_at"`
	Provider   string `json:"provider"`
	Operation  string `json:"operation"`
	ConfigPath string `json:"config_path"`
	Exists     bool   `json:"exists"`
	Size       int    `json:"size"`
}

type ClientConfigBackupResult struct {
	Directory        string                     `json:"directory"`
	DefaultDirectory string                     `json:"default_directory"`
	Revision         string                     `json:"revision"`
	ConfigPath       string                     `json:"config_path"`
	Records          []ClientConfigBackupRecord `json:"records"`
}

func (a *App) clientConfigHistory() (clientconfig.History, error) {
	directory := ""
	a.mu.RLock()
	svc := a.settingsService
	a.mu.RUnlock()
	if svc != nil {
		value, err := svc.GetValue(context.Background(), settingsservice.CategoryBackup, "directory")
		if err != nil {
			return clientconfig.History{}, errors.New("读取配置备份目录设置失败")
		}
		directory = value
	}
	if directory == "" {
		directory = filepath.Join(appdir.Dir(), "backup")
	}
	return clientconfig.History{Directory: directory, Vault: accounts.UserVault{}}, nil
}

// Only known client paths can be restored; the UI cannot supply a filesystem
// destination or retrieve raw configuration contents through this API.
func (a *App) ManageClientConfigBackups(input ClientConfigBackupCommand) (ClientConfigBackupResult, error) {
	subscriptionConnectionMu.Lock()
	defer subscriptionConnectionMu.Unlock()
	history, err := a.clientConfigHistory()
	if err != nil {
		return ClientConfigBackupResult{}, err
	}
	result := ClientConfigBackupResult{Directory: history.Directory, DefaultDirectory: filepath.Join(appdir.Dir(), "backup"), Records: []ClientConfigBackupRecord{}}
	if input.Action == "choose_directory" {
		result.Directory = ""
		if a.wailsApp == nil {
			return result, errors.New("目录选择需要在桌面应用中使用，也可直接填写路径")
		}
		selected, err := a.wailsApp.Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).SetTitle("选择客户端配置备份目录").PromptForSingleSelection()
		for cause := err; cause != nil; cause = errors.Unwrap(cause) {
			if cause.Error() == "cancelled by user" {
				return result, nil
			}
		}
		if selected != "" {
			result.Directory = selected
		}
		return result, err
	}
	if input.Action == "open_directory" && input.ID == "" {
		return result, a.openDirectory(history.Directory, "配置备份目录")
	}
	if input.Action != "list" && input.Action != "restore" && input.Action != "rename" && input.Action != "open_directory" {
		return result, errors.New("未知的配置备份操作")
	}
	if input.Provider != "claude" && input.Provider != "codex" {
		return result, errors.New("请选择 Claude 或 Codex")
	}
	path, err := subscriptionConfigPath(input.Provider)
	if err != nil {
		return result, err
	}
	result.ConfigPath = path
	if input.Action == "rename" {
		if err := history.Rename(input.Provider, path, input.ID, input.Name); err != nil {
			return result, err
		}
	}
	if input.Action == "restore" {
		if err := history.Restore(input.Provider, path, input.ID, input.Revision); err != nil {
			return result, err
		}
	}
	current, err := clientconfig.Read(path)
	if err != nil {
		return result, err
	}
	result.Revision = current.Revision()
	clear(current.Data)
	records, err := history.List(input.Provider, path)
	if err != nil {
		return result, err
	}
	for _, record := range records {
		result.Records = append(result.Records, ClientConfigBackupRecord{ID: record.ID, Name: record.Name, Directory: record.Directory, CreatedAt: record.CreatedAt.Format(time.RFC3339Nano), Provider: record.Provider, Operation: record.Operation, ConfigPath: record.ConfigPath, Exists: record.Exists, Size: record.Size})
		if input.Action == "open_directory" && record.ID == input.ID {
			return result, a.openDirectory(record.Directory, "备份记录目录")
		}
	}
	if input.Action == "open_directory" {
		return result, errors.New("找不到当前客户端的备份记录")
	}
	return result, nil
}
