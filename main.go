//go:build desktop

// main.go - Claude Request Forwarder Wails v3 应用入口
// 使用 Wails v3 原生系统托盘支持
//
// 注意：全局变量（Version, assets, setupLogger 等）定义在 globals.go 中
// 以便 IDE 在无 desktop build tag 时也能正确解析

package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

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

func main() {
	flag.Parse()

	// 处理版本标志
	if *showVersion {
		fmt.Printf("Claude Request Forwarder\n")
		fmt.Printf("Version: %s\n", Version)
		fmt.Printf("Commit: %s\n", Commit)
		fmt.Printf("Built: %s\n", BuildTime)
		os.Exit(0)
	}

	// 创建应用实例
	appInstance := NewApp()
	appInstance.configPath = *configPath

	// 创建 Wails v3 应用
	wailsApp := application.New(application.Options{
		Name:        "Claude Request Forwarder",
		Description: "Claude API 请求转发服务",
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
			UniqueID: "claude-request-forwarder-desktop",
			OnSecondInstanceLaunch: func(data application.SecondInstanceData) {
				// 当第二个实例启动时，显示主窗口
				if appInstance != nil && appInstance.window != nil {
					appInstance.window.Show()
					appInstance.window.Focus()
				}
			},
		},
	})

	// 保存 wailsApp 引用到 App 实例
	appInstance.wailsApp = wailsApp

	// 创建主窗口
	window := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Claude Request Forwarder",
		Width:     1440,
		Height:    900,
		MinWidth:  1200,
		MinHeight: 700,
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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// setupSystemTray 配置系统托盘（使用 Wails v3 原生支持）
func setupSystemTray(app *application.App, window *application.WebviewWindow, appInstance *App) {
	systray := app.SystemTray.New()

	// 设置托盘图标（使用专用托盘图标，避免缩放模糊）
	systray.SetIcon(trayIcon)

	// 设置工具提示
	systray.SetTooltip(fmt.Sprintf("Claude Request Forwarder %s", Version))

	// 创建托盘菜单
	menu := app.Menu.New()

	// 显示主窗口
	mShow := menu.Add("显示主窗口")
	mShow.OnClick(func(ctx *application.Context) {
		window.Show()
		window.Focus()
	})

	menu.AddSeparator()

	// 打开日志目录
	mOpenLogDir := menu.Add("打开日志目录")
	mOpenLogDir.OnClick(func(ctx *application.Context) {
		if appInstance != nil {
			appInstance.OpenLogDir()
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

	// 托盘点击防抖处理
	// 问题：双击时 OnClick 会被调用两次，导致窗口状态反复切换
	// 解决：使用防抖延迟，如果在双击时间窗口内收到 OnDoubleClick，则取消单击操作
	var (
		clickMu      sync.Mutex
		clickPending bool
		clickTimer   *time.Timer
	)
	const doubleClickDelay = 250 * time.Millisecond // Windows 默认双击间隔约 500ms

	// 托盘图标单击事件：延迟处理，等待可能的双击
	systray.OnClick(func() {
		clickMu.Lock()
		defer clickMu.Unlock()

		// 如果已有待处理的点击，说明是双击的第二下，忽略
		if clickPending {
			return
		}

		// 标记有待处理的点击
		clickPending = true

		// 延迟执行，给双击事件留出时间
		clickTimer = time.AfterFunc(doubleClickDelay, func() {
			clickMu.Lock()
			defer clickMu.Unlock()

			// 如果仍然标记为待处理，说明不是双击，执行单击操作
			if clickPending {
				clickPending = false
				if window.IsVisible() {
					window.Hide()
				} else {
					window.Show()
					window.Focus()
				}
			}
		})
	})

	// 右键点击显示菜单
	systray.OnRightClick(func() {
		systray.OpenMenu()
	})

	// 双击显示窗口：取消待处理的单击，直接显示
	systray.OnDoubleClick(func() {
		clickMu.Lock()
		defer clickMu.Unlock()

		// 取消待处理的单击操作
		clickPending = false
		if clickTimer != nil {
			clickTimer.Stop()
		}

		// 双击始终显示并聚焦窗口
		window.Show()
		window.Focus()
	})
}
