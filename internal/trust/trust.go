// Package trust persists accepted peer device IDs (TOFU) and derives a
// short pairing code both sides can compare for a first-time transfer.
package trust

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/alanwnuczko/local-mesh/internal/config"
)

const knownFile = "known_peers.json"

// Store is a set of device IDs this machine has previously accepted.
type Store struct {
	mu   sync.Mutex
	path string
	ids  map[string]struct{}
}

// Load reads the known-peer set from the config directory.
func Load() (*Store, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	s := &Store{
		path: filepath.Join(dir, knownFile),
		ids:  make(map[string]struct{}),
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return s, nil
	}
	for _, id := range list {
		if id != "" {
			s.ids[id] = struct{}{}
		}
	}
	return s, nil
}

// Known reports whether id has been accepted before.
func (s *Store) Known(id string) bool {
	if s == nil || id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.ids[id]
	return ok
}

// Remember records id as trusted. Safe to call repeatedly.
func (s *Store) Remember(id string) {
	if s == nil || id == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.ids[id]; ok {
		return
	}
	s.ids[id] = struct{}{}
	list := make([]string, 0, len(s.ids))
	for k := range s.ids {
		list = append(list, k)
	}
	data, err := json.Marshal(list)
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, data, 0600)
}

// PairingCode returns a 4-digit code both peers compute from the same pair of
// device IDs. Order-independent so each side shows the same number.
func PairingCode(localID, remoteID string) string {
	a, b := localID, remoteID
	if a > b {
		a, b = b, a
	}
	sum := sha256.Sum256([]byte(a + "|" + b))
	n := binary.BigEndian.Uint32(sum[:4]) % 10000
	return fmt.Sprintf("%04d", n)
}
