package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestViewKeepsTopBorderAndFitsHeight(t *testing.T) {
	m := NewModel("selfid01", "testhost", 80, 24)
	view := m.View()
	h := lipgloss.Height(view)
	if h > 24 {
		t.Fatalf("view height %d exceeds terminal 24\n%s", h, view)
	}
	first := strings.Split(view, "\n")[0]
	if !strings.Contains(first, "─") && !strings.Contains(first, "╭") && !strings.Contains(first, "┌") {
		t.Fatalf("missing top border: %q", first)
	}
	if !strings.Contains(view, "testhost") {
		t.Fatal("footer missing from view")
	}
	for i, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) >= 80 {
			t.Fatalf("line %d width %d >= 80; Unix PTYs would wrap", i, lipgloss.Width(line))
		}
	}
}

func TestFitHeightKeepsTop(t *testing.T) {
	s := "top\nmiddle\nbottom\nextra"
	got := clampView(s, 80, 3)
	if got != "top\nmiddle\nbottom" {
		t.Fatalf("got %q", got)
	}
}

func TestClampViewShortensFullWidthLines(t *testing.T) {
	line := strings.Repeat("x", 80)
	got := clampView(line+"\n"+line, 80, 24)
	for i, l := range strings.Split(got, "\n") {
		if lipgloss.Width(l) >= 80 {
			t.Fatalf("line %d width %d; PTY would wrap", i, lipgloss.Width(l))
		}
	}
}
