package transfer

import (
	"context"
	"log/slog"
	"net"
)

// Server is the TCP listener for incoming transfers.
// It is goroutine #3 in the spec's goroutine inventory.
type Server struct {
	listener net.Listener
	decider  OfferDecider
	progress chan<- ProgressEvent
	done     chan<- DoneEvent
	offers   chan<- OfferWithReply
}

// NewServer creates a TCP listener bound to addr (use ":0" for OS-assigned port).
func NewServer(addr string, offerCh chan<- OfferWithReply, progress chan<- ProgressEvent, done chan<- DoneEvent) (*Server, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Server{
		listener: l,
		decider:  ChannelDecider(offerCh),
		progress: progress,
		done:     done,
		offers:   offerCh,
	}, nil
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

// Port returns the TCP port the server is listening on.
func (s *Server) Port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// Serve runs the Accept loop until ctx is cancelled.
// For each accepted connection it spawns a per-connection receive goroutine
// (goroutine #4 in the spec's goroutine inventory).
func (s *Server) Serve(ctx context.Context) {
	go func() {
		<-ctx.Done()
		s.listener.Close()
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if the error is due to the listener being closed.
			select {
			case <-ctx.Done():
				return
			default:
				slog.Error("server accept", "err", err)
				return
			}
		}
		// Per-connection receive goroutine (spec §3.1 #4).
		go func(c net.Conn) {
			defer c.Close()
			RunReceive(RecvConfig{
				Ctx:      ctx,
				Conn:     c,
				Decider:  s.decider,
				Progress: s.progress,
				Done:     s.done,
			})
		}(conn)
	}
}
