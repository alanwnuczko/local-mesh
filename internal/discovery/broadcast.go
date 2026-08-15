package discovery

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"runtime"
	"time"
)

const fallbackPort = 53333

type fallbackBeacon struct {
	ID       string `json:"id"`
	Hostname string `json:"host"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Version  string `json:"v"`
	Port     int    `json:"port"`
}

// startFallbackBroadcast begins sending periodic UDP beacons and listening for them.
func (s *Service) startFallbackBroadcast(ctx context.Context) {
	go s.fallbackListenLoop(ctx)
	go s.fallbackSendLoop(ctx)
}

func (s *Service) fallbackListenLoop(ctx context.Context) {
	addr := &net.UDPAddr{
		Port: fallbackPort,
		IP:   net.ParseIP("0.0.0.0"),
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		slog.Warn("fallback listener failed to bind", "err", err)
		return
	}
	// M-2: use a derived context whose cancel is called on return, so the
	// watcher goroutine exits immediately when this function returns for any
	// reason (not just when the parent ctx is cancelled).
	innerCtx, innerCancel := context.WithCancel(ctx)
	// M-3: single-owner close — conn.Close is called only in the watcher,
	// which is triggered by innerCancel (deferred below). The previous code
	// had both a defer conn.Close() and a watcher conn.Close() running
	// concurrently — a double-close.
	defer innerCancel()

	go func() {
		<-innerCtx.Done()
		conn.Close()
	}()

	buf := make([]byte, 1024)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Debug("fallback read error", "err", err)
			continue
		}

		var b fallbackBeacon
		if err := json.Unmarshal(buf[:n], &b); err != nil {
			continue
		}

		if b.ID == "" || b.ID == s.deviceID {
			continue
		}

		peer := Peer{
			ID:       b.ID,
			Hostname: b.Hostname,
			OS:       b.OS,
			Arch:     b.Arch,
			Version:  b.Version,
			Addrs:    []net.IP{remoteAddr.IP},
			Port:     b.Port,
		}

		s.handleFallbackPeer(peer)
	}
}

func (s *Service) fallbackSendLoop(ctx context.Context) {
	// Collect all subnet broadcast addresses from selected LAN interfaces.
	var targets []*net.UDPAddr
	for _, iface := range s.ifaces {
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			// L-1: Go can return 16-byte masks for IPv4-mapped IPv6 addresses.
			// Normalise to a 4-byte mask before indexing.
			mask4 := ipnet.Mask
			if len(mask4) == 16 {
				mask4 = mask4[12:]
			}
			if len(mask4) != 4 {
				continue
			}
			// Calculate broadcast address: IP | ^Mask
			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bcast[i] = ip4[i] | ^mask4[i]
			}
			targets = append(targets, &net.UDPAddr{IP: bcast, Port: fallbackPort})
			slog.Debug("fallback: will broadcast to subnet", "addr", bcast, "iface", iface.Name)
		}
	}
	// Always include the global limited broadcast as a final fallback.
	targets = append(targets, &net.UDPAddr{IP: net.IPv4bcast, Port: fallbackPort})

	// Create a single UDP socket with SO_BROADCAST enabled.
	// Without SO_BROADCAST both Linux (EACCES) and Windows (WSAEACCES) silently
	// reject sends to broadcast addresses. The previous net.DialUDP approach
	// never set SO_BROADCAST, so every c.Write(data) failed silently, making
	// UDP-based peer discovery completely non-functional on all platforms.
	conn, err := newBroadcastSender()
	if err != nil {
		slog.Warn("fallback: UDP broadcast discovery disabled (cannot create SO_BROADCAST socket)", "err", err)
		return
	}
	defer conn.Close()

	beacon := fallbackBeacon{
		ID:       s.deviceID,
		Hostname: s.hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Version:  "1",
		Port:     s.port,
	}
	data, _ := json.Marshal(beacon)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	sendAll := func() {
		for _, target := range targets {
			if _, err := conn.WriteTo(data, target); err != nil {
				slog.Debug("fallback: broadcast send failed", "target", target.IP, "err", err)
			}
		}
	}

	// Send an initial beacon immediately.
	sendAll()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sendAll()
		case <-s.refreshBeacon:
			// Manual refresh (r key): fire beacons right away so peers on the
			// UDP fallback path reappear without waiting for the 3s ticker.
			sendAll()
			slog.Debug("fallback: immediate beacon sent (manual refresh)")
		}
	}
}

func (s *Service) handleFallbackPeer(peer Peer) {
	// Keep lastSeen in sync so the shared expiry sweep covers UDP peers too.
	s.lastSeenMu.Lock()
	s.lastSeen[peer.ID] = time.Now()
	s.lastSeenMu.Unlock()

	s.registry.Upsert(peer)

	// peerlist handles idempotency via UpsertPeer.
	select {
	case s.events <- Event{Kind: EventPeerFound, Peer: peer}:
	default:
	}
	slog.Debug("peer found via UDP broadcast", "id", peer.ID[:8], "host", peer.Hostname, "ip", peer.Addrs[0])
}
