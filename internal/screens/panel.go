package screens

import (
	"strings"

	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

// panelInnerSize returns the usable width/height inside a PanelStyle box for
// the given terminal dimensions. The panel has 1-cell border on each side and
// 2-cell horizontal padding, so we subtract accordingly.
func panelInnerSize(termW, termH int) (innerW, innerH int) {
	// panelW must match the logic in wrapInPanel exactly.
	panelW := termW - 2
	if panelW > 110 {
		panelW = 110
	}
	if panelW < 14 {
		panelW = 14
	}

	// border (1 each side) + padding (2 each side) = 6 chars total subtracted from panelW
	innerW = panelW - 6
	if innerW < 8 {
		innerW = 8
	}

	// border top/bottom (1 each) + padding top/bottom (1 each)
	innerH = termH - 4
	if innerH < 4 {
		innerH = 4
	}
	return
}

// wrapInPanel wraps content in a PanelStyle rounded-border box.
// The panel width is capped at 110 for wide terminals but always respects
// the actual terminal width - this prevents layout spill on resize.
func wrapInPanel(content string, termW, _ int) string {
	// 2 = 1 border char each side; keep 1 col margin so the border is visible.
	panelW := termW - 2
	if panelW > 110 {
		panelW = 110
	}
	if panelW < 10 {
		panelW = 10
	}
	// Width() in lipgloss specifies the width excluding borders.
	// To make the total panel exactly panelW chars wide, we must pass panelW - 2.
	contentW := panelW - 2
	if contentW < 1 {
		contentW = 1
	}
	return ui.PanelStyle.Width(contentW).MaxWidth(panelW).Render(content)
}

// renderDivider returns a horizontal rule string styled with ColorBorder.
func renderDivider(width int) string {
	if width < 1 {
		width = 1
	}
	return lipgloss.NewStyle().Foreground(ui.ColorBorder).Render(strings.Repeat("─", width))
}
