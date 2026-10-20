package app

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

// AttachDesktop supplies the host handles before application startup.
func (a *App) AttachDesktop(host *application.App, window *application.WebviewWindow) {
	a.wailsApp = host
	a.window = window
}

func (a *App) Startup() { a.startup() }

func (a *App) Shutdown() { a.markQuitting(); a.shutdown() }

func (a *App) WindowReady() { a.onWindowReady() }

func (a *App) IsQuitting() bool { return a.isQuitting() }

func (a *App) MarkQuitting() { a.markQuitting() }
