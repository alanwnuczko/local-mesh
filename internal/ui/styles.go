// Package ui provides shared styling constants for the local-mesh TUI.
// It is intentionally import-free of bubbletea and the app package so that
// both internal/app and internal/screens can import it without cycles.
package ui

import "github.com/charmbracelet/lipgloss"

// Colour palette — curated dark-mode colours.
var (
	ColorPrimary    = lipgloss.Color("#7C3AED") // violet-600
	ColorAccent     = lipgloss.Color("#06B6D4") // cyan-500
	ColorSuccess    = lipgloss.Color("#10B981") // emerald-500
	ColorError      = lipgloss.Color("#EF4444") // red-500
	ColorMuted      = lipgloss.Color("#6B7280") // gray-500
	ColorForeground = lipgloss.Color("#F9FAFB") // gray-50
	ColorBackground = lipgloss.Color("#111827") // gray-900
	ColorSurface    = lipgloss.Color("#1F2937") // gray-800
	ColorBorder     = lipgloss.Color("#374151") // gray-700
)

// Shared lipgloss styles.
var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Padding(0, 1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	HighlightStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true)

	MutedStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)

	OverlayStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Background(ColorSurface).
			Padding(1, 3)

	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Padding(0, 1)

	StatusBarStyle = lipgloss.NewStyle().
			Background(ColorSurface).
			Foreground(ColorMuted).
			Padding(0, 1)
)
