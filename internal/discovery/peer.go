package discovery

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// Peer represents a discovered remote instance of local-mesh.
type Peer struct {
	ID       string // stable 128-bit hex device ID (from TXT record "id")
	Hostname string // human-readable hostname (from TXT record "host")
	OS       string // GOOS value (from TXT record "os")
	Arch     string // GOARCH value (from TXT record "arch")
	Version  string // protocol version string (from TXT record "v")
	Addrs    []net.IP
	Port     int
}

// Addr returns the primary "ip:port" string for display.
// DialAddrs returns every candidate; send tries them in order.
//
// On multi-homed hosts (e.g. Windows + VMware Host-Only + NAT + Wi-Fi) a peer
// announces every interface IP. Prefer an IPv4 that shares a subnet with one
// of our local interfaces so the TCP dial lands on the shared link.
func (p Peer) Addr() string {
	addrs := p.DialAddrs()
	if len(addrs) == 0 {
		return fmt.Sprintf("unknown:%d", p.Port)
	}
	return addrs[0]
}

// DialAddrs returns candidate dial strings, preferred (same-subnet IPv4)
// first, then remaining IPv4, then IPv6. Duplicates are omitted.
func (p Peer) DialAddrs() []string {
	if len(p.Addrs) == 0 {
		return nil
	}

	var out []string
	seen := make(map[string]struct{})
	add := func(s string) {
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	localNets := localIPv4Nets()
	for _, ip := range p.Addrs {
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		for _, n := range localNets {
			if n.Contains(ip4) {
				add(formatIPPort(ip4, p.Port))
				break
			}
		}
	}
	for _, ip := range p.Addrs {
		if ip4 := ip.To4(); ip4 != nil {
			add(formatIPPort(ip4, p.Port))
		}
	}
	for _, ip := range p.Addrs {
		if ip.To4() == nil {
			add(formatIPPort(ip, p.Port))
		}
	}
	return out
}

func formatIPPort(ip net.IP, port int) string {
	if ip4 := ip.To4(); ip4 != nil {
		return fmt.Sprintf("%s:%d", ip4.String(), port)
	}
	return fmt.Sprintf("[%s]:%d", ip.String(), port)
}

func mergeIPs(a, b []net.IP) []net.IP {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]net.IP, 0, len(a)+len(b))
	for _, ip := range append(append([]net.IP{}, a...), b...) {
		if ip == nil {
			continue
		}
		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ip)
	}
	return out
}

var (
	localNetsMu      sync.Mutex
	localNetsCached  []*net.IPNet
	localNetsExpires time.Time
)

// InvalidateLocalNets drops the cached interface list so the next Addr()
// call re-enumerates (needed after VPN / adapter changes, and on Refresh).
func InvalidateLocalNets() {
	localNetsMu.Lock()
	localNetsCached = nil
	localNetsExpires = time.Time{}
	localNetsMu.Unlock()
}

func localIPv4Nets() []*net.IPNet {
	localNetsMu.Lock()
	defer localNetsMu.Unlock()
	if localNetsCached != nil && time.Now().Before(localNetsExpires) {
		return localNetsCached
	}
	var cached []*net.IPNet
	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				ipnet, ok := a.(*net.IPNet)
				if !ok {
					continue
				}
				if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
					cached = append(cached, &net.IPNet{IP: ip4.Mask(ipnet.Mask), Mask: ipnet.Mask})
				}
			}
		}
	}
	localNetsCached = cached
	localNetsExpires = time.Now().Add(5 * time.Second)
	return localNetsCached
}

// ShortID returns the first 8 characters of the device ID for display.
func (p Peer) ShortID() string {
	if len(p.ID) <= 8 {
		return p.ID
	}
	return p.ID[:8]
}

// Registry is a thread-safe map of device ID → Peer.
// It is keyed by device ID so two machines sharing a hostname are not collapsed.
//
// NOTE: Registry is intentionally NOT tied to Bubbletea state. Background
// goroutines may update it freely; the TUI reads the snapshot only when
// processing a PeerFoundMsg or PeerLostMsg inside Update.
type Registry struct {
	mu    sync.RWMutex
	peers map[string]Peer
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{peers: make(map[string]Peer)}
}

// Upsert adds or replaces the peer with the given device ID.
// Addresses from a previous observation are merged so a UDP fallback beacon
// cannot wipe mDNS's multi-address list (and vice versa).
func (r *Registry) Upsert(p Peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.peers[p.ID]; ok {
		p.Addrs = mergeIPs(existing.Addrs, p.Addrs)
		if p.Hostname == "" {
			p.Hostname = existing.Hostname
		}
		if p.OS == "" {
			p.OS = existing.OS
		}
		if p.Arch == "" {
			p.Arch = existing.Arch
		}
		if p.Port == 0 {
			p.Port = existing.Port
		}
	}
	r.peers[p.ID] = p
}

// Remove deletes the peer by device ID.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, id)
}

// Get returns a copy of the peer with the given ID plus a found flag.
func (r *Registry) Get(id string) (Peer, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.peers[id]
	return p, ok
}

// All returns a snapshot of all currently known peers.
func (r *Registry) All() []Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Peer, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, p)
	}
	return out
}
