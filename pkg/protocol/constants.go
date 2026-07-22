// Package protocol defines the wire protocol constants, frame types, and message
// structures for local-mesh. It is intentionally dependency-light so it can be
// tested in isolation and potentially reused outside the TUI application.
package protocol

// ProtocolVersion is the current wire protocol version. Receivers reject
// offers whose Version field does not match this value.
const ProtocolVersion = 1

// ServiceName is the DNS-SD service type used for mDNS discovery.
// A private, project-specific service type avoids colliding with standard
// services on the LAN.
const ServiceName = "_localmesh._tcp"

// ChunkSize is the payload size of each FrameChunk frame (64 KiB).
// Large enough to amortise syscall/frame overhead; small enough to keep
// progress updates smooth and memory bounded.
const ChunkSize = 64 * 1024

// MaxControlFrameSize caps the payload length of non-data frames (1 MiB).
// Prevents an unbounded allocation if a corrupt or malicious peer sends a
// huge Length field on a control frame.
const MaxControlFrameSize = 1 * 1024 * 1024

// Frame type constants - the FrameType byte that precedes every frame.
const (
	FrameOffer    uint8 = 0x01 // sender → receiver: transfer offer (JSON)
	FrameDecision uint8 = 0x02 // receiver → sender: accept/reject  (JSON)
	FrameChunk    uint8 = 0x03 // sender → receiver: raw file bytes
	FrameComplete uint8 = 0x04 // sender → receiver: end of stream   (JSON)
	FrameAck      uint8 = 0x05 // receiver → sender: commit result    (JSON)
	FrameError    uint8 = 0x06 // either direction: abort             (JSON)
)

// Error code constants used in ErrorMessage.Code. Machine-readable so the UI
// can display specific messages and tests can assert on failure modes.
const (
	ErrVersionMismatch = "ERR_VERSION_MISMATCH"
	ErrBusy            = "ERR_BUSY"
	ErrRejected        = "ERR_REJECTED"
	ErrIO              = "ERR_IO"
	ErrChecksum        = "ERR_CHECKSUM"
	ErrProtocol        = "ERR_PROTOCOL" // malformed frame or unexpected frame type
	ErrAbort           = "ERR_ABORT"    // user cancelled
)

// ProgressInterval is the minimum duration between progress messages emitted
// by transfer goroutines to avoid flooding the Bubbletea Update loop.
const ProgressInterval = 100 // milliseconds
