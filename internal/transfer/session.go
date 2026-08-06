// Package transfer implements the send and receive sides of the local-mesh
// wire protocol over TCP.
package transfer

import (
	"time"
)

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
	Err        error   // nil on success
	SavedPath  string  // set on successful receive
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
