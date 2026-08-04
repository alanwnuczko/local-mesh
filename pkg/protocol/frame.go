package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
)

// WriteFrame writes a single length-prefixed frame to w.
//
// Frame layout (big-endian):
//
//	[FrameType: 1 byte] [Length: 4 bytes uint32 BE] [Payload: Length bytes]
//
// The write is performed as a single io.Writer call via an assembled header
// slice followed by the payload, keeping writes atomic at the framing level.
func WriteFrame(w io.Writer, frameType uint8, payload []byte) error {
	// Combine header and payload into a single buffer so the Write is atomic.
	// Two separate Write calls on a TCP connection can interleave with concurrent
	// writes from other goroutines (e.g. sendErrorFrame during an active transfer).
	buf := make([]byte, 5+len(payload))
	buf[0] = frameType
	binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
	copy(buf[5:], payload)

	if _, err := w.Write(buf); err != nil {
		return fmt.Errorf("write frame: %w", err)
	}
	return nil
}

// ReadFrame reads the next frame from r and returns its type and payload.
//
// It enforces per-frame size limits to bound allocations:
//   - Data frames (FrameChunk): payload ≤ ChunkSize
//   - Control frames (all others): payload ≤ MaxControlFrameSize
//
// Justification: capping frame length prevents a corrupt/malicious peer from
// forcing an unbounded memory allocation via a large Length field.
func ReadFrame(r io.Reader) (frameType uint8, payload []byte, err error) {
	var header [5]byte
	if _, err = io.ReadFull(r, header[:]); err != nil {
		return 0, nil, fmt.Errorf("read frame header: %w", err)
	}

	frameType = header[0]
	length := binary.BigEndian.Uint32(header[1:5])

	// Enforce per-type max length.
	var maxLen uint32
	if frameType == FrameChunk {
		maxLen = ChunkSize
	} else {
		maxLen = MaxControlFrameSize
	}
	if length > maxLen {
		return 0, nil, fmt.Errorf("frame type 0x%02x: declared length %d exceeds limit %d: %w",
			frameType, length, maxLen, ErrFrameTooLarge)
	}

	if length == 0 {
		return frameType, nil, nil
	}

	payload = make([]byte, length)
	if _, err = io.ReadFull(r, payload); err != nil {
		return 0, nil, fmt.Errorf("read frame payload: %w", err)
	}
	return frameType, payload, nil
}

// ErrFrameTooLarge is returned by ReadFrame when the declared payload length
// exceeds the per-type cap.
var ErrFrameTooLarge = fmt.Errorf("frame payload too large")
