package transfer

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanFolderThenStreamMatchesHash(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanFolder(src)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Size == 0 || plan.Checksum == "" {
		t.Fatalf("empty plan: %+v", plan)
	}

	var buf bytes.Buffer
	if err := plan.Stream(&buf); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	got := hex.EncodeToString(sum[:])
	if got != plan.Checksum {
		t.Fatalf("stream checksum %s != plan %s", got, plan.Checksum)
	}
	if int64(buf.Len()) != plan.Size {
		t.Fatalf("stream size %d != plan %d", buf.Len(), plan.Size)
	}

	dest := filepath.Join(dir, "out")
	if err := os.Mkdir(dest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := UntarFolder(&buf, dest); err != nil {
		t.Fatal(err)
	}
	gotA, err := os.ReadFile(filepath.Join(dest, "tree", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotA) != "hello" {
		t.Fatalf("a.txt = %q", gotA)
	}
}

func TestPlanFolderStreamDetectsChange(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(src, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanFolder(src)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("HELLO!!"), 0644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := plan.Stream(&buf); err == nil {
		t.Fatal("expected error after file change")
	}
}

func TestUntarFolderRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:     "../evil.txt",
		Mode:     0644,
		Size:     4,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("boom")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := UntarFolder(&buf, dest); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); err == nil {
		t.Fatal("escaped file was written")
	}
}

func TestUntarFolderRejectsAbsolute(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	name := "/tmp/evil.txt"
	hdr := &tar.Header{Name: name, Mode: 0644, Size: 1, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := UntarFolder(&buf, dest); err == nil {
		t.Fatal("expected absolute path to be rejected")
	}
}

func TestUntarFolderSkipsSymlink(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name:     "link",
		Linkname: "/etc/passwd",
		Typeflag: tar.TypeSymlink,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := UntarFolder(&buf, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "link")); err == nil {
		t.Fatal("symlink should have been skipped")
	}
}

func TestTarFolderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- TarFolder(src, pw)
		pw.Close()
	}()
	dest := filepath.Join(dir, "dest")
	if err := os.Mkdir(dest, 0755); err != nil {
		t.Fatal(err)
	}
	if err := UntarFolder(pr, dest); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "src", "f"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "ok" {
		t.Fatalf("got %q", b)
	}
}

func TestConfinedTarTargetNestedOK(t *testing.T) {
	dir := t.TempDir()
	got, err := confinedTarTarget(dir, "tree/sub/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, dir) {
		t.Fatalf("%q not under %q", got, dir)
	}
}
