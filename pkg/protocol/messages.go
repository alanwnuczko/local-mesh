package protocol

import (
	"encoding/json"
	"fmt"
)

// OfferMessage is sent by the sender as a FrameOffer frame. It describes the
// transfer so the receiver can show the user an accurate summary and pre-validate
// size/checksum without streaming any data first.
type OfferMessage struct {
	Version    int    `json:"Version"`
	SenderID   string `json:"SenderID"`
	SenderHost string `json:"SenderHost"`
	Name       string `json:"Name"`
	IsDir      bool   `json:"IsDir"`
	Size       int64  `json:"Size"`
	Checksum   string `json:"Checksum"` // hex SHA-256 of the full payload
	TransferID string `json:"TransferID"`
}

// DecisionMessage is sent by the receiver as a FrameDecision frame.
// ResumeOffset is reserved for the resumable-transfer stretch milestone (§2.3);
// it is always 0 in v1.
type DecisionMessage struct {
	TransferID   string `json:"TransferID"`
	Accepted     bool   `json:"Accepted"`
	Reason       string `json:"Reason,omitempty"`
	ResumeOffset int64  `json:"ResumeOffset"`
}

// CompleteMessage is sent by the sender as a FrameComplete frame after the last
// chunk has been written. BytesSent must equal OfferMessage.Size.
type CompleteMessage struct {
	TransferID string `json:"TransferID"`
	BytesSent  int64  `json:"BytesSent"`
}

// AckMessage is sent by the receiver as a FrameAck frame after verifying the
// checksum and committing the file. OK = false with Detail explains the failure.
type AckMessage struct {
	TransferID string `json:"TransferID"`
	OK         bool   `json:"OK"`
	Detail     string `json:"Detail,omitempty"`
}

// ErrorMessage can be sent by either side as a FrameError frame to abort the
// session. Code is one of the Err* constants in constants.go.
type ErrorMessage struct {
	TransferID string `json:"TransferID,omitempty"`
	Code       string `json:"Code"`
	Message    string `json:"Message"`
}

// --- marshal helpers ---

// MarshalOffer JSON-encodes an OfferMessage.
func MarshalOffer(m OfferMessage) ([]byte, error) { return json.Marshal(m) }

// MarshalDecision JSON-encodes a DecisionMessage.
func MarshalDecision(m DecisionMessage) ([]byte, error) { return json.Marshal(m) }

// MarshalComplete JSON-encodes a CompleteMessage.
func MarshalComplete(m CompleteMessage) ([]byte, error) { return json.Marshal(m) }

// MarshalAck JSON-encodes an AckMessage.
func MarshalAck(m AckMessage) ([]byte, error) { return json.Marshal(m) }

// MarshalError JSON-encodes an ErrorMessage.
func MarshalError(m ErrorMessage) ([]byte, error) { return json.Marshal(m) }

// --- unmarshal helpers ---

// UnmarshalOffer decodes an OfferMessage from JSON.
func UnmarshalOffer(data []byte) (OfferMessage, error) {
	var m OfferMessage
	return m, json.Unmarshal(data, &m)
}

// UnmarshalDecision decodes a DecisionMessage from JSON.
func UnmarshalDecision(data []byte) (DecisionMessage, error) {
	var m DecisionMessage
	return m, json.Unmarshal(data, &m)
}

// UnmarshalComplete decodes a CompleteMessage from JSON.
func UnmarshalComplete(data []byte) (CompleteMessage, error) {
	var m CompleteMessage
	return m, json.Unmarshal(data, &m)
}

// UnmarshalAck decodes an AckMessage from JSON.
func UnmarshalAck(data []byte) (AckMessage, error) {
	var m AckMessage
	return m, json.Unmarshal(data, &m)
}

// UnmarshalError decodes an ErrorMessage from JSON.
func UnmarshalError(data []byte) (ErrorMessage, error) {
	var m ErrorMessage
	return m, json.Unmarshal(data, &m)
}

// Decoded is a sum-type wrapper returned by Decode. Exactly one field is non-nil.
type Decoded struct {
	Offer    *OfferMessage
	Decision *DecisionMessage
	Complete *CompleteMessage
	Ack      *AckMessage
	Error    *ErrorMessage
	// Chunk holds raw data payload when FrameType == FrameChunk.
	Chunk []byte
}

// Decode dispatches a raw frame (type + payload) into a typed Decoded value.
// It returns an error for unknown frame types (ErrProtocol-style).
// Centralising decode keeps state machines free of JSON handling.
func Decode(frameType uint8, payload []byte) (Decoded, error) {
	switch frameType {
	case FrameOffer:
		m, err := UnmarshalOffer(payload)
		if err != nil {
			return Decoded{}, fmt.Errorf("%s: decode offer: %w", ErrProtocol, err)
		}
		return Decoded{Offer: &m}, nil

	case FrameDecision:
		m, err := UnmarshalDecision(payload)
		if err != nil {
			return Decoded{}, fmt.Errorf("%s: decode decision: %w", ErrProtocol, err)
		}
		return Decoded{Decision: &m}, nil

	case FrameChunk:
		return Decoded{Chunk: payload}, nil

	case FrameComplete:
		m, err := UnmarshalComplete(payload)
		if err != nil {
			return Decoded{}, fmt.Errorf("%s: decode complete: %w", ErrProtocol, err)
		}
		return Decoded{Complete: &m}, nil

	case FrameAck:
		m, err := UnmarshalAck(payload)
		if err != nil {
			return Decoded{}, fmt.Errorf("%s: decode ack: %w", ErrProtocol, err)
		}
		return Decoded{Ack: &m}, nil

	case FrameError:
		m, err := UnmarshalError(payload)
		if err != nil {
			return Decoded{}, fmt.Errorf("%s: decode error: %w", ErrProtocol, err)
		}
		return Decoded{Error: &m}, nil

	default:
		return Decoded{}, fmt.Errorf("%s: unknown frame type 0x%02x", ErrProtocol, frameType)
	}
}
