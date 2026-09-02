package screens

import (
	"fmt"
	"strings"

	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

// ActivityStatus describes the current transfer state shown in the footer.
type ActivityStatus int

const (
	ActivityIdle ActivityStatus = iota
	ActivityTransferring
	ActivityReceiving
)

// Footer is the persistent status bar rendered at the bottom of every screen.
// It shows this instance's hostname + short device ID on the left, and a live
// activity indicator on the right.
type Footer struct {
	hostname string
	shortID  string
	width    int
}

// NewFooter creates the footer component.
func NewFooter(hostname, deviceID string, width int) *Footer {
	shortID := deviceID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return &Footer{hostname: hostname, shortID: shortID, width: width}
}

// SetWidth updates the terminal width (called on WindowSizeMsg).
func (f *Footer) SetWidth(w int) { f.width = w }

// View renders the footer for the given activity state.
func (f *Footer) View(activity ActivityStatus) string {
	// A line that fills the last column wraps on Unix PTYs (WSL, Linux,
	// macOS). That extra wrap scrolls the alt screen and clips the panel's
	// top border. Native Windows consoles do not wrap the same way, which is
	// why this only showed up in WSL.
	w := usableWidth(f.width)

	left := fmt.Sprintf(" %s %s",
		ui.FooterAccentStyle.Render(f.hostname),
		ui.FooterStyle.Render("· "+f.shortID),
	)

	var indicator string
	switch activity {
	case ActivityTransferring:
		indicator = ui.FooterAccentStyle.Render("↑ transferring")
	case ActivityReceiving:
		indicator = ui.FooterAccentStyle.Render("↓ receiving")
	default:
		indicator = ui.FooterStyle.Render("idle")
	}
	right := " " + indicator + " "

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := w - leftW - rightW
	if gap < 0 {
		gap = 0
	}

	divider := ui.FooterStyle.Render(repeatToWidth("─", w))
	bar := left + strings.Repeat(" ", gap) + right
	if lipgloss.Width(bar) > w && w > 0 {
		bar = lipgloss.NewStyle().MaxWidth(w).Inline(true).Render(bar)
	}

	return divider + "\n" + bar
}
