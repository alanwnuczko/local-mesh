// Package config resolves OS-appropriate paths for the application's config
// directory and the default download destination.
package config

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ConfigDir returns the path to the local-mesh configuration directory,
// creating it if it does not exist.
//
// Platforms:
//   - Windows: %APPDATA%\local-mesh
//   - macOS:   ~/Library/Application Support/local-mesh
//   - Linux:   ~/.config/local-mesh  (or $XDG_CONFIG_HOME/local-mesh)
func ConfigDir() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("APPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = home
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	default: // Linux and others
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			base = xdg
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
	}

	dir := filepath.Join(base, "local-mesh")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// testDownloadsDir, when set, overrides DownloadsDir. Used by tests so they
// never write into the real Downloads folder.
var testDownloadsDir string

// UseDownloadsDir redirects DownloadsDir to dir for the remainder of the
// process (tests). An empty dir clears the override.
func UseDownloadsDir(dir string) {
	testDownloadsDir = dir
}

// DownloadsDir returns the path to the directory where received files are
// saved. It creates the directory if it does not exist.
//
// Platforms:
//   - Windows: %USERPROFILE%\Downloads\local-mesh
//   - macOS:   ~/Downloads/local-mesh
//   - Linux:   ~/Downloads/local-mesh (or $XDG_DOWNLOAD_DIR/local-mesh)
func DownloadsDir() (string, error) {
	if testDownloadsDir != "" {
		if err := os.MkdirAll(testDownloadsDir, 0755); err != nil {
			return "", err
		}
		return testDownloadsDir, nil
	}

	var base string
	switch runtime.GOOS {
	case "windows":
		profile := os.Getenv("USERPROFILE")
		if profile == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			profile = home
		}
		base = filepath.Join(profile, "Downloads")
	default:
		if xdg := os.Getenv("XDG_DOWNLOAD_DIR"); xdg != "" {
			base = xdg
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "Downloads")
		}
	}

	dir := filepath.Join(base, "local-mesh")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// SanitizeFileName returns a single path element derived from name, stripping
// directories, ".." traversal, and empty values. The result is never a
// separator-containing path.
func SanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "unnamed"
	}
	if strings.ContainsAny(name, `/\`) {
		return "unnamed"
	}
	return name
}

// ConfineRel joins rel onto baseDir and returns an absolute path that is
// guaranteed to be baseDir itself or a descendant of it. Absolute rel values,
// ".." traversal, and volume-rooted names are rejected.
func ConfineRel(baseDir, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if path.IsAbs(rel) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q is absolute", rel)
	}
	// Windows volume-relative ("C:foo") or UNC.
	if len(rel) >= 2 && rel[1] == ':' {
		return "", fmt.Errorf("path %q is absolute", rel)
	}
	clean := path.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("path %q escapes destination", rel)
	}

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	baseAbs = filepath.Clean(baseAbs)

	target := filepath.Join(baseAbs, filepath.FromSlash(clean))
	target = filepath.Clean(target)

	relOut, err := filepath.Rel(baseAbs, target)
	if err != nil {
		return "", fmt.Errorf("path %q escapes destination: %w", rel, err)
	}
	if relOut == ".." || strings.HasPrefix(relOut, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path %q escapes destination", rel)
	}
	return target, nil
}

// UniqueDestPath returns a path under dir for a file/folder named name that
// does not already exist. If name is taken, it appends (1), (2), … before the
// extension (or at the end for directories). Never overwrites an existing entry.
//
// name is reduced to a single path element (no directories, no ".."). Any
// Stat error other than "not exist" is returned rather than treated as free.
func UniqueDestPath(dir, name string) (string, error) {
	name = SanitizeFileName(name)

	candidate, err := ConfineRel(dir, name)
	if err != nil {
		return "", err
	}

	available, err := pathAvailable(candidate)
	if err != nil {
		return "", err
	}
	if available {
		return candidate, nil
	}

	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]
	if base == "" {
		base = name
		ext = ""
	}

	for n := 1; ; n++ {
		suffixed := base + "(" + strconv.Itoa(n) + ")" + ext
		candidate, err = ConfineRel(dir, suffixed)
		if err != nil {
			return "", err
		}
		available, err = pathAvailable(candidate)
		if err != nil {
			return "", err
		}
		if available {
			return candidate, nil
		}
	}
}

func pathAvailable(p string) (bool, error) {
	_, err := os.Lstat(p)
	if err == nil {
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}
