package screens

import (
	"strings"

	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

// FooterLines is the height of the status bar rendered under the main panel
// (one divider row + one identity row). The panel must leave this many rows
// free: if the view is taller than the terminal, the buffer scrolls and the
// panel's top border is clipped.
const FooterLines = 2

func panelOuterSize(termW, termH int) (panelW, panelH int) {
	panelW = termW - 2
	if panelW > 110 {
		panelW = 110
	}
	if panelW < 14 {
		panelW = 14
	}

	panelH = termH - FooterLines
	if panelH < 8 {
		panelH = 8
	}
	return
}

// panelInnerSize returns the usable width/height inside a PanelStyle box for
// the given terminal dimensions, after reserving the footer.
func panelInnerSize(termW, termH int) (innerW, innerH int) {
	panelW, panelH := panelOuterSize(termW, termH)

	// border (1 each side) + padding (2 each side) = 6
	innerW = panelW - 6
	if innerW < 8 {
		innerW = 8
	}

	// border top/bottom (1 each) + padding top/bottom (1 each)
	innerH = panelH - 4
	if innerH < 4 {
		innerH = 4
	}
	return
}

// wrapInPanel wraps content in a PanelStyle rounded-border box whose outer
// height is exactly termH - FooterLines, so View() can append the footer
// without overflowing the terminal (which would clip the top border).
func wrapInPanel(content string, termW, termH int) string {
	panelW, panelH := panelOuterSize(termW, termH)

	// Width()/Height() in lipgloss exclude borders.
	contentW := panelW - 2
	contentH := panelH - 2
	if contentW < 1 {
		contentW = 1
	}
	if contentH < 1 {
		contentH = 1
	}
	// Truncate the payload ourselves. lipgloss MaxHeight applies to the
	// bordered box and would clip the bottom (or, with overflow, the top)
	// border. Height is a minimum; it pads short content.
	textH := contentH - 2 // PanelStyle vertical padding
	if textH < 1 {
		textH = 1
	}
	content = truncateLines(content, textH)

	return ui.PanelStyle.
		Width(contentW).
		Height(contentH).
		Render(content)
}

func truncateLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n")
}

// usableWidth is the largest line width that will not wrap on a Unix PTY.
// Writing exactly termW cells places the cursor in the last column; the next
// character (or a newline after an automatic wrap) advances to a new row and
// scrolls the alt screen.
func usableWidth(termW int) int {
	if termW > 1 {
		return termW - 1
	}
	return termW
}

func repeatToWidth(unit string, width int) string {
	if width <= 0 || unit == "" {
		return ""
	}
	unitW := lipgloss.Width(unit)
	if unitW <= 0 {
		return ""
	}
	n := width / unitW
	s := strings.Repeat(unit, n)
	for lipgloss.Width(s) > width && n > 0 {
		n--
		s = strings.Repeat(unit, n)
	}
	return s
}

// renderDivider returns a horizontal rule string styled with ColorBorder.
func renderDivider(width int) string {
	if width < 1 {
		width = 1
	}
	return lipgloss.NewStyle().Foreground(ui.ColorBorder).Render(strings.Repeat("─", width))
}
