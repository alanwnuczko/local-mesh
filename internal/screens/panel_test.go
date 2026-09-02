package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapInPanelHasTopBorder(t *testing.T) {
	const termW, termH = 80, 24
	panel := wrapInPanel("hello", termW, termH)
	lines := strings.Split(panel, "\n")
	if len(lines) == 0 {
		t.Fatal("empty panel")
	}
	first := lines[0]
	if !strings.Contains(first, "─") && !strings.Contains(first, "╭") && !strings.Contains(first, "┌") {
		t.Fatalf("first line has no top border: %q", first)
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "─") && !strings.Contains(last, "╰") && !strings.Contains(last, "└") {
		t.Fatalf("last line has no bottom border: %q", last)
	}
}

func TestPanelPlusFooterFitsTerminal(t *testing.T) {
	const termW, termH = 80, 24
	// Oversized content used to make the view taller than the terminal,
	// which scrolled the top border off-screen.
	content := strings.Repeat("line\n", 80)
	panel := wrapInPanel(content, termW, termH)
	footer := NewFooter("host", "abcdef12", termW).View(ActivityIdle)
	view := panel + "\n" + footer

	h := lipgloss.Height(view)
	if h > termH {
		t.Fatalf("view height %d exceeds terminal %d", h, termH)
	}

	lines := strings.Split(view, "\n")
	if !strings.Contains(lines[0], "─") && !strings.Contains(lines[0], "╭") {
		t.Fatalf("top border missing after joining footer: %q", lines[0])
	}
}

func TestFooterLinesDoNotFillTerminalWidth(t *testing.T) {
	const termW = 80
	footer := NewFooter("host", "abcdef12", termW).View(ActivityIdle)
	for i, line := range strings.Split(footer, "\n") {
		if lipgloss.Width(line) >= termW {
			t.Fatalf("footer line %d width %d >= %d; Unix PTYs would wrap", i, lipgloss.Width(line), termW)
		}
	}
}

func TestUsableWidth(t *testing.T) {
	if usableWidth(80) != 79 {
		t.Fatalf("usableWidth(80)=%d", usableWidth(80))
	}
	if usableWidth(1) != 1 {
		t.Fatalf("usableWidth(1)=%d", usableWidth(1))
	}
}

func TestPanelInnerSizeLeavesRoomForFooter(t *testing.T) {
	_, innerH := panelInnerSize(80, 24)
	_, panelH := panelOuterSize(80, 24)
	if panelH != 24-FooterLines {
		t.Fatalf("panelH=%d want %d", panelH, 24-FooterLines)
	}
	if innerH > panelH-4 {
		t.Fatalf("innerH=%d too large for panelH=%d", innerH, panelH)
	}
}
