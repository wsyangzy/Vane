//go:build !stub
// +build !stub

package tray

import (
	"context"
	"runtime"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

// Windows 托盘图标刷新间隔
// 用于解决 Windows 下长时间运行后右键菜单失效的问题
const windowsTrayRefreshInterval = 30 * time.Second

type systrayController struct {
	opts      Options
	ctx       context.Context
	quitCh    chan struct{}
	once      sync.Once
	running   bool
	runningMu sync.Mutex
	// Windows 定期刷新相关
	refreshTicker *time.Ticker
	refreshDone   chan struct{}
}

func (c *systrayController) Stop() {
	c.once.Do(func() {
		// 停止 Windows 刷新 ticker
		if c.refreshTicker != nil {
			c.refreshTicker.Stop()
			close(c.refreshDone)
		}

		c.runningMu.Lock()
		if c.running {
			systray.Quit()
			c.running = false
		}
		c.runningMu.Unlock()
		close(c.quitCh)
	})
}

func start(ctx context.Context, opts Options) (Controller, error) {
	ctrl := &systrayController{
		opts:        opts,
		ctx:         ctx,
		quitCh:      make(chan struct{}),
		refreshDone: make(chan struct{}),
	}

	// systray.Run 会阻塞，在单独的 goroutine 中运行。
	go func() {
		ctrl.runningMu.Lock()
		ctrl.running = true
		ctrl.runningMu.Unlock()

		systray.Run(
			func() { ctrl.onReady() },
			func() { ctrl.onExit() },
		)
	}()

	return ctrl, nil
}

func (c *systrayController) onReady() {
	if len(c.opts.Icon) > 0 {
		systray.SetIcon(c.opts.Icon)
	}

	tooltip := c.opts.Tooltip
	if tooltip == "" {
		tooltip = "CC-Forwarder"
	}
	systray.SetTooltip(tooltip)

	mShow := systray.AddMenuItem("显示主窗口", "显示应用主窗口")
	systray.SetDefaultMenuItem(mShow)
	systray.AddSeparator()
	mOpenLogDir := systray.AddMenuItem("打开日志目录", "在资源管理器中打开日志文件目录")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "退出应用")

	// Windows 平台启动定期刷新机制，解决长时间运行后右键菜单失效的问题
	if runtime.GOOS == "windows" {
		c.startWindowsRefresh(tooltip)
	}

	go func() {
		for {
			select {
			case <-c.quitCh:
				return
			case <-mShow.ClickedCh:
				if c.opts.OnShow != nil {
					c.opts.OnShow()
				}
			case <-mOpenLogDir.ClickedCh:
				if c.opts.OnOpenLogDir != nil {
					c.opts.OnOpenLogDir()
				}
			case <-mQuit.ClickedCh:
				if c.opts.OnQuit != nil {
					c.opts.OnQuit()
				}
			}
		}
	}()
}

func (c *systrayController) onExit() {
}

// startWindowsRefresh 启动 Windows 托盘图标定期刷新机制
// 通过定期调用 SetTooltip 触发底层的 Shell_NotifyIcon(NIM_MODIFY)
// 解决 getlantern/systray 库在 Windows 下长时间运行后右键菜单失效的问题
func (c *systrayController) startWindowsRefresh(tooltip string) {
	c.refreshTicker = time.NewTicker(windowsTrayRefreshInterval)

	go func() {
		for {
			select {
			case <-c.refreshDone:
				return
			case <-c.refreshTicker.C:
				// 重新设置 tooltip 会触发 nid.modify()，保持托盘图标活跃
				systray.SetTooltip(tooltip)
			}
		}
	}()
}
