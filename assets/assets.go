// Package assets embeds the desktop application icons.
package assets

import _ "embed"

//go:embed vane-app.png
var Icon []byte

//go:embed vane-tray.png
var TrayIcon []byte
