package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"file.txt", "file.txt"},
		{"foo/bar.txt", "bar.txt"},
		{"foo\\bar.txt", "bar.txt"},
		{"../etc/passwd", "passwd"},
		{"..", "unnamed"},
		{".", "unnamed"},
		{"", "unnamed"},
		{"///", "unnamed"},
		{"C:\\Windows\\evil.txt", "evil.txt"},
	}
	for _, tc := range tests {
		got := SanitizeFileName(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeFileName(%q)=%q want %q", tc.in, got, tc.want)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("SanitizeFileName(%q) still contains a separator: %q", tc.in, got)
		}
	}
}

func TestConfineRelRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	bads := []string{
		"../evil",
		"..\\evil",
		"foo/../../evil",
		"/etc/passwd",
		"../../../../../../tmp/x",
	}
	if filepath.Separator == '\\' {
		bads = append(bads, `C:\Windows\System32\evil`)
	}
	for _, rel := range bads {
		if _, err := ConfineRel(dir, rel); err == nil {
			t.Errorf("ConfineRel(%q) succeeded, want error", rel)
		}
	}
}

func TestConfineRelAllowsNested(t *testing.T) {
	dir := t.TempDir()
	got, err := ConfineRel(dir, "folder/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "folder", "file.txt")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestUniqueDestPathStripsTraversal(t *testing.T) {
	dir := t.TempDir()
	p, err := UniqueDestPath(dir, "../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(p) != dir {
		t.Fatalf("path escaped dest: %q (dir %q)", p, dir)
	}
	if filepath.Base(p) != "evil.txt" {
		t.Fatalf("base = %q want evil.txt", filepath.Base(p))
	}
}

func TestUniqueDestPathSuffix(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(first, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	p, err := UniqueDestPath(dir, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "a(1).txt" {
		t.Fatalf("got %q want a(1).txt", filepath.Base(p))
	}
}

func TestUniqueDestPathPermissionError(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0755) })

	// Lstat on the directory itself succeeds (it exists), so UniqueDestPath
	// should suffix rather than treat a later permission error as "free".
	p, err := UniqueDestPath(dir, "blocked")
	if err != nil {
		// Some platforms cannot chmod 0000 (Windows). That's fine.
		t.Logf("UniqueDestPath: %v", err)
		return
	}
	if p == blocked {
		t.Fatal("UniqueDestPath reused an existing path")
	}
}
