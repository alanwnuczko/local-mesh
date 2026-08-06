package transfer

import (
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

// setIdleDeadline extends the read deadline on c by idleTimeout from now.
// Called once before each blocking ReadFrame so a silently-dead peer is
// detected within idleTimeout rather than after the OS keepalive (2+ hours).
func setIdleDeadline(c net.Conn) {
	_ = c.SetReadDeadline(time.Now().Add(idleTimeout))
}

// StartSend opens a connection to addr and starts the send goroutine
// (goroutine #5 in the spec's inventory). It returns immediately; results
// arrive on cfg.Progress and cfg.Done.
func StartSend(addr string, cfg SendConfig) error {
	conn, err := Dial(addr)
	if err != nil {
		return err
	}
	cfg.Conn = conn
	go func() {
		defer conn.Close()
		RunSend(cfg)
	}()
	return nil
}
