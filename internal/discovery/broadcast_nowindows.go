//go:build !windows

package discovery

import (
	"fmt"
	"net"
	"syscall"
)

// newBroadcastSender creates a UDP socket with SO_BROADCAST set so that
// conn.WriteTo can address subnet and global broadcast IPs on Linux and macOS.
//
// The kernel rejects sends to broadcast addresses with EACCES unless the
// socket has SO_BROADCAST enabled. net.DialUDP never sets this option, so
// the previous approach silently dropped every beacon write.
func newBroadcastSender() (*net.UDPConn, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}

	rc, err := conn.SyscallConn()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("SyscallConn: %w", err)
	}

	var setSockErr error
	if ctrlErr := rc.Control(func(fd uintptr) {
		setSockErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	}); ctrlErr != nil {
		conn.Close()
		return nil, fmt.Errorf("control SO_BROADCAST: %w", ctrlErr)
	}
	if setSockErr != nil {
		conn.Close()
		return nil, fmt.Errorf("SO_BROADCAST: %w", setSockErr)
	}

	return conn, nil
}
