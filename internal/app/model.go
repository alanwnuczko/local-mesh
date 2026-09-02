package app

import (
	"context"

	"github.com/alanwnuczko/local-mesh/internal/discovery"
	"github.com/alanwnuczko/local-mesh/internal/screens"
	"github.com/alanwnuczko/local-mesh/internal/transfer"
	"github.com/alanwnuczko/local-mesh/internal/trust"
	tea "github.com/charmbracelet/bubbletea"
)

// OnStartSendFunc is called by Update when the user confirms a send.
// It receives the newly created send progress and done channels so the bus
// can register forwarders for them. This keeps channel wiring in main.go
// without adding a dependency on the bus from the app package.
type OnStartSendFunc func(progress chan transfer.ProgressEvent, done chan transfer.DoneEvent)

// OnRefreshFunc is called when the user presses r on the peer list to
// force-refresh discovery (re-query mDNS + fire a UDP beacon).
type OnRefreshFunc func()

// Screen identifies which screen is currently active.
type Screen int

const (
	ScreenPeerList Screen = iota
	ScreenPicker
	ScreenConfirm
	ScreenProgress
)

// Model is the root Bubbletea model. It owns the screen state machine and the
// optional overlay.
//
// CONCURRENCY INVARIANT (§0, §3.5): Model fields are ONLY mutated inside
// Update. No background goroutine touches any field here.
type Model struct {
	// Identity.
	SelfID   string
	SelfHost string
	Trust    *trust.Store

	// Active screen.
	ActiveScreen Screen

	// Screen models (all kept alive; only the active one is rendered).
	PeerList *screens.PeerList
	Picker   *screens.Picker
	Confirm  *screens.Confirm
	Progress *screens.Progress

	// Overlay (nil when not active).
	Overlay *screens.OverlayState

	// Selection state carried between screens.
	SelectedPeer  discovery.Peer
	SelectedPath  string
	SelectedIsDir bool
	SelectedBatch []string

	// Transfer channels - created per-transfer and forwarded through the bus.
	SendProgress chan transfer.ProgressEvent
	SendDone     chan transfer.DoneEvent
	RecvProgress chan transfer.ProgressEvent
	RecvDone     chan transfer.DoneEvent

	// Indicates an active inbound transfer (for busy-reject logic).
	ReceiveBusy bool

	// Identity of the in-flight transfer the progress screen belongs to.
	// Done/progress events for any other id are ignored.
	activeTransferID string
	activeDirection  transfer.Direction
	xferAbort        *transfer.Handle
	busyPeerID       string // peer shown as busy in the list for this transfer

	// Precomputed payload meta from the confirm screen, reused by startSend
	// so a large folder is not hashed twice.
	folderPlan      *transfer.FolderPlan
	payloadSize     int64
	payloadChecksum string
	sizeCancel      context.CancelFunc // cancels an in-flight confirm pre-pass

	// OnStartSend is called when a send transfer begins so main.go can
	// register the new channels with the bus. Set before calling p.Run().
	OnStartSend OnStartSendFunc

	// OnRefresh is called when the user force-refreshes the peer list.
	// Set before calling p.Run(); it must only signal discovery (no Model access).
	OnRefresh OnRefreshFunc

	// Terminal dimensions.
	Width  int
	Height int

	// Whether the full help overlay is visible (? toggles it).
	ShowHelp bool

	// Footer is the persistent status bar rendered at the bottom of every screen.
	Footer *screens.Footer

	// Activity tracks the current transfer state for the footer indicator.
	Activity screens.ActivityStatus
}

// NewModel creates the root model, wiring up all screen sub-models.
func NewModel(selfID, selfHost string, width, height int) Model {
	store, _ := trust.Load()
	return Model{
		SelfID:   selfID,
		SelfHost: selfHost,
		Trust:    store,
		Width:    width,
		Height:   height,
		PeerList: screens.NewPeerList(selfID, selfHost, width, height),
		Picker:   screens.NewPicker(width, height),
		Footer:   screens.NewFooter(selfHost, selfID, width),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		// tea.ClearScreen forces an immediate repaint. Without this, on Windows
		// the alt-screen buffer stays blank until the first keypress or event.
		tea.ClearScreen,
		m.PeerList.Init(),
		m.Picker.Init(),
	)
}

func (m *Model) cancelSizeCompute() {
	if m.sizeCancel != nil {
		m.sizeCancel()
		m.sizeCancel = nil
	}
}
