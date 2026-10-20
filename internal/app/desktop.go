package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/wsyangzy/Vane/internal/buildinfo"
	"github.com/wsyangzy/Vane/internal/platform/appdir"
	codexconfig "github.com/wsyangzy/Vane/internal/providers/codex/clientconfig"
)

func (a *App) ShowMainWindow() {
	if a.window == nil {
		return
	}
	a.window.Show()
	a.window.Focus()
}

func (a *App) HideMainWindow() {
	if a.window == nil {
		return
	}
	a.window.Hide()
}

func (a *App) RequestQuit() {
	a.markQuitting()
	a.stopDevWatcherAfterQuit()

	// 在 goroutine 中执行退出，避免阻塞 UI
	go func() {
		// 调用 Wails Quit
		if a.wailsApp != nil {
			a.wailsApp.Quit()
		}

		// 等待 500ms 让 Wails 完成清理
		time.Sleep(500 * time.Millisecond)

		// 保底退出
		os.Exit(0)
	}()
}

const devWatcherPIDFile = ".wails-dev-watcher.pid"

func parseDevWatcherPID(data []byte) (int, error) {
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil || pid <= 0 {
		return 0, fmt.Errorf("无效的 Wails dev watcher PID")
	}
	return pid, nil
}

func devWatcherStopScript(pid int) string {
	return fmt.Sprintf(`
Start-Sleep -Milliseconds 1000
$process = Get-CimInstance Win32_Process -Filter "ProcessId = %d" -ErrorAction SilentlyContinue
if ($null -eq $process) { exit 0 }
$commandLine = [string]$process.CommandLine
if ($process.Name -ne 'wails3.exe' -or $commandLine -notmatch '(^|\s)dev(\s|$)') { exit 0 }
& taskkill.exe /T /F /PID %d *> $null
`, pid, pid)
}

func (a *App) stopDevWatcherAfterQuit() {
	if buildinfo.BuildMode != "dev" || goruntime.GOOS != "windows" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		return
	}
	pidFile := filepath.Join(filepath.Dir(executable), devWatcherPIDFile)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := parseDevWatcherPID(data)
	if err != nil {
		return
	}
	_ = os.Remove(pidFile)

	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-WindowStyle", "Hidden",
		"-Command", devWatcherStopScript(pid),
	)
	if err := cmd.Start(); err != nil && a.logger != nil {
		a.logger.Warn("无法通知 Wails dev watcher 退出", "error", err)
	}
}

func (a *App) openDirectory(path, label string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("创建%s失败: %w", label, err)
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("当前操作系统不支持打开目录: %s", goruntime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开%s失败: %w", label, err)
	}
	return nil
}

func (a *App) OpenLogDir() error {
	return a.openDirectory(appdir.GetLogDir(), "日志目录")
}

func (a *App) OpenDataDir() error {
	return a.openDirectory(appdir.GetDataDir(), "数据目录")
}

func (a *App) OpenClaudeConfigDir() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户主目录失败: %w", err)
	}
	return a.openDirectory(filepath.Join(homeDir, ".claude"), "Claude 配置目录")
}

func (a *App) OpenCodexConfigDir() error {
	codexHome, err := codexconfig.GetCodexHomeDir()
	if err != nil {
		return fmt.Errorf("获取 Codex 配置目录失败: %w", err)
	}
	return a.openDirectory(codexHome, "Codex 配置目录")
}

func (a *App) GetProxyAddress() string {
	host := "127.0.0.1"
	port := 8087 // 默认端口

	if a.config != nil && a.config.Server.Host != "" && a.config.Server.Host != "localhost" {
		host = a.config.Server.Host
	}
	if a.portManager != nil {
		port = a.portManager.GetActualPort()
	} else if a.config != nil {
		port = a.config.Server.Port
	}

	return fmt.Sprintf("http://%s:%d", host, port)
}
