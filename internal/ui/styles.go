// Package ui provides shared styling constants for the local-mesh TUI.
// It is intentionally import-free of bubbletea and the app package so that
// both internal/app and internal/screens can import it without cycles.
//
// ALL colour and style decisions live here. No screen file may declare an
// ad-hoc lipgloss colour or style - they must reference a named export below.
// This keeps the palette centrally controlled and trivially re-themeable.
package ui

import "github.com/charmbracelet/lipgloss"

// --------------------------------------------------------------------------
// Colour palette - curated dark-mode colours.
// --------------------------------------------------------------------------

var (
	ColorPrimary    = lipgloss.Color("#EC4899") // pink-500    - brand / borders
	ColorAccent     = lipgloss.Color("#F97316") // orange-500  - interactive values
	ColorSuccess    = lipgloss.Color("#10B981") // emerald-500 - accept / done
	ColorError      = lipgloss.Color("#EF4444") // red-500     - danger / reject / error
	ColorMuted      = lipgloss.Color("#6B7280") // gray-500    - secondary text
	ColorForeground = lipgloss.Color("#F9FAFB") // gray-50     - primary text
	ColorBackground = lipgloss.Color("#111827") // gray-900    - terminal bg
	ColorSurface    = lipgloss.Color("#1F2937") // gray-800    - panel / overlay bg
	ColorBorder     = lipgloss.Color("#374151") // gray-700    - subtle borders
	ColorLabel      = lipgloss.Color("#9CA3AF") // gray-400    - field labels
	ColorDivider    = lipgloss.Color("#2D3748") // gray-700/75 - footer rule
)

// --------------------------------------------------------------------------
// Named semantic styles - use these everywhere; never add colours inline.
// --------------------------------------------------------------------------

var (
	// StyleAccent renders primary interactive values: hostnames, sizes, statuses.
	StyleAccent = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	// StyleMuted renders secondary / contextual text (IDs, addresses, hints).
	StyleMuted = lipgloss.NewStyle().Foreground(ColorMuted)

	// StyleLabel renders field label text (e.g. "Target:", "Size:").
	StyleLabel = lipgloss.NewStyle().Foreground(ColorLabel)

	// StyleValue renders field values that are not accented (paths, checksums).
	StyleValue = lipgloss.NewStyle().Foreground(ColorForeground)

	// StyleDanger renders destructive-action keys and error messages.
	StyleDanger = lipgloss.NewStyle().Foreground(ColorError).Bold(true)

	// StyleSuccess renders confirm-action keys and success messages.
	StyleSuccess = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
)

// --------------------------------------------------------------------------
// Structural / layout styles.
// --------------------------------------------------------------------------

var (
	// TitleStyle is used for screen heading text.
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Padding(0, 1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	// HighlightStyle is an alias kept for call-sites that haven't migrated yet.
	// Prefer StyleAccent for new code.
	HighlightStyle = StyleAccent

	// ErrorStyle is an alias kept for call-sites that haven't migrated yet.
	// Prefer StyleDanger for new code.
	ErrorStyle = StyleDanger

	// SuccessStyle is an alias kept for call-sites that haven't migrated yet.
	// Prefer StyleSuccess for new code.
	SuccessStyle = StyleSuccess

	// HelpStyle renders dim key-hint text at the bottom of a screen.
	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Padding(0, 1)

	// PanelStyle wraps a screen's main content in a consistent rounded border.
	// Width must be set per-render via .Width(n) to avoid global mutation.
	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)

	// OverlayStyle is the incoming-request overlay panel (accent border to draw attention).
	OverlayStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Background(ColorSurface).
			Padding(1, 3)

	// BoxStyle is a legacy alias - kept for compatibility.
	BoxStyle = PanelStyle

	// FooterStyle renders the persistent status bar at the bottom of every screen.
	FooterStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// FooterAccentStyle renders highlighted segments inside the footer.
	FooterAccentStyle = lipgloss.NewStyle().
				Foreground(ColorAccent).
				Bold(true)

	// StatusBarStyle is a legacy alias.
	StatusBarStyle = FooterStyle

	// LogoStyle renders the ASCII logo on the peer-list screen.
	LogoStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)
)
