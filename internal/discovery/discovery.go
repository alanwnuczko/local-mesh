package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"runtime"
	"strings"
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

// Service wraps the zeroconf responder and browser.
type Service struct {
	deviceID string
	hostname string
	port     int
	registry *Registry
	events   chan Event
	server   *zeroconf.Server
	ifaces   []net.Interface
}

// New creates a Service and registers this instance via mDNS/DNS-SD.
func New(deviceID, hostname string, port int) (*Service, error) {
	registry := NewRegistry()

	txt := []string{
		"v=1",
		fmt.Sprintf("id=%s", deviceID),
		fmt.Sprintf("host=%s", hostname),
		fmt.Sprintf("os=%s", runtime.GOOS),
		fmt.Sprintf("arch=%s", runtime.GOARCH),
	}

	// Explicitly pick LAN interfaces (Wi-Fi, Ethernet, VMware Host-Only/NAT,
	// etc.) instead of passing nil. nil makes grandcat/zeroconf call
	// listMulticastInterfaces(), which only keeps FlagMulticast interfaces
	// and is applied independently by Register vs the Resolver - on multi-
	// homed Windows hosts that is a common source of "responder on VMnet1,
	// browser on Wi-Fi only" mismatches. Passing the same slice to both
	// sides forces announcements and queries onto every usable adapter,
	// including VMware Host-Only (VMnet1) and NAT (VMnet8).
	ifaces := lanInterfaces()
	logInterfaces(ifaces)

	if len(ifaces) == 0 {
		return nil, fmt.Errorf("no usable LAN interfaces for mDNS (need an Up interface with a non-link-local IPv4)")
	}

	server, err := zeroconf.Register(hostname, "_localmesh._tcp", "local.", port, txt, ifaces)
	if err != nil {
		return nil, fmt.Errorf("zeroconf register: %w", err)
	}

	slog.Info("mDNS registered",
		"host", hostname,
		"port", port,
		"id", deviceID[:8],
		"ifaces", ifaceNames(ifaces),
	)

	return &Service{
		deviceID: deviceID,
		hostname: hostname,
		port:     port,
		registry: registry,
		events:   make(chan Event, 64),
		server:   server,
		ifaces:   ifaces,
	}, nil
}

// Events returns the read-only channel of discovery events.
func (s *Service) Events() <-chan Event { return s.events }

// Registry returns the shared peer registry.
func (s *Service) Registry() *Registry { return s.registry }

// Browse starts the mDNS browser goroutine (spec §3.1 goroutine #2).
func (s *Service) Browse(ctx context.Context) {
	go s.browseLoop(ctx)
}

func (s *Service) browseLoop(ctx context.Context) {
	defer close(s.events)

	// IPv4 only: more reliable on Windows where IPv6 link-local mDNS is patchy.
	// SelectIfaces MUST use the same set as Register so browse queries leave
	// every adapter (including VMware Host-Only) rather than only the default
	// route interface.
	resolver, err := zeroconf.NewResolver(
		zeroconf.SelectIPTraffic(zeroconf.IPv4),
		zeroconf.SelectIfaces(s.ifaces),
	)
	if err != nil {
		slog.Error("zeroconf resolver failed", "err", err)
		return
	}

	entries := make(chan *zeroconf.ServiceEntry, 64)
	browseCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if err := resolver.Browse(browseCtx, "_localmesh._tcp", "local.", entries); err != nil {
		slog.Error("zeroconf browse failed", "err", err)
		return
	}
	slog.Info("mDNS browser started", "ifaces", ifaceNames(s.ifaces))

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
	if id == "" || id == s.deviceID {
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

	slog.Info("peer found", "id", id[:8], "host", peer.Hostname, "addr", peer.Addr())

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

// Shutdown sends an mDNS goodbye packet so remote peers see PeerLost quickly.
func (s *Service) Shutdown() {
	if s.server != nil {
		s.server.Shutdown()
	}
}

// lanInterfaces returns interfaces suitable for mDNS on multi-homed hosts
// (physical NICs plus VMware Host-Only / NAT virtual adapters).
//
// Selection rules:
//   - must be Up and not loopback
//   - must have at least one non-link-local IPv4 (skips APIPA 169.254/16)
//   - prefers FlagMulticast, but also accepts FlagBroadcast-only virtual NICs
//     that Windows sometimes reports without FlagMulticast
//
// Passing nil to zeroconf is NOT equivalent: its internal filter requires
// FlagMulticast strictly and is re-evaluated separately by client and server.
func lanInterfaces() []net.Interface {
	all, err := net.Interfaces()
	if err != nil {
		slog.Warn("could not enumerate interfaces", "err", err)
		return nil
	}

	var out []net.Interface
	for _, iface := range all {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// Skip point-to-point tunnels that can't carry LAN multicast usefully.
		// (Keep VMware adapters: they are broadcast/multicast ethernet-like.)
		if !hasUsableIPv4(iface) {
			continue
		}
		// grandcat joins 224.0.0.251 per interface; without multicast OR
		// broadcast the join is almost certain to fail.
		if iface.Flags&net.FlagMulticast == 0 && iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		out = append(out, iface)
	}
	return out
}

func hasUsableIPv4(iface net.Interface) bool {
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
			continue
		}
		return true
	}
	return false
}

func ifaceNames(ifaces []net.Interface) string {
	names := make([]string, 0, len(ifaces))
	for _, i := range ifaces {
		names = append(names, i.Name)
	}
	return strings.Join(names, ", ")
}

// logInterfaces logs every OS interface plus which ones were selected for mDNS.
func logInterfaces(selected []net.Interface) {
	all, err := net.Interfaces()
	if err != nil {
		slog.Warn("could not enumerate interfaces", "err", err)
		return
	}
	selectedSet := make(map[int]bool, len(selected))
	for _, s := range selected {
		selectedSet[s.Index] = true
	}
	for _, iface := range all {
		addrs, _ := iface.Addrs()
		var ipStrs []string
		for _, a := range addrs {
			ipStrs = append(ipStrs, a.String())
		}
		slog.Debug("network interface",
			"name", iface.Name,
			"flags", iface.Flags.String(),
			"addrs", strings.Join(ipStrs, ", "),
			"mdns", selectedSet[iface.Index],
		)
	}
	slog.Info("discovery: using interfaces", "ifaces", ifaceNames(selected))
}

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
