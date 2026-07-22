package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"github.com/grandcat/zeroconf"
)

// Event kinds emitted by the browser goroutine.
type EventKind int

const (
	EventPeerFound EventKind = iota
	EventPeerLost
)

// Event is emitted by the browser when a peer is discovered or disappears.
type Event struct {
	Kind EventKind
	Peer Peer
}

// Service wraps the zeroconf responder (our announcement) and browser
// (peer discovery). It emits Events on the returned channel.
type Service struct {
	deviceID string
	hostname string
	port     int
	registry *Registry
	events   chan Event
	server   *zeroconf.Server
}

// New creates a Service and registers this instance via mDNS/DNS-SD on the
// given port. Call Browse to start peer discovery.
func New(deviceID, hostname string, port int) (*Service, error) {
	registry := NewRegistry()

	// Build TXT records per spec §2.1.
	txt := []string{
		fmt.Sprintf("v=1"),
		fmt.Sprintf("id=%s", deviceID),
		fmt.Sprintf("host=%s", hostname),
		fmt.Sprintf("os=%s", runtime.GOOS),
		fmt.Sprintf("arch=%s", runtime.GOARCH),
	}

	// Register this instance. zeroconf handles the SRV record (carrying port).
	// The service type is the project-specific constant to avoid LAN collisions.
	server, err := zeroconf.Register(hostname, "_localmesh._tcp", "local.", port, txt, nil)
	if err != nil {
		return nil, fmt.Errorf("zeroconf register: %w", err)
	}

	return &Service{
		deviceID: deviceID,
		hostname: hostname,
		port:     port,
		registry: registry,
		events:   make(chan Event, 64),
		server:   server,
	}, nil
}

// Events returns the read-only channel of discovery events.
// The bus goroutine ranges over this and calls program.Send for each event.
func (s *Service) Events() <-chan Event {
	return s.events
}

// Registry returns the shared peer registry for read-only snapshots.
func (s *Service) Registry() *Registry {
	return s.registry
}

// Browse starts the mDNS browser goroutine. It runs until ctx is cancelled,
// at which point it closes the Events channel and returns.
//
// This is goroutine #2 in the spec's goroutine inventory.
func (s *Service) Browse(ctx context.Context) {
	go s.browseLoop(ctx)
}

func (s *Service) browseLoop(ctx context.Context) {
	defer close(s.events)

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		slog.Error("zeroconf resolver", "err", err)
		return
	}

	entries := make(chan *zeroconf.ServiceEntry, 64)
	browseCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := resolver.Browse(browseCtx, "_localmesh._tcp", "local.", entries); err != nil {
		slog.Error("zeroconf browse", "err", err)
		return
	}

	// Track which IDs we've seen so we can emit PeerLost when they disappear.
	// We use a simple TTL approach: record the last-seen time and periodically
	// sweep for entries that haven't been refreshed in 2× the mDNS TTL window.
	lastSeen := make(map[string]time.Time)
	sweep := time.NewTicker(15 * time.Second)
	defer sweep.Stop()

	for {
		select {
		case entry, ok := <-entries:
			if !ok {
				return
			}
			s.handleEntry(entry, lastSeen)

		case <-sweep.C:
			s.sweepExpired(lastSeen)

		case <-ctx.Done():
			// Emit PeerLost for all known peers on shutdown.
			for _, p := range s.registry.All() {
				s.registry.Remove(p.ID)
				select {
				case s.events <- Event{Kind: EventPeerLost, Peer: p}:
				default:
				}
			}
			return
		}
	}
}

func (s *Service) handleEntry(entry *zeroconf.ServiceEntry, lastSeen map[string]time.Time) {
	txt := parseTXT(entry.Text)

	id := txt["id"]
	if id == "" {
		return // not a local-mesh peer
	}

	// Skip our own announcement.
	if id == s.deviceID {
		return
	}

	peer := Peer{
		ID:       id,
		Hostname: txt["host"],
		OS:       txt["os"],
		Arch:     txt["arch"],
		Version:  txt["v"],
		Addrs:    append(entry.AddrIPv4, entry.AddrIPv6...),
		Port:     entry.Port,
	}
	if peer.Hostname == "" {
		peer.Hostname = entry.HostName
	}

	lastSeen[id] = time.Now()
	s.registry.Upsert(peer)
	s.events <- Event{Kind: EventPeerFound, Peer: peer}
}

func (s *Service) sweepExpired(lastSeen map[string]time.Time) {
	const expiry = 120 * time.Second
	now := time.Now()
	for id, t := range lastSeen {
		if now.Sub(t) > expiry {
			if peer, ok := s.registry.Get(id); ok {
				s.registry.Remove(id)
				delete(lastSeen, id)
				select {
				case s.events <- Event{Kind: EventPeerLost, Peer: peer}:
				default:
				}
			}
		}
	}
}

// Shutdown gracefully stops the mDNS responder (sends a goodbye packet so
// other peers receive a PeerLost quickly instead of waiting for TTL expiry).
func (s *Service) Shutdown() {
	if s.server != nil {
		s.server.Shutdown()
	}
}

// parseTXT converts the raw TXT record strings ("key=value") into a map.
func parseTXT(records []string) map[string]string {
	m := make(map[string]string, len(records))
	for _, r := range records {
		for i, c := range r {
			if c == '=' {
				m[r[:i]] = r[i+1:]
				break
			}
		}
	}
	return m
}
