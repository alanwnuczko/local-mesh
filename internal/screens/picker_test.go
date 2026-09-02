package screens

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewPickerUsesConstructorSize(t *testing.T) {
	p := NewPicker(80, 24)
	if p.termW != 80 || p.termH != 24 {
		t.Fatalf("term size = %dx%d, want 80x24", p.termW, p.termH)
	}
}

func TestPickerWrapsAtListEnds(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPicker(80, 24)
	p.fp.CurrentDirectory = dir
	p.listed = ""
	p.reloadEntries()
	if len(p.entries) < 3 {
		t.Fatalf("entries=%d", len(p.entries))
	}

	p.cursor = len(p.entries) - 1
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if p.cursor != 0 {
		t.Fatalf("down at end: cursor=%d want 0", p.cursor)
	}

	p.cursor = 0
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	if p.cursor != len(p.entries)-1 {
		t.Fatalf("up at start: cursor=%d want %d", p.cursor, len(p.entries)-1)
	}
}
