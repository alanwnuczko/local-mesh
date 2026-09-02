// Package app is the root Bubbletea application: model, update, view, and all
// custom tea.Msg types.
//
// CONCURRENCY INVARIANT (§0, §3.5): The Model is only ever mutated inside
// Update. Background goroutines communicate exclusively by sending tea.Msg
// values through the Bubbletea program (p.Send) or via commands. No background
// goroutine may read or write Model fields directly.
package app

import (
	"github.com/alanwnuczko/local-mesh/internal/discovery"
	"github.com/alanwnuczko/local-mesh/internal/transfer"
	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

// --- tea.Msg types (§3.2) ---

// PeerFoundMsg is sent by the bus when the discovery browser finds or refreshes
// a peer. Deduplicate by Peer.ID in Update.
type PeerFoundMsg struct {
	Peer discovery.Peer
}

// PeerLostMsg is sent by the bus when a peer disappears (TTL expiry or goodbye).
type PeerLostMsg struct {
	Peer discovery.Peer
}

// IncomingOfferMsg is sent by the bus when the TCP server receives an offer
// from a remote sender. The Reply channel must receive exactly one
// DecisionMessage; sending on it is done inside a tea.Cmd (never directly in
// Update) so the goroutine-safety rule is respected.
type IncomingOfferMsg struct {
	Offer protocol.OfferMessage
	// Reply is the channel the receive goroutine is blocked on. The UI sends
	// a DecisionMessage on it (via a tea.Cmd) to unblock the goroutine.
	Reply chan<- protocol.DecisionMessage
	// Handle aborts the receive session (close + ERR_ABORT) if the user cancels.
	Handle *transfer.Handle
}

// SendProgressMsg carries a progress snapshot from the active send goroutine.
type SendProgressMsg struct {
	Event transfer.ProgressEvent
}

// RecvProgressMsg carries a progress snapshot from the active receive goroutine.
type RecvProgressMsg struct {
	Event transfer.ProgressEvent
}

// SendDoneMsg signals that the send goroutine has finished (OK or error).
type SendDoneMsg struct {
	Event transfer.DoneEvent
}

// RecvDoneMsg signals that the receive goroutine has finished (OK or error).
type RecvDoneMsg struct {
	Event transfer.DoneEvent
}

// TransferErrorMsg is a generic error emitted when a transfer encounters an
// unrecoverable problem before it has been classified as send or receive.
type TransferErrorMsg struct {
	Err error
}

// StartSendMsg is dispatched by the confirm screen's Cmd to kick off a transfer.
type StartSendMsg struct {
	PeerAddr   string
	Path       string
	IsDir      bool
	SenderID   string
	SenderHost string
}

// SizeComputedMsg is sent when the pre-pass (size + checksum) finishes on the
// confirm screen, so the UI can display the exact values.
type SizeComputedMsg struct {
	Path     string // the file/dir this result belongs to
	Size     int64
	Checksum string
	Plan     *transfer.FolderPlan // non-nil for directory selections
	Err      error
}

// OverlayTimeoutMsg fires when the user has not accepted/rejected an incoming
// offer within OverlayTimeout.
type OverlayTimeoutMsg struct {
	TransferID string
}

// NetworkWarningMsg sets a persistent peer-list banner (firewall / UDP issues).
type NetworkWarningMsg struct {
	Text string
}
