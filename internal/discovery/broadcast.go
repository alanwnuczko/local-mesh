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
	defer conn.Close()

	go func() {
		<-ctx.Done()
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
	// Find all subnet broadcast addresses for our selected interfaces.
	var conns []*net.UDPConn
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
			// Calculate broadcast address: IP | ^Mask
			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bcast[i] = ip4[i] | ^ipnet.Mask[i]
			}
			
			conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{
				IP:   bcast,
				Port: fallbackPort,
			})
			if err == nil {
				conns = append(conns, conn)
			}
		}
	}

	// Also add the global broadcast just in case
	if globalConn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4bcast, Port: fallbackPort}); err == nil {
		conns = append(conns, globalConn)
	}

	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

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
		for _, c := range conns {
			c.Write(data)
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
