package transfer

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

// Server is the TCP listener for incoming transfers.
// It is goroutine #3 in the spec's goroutine inventory.
type Server struct {
	listener net.Listener
	progress chan<- ProgressEvent
	done     chan<- DoneEvent
	offers   chan<- OfferWithReply
	// sem limits concurrent receive sessions (H-4: DoS prevention).
	sem chan struct{}
}

const maxConcurrentReceives = 5

// NewServer creates a TCP listener bound to addr (use ":0" for OS-assigned port).
func NewServer(addr string, offerCh chan<- OfferWithReply, progress chan<- ProgressEvent, done chan<- DoneEvent) (*Server, error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Server{
		listener: l,
		progress: progress,
		done:     done,
		offers:   offerCh,
		sem:      make(chan struct{}, maxConcurrentReceives),
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
			select {
			case <-ctx.Done():
				return
			default:
				slog.Error("server accept", "err", err)
				return
			}
		}
		select {
		case s.sem <- struct{}{}:
		default:
			slog.Warn("server: max concurrent receives reached, dropping connection")
			go rejectBusy(conn)
			continue
		}
		go func(c net.Conn) {
			defer func() { <-s.sem }()
			defer c.Close()

			connCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			h := NewHandle()
			h.SetCancel(cancel)
			wrapped := h.Wrap(c)

			RunReceive(RecvConfig{
				Ctx:      connCtx,
				Conn:     wrapped,
				Handle:   h,
				Decider:  ChannelDecider(s.offers, h),
				Progress: s.progress,
				Done:     s.done,
			})
		}(conn)
	}
}

func rejectBusy(c net.Conn) {
	defer c.Close()
	_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
	sendErrorFrame(c, "", protocol.ErrBusy, "server at capacity")
}
