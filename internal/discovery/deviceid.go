// Package discovery implements mDNS/DNS-SD peer discovery using zeroconf,
// and manages the stable per-device identity.
package discovery

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alanwnuczko/local-mesh/internal/config"
)

const deviceIDFile = "device_id"

// LoadOrCreateDeviceID reads the stable 128-bit device ID from the config dir,
// creating and persisting a new one if absent or unreadable.
//
// The ID is generated from crypto/rand for collision-resistance without relying
// on MAC addresses (which are unreliable/privacy-sensitive across platforms).
func LoadOrCreateDeviceID() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", fmt.Errorf("config dir: %w", err)
	}
	path := filepath.Join(dir, deviceIDFile)

	// Try to read an existing ID.
	data, err := os.ReadFile(path)
	if err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) == 32 { // 16 bytes → 32 hex chars
			return id, nil
		}
		// File exists but is malformed — regenerate.
	}

	// Generate a fresh 128-bit random ID.
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate device ID: %w", err)
	}
	id := hex.EncodeToString(raw)

	if err := os.WriteFile(path, []byte(id), 0600); err != nil {
		return "", fmt.Errorf("persist device ID: %w", err)
	}
	return id, nil
}
