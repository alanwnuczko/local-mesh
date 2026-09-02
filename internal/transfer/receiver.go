package transfer

import (
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
type OfferDecider func(ctx context.Context, offer protocol.OfferMessage) protocol.DecisionMessage

// RecvConfig holds everything the receive goroutine needs.
type RecvConfig struct {
	Ctx      context.Context
	Conn     io.ReadWriter
	Handle   *Handle
	Decider  OfferDecider
	Progress chan<- ProgressEvent
	Done     chan<- DoneEvent
}

// RunReceive executes the full receive-side state machine on the given
// connection. It should be called in its own goroutine.
//
// Background goroutines MUST NOT touch Model fields (§3.5).
func RunReceive(cfg RecvConfig) {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	var (
		transferID string
		saveErr    error
		savedPath  string
	)

	defer func() {
		if cfg.Done == nil {
			return
		}
		cfg.Done <- DoneEvent{
			TransferID: transferID,
			Err:        ctxErr(ctx, saveErr),
			SavedPath:  savedPath,
			Direction:  DirRecv,
		}
	}()

	ft, raw, err := readFrame(cfg.Conn)
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
	if cfg.Handle != nil {
		cfg.Handle.SetTransferID(transferID)
	}

	if offer.Version != protocol.ProtocolVersion {
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrVersionMismatch,
			fmt.Sprintf("expected version %d got %d", protocol.ProtocolVersion, offer.Version))
		saveErr = fmt.Errorf("version mismatch: got %d want %d", offer.Version, protocol.ProtocolVersion)
		return
	}

	if err := protocol.ValidateOffer(offer); err != nil {
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrProtocol, err.Error())
		saveErr = err
		return
	}

	downloadsDir, err := config.DownloadsDir()
	if err != nil {
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrIO, err.Error())
		saveErr = fmt.Errorf("downloads dir: %w", err)
		return
	}
	if err := config.EnsureSpace(downloadsDir, offer.Size); err != nil {
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrIO, err.Error())
		saveErr = err
		return
	}

	decision := cfg.Decider(ctx, offer)

	decPayload, err := protocol.MarshalDecision(decision)
	if err != nil {
		saveErr = fmt.Errorf("marshal decision: %w", err)
		return
	}
	if err := writeFrame(cfg.Conn, protocol.FrameDecision, decPayload); err != nil {
		saveErr = fmt.Errorf("write decision: %w", err)
		return
	}
	if !decision.Accepted {
		saveErr = fmt.Errorf("transfer rejected: %s", decision.Reason)
		return
	}

	if contextDone(ctx) {
		saveErr = ErrAborted
		return
	}

	emitProgress(cfg.Progress, ProgressEvent{TransferID: transferID, Phase: PhaseTransferring, BytesTotal: offer.Size})

	hasher := sha256.New()
	pr, pw := io.Pipe()

	var (
		frameErr   error
		localRecvd atomic.Int64
	)
	doneCh := make(chan struct{})

	go func() {
		defer close(doneCh)
		lastEmit := time.Now()
		lastBytes := int64(0)

		for {
			if contextDone(ctx) {
				pw.CloseWithError(ErrAborted)
				frameErr = ErrAborted
				return
			}
			ftype, payload, err := readFrame(cfg.Conn)
			if err != nil {
				pw.CloseWithError(fmt.Errorf("%s: read frame: %w", protocol.ErrIO, err))
				frameErr = err
				return
			}
			switch ftype {
			case protocol.FrameChunk:
				if _, err := pw.Write(payload); err != nil {
					pw.CloseWithError(err)
					frameErr = err
					return
				}
				recvd := localRecvd.Add(int64(len(payload)))

				if time.Since(lastEmit) >= progressInterval {
					elapsed := time.Since(lastEmit).Seconds()
					bps := float64(recvd-lastBytes) / elapsed
					emitProgress(cfg.Progress, ProgressEvent{
						TransferID:  transferID,
						BytesDone:   recvd,
						BytesTotal:  offer.Size,
						BytesPerSec: bps,
						Phase:       PhaseTransferring,
					})
					lastEmit = time.Now()
					lastBytes = recvd
				}

			case protocol.FrameComplete:
				if _, err := protocol.UnmarshalComplete(payload); err != nil {
					pw.CloseWithError(err)
					frameErr = err
					return
				}
				pw.Close()
				return

			case protocol.FrameError:
				em, _ := protocol.UnmarshalError(payload)
				frameErr = fmt.Errorf("sender error: %s: %s", em.Code, em.Message)
				pw.CloseWithError(frameErr)
				return

			default:
				frameErr = fmt.Errorf("%s: unexpected frame 0x%02x", protocol.ErrProtocol, ftype)
				pw.CloseWithError(frameErr)
				return
			}
		}
	}()

	tee := io.TeeReader(pr, hasher)

	var (
		commitErr error
		tmpPath   string
		destPath  string
		tmpDir    string
	)

	destPath, commitErr = config.UniqueDestPath(downloadsDir, offer.Name)
	if commitErr == nil && offer.IsDir {
		tmpDir, commitErr = os.MkdirTemp(downloadsDir, "lm-recv-*")
	}
	var tmpFile *os.File
	if commitErr == nil && !offer.IsDir {
		tmpFile, commitErr = os.CreateTemp(downloadsDir, "lm-recv-*")
		if commitErr == nil {
			tmpPath = tmpFile.Name()
		}
	}

	if commitErr != nil {
		if tmpFile != nil {
			_ = tmpFile.Close()
		}
		pw.CloseWithError(commitErr)
		<-doneCh
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrIO, commitErr.Error())
		saveErr = commitErr
		return
	}

	if offer.IsDir {
		commitErr = UntarFolder(tee, tmpDir)
	} else {
		_, copyErr := io.Copy(tmpFile, tee)
		closeErr := tmpFile.Close()
		tmpFile = nil
		if copyErr != nil {
			commitErr = copyErr
		} else if closeErr != nil {
			commitErr = closeErr
		}
	}

	if commitErr == nil {
		_, _ = io.Copy(io.Discard, tee)
	}

	<-doneCh
	if frameErr != nil && commitErr == nil {
		commitErr = frameErr
	}

	cleanupTemps := func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	}

	if commitErr != nil {
		cleanupTemps()
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrIO, commitErr.Error())
		saveErr = commitErr
		return
	}

	emitProgress(cfg.Progress, ProgressEvent{TransferID: transferID, Phase: PhaseVerifying, BytesDone: localRecvd.Load(), BytesTotal: offer.Size})

	gotChecksum := hex.EncodeToString(hasher.Sum(nil))
	if gotChecksum != offer.Checksum || localRecvd.Load() != offer.Size {
		detail := fmt.Sprintf("checksum mismatch: got %s want %s", gotChecksum, offer.Checksum)
		if localRecvd.Load() != offer.Size {
			detail = fmt.Sprintf("size mismatch: received %d want %d", localRecvd.Load(), offer.Size)
		}
		cleanupTemps()
		sendErrorFrame(cfg.Conn, transferID, protocol.ErrChecksum, detail)
		saveErr = fmt.Errorf("%s: %s", protocol.ErrChecksum, detail)
		return
	}

	// Commit first, then ack. A failed rename must not look like success to the sender.
	if offer.IsDir {
		if err := os.Rename(tmpDir, destPath); err != nil {
			cleanupTemps()
			sendErrorFrame(cfg.Conn, transferID, protocol.ErrIO, err.Error())
			saveErr = fmt.Errorf("commit folder: %w", err)
			return
		}
		tmpDir = ""
		savedPath = destPath
	} else {
		if err := os.Rename(tmpPath, destPath); err != nil {
			cleanupTemps()
			sendErrorFrame(cfg.Conn, transferID, protocol.ErrIO, err.Error())
			saveErr = fmt.Errorf("commit file: %w", err)
			return
		}
		tmpPath = ""
		savedPath = destPath
	}

	ackPayload, err := protocol.MarshalAck(protocol.AckMessage{
		TransferID: transferID,
		OK:         true,
	})
	if err != nil {
		saveErr = fmt.Errorf("marshal ack: %w", err)
		return
	}
	if err := writeFrame(cfg.Conn, protocol.FrameAck, ackPayload); err != nil {
		saveErr = fmt.Errorf("write ack: %w", err)
		return
	}

	emitProgress(cfg.Progress, ProgressEvent{TransferID: transferID, Phase: PhaseDone, BytesDone: localRecvd.Load(), BytesTotal: offer.Size})
}

// sendErrorFrame is a best-effort helper to notify the peer of an error.
func sendErrorFrame(w io.Writer, transferID, code, msg string) {
	payload, err := protocol.MarshalError(protocol.ErrorMessage{
		TransferID: transferID,
		Code:       code,
		Message:    msg,
	})
	if err != nil {
		return
	}
	_ = writeFrame(w, protocol.FrameError, payload)
}

// AutoAcceptDecider always accepts offers. Used during headless testing (M3).
func AutoAcceptDecider(_ context.Context, offer protocol.OfferMessage) protocol.DecisionMessage {
	return protocol.DecisionMessage{
		TransferID: offer.TransferID,
		Accepted:   true,
	}
}

// ChannelDecider creates an OfferDecider that sends the offer on offerCh and
// blocks waiting for a reply on the returned reply channel.
func ChannelDecider(offerCh chan<- OfferWithReply, h *Handle) OfferDecider {
	return func(ctx context.Context, offer protocol.OfferMessage) protocol.DecisionMessage {
		reply := make(chan protocol.DecisionMessage, 1)
		select {
		case offerCh <- OfferWithReply{Offer: offer, Reply: reply, Handle: h}:
		case <-ctx.Done():
			return protocol.DecisionMessage{
				TransferID: offer.TransferID,
				Accepted:   false,
				Reason:     protocol.ErrBusy,
			}
		}
		select {
		case dec := <-reply:
			return dec
		case <-ctx.Done():
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
	Offer  protocol.OfferMessage
	Reply  chan<- protocol.DecisionMessage
	Handle *Handle
}
