//go:build desktop

// main.go - AI Relay Forwarder Wails v3 应用入口
// 使用 Wails v3 原生系统托盘支持
//
// 注意：全局变量（Version, assets, setupLogger 等）定义在 globals.go 中
// 以便 IDE 在无 desktop build tag 时也能正确解析

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"ai-relay-forwarder/internal/utils"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// 命令行参数
var (
	configPath  = flag.String("config", "config/config.yaml", "配置文件路径")
	showVersion = flag.Bool("version", false, "显示版本信息")
)

// 运行时变量
var startTime = time.Now()

const (
	mainWindowDefaultWidth  = 1440
	mainWindowDefaultHeight = 900
	mainWindowMinWidth      = 1200
	mainWindowMinHeight     = 620
	mainWindowWorkAreaInset = 48
)

func main() {
	flag.Parse()

	// 数据目录迁移（v7.3 品牌升级）
	migrated, migrateErr := utils.MigrateLegacyAppDir()
	utils.LogMigrationResult(migrated, migrateErr)

	// 处理版本标志
	if *showVersion {
		fmt.Printf("AI Relay Forwarder\n")
		fmt.Printf("Version: %s\n", Version)
		fmt.Printf("Commit: %s\n", Commit)
		fmt.Printf("Built: %s\n", BuildTime)
		os.Exit(0)
	}

	// 创建应用实例
	appInstance := NewApp()
	appInstance.configPath = *configPath

	// 单实例锁标识：dev 和 release 使用不同的 UniqueID，允许两个版本同时运行
	singleInstanceID := "ai-relay-forwarder-desktop"
	if BuildMode == "dev" {
		singleInstanceID = "ai-relay-forwarder-desktop-dev"
	}

	// 创建 Wails v3 应用
	wailsApp := application.New(application.Options{
		Name:        "AI Relay Forwarder",
		Description: "AI 多模型中继转发服务",
		Icon:        icon, // 设置应用图标
		Services: []application.Service{
			application.NewService(appInstance),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		// 单实例锁
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: singleInstanceID,
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				// 当第二个实例启动时，显示主窗口
				if appInstance != nil && appInstance.window != nil {
					appInstance.window.Show()
					appInstance.window.Focus()
				}
			},
		},
		ShouldQuit: func() bool {
			if appInstance != nil {
				appInstance.markQuitting()
			}
			return true
		},
	})

	// 保存 wailsApp 引用到 App 实例
	appInstance.wailsApp = wailsApp

	// 应用进入退出流程时，先标记 quitting，避免 WindowClosing 钩子误拦截关闭
	wailsApp.OnShutdown(func() {
		if appInstance != nil {
			appInstance.markQuitting()
			appInstance.shutdown()
		}
	})

	// 窗口标题：dev 模式添加标识
	windowTitle := "AI Relay Forwarder"
	if BuildMode == "dev" {
		windowTitle = "AI Relay Forwarder (Dev)"
	}

	// 创建主窗口
	window := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            windowTitle,
		Width:            mainWindowDefaultWidth,
		Height:           mainWindowDefaultHeight,
		MinWidth:         mainWindowMinWidth,
		MinHeight:        mainWindowMinHeight,
		BackgroundColour: application.RGBA{Red: 26, Green: 26, Blue: 46, Alpha: 255},
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		Windows: application.WindowsWindow{
			DisableIcon: false,
		},
		URL: "/",
		// 启用开发者工具（用于调试前端问题）
		DevToolsEnabled: true,
	})

	// 保存窗口引用
	appInstance.window = window
	var fitMainWindowOnce sync.Once

	// 注册窗口关闭钩子：隐藏到托盘而非真正关闭
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		// 如果不是显式退出请求，隐藏窗口而非关闭
		if !appInstance.isQuitting() {
			window.Hide()
			e.Cancel() // 阻止窗口关闭
		}
	})

	// 注册窗口显示钩子（相当于 domReady）
	window.RegisterHook(events.Common.WindowShow, func(e *application.WindowEvent) {
		fitMainWindowOnce.Do(func() {
			fitMainWindowToWorkArea(wailsApp, window)
		})
		// 首次显示时执行初始化
		appInstance.onWindowReady()
	})

	// 创建系统托盘
	setupSystemTray(wailsApp, window, appInstance)

	// 启动应用初始化
	appInstance.startup()

	// 运行应用
	err := wailsApp.Run()
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "graceful exit requested") {
			return
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func fitMainWindowToWorkArea(app *application.App, window *application.WebviewWindow) {
	if app == nil || window == nil || window.IsMaximised() || window.IsFullscreen() {
		return
	}

	screen, err := window.GetScreen()
	if err != nil || screen == nil {
		screen = app.Screen.GetPrimary()
	}
	if screen == nil || screen.WorkArea.Width <= mainWindowWorkAreaInset || screen.WorkArea.Height <= mainWindowWorkAreaInset {
		return
	}

	width, height := window.Size()
	if width <= 0 || height <= 0 {
		return
	}

	maxWidth := screen.WorkArea.Width - mainWindowWorkAreaInset
	maxHeight := screen.WorkArea.Height - mainWindowWorkAreaInset
	nextWidth := min(width, maxWidth)
	nextHeight := min(height, maxHeight)
	if nextWidth != width || nextHeight != height {
		window.SetSize(nextWidth, nextHeight)
	}
	window.SetScreen(screen)
}

// setupSystemTray 配置系统托盘（使用 Wails v3 原生支持）
func setupSystemTray(app *application.App, window *application.WebviewWindow, appInstance *App) {
	systray := app.SystemTray.New()

	// 设置托盘图标（使用专用托盘图标，避免缩放模糊）
	systray.SetIcon(trayIcon)

	// 设置工具提示（dev 模式显示 dev 后缀）
	versionDisplay := Version
	if BuildMode == "dev" {
		versionDisplay = Version + "-dev"
	}
	systray.SetTooltip(fmt.Sprintf("AI Relay Forwarder %s", versionDisplay))

	// 创建托盘菜单
	menu := app.Menu.New()

	// 显示主窗口
	mShow := menu.Add("显示主窗口")
	mShow.OnClick(func(ctx *application.Context) {
		window.Show()
		window.Focus()
	})

	menu.AddSeparator()

	// 复制代理地址
	mCopyAddr := menu.Add("复制代理地址")
	mCopyAddr.OnClick(func(ctx *application.Context) {
		if appInstance != nil {
			addr := appInstance.GetProxyAddress()
			success := app.Clipboard.SetText(addr)
			if success {
				appInstance.logger.Info("📋 代理地址已复制到剪贴板", "address", addr)
			}
		}
	})

	// 打开日志目录
	mOpenLogDir := menu.Add("打开日志目录")
	mOpenLogDir.OnClick(func(ctx *application.Context) {
		if appInstance != nil {
			appInstance.OpenLogDir()
		}
	})

	// 打开 Claude 配置目录
	mOpenClaudeDir := menu.Add("打开 Claude 配置目录")
	mOpenClaudeDir.OnClick(func(ctx *application.Context) {
		if appInstance != nil {
			appInstance.OpenClaudeConfigDir()
		}
	})

	// 打开 Codex 配置目录
	mOpenCodexDir := menu.Add("打开 Codex 配置目录")
	mOpenCodexDir.OnClick(func(ctx *application.Context) {
		if appInstance != nil {
			appInstance.OpenCodexConfigDir()
		}
	})

	menu.AddSeparator()

	// 退出
	mQuit := menu.Add("退出")
	mQuit.OnClick(func(ctx *application.Context) {
		if appInstance != nil {
			appInstance.RequestQuit()
		}
	})

	systray.SetMenu(menu)

	// 托盘图标点击事件处理
	// 简化设计：左键（单击/双击）始终显示窗口，右键显示菜单
	// 不使用 toggle 逻辑，避免复杂的防抖和锁机制导致的死锁问题

	// 左键单击：显示窗口
	systray.OnClick(func() {
		window.Show()
		window.Focus()
	})

	// 右键点击：显示菜单
	systray.OnRightClick(func() {
		systray.OpenMenu()
	})

	// 双击：显示窗口
	systray.OnDoubleClick(func() {
		window.Show()
		window.Focus()
	})
}
