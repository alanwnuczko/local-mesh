package protocol_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

// pipe returns a connected net.Pipe pair. Using net.Pipe exercises the exact
// ReadFrame/WriteFrame code paths without real sockets, ports, or flakiness.
func pipe(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		a.Close()
		b.Close()
	})
	return a, b
}

// --- round-trip tests for each control message type ---

func TestOfferRoundTrip(t *testing.T) {
	a, b := pipe(t)
	want := protocol.OfferMessage{
		Version:    protocol.ProtocolVersion,
		SenderID:   "aabbccdd",
		SenderHost: "testhost",
		Name:       "file.txt",
		IsDir:      false,
		Size:       12345,
		Checksum:   "deadbeef",
		TransferID: "tid-001",
	}
	payload, err := protocol.MarshalOffer(want)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- protocol.WriteFrame(a, protocol.FrameOffer, payload)
	}()

	ft, raw, err := protocol.ReadFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if ft != protocol.FrameOffer {
		t.Fatalf("frame type: got 0x%02x want 0x%02x", ft, protocol.FrameOffer)
	}
	got, err := protocol.UnmarshalOffer(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("offer mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestDecisionRoundTrip(t *testing.T) {
	a, b := pipe(t)
	want := protocol.DecisionMessage{
		TransferID:   "tid-001",
		Accepted:     true,
		ResumeOffset: 0,
	}
	payload, _ := protocol.MarshalDecision(want)

	go protocol.WriteFrame(a, protocol.FrameDecision, payload) //nolint:errcheck

	ft, raw, err := protocol.ReadFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if ft != protocol.FrameDecision {
		t.Fatalf("frame type: got 0x%02x want 0x%02x", ft, protocol.FrameDecision)
	}
	got, err := protocol.UnmarshalDecision(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("decision mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestCompleteRoundTrip(t *testing.T) {
	a, b := pipe(t)
	want := protocol.CompleteMessage{TransferID: "tid-002", BytesSent: 9999}
	payload, _ := protocol.MarshalComplete(want)
	go protocol.WriteFrame(a, protocol.FrameComplete, payload) //nolint:errcheck

	ft, raw, err := protocol.ReadFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if ft != protocol.FrameComplete {
		t.Fatalf("wrong frame type")
	}
	got, err := protocol.UnmarshalComplete(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("complete mismatch")
	}
}

func TestAckRoundTrip(t *testing.T) {
	a, b := pipe(t)
	want := protocol.AckMessage{TransferID: "tid-003", OK: true}
	payload, _ := protocol.MarshalAck(want)
	go protocol.WriteFrame(a, protocol.FrameAck, payload) //nolint:errcheck

	ft, raw, err := protocol.ReadFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if ft != protocol.FrameAck {
		t.Fatalf("wrong frame type")
	}
	got, err := protocol.UnmarshalAck(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ack mismatch")
	}
}

func TestErrorRoundTrip(t *testing.T) {
	a, b := pipe(t)
	want := protocol.ErrorMessage{TransferID: "tid-004", Code: protocol.ErrChecksum, Message: "bad hash"}
	payload, _ := protocol.MarshalError(want)
	go protocol.WriteFrame(a, protocol.FrameError, payload) //nolint:errcheck

	ft, raw, err := protocol.ReadFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if ft != protocol.FrameError {
		t.Fatalf("wrong frame type")
	}
	got, err := protocol.UnmarshalError(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("error mismatch")
	}
}

// --- data chunk streaming ---

func TestChunkStreaming(t *testing.T) {
	a, b := pipe(t)
	// Build a synthetic payload of 2.5 chunks.
	payloadSize := int(protocol.ChunkSize)*2 + protocol.ChunkSize/2
	data := make([]byte, payloadSize)
	for i := range data {
		data[i] = byte(i % 251)
	}
	wantHash := sha256.Sum256(data)
	wantChecksum := hex.EncodeToString(wantHash[:])

	go func() {
		offset := 0
		for offset < len(data) {
			end := offset + protocol.ChunkSize
			if end > len(data) {
				end = len(data)
			}
			if err := protocol.WriteFrame(a, protocol.FrameChunk, data[offset:end]); err != nil {
				return
			}
			offset = end
		}
		complete, _ := protocol.MarshalComplete(protocol.CompleteMessage{
			TransferID: "stream-test",
			BytesSent:  int64(payloadSize),
		})
		protocol.WriteFrame(a, protocol.FrameComplete, complete) //nolint:errcheck
	}()

	hasher := sha256.New()
	var received []byte
	for {
		ft, payload, err := protocol.ReadFrame(b)
		if err != nil {
			t.Fatal(err)
		}
		switch ft {
		case protocol.FrameChunk:
			received = append(received, payload...)
			hasher.Write(payload)
		case protocol.FrameComplete:
			cm, err := protocol.UnmarshalComplete(payload)
			if err != nil {
				t.Fatal(err)
			}
			if cm.BytesSent != int64(payloadSize) {
				t.Fatalf("BytesSent: got %d want %d", cm.BytesSent, payloadSize)
			}
			gotChecksum := hex.EncodeToString(hasher.Sum(nil))
			if gotChecksum != wantChecksum {
				t.Fatalf("checksum mismatch: got %s want %s", gotChecksum, wantChecksum)
			}
			if !bytes.Equal(received, data) {
				t.Fatal("assembled payload does not match original")
			}
			return
		default:
			t.Fatalf("unexpected frame type 0x%02x", ft)
		}
	}
}

// --- negative tests ---

func TestOversizedControlFrame(t *testing.T) {
	a, b := pipe(t)

	// Manually craft a frame claiming a huge payload (MaxControlFrameSize + 1).
	badLen := protocol.MaxControlFrameSize + 1
	header := [5]byte{}
	header[0] = protocol.FrameOffer
	header[1] = byte(badLen >> 24)
	header[2] = byte(badLen >> 16)
	header[3] = byte(badLen >> 8)
	header[4] = byte(badLen)

	go func() {
		a.Write(header[:]) //nolint:errcheck
		a.Close()
	}()

	_, _, err := protocol.ReadFrame(b)
	if err == nil {
		t.Fatal("expected error for oversized frame, got nil")
	}
	if !errors.Is(err, protocol.ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got: %v", err)
	}
}

func TestUnknownFrameType(t *testing.T) {
	a, b := pipe(t)
	go protocol.WriteFrame(a, 0xFF, []byte(`{}`)) //nolint:errcheck

	ft, payload, err := protocol.ReadFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	// ReadFrame itself succeeds (it doesn't know about types), but Decode must fail.
	_, decErr := protocol.Decode(ft, payload)
	if decErr == nil {
		t.Fatal("Decode: expected error for unknown frame type, got nil")
	}
}

func TestTruncatedFrame(t *testing.T) {
	a, b := pipe(t)
	go func() {
		// Write header claiming 100 bytes, then close without writing payload.
		header := [5]byte{}
		header[0] = protocol.FrameOffer
		header[4] = 100 // length = 100
		a.Write(header[:]) //nolint:errcheck
		a.Close()
	}()

	_, _, err := protocol.ReadFrame(b)
	if err == nil {
		t.Fatal("expected error on truncated payload, got nil")
	}
	// net.Pipe may return io.EOF or io.ErrUnexpectedEOF depending on platform
	// and timing; both are acceptable signals that the frame was truncated.
	if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.ErrUnexpectedEOF or io.EOF, got: %v", err)
	}
}

// TestDecode verifies the dispatch table for all known frame types.
func TestDecode(t *testing.T) {
	tests := []struct {
		name      string
		frameType uint8
		payload   []byte
		wantErr   bool
	}{
		{
			name:      "offer",
			frameType: protocol.FrameOffer,
			payload:   []byte(`{"Version":1,"SenderID":"x","SenderHost":"h","Name":"f","IsDir":false,"Size":1,"Checksum":"c","TransferID":"t"}`),
		},
		{
			name:      "decision",
			frameType: protocol.FrameDecision,
			payload:   []byte(`{"TransferID":"t","Accepted":true,"ResumeOffset":0}`),
		},
		{
			name:      "chunk",
			frameType: protocol.FrameChunk,
			payload:   []byte("raw bytes"),
		},
		{
			name:      "complete",
			frameType: protocol.FrameComplete,
			payload:   []byte(`{"TransferID":"t","BytesSent":9}`),
		},
		{
			name:      "ack",
			frameType: protocol.FrameAck,
			payload:   []byte(`{"TransferID":"t","OK":true}`),
		},
		{
			name:      "error",
			frameType: protocol.FrameError,
			payload:   []byte(`{"Code":"ERR_IO","Message":"oops"}`),
		},
		{
			name:      "unknown",
			frameType: 0xAA,
			payload:   nil,
			wantErr:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := protocol.Decode(tc.frameType, tc.payload)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
