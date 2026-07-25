package transfer

import (
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
	Conn       io.ReadWriter
	SenderID   string
	SenderHost string
	Path       string  // path to file or folder
	IsDir      bool
	Progress   chan<- ProgressEvent
	Done       chan<- DoneEvent
}

// RunSend executes the full send-side state machine on the given connection.
// It should be called in its own goroutine.
//
// Background goroutines MUST NOT touch Model fields (§3.5). All state updates
// are communicated via the Progress and Done channels.
func RunSend(cfg SendConfig) {
	transferID := newTransferID()
	var sendErr error

	defer func() {
		cfg.Done <- DoneEvent{
			TransferID: transferID,
			Err:        sendErr,
			Direction:  DirSend,
		}
	}()

	cfg.Progress <- ProgressEvent{TransferID: transferID, Phase: PhaseOffering}

	var (
		size      int64
		checksum  string
		tmpTar    string // non-empty only when cfg.IsDir is true
	)

	if cfg.IsDir {
		var err error
		size, checksum, tmpTar, err = tarFolderToTemp(cfg.Path)
		if err != nil {
			sendErr = fmt.Errorf("tar folder: %w", err)
			return
		}
		defer os.Remove(tmpTar)
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
	payload, _ := protocol.MarshalOffer(offer)
	if err := protocol.WriteFrame(cfg.Conn, protocol.FrameOffer, payload); err != nil {
		sendErr = fmt.Errorf("write offer: %w", err)
		return
	}

	cfg.Progress <- ProgressEvent{TransferID: transferID, Phase: PhaseWaiting, BytesTotal: size}
	ft, raw, err := protocol.ReadFrame(cfg.Conn)
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

	cfg.Progress <- ProgressEvent{TransferID: transferID, Phase: PhaseTransferring, BytesTotal: size}
	bytesSent, err := streamPayload(cfg.Conn, cfg.Path, cfg.IsDir, tmpTar, transferID, size, cfg.Progress)
	if err != nil {
		sendErr = err
		return
	}

	comp, _ := protocol.MarshalComplete(protocol.CompleteMessage{
		TransferID: transferID,
		BytesSent:  bytesSent,
	})
	if err := protocol.WriteFrame(cfg.Conn, protocol.FrameComplete, comp); err != nil {
		sendErr = fmt.Errorf("write complete: %w", err)
		return
	}

	cfg.Progress <- ProgressEvent{TransferID: transferID, Phase: PhaseVerifying, BytesDone: bytesSent, BytesTotal: size}
	ft, raw, err = protocol.ReadFrame(cfg.Conn)
	if err != nil {
		sendErr = fmt.Errorf("read ack: %w", err)
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

	cfg.Progress <- ProgressEvent{TransferID: transferID, Phase: PhaseDone, BytesDone: bytesSent, BytesTotal: size}
}

// computePayloadMeta returns the byte-length and hex SHA-256 of the payload
// that will be sent. For directories this is the tar stream; for files it is
// the file itself. Computed in a first pass so the offer can carry accurate info.
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

// tarFolderToTemp tars the directory at srcDir into an OS temp file.
// It returns the byte size, hex SHA-256, and the temp-file path.
// The caller is responsible for removing the temp file when done.
func tarFolderToTemp(srcDir string) (size int64, checksum string, tmpPath string, err error) {
	tmp, err := os.CreateTemp("", "lm-send-*.tar")
	if err != nil {
		return 0, "", "", fmt.Errorf("create temp tar: %w", err)
	}
	tmpPath = tmp.Name()

	h := sha256.New()
	mw := io.MultiWriter(tmp, h)
	if err = TarFolder(srcDir, mw); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return 0, "", "", fmt.Errorf("tar folder: %w", err)
	}
	if err = tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return 0, "", "", fmt.Errorf("close temp tar: %w", err)
	}

	fi, err := os.Stat(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return 0, "", "", fmt.Errorf("stat temp tar: %w", err)
	}
	return fi.Size(), hex.EncodeToString(h.Sum(nil)), tmpPath, nil
}

// streamPayload sends the file/folder payload as FrameChunk frames and returns
// the total bytes sent. tmpTar, if non-empty, is a pre-built tar file to stream
// instead of re-running TarFolder (used for directory transfers).
func streamPayload(w io.Writer, path string, isDir bool, tmpTar string, transferID string, totalSize int64, progress chan<- ProgressEvent) (int64, error) {
	var src io.Reader

	if isDir && tmpTar != "" {
		// Stream from the pre-built temp tar file.
		f, err := os.Open(tmpTar)
		if err != nil {
			return 0, fmt.Errorf("open temp tar: %w", err)
		}
		defer f.Close()
		src = f
	} else if isDir {
		// Fallback: stream on-the-fly (should not normally reach here).
		pr, pw := io.Pipe()
		go func() {
			pw.CloseWithError(TarFolder(path, pw))
		}()
		src = pr
	} else {
		f, err := os.Open(path)
		if err != nil {
			return 0, fmt.Errorf("open file: %w", err)
		}
		defer f.Close()
		src = f
	}

	buf := make([]byte, protocol.ChunkSize)
	var sent int64
	lastEmit := time.Now()
	lastBytes := int64(0)

	for {
		n, err := io.ReadFull(src, buf)
		if n > 0 {
			if werr := protocol.WriteFrame(w, protocol.FrameChunk, buf[:n]); werr != nil {
				return sent, fmt.Errorf("write chunk: %w", werr)
			}
			sent += int64(n)

			// Throttled progress: at most once per 100 ms.
			if time.Since(lastEmit) >= 100*time.Millisecond {
				elapsed := time.Since(lastEmit).Seconds()
				bps := float64(sent-lastBytes) / elapsed
				progress <- ProgressEvent{
					TransferID:  transferID,
					BytesDone:   sent,
					BytesTotal:  totalSize,
					BytesPerSec: bps,
					Phase:       PhaseTransferring,
				}
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

// newTransferID generates a random 16-byte hex transfer ID.
func newTransferID() string {
	b := make([]byte, 16)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}
