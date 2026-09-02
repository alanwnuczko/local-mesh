package screens

import (
	"strings"
	"testing"

	"github.com/alanwnuczko/local-mesh/internal/transfer"
)

func TestProgressShowsSavedPath(t *testing.T) {
	p := NewProgress(transfer.DirRecv, 10, 80, 24)
	p.SetDone(nil, `/tmp/local-mesh/hello.txt`)
	view := p.View()
	if !strings.Contains(view, "hello.txt") {
		t.Fatalf("view missing saved path:\n%s", view)
	}
	if !strings.Contains(view, "Transfer complete") {
		t.Fatalf("view missing complete:\n%s", view)
	}
}
