package discovery

import (
	"fmt"
	"net"
	"sync"
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

// Addr returns the primary "ip:port" string for dialing.
//
// On multi-homed hosts (e.g. Windows + VMware Host-Only + NAT + Wi-Fi) a peer
// announces every interface IP. The first IPv4 is often unreachable from the
// other side (Wi-Fi address seen over the Host-Only segment). Prefer an IPv4
// that shares a subnet with one of our local interfaces so the TCP dial lands
// on the shared link.
func (p Peer) Addr() string {
	if len(p.Addrs) == 0 {
		return fmt.Sprintf("unknown:%d", p.Port)
	}

	localNets := localIPv4Nets()
	for _, ip := range p.Addrs {
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		for _, n := range localNets {
			if n.Contains(ip4) {
				return fmt.Sprintf("%s:%d", ip4.String(), p.Port)
			}
		}
	}

	// Fallback: first IPv4, then first address.
	for _, ip := range p.Addrs {
		if ip4 := ip.To4(); ip4 != nil {
			return fmt.Sprintf("%s:%d", ip4.String(), p.Port)
		}
	}
	return fmt.Sprintf("[%s]:%d", p.Addrs[0].String(), p.Port)
}

// localIPv4Nets returns the IPv4 networks configured on this machine.
// Used by Peer.Addr to prefer a peer IP on a shared subnet.
func localIPv4Nets() []*net.IPNet {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var nets []*net.IPNet
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
				nets = append(nets, &net.IPNet{IP: ip4.Mask(ipnet.Mask), Mask: ipnet.Mask})
			}
		}
	}
	return nets
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
func (r *Registry) Upsert(p Peer) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
