package app

import (
	"fmt"
	"testing"

	"github.com/alanwnuczko/local-mesh/internal/screens"
	"github.com/alanwnuczko/local-mesh/internal/transfer"
	"github.com/alanwnuczko/local-mesh/pkg/protocol"
	tea "github.com/charmbracelet/bubbletea"
)

func asModel(t *testing.T, m tea.Model) Model {
	t.Helper()
	got, ok := m.(Model)
	if !ok {
		t.Fatalf("got %T, want Model", m)
	}
	return got
}

func runCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd != nil {
		_ = cmd()
	}
}

func TestIncomingOfferRejectedWhileSending(t *testing.T) {
	m := NewModel("self", "host", 80, 24)
	m.activeTransferID = "send-1"
	m.activeDirection = transfer.DirSend
	m.Activity = screens.ActivityTransferring
	m.Progress = screens.NewProgress(transfer.DirSend, 100, 80, 24)

	reply := make(chan protocol.DecisionMessage, 1)
	got, cmd := m.Update(IncomingOfferMsg{
		Offer: protocol.OfferMessage{TransferID: "recv-2", Name: "x"},
		Reply: reply,
	})
	model := asModel(t, got)
	if model.Overlay != nil {
		t.Fatal("overlay must not appear while sending")
	}
	runCmd(t, cmd)
	select {
	case dec := <-reply:
		if dec.Accepted {
			t.Fatal("offer should be rejected")
		}
		if dec.Reason != protocol.ErrBusy {
			t.Fatalf("reason=%q want %s", dec.Reason, protocol.ErrBusy)
		}
	default:
		t.Fatal("no decision sent")
	}
}

func TestIncomingOfferRejectedWhileReceiving(t *testing.T) {
	m := NewModel("self", "host", 80, 24)
	m.activeTransferID = "recv-1"
	m.activeDirection = transfer.DirRecv
	m.ReceiveBusy = true
	m.Activity = screens.ActivityReceiving

	reply := make(chan protocol.DecisionMessage, 1)
	_, cmd := m.Update(IncomingOfferMsg{
		Offer: protocol.OfferMessage{TransferID: "recv-2"},
		Reply: reply,
	})
	runCmd(t, cmd)
	dec := <-reply
	if dec.Accepted || dec.Reason != protocol.ErrBusy {
		t.Fatalf("dec=%+v", dec)
	}
}

func TestIncomingOfferRejectedWhenOverlayUp(t *testing.T) {
	m := NewModel("self", "host", 80, 24)
	m.Overlay = &screens.OverlayState{
		Offer: protocol.OfferMessage{TransferID: "first"},
		Reply: make(chan protocol.DecisionMessage, 1),
	}

	reply := make(chan protocol.DecisionMessage, 1)
	got, cmd := m.Update(IncomingOfferMsg{
		Offer: protocol.OfferMessage{TransferID: "second"},
		Reply: reply,
	})
	model := asModel(t, got)
	if model.Overlay.Offer.TransferID != "first" {
		t.Fatal("first overlay was replaced")
	}
	runCmd(t, cmd)
	dec := <-reply
	if dec.Accepted {
		t.Fatal("second offer should be busy")
	}
}

func TestRecvDoneIgnoresOtherTransfer(t *testing.T) {
	m := NewModel("self", "host", 80, 24)
	m.activeTransferID = "a"
	m.activeDirection = transfer.DirRecv
	m.ReceiveBusy = true
	m.Activity = screens.ActivityReceiving
	m.Progress = screens.NewProgress(transfer.DirRecv, 100, 80, 24)

	got, _ := m.Update(RecvDoneMsg{Event: transfer.DoneEvent{
		TransferID: "b",
		Err:        fmt.Errorf("transfer rejected: %s", protocol.ErrBusy),
		Direction:  transfer.DirRecv,
	}})
	model := asModel(t, got)
	if !model.ReceiveBusy {
		t.Fatal("ReceiveBusy cleared by a foreign RecvDoneMsg")
	}
	if model.activeTransferID != "a" {
		t.Fatalf("active id=%q", model.activeTransferID)
	}
	if model.Progress.IsDone() {
		t.Fatal("progress marked done by a foreign RecvDoneMsg")
	}
}

func TestRecvDoneMatchesActive(t *testing.T) {
	m := NewModel("self", "host", 80, 24)
	m.activeTransferID = "a"
	m.activeDirection = transfer.DirRecv
	m.ReceiveBusy = true
	m.Activity = screens.ActivityReceiving
	m.Progress = screens.NewProgress(transfer.DirRecv, 100, 80, 24)

	got, _ := m.Update(RecvDoneMsg{Event: transfer.DoneEvent{
		TransferID: "a",
		SavedPath:  "/tmp/saved",
		Direction:  transfer.DirRecv,
	}})
	model := asModel(t, got)
	if model.ReceiveBusy {
		t.Fatal("still busy after matching RecvDone")
	}
	if model.activeTransferID != "" {
		t.Fatal("active id not cleared")
	}
	if !model.Progress.IsDone() {
		t.Fatal("progress not done")
	}
	if model.Progress.SavedPath() != "/tmp/saved" {
		t.Fatalf("saved=%q", model.Progress.SavedPath())
	}
}

func TestSendProgressIgnoredForReceive(t *testing.T) {
	m := NewModel("self", "host", 80, 24)
	m.activeTransferID = "recv-1"
	m.activeDirection = transfer.DirRecv
	m.Progress = screens.NewProgress(transfer.DirRecv, 100, 80, 24)

	got, _ := m.Update(SendProgressMsg{Event: transfer.ProgressEvent{
		TransferID: "recv-1",
		BytesDone:  50,
		Phase:      transfer.PhaseTransferring,
	}})
	model := asModel(t, got)
	view := model.Progress.View()
	if model.Progress.IsDone() {
		t.Fatal("unexpectedly done")
	}
	_ = view
}

func TestCancelDoesNotLeaveProgressScreen(t *testing.T) {
	m := NewModel("self", "host", 80, 24)
	m.activeTransferID = "a"
	m.activeDirection = transfer.DirRecv
	m.ReceiveBusy = true
	m.Activity = screens.ActivityReceiving
	m.ActiveScreen = ScreenProgress
	m.Progress = screens.NewProgress(transfer.DirRecv, 100, 80, 24)
	m.xferAbort = transfer.NewHandle()

	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	model := asModel(t, got)
	if model.ActiveScreen != ScreenProgress {
		t.Fatal("cancel must stay on progress until the session ends")
	}
	if !model.ReceiveBusy {
		t.Fatal("cancel must not clear ReceiveBusy before the session ends")
	}
	if model.Progress == nil || model.Progress.IsDone() {
		t.Fatal("progress should still be in-flight")
	}
}

func TestQQuitsFromPicker(t *testing.T) {
	m := NewModel("self", "host", 80, 24)
	m.ActiveScreen = ScreenPicker
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit cmd")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("cmd returned %T, want tea.QuitMsg", msg)
	}
}

func TestCannotStartSendWhileBusy(t *testing.T) {
	m := NewModel("self", "host", 80, 24)
	m.ReceiveBusy = true
	m.activeTransferID = "recv-1"
	m.activeDirection = transfer.DirRecv
	m.Confirm = screens.NewConfirm(m.SelectedPeer, "/tmp/x", false, 80, 24)
	m.Confirm.SetComputed(1, "abc", nil)
	m.ActiveScreen = ScreenConfirm

	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd != nil {
		t.Fatal("must not start a send while receiving")
	}
	model := asModel(t, got)
	if model.ActiveScreen != ScreenConfirm {
		t.Fatalf("screen=%v", model.ActiveScreen)
	}
}
