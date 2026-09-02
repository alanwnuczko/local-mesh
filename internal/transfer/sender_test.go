package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alanwnuczko/local-mesh/internal/config"
	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

func TestFileSendReceiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	config.UseDownloadsDir(dir)
	t.Cleanup(func() { config.UseDownloadsDir("") })

	src := filepath.Join(dir, "src.txt")
	payload := bytes.Repeat([]byte("hello-mesh-"), 8000) // several chunks
	if err := os.WriteFile(src, payload, 0644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	checksum := hex.EncodeToString(sum[:])

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	sendDone := make(chan DoneEvent, 1)
	recvDone := make(chan DoneEvent, 1)
	sendProg := make(chan ProgressEvent, 64)
	recvProg := make(chan ProgressEvent, 64)

	go RunSend(SendConfig{
		Conn:       a,
		SenderID:   "sid",
		SenderHost: "shost",
		Path:       src,
		IsDir:      false,
		Size:       int64(len(payload)),
		Checksum:   checksum,
		Progress:   sendProg,
		Done:       sendDone,
	})
	go RunReceive(RecvConfig{
		Ctx:      context.Background(),
		Conn:     b,
		Decider:  AutoAcceptDecider,
		Progress: recvProg,
		Done:     recvDone,
	})

	var recvEv, sendEv DoneEvent
	select {
	case recvEv = <-recvDone:
	case <-time.After(10 * time.Second):
		t.Fatal("recv timeout")
	}
	select {
	case sendEv = <-sendDone:
	case <-time.After(10 * time.Second):
		t.Fatal("send timeout")
	}
	if recvEv.Err != nil {
		t.Fatalf("recv: %v", recvEv.Err)
	}
	if sendEv.Err != nil {
		t.Fatalf("send: %v", sendEv.Err)
	}
	if recvEv.SavedPath == "" {
		t.Fatal("no saved path")
	}
	got, err := os.ReadFile(recvEv.SavedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch (%d vs %d bytes)", len(got), len(payload))
	}
	if filepath.Dir(recvEv.SavedPath) != dir {
		t.Fatalf("saved outside downloads: %q", recvEv.SavedPath)
	}
}

func TestFolderSendReceiveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	config.UseDownloadsDir(dir)
	t.Cleanup(func() { config.UseDownloadsDir("") })

	src := filepath.Join(dir, "tree")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("B"), 0644); err != nil {
		t.Fatal(err)
	}

	plan, err := PlanFolder(src)
	if err != nil {
		t.Fatal(err)
	}

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	sendDone := make(chan DoneEvent, 1)
	recvDone := make(chan DoneEvent, 1)

	go RunSend(SendConfig{
		Conn:       a,
		SenderID:   "sid",
		SenderHost: "shost",
		Path:       src,
		IsDir:      true,
		Plan:       plan,
		Progress:   make(chan ProgressEvent, 64),
		Done:       sendDone,
	})
	go RunReceive(RecvConfig{
		Ctx:      context.Background(),
		Conn:     b,
		Decider:  AutoAcceptDecider,
		Progress: make(chan ProgressEvent, 64),
		Done:     recvDone,
	})

	var recvEv DoneEvent
	select {
	case recvEv = <-recvDone:
	case <-time.After(10 * time.Second):
		t.Fatal("recv timeout")
	}
	select {
	case <-sendDone:
	case <-time.After(10 * time.Second):
		t.Fatal("send timeout")
	}
	if recvEv.Err != nil {
		t.Fatalf("recv: %v", recvEv.Err)
	}
	gotA, err := os.ReadFile(filepath.Join(recvEv.SavedPath, "tree", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotA) != "A" {
		t.Fatalf("a.txt=%q", gotA)
	}
}

func TestOfferNameCannotEscapeDownloads(t *testing.T) {
	dir := t.TempDir()
	config.UseDownloadsDir(dir)
	t.Cleanup(func() { config.UseDownloadsDir("") })

	src := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(src, []byte("ok"), 0644); err != nil {
		t.Fatal(err)
	}

	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()

	sendDone := make(chan DoneEvent, 1)
	recvDone := make(chan DoneEvent, 1)

	// Craft a send that will use Base(path) as the offer name; we instead
	// intercept by sending a custom offer from a tiny goroutine... easier:
	// RunReceive with a sender that writes a malicious offer.
	go func() {
		defer close(sendDone)
		sum := sha256.Sum256([]byte("ok"))
		offer := protocol.OfferMessage{
			Version:    protocol.ProtocolVersion,
			SenderID:   "s",
			SenderHost: "h",
			Name:       "../escaped.txt",
			Size:       2,
			Checksum:   hex.EncodeToString(sum[:]),
			TransferID: "escape-1",
		}
		payload, _ := protocol.MarshalOffer(offer)
		if err := protocol.WriteFrame(a, protocol.FrameOffer, payload); err != nil {
			return
		}
		ft, raw, err := protocol.ReadFrame(a)
		if err != nil || ft != protocol.FrameDecision {
			return
		}
		dec, _ := protocol.UnmarshalDecision(raw)
		if !dec.Accepted {
			return
		}
		_ = protocol.WriteFrame(a, protocol.FrameChunk, []byte("ok"))
		comp, _ := protocol.MarshalComplete(protocol.CompleteMessage{TransferID: "escape-1", BytesSent: 2})
		_ = protocol.WriteFrame(a, protocol.FrameComplete, comp)
		_, _, _ = protocol.ReadFrame(a)
	}()

	go RunReceive(RecvConfig{
		Ctx:      context.Background(),
		Conn:     b,
		Decider:  AutoAcceptDecider,
		Progress: make(chan ProgressEvent, 16),
		Done:     recvDone,
	})

	select {
	case ev := <-recvDone:
		if ev.Err != nil {
			t.Fatalf("recv: %v", ev.Err)
		}
		if filepath.Dir(ev.SavedPath) != dir {
			t.Fatalf("escaped downloads: %q (dir %q)", ev.SavedPath, dir)
		}
		if filepath.Base(ev.SavedPath) != "escaped.txt" {
			t.Fatalf("base=%q", filepath.Base(ev.SavedPath))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
}

func TestRejectBusyWritesErrorFrame(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	go rejectBusy(a)

	_ = b.SetReadDeadline(time.Now().Add(2 * time.Second))
	ft, raw, err := protocol.ReadFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if ft != protocol.FrameError {
		t.Fatalf("ft=0x%02x", ft)
	}
	em, err := protocol.UnmarshalError(raw)
	if err != nil {
		t.Fatal(err)
	}
	if em.Code != protocol.ErrBusy {
		t.Fatalf("code=%q", em.Code)
	}
}

func TestAbortUnblocksSendWaitingForDecision(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()

	src := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(src, []byte("hi"), 0644); err != nil {
		t.Fatal(err)
	}

	h := NewHandle()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.SetCancel(cancel)
	wrapped := h.Wrap(a)

	done := make(chan DoneEvent, 1)
	go RunSend(SendConfig{
		Ctx:        ctx,
		Conn:       wrapped,
		Handle:     h,
		SenderID:   "s",
		SenderHost: "h",
		Path:       src,
		Progress:   make(chan ProgressEvent, 16),
		Done:       done,
	})

	_ = b.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, _, err := protocol.ReadFrame(b); err != nil {
		t.Fatalf("read offer: %v", err)
	}

	// Drain so Abort's best-effort ERR_ABORT frame cannot block on net.Pipe.
	go func() {
		for {
			if _, _, err := protocol.ReadFrame(b); err != nil {
				return
			}
		}
	}()

	h.Abort()

	select {
	case ev := <-done:
		if ev.Err == nil {
			t.Fatal("expected abort/io error, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("send did not unblock after Abort")
	}
}
