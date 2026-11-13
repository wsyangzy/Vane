package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/platform/appdir"
	settingsservice "github.com/wsyangzy/Vane/internal/settings/service"
)

type DataStoragePaths struct {
	DatabasePath       string `json:"database_path"`
	ArchiveDirectory   string `json:"archive_directory"`
	DefaultArchiveRoot string `json:"default_archive_root"`
	Environment        string `json:"environment"`
}

func historyEnvironment() string {
	if strings.EqualFold(buildinfo.BuildMode, "release") {
		return "release"
	}
	return "dev"
}

func defaultArchiveRoot() string { return filepath.Join(appdir.GetDataDir(), "archive") }

func (a *App) GetDataStoragePaths() DataStoragePaths {
	paths := DataStoragePaths{DatabasePath: a.getEffectiveUsageDBPath(), DefaultArchiveRoot: defaultArchiveRoot(), Environment: historyEnvironment()}
	if a.usageTracker != nil {
		paths.ArchiveDirectory = a.usageTracker.ArchiveDirectory()
	}
	return paths
}

func (a *App) ChooseHistoryArchiveDirectory() (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("目录选择需要在桌面应用中使用")
	}
	path, err := a.wailsApp.Dialog.OpenFile().CanChooseFiles(false).CanChooseDirectories(true).SetTitle("选择历史归档根目录").PromptForSingleSelection()
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if cause.Error() == "cancelled by user" {
			return "", nil
		}
	}
	return path, err
}

// The tracker reconciles archives during construction, before the settings
// service exists. Read this one persisted location first so startup cannot
// accidentally archive new history into the previous default directory.
func savedArchiveRoot(ctx context.Context, dbPath string) (string, error) {
	root := defaultArchiveRoot()
	if dbPath == ":memory:" {
		return root, nil
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return root, nil
	} else if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(dbPath)
	if err != nil {
		return "", err
	}
	// SQLite requires file:///C:/... on Windows; file://C:/... is parsed as
	// an authority and fails before any query is executed.
	uriPath := filepath.ToSlash(absolute)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return "", err
	}
	defer db.Close()
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='settings'`).Scan(&exists); err != nil {
		return "", err
	}
	if exists == 0 {
		return root, nil
	}
	var configured string
	err = db.QueryRowContext(ctx, `SELECT value FROM settings WHERE category='retention' AND key='archive_directory' LIMIT 1`).Scan(&configured)
	if errors.Is(err, sql.ErrNoRows) {
		return root, nil
	}
	if err != nil {
		return "", err
	}
	if configured != "" {
		if !filepath.IsAbs(configured) {
			return "", fmt.Errorf("保存的历史归档路径不是绝对路径")
		}
		root = configured
	}
	return root, nil
}

func (a *App) applyHistoryArchiveDirectory(ctx context.Context) error {
	if a.usageTracker == nil {
		return nil
	}
	root := a.getSettingString(ctx, settingsservice.CategoryRetention, "archive_directory", "")
	if root == "" {
		root = defaultArchiveRoot()
	}
	return a.usageTracker.SwitchArchiveDirectory(ctx, filepath.Join(root, historyEnvironment()))
}
