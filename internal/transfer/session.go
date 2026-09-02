// Package transfer implements the send and receive sides of the local-mesh
// wire protocol over TCP.
package transfer

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

// ErrAborted is returned when the user (or a session Handle) cancels a transfer.
var ErrAborted = errors.New("transfer cancelled")

// ProgressEvent is emitted by sender/receiver goroutines to report progress.
// Background goroutines MUST NOT touch model fields; they emit these events on
// channels which the bus forwards via program.Send - honouring the concurrency
// invariant from §0 and §3.5.
type ProgressEvent struct {
	TransferID  string
	BytesDone   int64
	BytesTotal  int64
	BytesPerSec float64
	Phase       Phase
}

// DoneEvent is emitted when a transfer completes (success or failure).
type DoneEvent struct {
	TransferID string
	Err        error  // nil on success
	SavedPath  string // set on successful receive
	Direction  Direction
}

// Phase labels the current stage of a transfer for display.
type Phase int

const (
	PhaseOffering Phase = iota
	PhaseWaiting
	PhaseTransferring
	PhaseVerifying
	PhaseDone
	PhaseFailed
)

func (p Phase) String() string {
	switch p {
	case PhaseOffering:
		return "Offering"
	case PhaseWaiting:
		return "Waiting for accept"
	case PhaseTransferring:
		return "Transferring"
	case PhaseVerifying:
		return "Verifying"
	case PhaseDone:
		return "Done"
	case PhaseFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// Direction indicates whether a session is sending or receiving.
type Direction int

const (
	DirSend Direction = iota
	DirRecv
)

// progressInterval is the minimum time between consecutive progress events.
const progressInterval = 100 * time.Millisecond

// Handle is the UI-facing abort switch for an in-flight transfer.
// Abort closes the connection (unblocking ReadFrame) after a best-effort
// FrameError/ERR_ABORT. It never touches Bubbletea Model fields.
type Handle struct {
	mu         sync.Mutex
	conn       net.Conn
	cancel     context.CancelFunc
	aborted    bool
	transferID string
}

// NewHandle returns an empty handle. SetConn / SetCancel are filled in when
// the session actually opens a connection.
func NewHandle() *Handle {
	return &Handle{}
}

// SetConn records the live connection so Abort can close it.
func (h *Handle) SetConn(c net.Conn) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conn = c
}

// SetCancel records the per-session cancel func.
func (h *Handle) SetCancel(cancel context.CancelFunc) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancel = cancel
}

// SetTransferID stores the id used in an abort error frame.
func (h *Handle) SetTransferID(id string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.transferID = id
}

// Wrap returns a connection whose writes are serialized with Abort so an
// ERR_ABORT frame cannot interleave with a data frame.
func (h *Handle) Wrap(conn net.Conn) net.Conn {
	if h == nil {
		return conn
	}
	h.SetConn(conn)
	return &lockedConn{Conn: conn, h: h}
}

// Abort cancels the session. Safe to call from Update (it only touches the
// connection and context, never Model fields). Idempotent.
func (h *Handle) Abort() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.aborted {
		return
	}
	h.aborted = true
	if h.cancel != nil {
		h.cancel()
	}
	if h.conn != nil {
		// Short write deadline: the peer is often not in Read (e.g. waiting
		// on the accept overlay), and net.Pipe has no send buffer.
		_ = h.conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		sendErrorFrame(h.conn, h.transferID, protocol.ErrAbort, "user cancelled")
		_ = h.conn.Close()
	}
}

type lockedConn struct {
	net.Conn
	h *Handle
}

func (c *lockedConn) Write(p []byte) (int, error) {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	if c.h.aborted {
		return 0, net.ErrClosed
	}
	return c.Conn.Write(p)
}

// ctxErr returns ErrAborted when ctx has been cancelled, otherwise err.
func ctxErr(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ErrAborted
	}
	return err
}

func contextDone(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// bumpIdleDeadline extends read and write deadlines so a hung peer is detected
// within idleTimeout of the last frame, not from connection start.
func bumpIdleDeadline(rw any) {
	type deadliner interface {
		SetReadDeadline(time.Time) error
		SetWriteDeadline(time.Time) error
	}
	d, ok := rw.(deadliner)
	if !ok {
		return
	}
	deadline := time.Now().Add(idleTimeout)
	_ = d.SetReadDeadline(deadline)
	_ = d.SetWriteDeadline(deadline)
}

func readFrame(r io.Reader) (uint8, []byte, error) {
	bumpIdleDeadline(r)
	return protocol.ReadFrame(r)
}

func writeFrame(w io.Writer, frameType uint8, payload []byte) error {
	bumpIdleDeadline(w)
	return protocol.WriteFrame(w, frameType, payload)
}
