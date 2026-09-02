package transfer

import (
	"context"
	"fmt"
	"net"
	"time"
)

// dialTimeout is the maximum time to wait when connecting to a peer.
// The OS default TCP timeout can be 2+ minutes on Windows (M-5); we cap it
// at 10 seconds so the UI doesn't appear frozen on unreachable peers.
const dialTimeout = 10 * time.Second

// idleTimeout is the maximum time to wait for the next frame before
// treating the connection as dead (P-2: peer crashed without FIN).
// OS TCP keepalive typically fires only after 2+ hours.
const idleTimeout = 5 * time.Minute

// Dial connects to the remote peer at addr and returns a send session.
// The caller is responsible for closing the connection.
func Dial(addr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
}

// DialAny tries each address in order and returns the first successful
// connection. Multi-homed peers announce several IPs; only one is typically
// reachable from a given interface.
func DialAny(addrs []string) (net.Conn, error) {
	if len(addrs) == 0 {
		return nil, fmt.Errorf("dial: no addresses")
	}
	var lastErr error
	for _, addr := range addrs {
		conn, err := Dial(addr)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// StartSend opens a connection to the first reachable address and starts the
// send goroutine (goroutine #5 in the spec's inventory). It returns immediately;
// results arrive on cfg.Progress and cfg.Done.
func StartSend(addrs []string, cfg SendConfig) error {
	conn, err := DialAny(addrs)
	if err != nil {
		return err
	}

	parent := cfg.Ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	cfg.Ctx = ctx

	if cfg.Handle != nil {
		cfg.Handle.SetCancel(cancel)
		if cfg.TransferID != "" {
			cfg.Handle.SetTransferID(cfg.TransferID)
		}
		cfg.Conn = cfg.Handle.Wrap(conn)
	} else {
		cfg.Conn = conn
	}

	go func() {
		defer cancel()
		defer conn.Close()
		RunSend(cfg)
	}()
	return nil
}
