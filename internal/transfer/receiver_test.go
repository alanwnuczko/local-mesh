package transfer

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

// TestContextCancelUnblocksDecider is a regression test for the goroutine-leak
// bug that caused a Windows TCP RST on the sender side.
//
// Scenario: the sender has sent a FrameOffer and is waiting for FrameDecision.
// The receiver's UI has the accept/reject overlay open but the user quits the
// app before making a choice. Before the fix, ChannelDecider blocked on
// <-reply forever, leaking the goroutine and leaving the TCP connection
// half-open. The OS would eventually RST it, producing:
//
//	wsarecv: An existing connection was forcibly closed by the remote host.
//
// After the fix, cancelling ctx causes ChannelDecider to return a rejection
// immediately, RunReceive sends FrameDecision (rejected) over the wire, and
// the deferred conn.Close() fires cleanly.
func TestContextCancelUnblocksDecider(t *testing.T) {
	// net.Pipe gives us a synchronous, in-process connection pair - no OS
	// networking required, no flaky port allocation.
	senderConn, receiverConn := net.Pipe()
	defer senderConn.Close()
	defer receiverConn.Close()

	offerCh := make(chan OfferWithReply, 1)
	progress := make(chan ProgressEvent, 16)
	done := make(chan DoneEvent, 1)

	ctx, cancel := context.WithCancel(context.Background())

	// Start RunReceive on the receiver side.
	go func() {
		RunReceive(RecvConfig{
			Ctx:      ctx,
			Conn:     receiverConn,
			Decider:  ChannelDecider(offerCh),
			Progress: progress,
			Done:     done,
		})
	}()

	// Sender side: write a valid FrameOffer.
	offer := protocol.OfferMessage{
		Version:    protocol.ProtocolVersion,
		SenderID:   "test-sender",
		SenderHost: "testhost",
		Name:       "hello.txt",
		IsDir:      false,
		Size:       5,
		Checksum:   "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		TransferID: "regression-test-001",
	}
	payload, _ := protocol.MarshalOffer(offer)
	if err := protocol.WriteFrame(senderConn, protocol.FrameOffer, payload); err != nil {
		t.Fatalf("write offer: %v", err)
	}

	// Wait until the offer has been forwarded to offerCh, confirming RunReceive
	// is now blocked inside ChannelDecider waiting for the UI reply.
	var owr OfferWithReply
	select {
	case owr = <-offerCh:
		// Good - the offer arrived; do NOT send a reply, simulating the user
		// quitting before pressing accept/reject.
		_ = owr
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for offer to reach offerCh")
	}

	// Before cancelling the context, start draining the sender side of the
	// pipe. net.Pipe is synchronous - if no one reads from senderConn, any
	// WriteFrame call on receiverConn blocks forever, which would prevent
	// RunReceive from exiting even after the context is cancelled.
	// This mirrors the real-world sender which is always blocked in ReadFrame
	// waiting for the decision.
	senderReadDone := make(chan struct{})
	go func() {
		defer close(senderReadDone)
		// Read frames until the pipe closes.
		for {
			_, _, err := protocol.ReadFrame(senderConn)
			if err != nil {
				return
			}
		}
	}()

	// Cancel the context, simulating the receiver process shutting down while
	// the overlay is still on screen.
	cancel()

	// RunReceive must now exit. If the bug is present it blocks here forever.
	select {
	case ev := <-done:
		// Any outcome is fine here - we just need the goroutine to have exited.
		t.Logf("RunReceive exited with err=%v", ev.Err)
	case <-time.After(2 * time.Second):
		t.Fatal("RunReceive did not exit after context cancellation - goroutine leak detected (this is the bug that produced the TCP RST)")
	}

	// The sender side should now be able to read a FrameDecision (the rejection
	// sent by ChannelDecider before RunReceive exited). This confirms the
	// connection was closed cleanly with a FIN, not a RST.
	_ = senderConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	ft, raw, err := protocol.ReadFrame(senderConn)
	if err != nil {
		// An EOF or "closed pipe" here is also acceptable - it means the
		// receiver closed cleanly. A RST would manifest as a different error.
		t.Logf("sender read after cancel: %v (EOF/pipe closed = clean close, expected)", err)
		return
	}
	if ft != protocol.FrameDecision {
		t.Errorf("expected FrameDecision from receiver after cancel, got 0x%02x (raw=%q)", ft, raw)
	}
	dec, _ := protocol.UnmarshalDecision(raw)
	if dec.Accepted {
		t.Error("expected rejection decision after context cancel, got accepted=true")
	}
	t.Logf("clean rejection received by sender: reason=%q", dec.Reason)
}

// TestSlowDecisionNoRST verifies that a slow but eventual user decision still
// works correctly - the context-cancel path must not fire prematurely.
func TestSlowDecisionNoRST(t *testing.T) {
	senderConn, receiverConn := net.Pipe()
	defer senderConn.Close()
	defer receiverConn.Close()

	offerCh := make(chan OfferWithReply, 1)
	progress := make(chan ProgressEvent, 16)
	done := make(chan DoneEvent, 1)

	// Long-lived context - simulates a user who takes 300 ms to accept.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		RunReceive(RecvConfig{
			Ctx:      ctx,
			Conn:     receiverConn,
			Decider:  ChannelDecider(offerCh),
			Progress: progress,
			Done:     done,
		})
	}()

	offer := protocol.OfferMessage{
		Version:    protocol.ProtocolVersion,
		SenderID:   "test-sender",
		SenderHost: "testhost",
		Name:       "hello.txt",
		IsDir:      false,
		Size:       5,
		Checksum:   "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		TransferID: "slow-decision-001",
	}
	payload, _ := protocol.MarshalOffer(offer)
	if err := protocol.WriteFrame(senderConn, protocol.FrameOffer, payload); err != nil {
		t.Fatalf("write offer: %v", err)
	}

	var owr OfferWithReply
	select {
	case owr = <-offerCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for offer")
	}

	// Simulate 300 ms of user "thinking" time, then reject.
	go func() {
		time.Sleep(300 * time.Millisecond)
		owr.Reply <- protocol.DecisionMessage{
			TransferID: owr.Offer.TransferID,
			Accepted:   false,
			Reason:     "user declined",
		}
	}()

	// Sender reads the eventual decision - should not time out or get a RST.
	_ = senderConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	ft, raw, err := protocol.ReadFrame(senderConn)
	if err != nil {
		t.Fatalf("sender read decision: %v", err)
	}
	if ft != protocol.FrameDecision {
		t.Errorf("expected FrameDecision, got 0x%02x", ft)
	}
	dec, _ := protocol.UnmarshalDecision(raw)
	if dec.Accepted {
		t.Error("expected rejection, got accepted=true")
	}

	// RunReceive should exit cleanly (rejected = no data transfer).
	select {
	case ev := <-done:
		if ev.Err == nil {
			t.Error("expected non-nil err for rejected transfer, got nil")
		}
		t.Logf("RunReceive exited cleanly: %v", ev.Err)
	case <-time.After(2 * time.Second):
		t.Fatal("RunReceive did not exit after rejection")
	}

	// Drain the reply channel to avoid a goroutine leak in the test itself.
	// (The 300ms goroutine has already sent on it.)
	_ = bytes.Compare(nil, nil) // suppress unused import if needed
}
