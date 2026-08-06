// Package config resolves OS-appropriate paths for the application's config
// directory and the default download destination.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

// DownloadsDir returns the path to the directory where received files are
// saved. It creates the directory if it does not exist.
//
// Platforms:
//   - Windows: %USERPROFILE%\Downloads\local-mesh
//   - macOS:   ~/Downloads/local-mesh
//   - Linux:   ~/Downloads/local-mesh (or $XDG_DOWNLOAD_DIR/local-mesh)
func DownloadsDir() (string, error) {
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

// UniqueDestPath returns a path under dir for a file/folder named name that
// does not already exist. If name is taken, it appends (1), (2), … before the
// extension (or at the end for directories). Never overwrites an existing entry.
func UniqueDestPath(dir, name string) string {
	candidate := filepath.Join(dir, name)
	if _, err := os.Stat(candidate); err != nil {
		// Any error (including ErrNotExist) means the path is available.
		return candidate
	}

	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]

	for n := 1; ; n++ {
		// L-6: use strconv.Itoa for all values instead of the fragile n%10 trick.
		candidate = filepath.Join(dir, base+"("+strconv.Itoa(n)+")"+ext)
		if _, err := os.Stat(candidate); err != nil {
			// Any error (including ErrNotExist) means the path is available.
			return candidate
		}
	}
}



