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

// Dial connects to the remote peer at addr and returns a send session.
// The caller is responsible for closing the connection.
func Dial(addr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", addr, dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn, nil
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
