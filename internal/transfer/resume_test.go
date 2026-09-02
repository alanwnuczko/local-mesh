package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPartialRoundTrip(t *testing.T) {
	dir := t.TempDir()
	UseStateDir(dir)
	t.Cleanup(func() { UseStateDir(t.TempDir()) })
	tmp := filepath.Join(dir, "partial")
	if err := os.WriteFile(tmp, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	id := "tid-resume"
	SavePartial(Partial{
		TransferID: id,
		Checksum:   "abc",
		Size:       100,
		Name:       "f",
		TmpPath:    tmp,
	})
	got, off, ok := LookupPartial(id, "abc", 100, false)
	if !ok || got != tmp || off != 5 {
		t.Fatalf("lookup got=%q off=%d ok=%v", got, off, ok)
	}
	ClearPartial(id)
	_, _, ok = LookupPartial(id, "abc", 100, false)
	if ok {
		t.Fatal("cleared entry still present")
	}
}

func TestTransferIDForChecksumStable(t *testing.T) {
	UseStateDir(t.TempDir())
	t.Cleanup(func() { UseStateDir(t.TempDir()) })
	sum := "deadbeef"
	a := TransferIDForChecksum(sum)
	b := TransferIDForChecksum(sum)
	if a != b {
		t.Fatalf("%s vs %s", a, b)
	}
	ForgetSendID(sum)
}
