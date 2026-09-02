package protocol_test

import (
	"strings"
	"testing"

	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

func validOffer() protocol.OfferMessage {
	return protocol.OfferMessage{
		Version:    protocol.ProtocolVersion,
		SenderID:   "aabb",
		SenderHost: "host",
		Name:       "file.txt",
		Size:       10,
		Checksum:   strings.Repeat("ab", 32),
		TransferID: "tid-001",
	}
}

func TestValidateOfferOK(t *testing.T) {
	if err := protocol.ValidateOffer(validOffer()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateOfferRejects(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*protocol.OfferMessage)
	}{
		{"empty id", func(o *protocol.OfferMessage) { o.TransferID = "" }},
		{"empty name", func(o *protocol.OfferMessage) { o.Name = "  " }},
		{"negative size", func(o *protocol.OfferMessage) { o.Size = -1 }},
		{"huge size", func(o *protocol.OfferMessage) { o.Size = protocol.MaxOfferBytes + 1 }},
		{"empty checksum", func(o *protocol.OfferMessage) { o.Checksum = "" }},
		{"short checksum", func(o *protocol.OfferMessage) { o.Checksum = "abcd" }},
		{"non-hex checksum", func(o *protocol.OfferMessage) { o.Checksum = strings.Repeat("zz", 32) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := validOffer()
			tc.mut(&o)
			if err := protocol.ValidateOffer(o); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
