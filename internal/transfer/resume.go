package transfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/alanwnuczko/local-mesh/internal/config"
)

const (
	resumeRecvFile = "resume-recv.json"
	resumeSendFile = "resume-send.json"
)

// Partial is a partially received payload that can be resumed.
type Partial struct {
	TransferID string `json:"id"`
	Checksum   string `json:"checksum"`
	Size       int64  `json:"size"`
	Name       string `json:"name"`
	IsDir      bool   `json:"isDir"`
	TmpPath    string `json:"tmp"`
}

type recvIndex struct {
	mu    sync.Mutex
	path  string
	items map[string]Partial
}

type sendIndex struct {
	mu   sync.Mutex
	path string
	ids  map[string]string // checksum -> transferID
}

func recvStore() *recvIndex {
	dir, err := config.ConfigDir()
	if err != nil {
		return &recvIndex{items: map[string]Partial{}}
	}
	return &recvIndex{path: filepath.Join(dir, resumeRecvFile), items: map[string]Partial{}}
}

func sendStore() *sendIndex {
	dir, err := config.ConfigDir()
	if err != nil {
		return &sendIndex{ids: map[string]string{}}
	}
	return &sendIndex{path: filepath.Join(dir, resumeSendFile), ids: map[string]string{}}
}

var (
	storeMu sync.Mutex
	recvIdx *recvIndex
	sendIdx *sendIndex
)

func defaultRecv() *recvIndex {
	storeMu.Lock()
	defer storeMu.Unlock()
	if recvIdx == nil {
		recvIdx = recvStore()
		recvIdx.load()
	}
	return recvIdx
}

func defaultSend() *sendIndex {
	storeMu.Lock()
	defer storeMu.Unlock()
	if sendIdx == nil {
		sendIdx = sendStore()
		sendIdx.load()
	}
	return sendIdx
}

// UseStateDir points resume files at dir (tests).
func UseStateDir(dir string) {
	storeMu.Lock()
	defer storeMu.Unlock()
	recvIdx = &recvIndex{path: filepath.Join(dir, resumeRecvFile), items: map[string]Partial{}}
	sendIdx = &sendIndex{path: filepath.Join(dir, resumeSendFile), ids: map[string]string{}}
}

func (r *recvIndex) load() {
	if r.path == "" {
		return
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	var items []Partial
	if json.Unmarshal(data, &items) != nil {
		return
	}
	r.items = make(map[string]Partial, len(items))
	for _, p := range items {
		r.items[p.TransferID] = p
	}
}

func (r *recvIndex) save() {
	if r.path == "" {
		return
	}
	items := make([]Partial, 0, len(r.items))
	for _, p := range r.items {
		items = append(items, p)
	}
	data, err := json.Marshal(items)
	if err != nil {
		return
	}
	_ = os.WriteFile(r.path, data, 0600)
}

func (s *sendIndex) load() {
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.ids)
	if s.ids == nil {
		s.ids = map[string]string{}
	}
}

func (s *sendIndex) save() {
	if s.path == "" {
		return
	}
	data, err := json.Marshal(s.ids)
	if err != nil {
		return
	}
	_ = os.WriteFile(s.path, data, 0600)
}

// LookupPartial returns a matching incomplete receive for this offer, and the
// number of bytes already on disk.
func LookupPartial(transferID, checksum string, size int64, isDir bool) (tmpPath string, offset int64, ok bool) {
	r := defaultRecv()
	r.mu.Lock()
	defer r.mu.Unlock()
	p, found := r.items[transferID]
	if !found || p.Checksum != checksum || p.Size != size || p.IsDir != isDir {
		return "", 0, false
	}
	fi, err := os.Stat(p.TmpPath)
	if err != nil || fi.Size() <= 0 || fi.Size() >= size {
		delete(r.items, transferID)
		r.save()
		return "", 0, false
	}
	return p.TmpPath, fi.Size(), true
}

// SavePartial records an interrupted receive so it can be resumed.
func SavePartial(p Partial) {
	if p.TransferID == "" || p.TmpPath == "" {
		return
	}
	r := defaultRecv()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.items == nil {
		r.items = map[string]Partial{}
	}
	r.items[p.TransferID] = p
	r.save()
}

// ClearPartial removes resume state for a finished or failed transfer.
func ClearPartial(transferID string) {
	r := defaultRecv()
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, transferID)
	r.save()
}

// TransferIDForChecksum reuses the ID last used to send this payload so a
// receiver can resume. Generates and stores a new ID if none exists.
func TransferIDForChecksum(checksum string) string {
	if checksum == "" {
		return NewTransferID()
	}
	s := defaultSend()
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.ids[checksum]; ok && id != "" {
		return id
	}
	id := NewTransferID()
	if s.ids == nil {
		s.ids = map[string]string{}
	}
	s.ids[checksum] = id
	s.save()
	return id
}

// ForgetSendID drops a completed send's reusable transfer ID.
func ForgetSendID(checksum string) {
	s := defaultSend()
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.ids, checksum)
	s.save()
}
