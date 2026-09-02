package transfer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

// SendConfig holds all parameters needed to start a send session.
type SendConfig struct {
	Ctx        context.Context
	Conn       io.ReadWriter
	Handle     *Handle
	TransferID string
	SenderID   string
	SenderHost string
	Path       string // path to file or folder
	IsDir      bool
	Size       int64  // optional precomputed size from the confirm screen
	Checksum   string // optional precomputed checksum from the confirm screen
	Plan       *FolderPlan
	Progress   chan<- ProgressEvent
	Done       chan<- DoneEvent
}

// RunSend executes the full send-side state machine on the given connection.
// It should be called in its own goroutine.
//
// Background goroutines MUST NOT touch Model fields (§3.5). All state updates
// are communicated via the Progress and Done channels.
func RunSend(cfg SendConfig) {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	transferID := cfg.TransferID
	if transferID == "" {
		transferID = NewTransferID()
	}
	if cfg.Handle != nil {
		cfg.Handle.SetTransferID(transferID)
	}

	var sendErr error
	defer func() {
		if cfg.Done == nil {
			return
		}
		cfg.Done <- DoneEvent{
			TransferID: transferID,
			Err:        ctxErr(ctx, sendErr),
			Direction:  DirSend,
		}
	}()

	if contextDone(ctx) {
		sendErr = ErrAborted
		return
	}

	emitProgress(cfg.Progress, ProgressEvent{TransferID: transferID, Phase: PhaseOffering})

	var (
		size     int64
		checksum string
		plan     *FolderPlan
	)

	if cfg.IsDir {
		plan = cfg.Plan
		if plan == nil {
			var err error
			plan, err = PlanFolder(cfg.Path)
			if err != nil {
				sendErr = fmt.Errorf("plan folder: %w", err)
				return
			}
		}
		size, checksum = plan.Size, plan.Checksum
	} else if cfg.Checksum != "" {
		size, checksum = cfg.Size, cfg.Checksum
		fi, err := os.Stat(cfg.Path)
		if err != nil {
			sendErr = fmt.Errorf("stat payload: %w", err)
			return
		}
		if fi.Size() != size {
			sendErr = fmt.Errorf("file changed since confirm (size %d -> %d)", size, fi.Size())
			return
		}
	} else {
		var err error
		size, checksum, err = computePayloadMeta(cfg.Path, false)
		if err != nil {
			sendErr = fmt.Errorf("compute payload meta: %w", err)
			return
		}
	}

	name := filepath.Base(cfg.Path)

	offer := protocol.OfferMessage{
		Version:    protocol.ProtocolVersion,
		SenderID:   cfg.SenderID,
		SenderHost: cfg.SenderHost,
		Name:       name,
		IsDir:      cfg.IsDir,
		Size:       size,
		Checksum:   checksum,
		TransferID: transferID,
	}
	payload, err := protocol.MarshalOffer(offer)
	if err != nil {
		sendErr = fmt.Errorf("marshal offer: %w", err)
		return
	}
	if err := writeFrame(cfg.Conn, protocol.FrameOffer, payload); err != nil {
		sendErr = fmt.Errorf("write offer: %w", err)
		return
	}

	emitProgress(cfg.Progress, ProgressEvent{TransferID: transferID, Phase: PhaseWaiting, BytesTotal: size})
	ft, raw, err := readFrame(cfg.Conn)
	if err != nil {
		sendErr = fmt.Errorf("read decision: %w", err)
		return
	}
	if ft == protocol.FrameError {
		em, _ := protocol.UnmarshalError(raw)
		sendErr = fmt.Errorf("peer error: %s: %s", em.Code, em.Message)
		return
	}
	if ft != protocol.FrameDecision {
		sendErr = fmt.Errorf("%s: expected FrameDecision got 0x%02x", protocol.ErrProtocol, ft)
		return
	}
	dec, err := protocol.UnmarshalDecision(raw)
	if err != nil {
		sendErr = fmt.Errorf("unmarshal decision: %w", err)
		return
	}
	if !dec.Accepted {
		sendErr = fmt.Errorf("peer declined: %s", dec.Reason)
		return
	}

	if contextDone(ctx) {
		sendErr = ErrAborted
		return
	}

	emitProgress(cfg.Progress, ProgressEvent{TransferID: transferID, Phase: PhaseTransferring, BytesTotal: size})
	bytesSent, err := streamPayload(ctx, cfg.Conn, cfg.Path, cfg.IsDir, plan, transferID, size, cfg.Progress)
	if err != nil {
		sendErr = err
		return
	}

	comp, err := protocol.MarshalComplete(protocol.CompleteMessage{
		TransferID: transferID,
		BytesSent:  bytesSent,
	})
	if err != nil {
		sendErr = fmt.Errorf("marshal complete: %w", err)
		return
	}
	if err := writeFrame(cfg.Conn, protocol.FrameComplete, comp); err != nil {
		sendErr = fmt.Errorf("write complete: %w", err)
		return
	}

	emitProgress(cfg.Progress, ProgressEvent{TransferID: transferID, Phase: PhaseVerifying, BytesDone: bytesSent, BytesTotal: size})
	ft, raw, err = readFrame(cfg.Conn)
	if err != nil {
		sendErr = fmt.Errorf("read ack: %w", err)
		return
	}
	if ft == protocol.FrameError {
		em, _ := protocol.UnmarshalError(raw)
		sendErr = fmt.Errorf("peer error: %s: %s", em.Code, em.Message)
		return
	}
	if ft != protocol.FrameAck {
		sendErr = fmt.Errorf("%s: expected FrameAck got 0x%02x", protocol.ErrProtocol, ft)
		return
	}
	ack, err := protocol.UnmarshalAck(raw)
	if err != nil {
		sendErr = fmt.Errorf("unmarshal ack: %w", err)
		return
	}
	if !ack.OK {
		sendErr = fmt.Errorf("receiver rejected file: %s", ack.Detail)
		return
	}

	emitProgress(cfg.Progress, ProgressEvent{TransferID: transferID, Phase: PhaseDone, BytesDone: bytesSent, BytesTotal: size})
}

// computePayloadMeta returns the byte-length and hex SHA-256 of the payload
// that will be sent. For directories this is the tar stream; for files it is
// the file itself.
func computePayloadMeta(path string, isDir bool) (size int64, checksum string, err error) {
	h := sha256.New()
	cw := &countingWriter{w: h}

	if isDir {
		if err = TarFolder(path, cw); err != nil {
			return
		}
	} else {
		f, ferr := os.Open(path)
		if ferr != nil {
			err = ferr
			return
		}
		defer f.Close()
		if _, cerr := io.Copy(cw, f); cerr != nil {
			err = cerr
			return
		}
	}
	return cw.n, hex.EncodeToString(h.Sum(nil)), nil
}

// streamPayload sends the file/folder payload as FrameChunk frames and returns
// the total bytes sent.
func streamPayload(ctx context.Context, w io.Writer, path string, isDir bool, plan *FolderPlan, transferID string, totalSize int64, progress chan<- ProgressEvent) (int64, error) {
	var src io.Reader
	var closer io.Closer

	if isDir {
		pr, pw := io.Pipe()
		src = pr
		go func() {
			var err error
			if plan != nil {
				err = plan.Stream(pw)
			} else {
				err = TarFolder(path, pw)
			}
			pw.CloseWithError(err)
		}()
		closer = pr
	} else {
		f, err := os.Open(path)
		if err != nil {
			return 0, fmt.Errorf("open file: %w", err)
		}
		src = f
		closer = f
	}
	if closer != nil {
		defer closer.Close()
	}

	buf := make([]byte, protocol.ChunkSize)
	var sent int64
	lastEmit := time.Now()
	lastBytes := int64(0)

	for {
		if contextDone(ctx) {
			return sent, ErrAborted
		}
		n, err := io.ReadFull(src, buf)
		if n > 0 {
			if werr := writeFrame(w, protocol.FrameChunk, buf[:n]); werr != nil {
				return sent, fmt.Errorf("write chunk: %w", werr)
			}
			sent += int64(n)

			if time.Since(lastEmit) >= progressInterval {
				elapsed := time.Since(lastEmit).Seconds()
				bps := float64(sent-lastBytes) / elapsed
				emitProgress(progress, ProgressEvent{
					TransferID:  transferID,
					BytesDone:   sent,
					BytesTotal:  totalSize,
					BytesPerSec: bps,
					Phase:       PhaseTransferring,
				})
				lastEmit = time.Now()
				lastBytes = sent
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return sent, fmt.Errorf("read payload: %w", err)
		}
	}
	return sent, nil
}

func emitProgress(ch chan<- ProgressEvent, ev ProgressEvent) {
	if ch == nil {
		return
	}
	select {
	case ch <- ev:
	default:
	}
}

// countingWriter counts bytes written and proxies to an inner writer.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// NewTransferID generates a random 16-byte hex transfer ID.
func NewTransferID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
