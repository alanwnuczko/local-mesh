package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/alanwnuczko/local-mesh/internal/config"
	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

// OfferDecider is called by the receiver when an offer arrives. It must block
// until the UI (or auto-logic) provides a DecisionMessage, or until ctx is
// cancelled. On cancellation it must return a rejection immediately so that
// RunReceive can exit and the connection can be closed cleanly.
// The channel-based approach (§3.3) keeps the blocked goroutine and the UI
// decoupled while preserving the "mutate only in Update" rule.
type OfferDecider func(ctx context.Context, offer protocol.OfferMessage) protocol.DecisionMessage

// RecvConfig holds everything the receive goroutine needs.
type RecvConfig struct {
	Ctx      context.Context // governs the lifetime of this receive session
	Conn     io.ReadWriter
	Decider  OfferDecider
	Progress chan<- ProgressEvent
	Done     chan<- DoneEvent
}

// RunReceive executes the full receive-side state machine on the given
// connection. It should be called in its own goroutine.
//
// Background goroutines MUST NOT touch Model fields (§3.5).
func RunReceive(cfg RecvConfig) {
	var (
		transferID string
		saveErr    error
		savedPath  string
	)

	defer func() {
		cfg.Done <- DoneEvent{
			TransferID: transferID,
			Err:        saveErr,
			SavedPath:  savedPath,
			Direction:  DirRecv,
		}
	}()

	// Phase 1: read FrameOffer.
	ft, raw, err := protocol.ReadFrame(cfg.Conn)
	if err != nil {
		saveErr = fmt.Errorf("read offer: %w", err)
		return
	}
	if ft != protocol.FrameOffer {
		sendErrorFrame(cfg.Conn, "", protocol.ErrProtocol, "expected FrameOffer")
		saveErr = fmt.Errorf("%s: expected FrameOffer got 0x%02x", protocol.ErrProtocol, ft)
		return
	}
	offer, err := protocol.UnmarshalOffer(raw)
	if err != nil {
		sendErrorFrame(cfg.Conn, "", protocol.ErrProtocol, "malformed offer")
		saveErr = fmt.Errorf("unmarshal offer: %w", err)
		return
	}
	transferID = offer.TransferID

	// Version check.
	if offer.Version != protocol.ProtocolVersion {
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrVersionMismatch,
			fmt.Sprintf("expected version %d got %d", protocol.ProtocolVersion, offer.Version))
		saveErr = fmt.Errorf("version mismatch: got %d want %d", offer.Version, protocol.ProtocolVersion)
		return
	}

	// Phase 2: ask the decider (blocks until UI responds via reply channel, or
	// until cfg.Ctx is cancelled - e.g. the user quits while the overlay is up).
	decision := cfg.Decider(cfg.Ctx, offer)

	// Send FrameDecision.
	decPayload, _ := protocol.MarshalDecision(decision)
	if err := protocol.WriteFrame(cfg.Conn, protocol.FrameDecision, decPayload); err != nil {
		saveErr = fmt.Errorf("write decision: %w", err)
		return
	}
	if !decision.Accepted {
		saveErr = fmt.Errorf("transfer rejected: %s", decision.Reason)
		return
	}

	// Phase 3: stream chunks into memory buffer + hasher.
	cfg.Progress <- ProgressEvent{TransferID: transferID, Phase: PhaseTransferring, BytesTotal: offer.Size}

	hasher := sha256.New()
	// Use a pipe to feed the chunk frames into either io.Copy or UntarFolder.
	pr, pw := io.Pipe()

	// Writer goroutine: reads FrameChunk / FrameComplete frames, writes raw
	// bytes into the pipe, closes when FrameComplete is received.
	var (
		frameErr  error
		localRecvd atomic.Int64 // C-2: count bytes locally, not from sender's claim
	)
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		lastEmit := time.Now()
		lastBytes := int64(0)

		for {
			ftype, payload, err := protocol.ReadFrame(cfg.Conn)
			if err != nil {
				pw.CloseWithError(fmt.Errorf("%s: read frame: %w", protocol.ErrIO, err))
				frameErr = err
				return
			}
			switch ftype {
			case protocol.FrameChunk:
				if _, err := pw.Write(payload); err != nil {
					// C-2 / M-4: close the pipe so the reader doesn't hang.
					pw.CloseWithError(err)
					frameErr = err
					return
				}
				recvd := localRecvd.Add(int64(len(payload)))

				if time.Since(lastEmit) >= 100*time.Millisecond {
					elapsed := time.Since(lastEmit).Seconds()
					bps := float64(recvd-lastBytes) / elapsed
					cfg.Progress <- ProgressEvent{
						TransferID:  transferID,
						BytesDone:   recvd,
						BytesTotal:  offer.Size,
						BytesPerSec: bps,
						Phase:       PhaseTransferring,
					}
					lastEmit = time.Now()
					lastBytes = recvd
				}

			case protocol.FrameComplete:
				_, err := protocol.UnmarshalComplete(payload)
				if err != nil {
					pw.CloseWithError(err)
					frameErr = err
					return
				}
				// C-2: ignore sender's self-reported BytesSent; use localRecvd instead.
				pw.Close()
				return

			case protocol.FrameError:
				em, _ := protocol.UnmarshalError(payload)
				pw.CloseWithError(fmt.Errorf("sender error: %s: %s", em.Code, em.Message))
				frameErr = fmt.Errorf("sender error: %s: %s", em.Code, em.Message)
				return

			default:
				pw.CloseWithError(fmt.Errorf("%s: unexpected frame 0x%02x", protocol.ErrProtocol, ftype))
				frameErr = fmt.Errorf("unexpected frame 0x%02x", ftype)
				return
			}
		}
	}()

	// Phase 4: consume the pipe - either write to file or untar.
	// C-4: do NOT emit PhaseVerifying here; only emit it after the goroutine
	// finishes and all bytes have been written to disk.

	downloadsDir, err := config.DownloadsDir()
	if err != nil {
		pw.CloseWithError(err)
		saveErr = fmt.Errorf("downloads dir: %w", err)
		<-doneCh
		return
	}

	// Tee the pipe into the SHA-256 hasher while writing to disk.
	tee := io.TeeReader(pr, hasher)

	var commitErr error
	if offer.IsDir {
		// Untar into a uniquely-named subdirectory.
		destDir := config.UniqueDestPath(downloadsDir, offer.Name)
		if mkErr := os.MkdirAll(destDir, 0755); mkErr != nil {
			commitErr = mkErr
		} else {
			commitErr = UntarFolder(tee, destDir)
			if commitErr == nil {
				savedPath = destDir
			} else {
				// Remove partial extraction.
				os.RemoveAll(destDir)
			}
		}
	} else {
		// Write to a uniquely-named file via a temp file, then rename atomically.
		destPath := config.UniqueDestPath(downloadsDir, offer.Name)
		tmp, tmpErr := os.CreateTemp(downloadsDir, "lm-recv-*")
		if tmpErr != nil {
			commitErr = tmpErr
		} else {
			// Drain tee into the temp file (tee also feeds the hasher).
			if _, copyErr := io.Copy(tmp, tee); copyErr != nil {
				tmp.Close()
				os.Remove(tmp.Name())
				commitErr = copyErr
			} else {
				tmp.Close()
				// We'll rename after checksum verification below.
				defer func() {
					if commitErr != nil {
						os.Remove(tmp.Name())
					}
				}()
				savedPath = tmp.Name()
				_ = destPath // will use below after verify
				// Rename deferred below after checksum check.
				defer func(tp, dp string) {
					if commitErr == nil {
						if renErr := os.Rename(tp, dp); renErr != nil {
							commitErr = renErr
							saveErr = renErr
							savedPath = ""
						} else {
							savedPath = dp
						}
					}
				}(tmp.Name(), destPath)
			}
		}
	}

	// Drain any remaining bytes from tee to ensure the hasher sees everything.
	if commitErr == nil {
		io.Copy(io.Discard, tee) //nolint:errcheck
	}

	// Wait for the frame reader goroutine to finish.
	<-doneCh
	if frameErr != nil && commitErr == nil {
		commitErr = frameErr
	}

	if commitErr != nil {
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrIO, commitErr.Error())
		saveErr = commitErr
		return
	}

	// Phase 5: verify checksum + byte count.
	// C-4: PhaseVerifying is emitted here, after all data has been read.
	// C-2: Use locally counted bytes, not sender's self-reported BytesSent.
	cfg.Progress <- ProgressEvent{TransferID: transferID, Phase: PhaseVerifying, BytesDone: localRecvd.Load(), BytesTotal: offer.Size}

	gotChecksum := hex.EncodeToString(hasher.Sum(nil))
	if gotChecksum != offer.Checksum || localRecvd.Load() != offer.Size {
		detail := fmt.Sprintf("checksum mismatch: got %s want %s", gotChecksum, offer.Checksum)
		if localRecvd.Load() != offer.Size {
			detail = fmt.Sprintf("size mismatch: received %d want %d", localRecvd.Load(), offer.Size)
		}
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrChecksum, detail)
		// Remove the partial/wrong file.
		if offer.IsDir {
			os.RemoveAll(savedPath)
		} else {
			os.Remove(savedPath)
		}
		savedPath = ""
		saveErr = fmt.Errorf("%s: %s", protocol.ErrChecksum, detail)
		return
	}

	// Phase 6: send FrameAck.
	ackPayload, _ := protocol.MarshalAck(protocol.AckMessage{
		TransferID: transferID,
		OK:         true,
	})
	if err := protocol.WriteFrame(cfg.Conn, protocol.FrameAck, ackPayload); err != nil {
		saveErr = fmt.Errorf("write ack: %w", err)
		return
	}

	cfg.Progress <- ProgressEvent{TransferID: transferID, Phase: PhaseDone, BytesDone: localRecvd.Load(), BytesTotal: offer.Size}
}

// sendErrorFrame is a best-effort helper to notify the peer of an error.
func sendErrorFrame(w io.Writer, transferID, code, msg string) {
	payload, _ := protocol.MarshalError(protocol.ErrorMessage{
		TransferID: transferID,
		Code:       code,
		Message:    msg,
	})
	protocol.WriteFrame(w, protocol.FrameError, payload) //nolint:errcheck
}

// AutoAcceptDecider always accepts offers. Used during headless testing (M3).
func AutoAcceptDecider(_ context.Context, offer protocol.OfferMessage) protocol.DecisionMessage {
	return protocol.DecisionMessage{
		TransferID: offer.TransferID,
		Accepted:   true,
	}
}

// ChannelDecider creates an OfferDecider that sends the offer on offerCh and
// blocks waiting for a reply on the returned reply channel. The caller (bus)
// forwards offerCh entries to the UI via program.Send(IncomingOfferMsg{...}).
//
// If ctx is cancelled before the UI replies (e.g. the receiver process exits
// while the accept/reject overlay is displayed), ChannelDecider returns an
// immediate rejection instead of blocking forever. Without this, the receive
// goroutine would leak, keeping the TCP connection half-open until the OS
// resets it - which produces the sender-side "wsarecv: connection forcibly
// closed" error.
func ChannelDecider(offerCh chan<- OfferWithReply) OfferDecider {
	return func(ctx context.Context, offer protocol.OfferMessage) protocol.DecisionMessage {
		reply := make(chan protocol.DecisionMessage, 1)
		offerCh <- OfferWithReply{Offer: offer, Reply: reply}
		select {
		case dec := <-reply:
			return dec
		case <-ctx.Done():
			// Receiver is shutting down - reject so RunReceive exits promptly
			// and the deferred conn.Close() fires, sending a clean TCP FIN
			// instead of leaving the socket half-open until the OS RSTs it.
			return protocol.DecisionMessage{
				TransferID: offer.TransferID,
				Accepted:   false,
				Reason:     protocol.ErrBusy,
			}
		}
	}
}

// OfferWithReply bundles an incoming offer with the channel to send the
// decision back on. This is forwarded to the UI as IncomingOfferMsg.
type OfferWithReply struct {
	Offer protocol.OfferMessage
	Reply chan<- protocol.DecisionMessage
}

// checksumOf is a test helper that computes the hex SHA-256 of a byte slice.
func checksumOf(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ensure checksumOf is used (it's a test helper, suppress unused warning).
var _ = bytes.Compare
var _ = checksumOf
