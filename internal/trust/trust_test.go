package trust

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPairingCodeSymmetric(t *testing.T) {
	a, b := "aaa", "bbb"
	if PairingCode(a, b) != PairingCode(b, a) {
		t.Fatal("code depends on argument order")
	}
	if PairingCode(a, b) == PairingCode(a, "ccc") {
		t.Fatal("different peers produced the same code")
	}
	if len(PairingCode(a, b)) != 4 {
		t.Fatalf("len=%d", len(PairingCode(a, b)))
	}
}

func TestRememberPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, knownFile)
	s := &Store{path: path, ids: map[string]struct{}{}}
	s.Remember("abc")
	if !s.Known("abc") {
		t.Fatal("expected known")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0] != "abc" {
		t.Fatalf("got %v", list)
	}
}
