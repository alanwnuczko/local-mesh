// Package app re-exports style variables from internal/ui for convenience.
// Both app and screens import internal/ui directly; this file exists only to
// avoid breaking any internal app-level code that referenced these vars.
package app

import "github.com/alanwnuczko/local-mesh/internal/ui"

// Re-export style constants so other files in the app package can use them.
var (
	ColorPrimary    = ui.ColorPrimary
	ColorAccent     = ui.ColorAccent
	ColorSuccess    = ui.ColorSuccess
	ColorError      = ui.ColorError
	ColorMuted      = ui.ColorMuted
	ColorForeground = ui.ColorForeground
	ColorBackground = ui.ColorBackground
	ColorSurface    = ui.ColorSurface
	ColorBorder     = ui.ColorBorder

	TitleStyle     = ui.TitleStyle
	HighlightStyle = ui.HighlightStyle
	SuccessStyle   = ui.SuccessStyle
	ErrorStyle     = ui.ErrorStyle
	MutedStyle     = ui.MutedStyle
	HelpStyle      = ui.HelpStyle
	OverlayStyle   = ui.OverlayStyle
	BoxStyle       = ui.BoxStyle
)
