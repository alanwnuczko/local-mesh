package screens

import "testing"

func TestNewPickerUsesConstructorSize(t *testing.T) {
	p := NewPicker(80, 24)
	if p.termW != 80 || p.termH != 24 {
		t.Fatalf("term size = %dx%d, want 80x24", p.termW, p.termH)
	}
}
