package app

import (
	"github.com/alanwnuczko/local-mesh/internal/discovery"
	"github.com/alanwnuczko/local-mesh/internal/screens"
	"github.com/alanwnuczko/local-mesh/internal/transfer"
	tea "github.com/charmbracelet/bubbletea"
)

// OnStartSendFunc is called by Update when the user confirms a send.
// It receives the newly created send progress and done channels so the bus
// can register forwarders for them. This keeps channel wiring in main.go
// without adding a dependency on the bus from the app package.
type OnStartSendFunc func(progress chan transfer.ProgressEvent, done chan transfer.DoneEvent)

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
	SelectedPeer discovery.Peer
	SelectedPath string
	SelectedIsDir bool

	// Transfer channels - created per-transfer and forwarded through the bus.
	SendProgress chan transfer.ProgressEvent
	SendDone     chan transfer.DoneEvent
	RecvProgress chan transfer.ProgressEvent
	RecvDone     chan transfer.DoneEvent

	// Indicates an active inbound transfer (for busy-reject logic).
	ReceiveBusy bool

	// OnStartSend is called when a send transfer begins so main.go can
	// register the new channels with the bus. Set before calling p.Run().
	OnStartSend OnStartSendFunc

	// Terminal dimensions.
	Width  int
	Height int

	// Whether the help footer is expanded.
	ShowHelp bool

	// Footer is the persistent status bar rendered at the bottom of every screen.
	Footer *screens.Footer

	// Activity tracks the current transfer state for the footer indicator.
	Activity screens.ActivityStatus
}

// NewModel creates the root model, wiring up all screen sub-models.
func NewModel(selfID, selfHost string, width, height int) Model {
	return Model{
		SelfID:   selfID,
		SelfHost: selfHost,
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
