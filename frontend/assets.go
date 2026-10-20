// Package frontend embeds the Vite production build for the desktop host.
package frontend

import "embed"

//go:embed all:dist
var Assets embed.FS
