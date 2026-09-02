package app

import (
	"fmt"
	"strings"

	"github.com/alanwnuczko/local-mesh/internal/screens"
	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

// View renders the active screen and, if an overlay is active, composites it
// on top of the dimmed base view (§4.5).
//
// CONCURRENCY INVARIANT: View is called by Bubbletea on the same goroutine as
// Update; it must not trigger side effects.
func (m Model) View() string {
	base := m.renderBase()

	// Overlays are composited onto the panel only; the footer is appended
	// after so it cannot push the panel's top border off-screen.
	panelH := m.Height - screens.FooterLines
	if panelH < 1 {
		panelH = m.Height
	}

	if m.Overlay != nil {
		base = screens.RenderOverlay(base, m.Overlay, m.Width, panelH)
	} else if m.ShowHelp {
		base = renderHelpOverlay(base, m.Width, panelH)
	}

	if m.Footer != nil {
		base += "\n" + m.Footer.View(m.Activity)
	}

	return clampView(base, m.Width, m.Height)
}

// clampView keeps the view inside the terminal: at most height rows, and no
// row occupying the last column (Unix PTYs wrap that cell and scroll).
func clampView(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	maxW := width - 1
	if width <= 1 {
		maxW = width
	}
	if maxW > 0 {
		clip := lipgloss.NewStyle().MaxWidth(maxW).Inline(true)
		for i, line := range lines {
			if lipgloss.Width(line) > maxW {
				lines[i] = clip.Render(line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// renderHelpOverlay draws the full keybinding help panel over a dimmed base.
func renderHelpOverlay(base string, width, height int) string {
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	lines := strings.Split(base, "\n")
	dimmed := make([]string, len(lines))
	for i, l := range lines {
		dimmed[i] = dimStyle.Render(l)
	}

	var sb strings.Builder
	sb.WriteString(ui.TitleStyle.Render("Keybindings") + "\n\n")
	for _, row := range helpLines {
		sb.WriteString(fmt.Sprintf("  %s  %s\n",
			ui.StyleAccent.Render(fmt.Sprintf("%-14s", row.keys)),
			ui.StyleMuted.Render(row.desc),
		))
	}
	sb.WriteString("\n")
	sb.WriteString("  " + ui.StyleMuted.Render("press ? or esc to close"))

	boxWidth := width - 8
	if boxWidth < 1 {
		boxWidth = 1
	}
	if boxWidth > 56 {
		boxWidth = 56
	}
	box := ui.OverlayStyle.Width(boxWidth).Render(sb.String())
	boxLines := strings.Split(box, "\n")

	// The logo occupies 6 rows plus 2 rows of panel border/padding at the top.
	// We must not place the help box above that region so it never covers the logo.
	const logoOffset = 8 // 6 logo lines + 1 panel top border + 1 panel top padding

	// Center within the space below the logo.
	availableH := height - logoOffset
	if availableH < 1 {
		availableH = 1
	}
	topPad := logoOffset + (availableH-len(boxLines))/2
	if topPad < logoOffset {
		topPad = logoOffset
	}
	if topPad < 0 {
		topPad = 0
	}

	result := make([]string, 0, height)
	for i, l := range dimmed {
		if i == topPad {
			result = append(result, boxLines...)
		}
		if i < topPad || i >= topPad+len(boxLines) {
			result = append(result, l)
		}
	}
	for len(result) < topPad+len(boxLines) {
		boxIdx := len(result) - topPad
		if boxIdx < 0 || boxIdx >= len(boxLines) {
			break
		}
		result = append(result, boxLines[boxIdx])
	}
	out := strings.Join(result, "\n")
	if height > 0 {
		lines := strings.Split(out, "\n")
		if len(lines) > height {
			out = strings.Join(lines[:height], "\n")
		}
	}
	return out
}

func (m Model) renderBase() string {
	switch m.ActiveScreen {
	case ScreenPeerList:
		return m.PeerList.View()

	case ScreenPicker:
		return m.Picker.View()

	case ScreenConfirm:
		if m.Confirm != nil {
			return m.Confirm.View()
		}

	case ScreenProgress:
		if m.Progress != nil {
			return m.Progress.View()
		}
	}
	return ""
}
