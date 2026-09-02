package protocol

import (
	"fmt"
	"strings"
)

// MaxOfferBytes is a hard upper bound on OfferMessage.Size. Transfers larger
// than this are rejected before the UI is involved.
const MaxOfferBytes int64 = 5 << 40 // 5 TiB

// ValidateOffer checks that an OfferMessage is well-formed enough to show the
// user and to drive a receive session. It does not check disk space.
func ValidateOffer(o OfferMessage) error {
	if o.TransferID == "" {
		return fmt.Errorf("%s: empty TransferID", ErrProtocol)
	}
	if len(o.TransferID) > 128 {
		return fmt.Errorf("%s: TransferID too long", ErrProtocol)
	}
	if strings.TrimSpace(o.Name) == "" {
		return fmt.Errorf("%s: empty Name", ErrProtocol)
	}
	if len(o.Name) > 255 {
		return fmt.Errorf("%s: Name too long", ErrProtocol)
	}
	if o.Size < 0 {
		return fmt.Errorf("%s: negative Size", ErrProtocol)
	}
	if o.Size > MaxOfferBytes {
		return fmt.Errorf("%s: Size %d exceeds limit %d", ErrProtocol, o.Size, MaxOfferBytes)
	}
	if o.Checksum == "" {
		return fmt.Errorf("%s: empty Checksum", ErrProtocol)
	}
	if len(o.Checksum) != 64 || !isHex(o.Checksum) {
		return fmt.Errorf("%s: Checksum must be 64 hex characters", ErrProtocol)
	}
	return nil
}

func isHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
