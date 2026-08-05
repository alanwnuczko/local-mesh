package app

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"

	"github.com/alanwnuczko/local-mesh/internal/screens"
	"github.com/alanwnuczko/local-mesh/internal/transfer"
	"github.com/alanwnuczko/local-mesh/pkg/protocol"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Update is the single entry point for all state transitions. It is called by
// Bubbletea's event loop - always on the same goroutine - so Model fields may
// be mutated freely here and only here.
//
// CONCURRENCY INVARIANT (§0, §3.5): No background goroutine touches Model
// fields. All cross-goroutine communication is via tea.Msg (through program.Send
// forwarded by the bus) or tea.Cmd return values.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// ── global messages (processed on every screen) ──────────────────────────
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		if m.Footer != nil {
			m.Footer.SetWidth(msg.Width)
		}
		// M-11: collect returned tea.Cmd values from child updates so that
		// in-flight commands (spinner ticks, directory reads) are not dropped
		// when a resize arrives.
		var sizeCmds []tea.Cmd
		var peerCmd, pickerCmd, confirmCmd, progressCmd tea.Cmd
		m.PeerList, peerCmd = m.PeerList.Update(msg)
		m.Picker, pickerCmd = m.Picker.Update(msg)
		if m.Confirm != nil {
			m.Confirm, confirmCmd = m.Confirm.Update(msg)
		}
		if m.Progress != nil {
			m.Progress, progressCmd = m.Progress.Update(msg)
		}
		sizeCmds = append(sizeCmds, peerCmd, pickerCmd, confirmCmd, progressCmd)
		return m, tea.Batch(sizeCmds...)

	case tea.KeyMsg:
		// ctrl+c always quits, even when the overlay is active (H-10).
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// Overlay captures all other key input while active.
		if m.Overlay != nil {
			return m.handleOverlayKey(msg)
		}

		// ? toggles the help overlay on every screen (spec §4 global keys).
		// Must return here so the key is not also consumed by sub-models
		// (e.g. bubbles/list's own ShowFullHelp binding).
		if key.Matches(msg, GlobalKeys.Help) {
			m.ShowHelp = !m.ShowHelp
			return m, nil
		}

		// While help is open, capture all input: esc/? closes, q quits from
		// the peer list, everything else is ignored so keys don't leak through.
		if m.ShowHelp {
			switch msg.String() {
			case "esc":
				m.ShowHelp = false
			case "q":
				if m.ActiveScreen == ScreenPeerList {
					return m, tea.Quit
				}
				m.ShowHelp = false
			}
			return m, nil
		}

		if msg.String() == "q" && m.ActiveScreen == ScreenPeerList {
			return m, tea.Quit
		}

	case PeerFoundMsg:
		cmd := m.PeerList.UpsertPeer(msg.Peer)
		return m, cmd

	case PeerLostMsg:
		m.PeerList.RemovePeer(msg.Peer.ID)
		return m, nil

	case IncomingOfferMsg:
		// H-1: reject if already receiving OR if overlay is already showing.
		// Without the Overlay check the first offer's Reply channel is abandoned,
		// leaking the goroutine and the TCP connection.
		if m.ReceiveBusy || m.Overlay != nil {
			// Already transferring or showing overlay - auto-reject with ERR_BUSY (§4.5, §6).
			return m, sendDecisionCmd(msg.Reply, protocol.DecisionMessage{
				TransferID: msg.Offer.TransferID,
				Accepted:   false,
				Reason:     protocol.ErrBusy,
			})
		}
		m.Overlay = &screens.OverlayState{Offer: msg.Offer, Reply: msg.Reply}
		return m, nil

	case SizeComputedMsg:
		if m.Confirm != nil {
			m.Confirm.SetComputed(msg.Size, msg.Checksum, msg.Err)
		}
		return m, nil

	case SendProgressMsg:
		if m.Progress != nil {
			m.Progress.ApplyProgress(msg.Event)
		}
		return m, nil

	case RecvProgressMsg:
		if m.Progress != nil {
			m.Progress.ApplyProgress(msg.Event)
		}
		return m, nil

	case RecvDoneMsg:
		m.ReceiveBusy = false
		m.Activity = screens.ActivityIdle
		if m.Progress != nil {
			m.Progress.SetDone(msg.Event.Err)
		}
		return m, nil

	case SendDoneMsg:
		m.Activity = screens.ActivityIdle
		if m.Progress != nil {
			m.Progress.SetDone(msg.Event.Err)
		}
		return m, nil

	case TransferErrorMsg:
		if m.Progress != nil {
			m.Progress.SetDone(msg.Err)
		}
		return m, nil
	}

	// ── screen-specific routing ───────────────────────────────────────────────
	switch m.ActiveScreen {
	case ScreenPeerList:
		return m.updatePeerList(msg)
	case ScreenPicker:
		return m.updatePicker(msg)
	case ScreenConfirm:
		return m.updateConfirm(msg)
	case ScreenProgress:
		return m.updateProgress(msg)
	}

	return m, tea.Batch(cmds...)
}

// ── peer list ─────────────────────────────────────────────────────────────────

func (m Model) updatePeerList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.String() == "enter":
			if peer, ok := m.PeerList.SelectedPeer(); ok {
				m.SelectedPeer = peer
				m.Picker.Reset()
				m.ActiveScreen = ScreenPicker
				return m, m.Picker.Init()
			}
		case key.Matches(msg, GlobalKeys.Refresh):
			// Force-refresh: re-query mDNS and fire a UDP beacon (spec §4.1).
			// OnRefresh only signals discovery channels — no Model access.
			if m.OnRefresh != nil {
				m.OnRefresh()
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.PeerList, cmd = m.PeerList.Update(msg)
	return m, cmd
}

// ── file picker ───────────────────────────────────────────────────────────────

func (m Model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "esc" {
		m.ActiveScreen = ScreenPeerList
		return m, nil
	}

	var cmd tea.Cmd
	m.Picker, cmd = m.Picker.Update(msg)
	cmds = append(cmds, cmd)

	if path, isDir, ok := m.Picker.FileSelected(); ok {
		m.SelectedPath = path
		m.SelectedIsDir = isDir
		m.Picker.Reset()
		m.Confirm = screens.NewConfirm(m.SelectedPeer, path, isDir, m.Width, m.Height)
		m.ActiveScreen = ScreenConfirm
		// Kick off the size+checksum pre-pass as a non-blocking tea.Cmd.
		cmds = append(cmds, computeSizeCmd(path, isDir), m.Confirm.Init())
		return m, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

// ── confirm ───────────────────────────────────────────────────────────────────

func (m Model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc", "N":
			m.ActiveScreen = ScreenPicker
			return m, nil
		case "y", "enter":
			if m.Confirm != nil && m.Confirm.IsReady() {
				return m.startSend()
			}
		}
	}

	var cmd tea.Cmd
	if m.Confirm != nil {
		m.Confirm, cmd = m.Confirm.Update(msg)
	}
	return m, cmd
}

func (m Model) startSend() (tea.Model, tea.Cmd) {
	sendProgress := make(chan transfer.ProgressEvent, 32)
	sendDone := make(chan transfer.DoneEvent, 1)
	m.SendProgress = sendProgress
	m.SendDone = sendDone

	// Notify main.go to register these channels with the bus.
	// OnStartSend only writes to a buffered channel - no Model fields touched.
	if m.OnStartSend != nil {
		m.OnStartSend(sendProgress, sendDone)
	}

	m.Progress = screens.NewProgress(transfer.DirSend, m.Confirm.Size(), m.Width, m.Height)
	m.ActiveScreen = ScreenProgress
	m.Activity = screens.ActivityTransferring

	addr := m.SelectedPeer.Addr()
	path := m.SelectedPath
	isDir := m.SelectedIsDir
	selfID := m.SelfID
	selfHost := m.SelfHost

	cmd := func() tea.Msg {
		if err := transfer.StartSend(addr, transfer.SendConfig{
			SenderID:   selfID,
			SenderHost: selfHost,
			Path:       path,
			IsDir:      isDir,
			Progress:   sendProgress,
			Done:       sendDone,
		}); err != nil {
			return TransferErrorMsg{Err: err}
		}
		return nil
	}
	return m, cmd
}

// ── progress ──────────────────────────────────────────────────────────────────

func (m Model) updateProgress(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "c":
			// H-2: reset ReceiveBusy so future incoming transfers are not
			// permanently blocked after the user dismisses the progress screen.
			m.ActiveScreen = ScreenPeerList
			m.Progress = nil
			m.Activity = screens.ActivityIdle
			m.ReceiveBusy = false
			return m, nil
		case "enter", "esc":
			if m.Progress != nil && m.Progress.IsDone() {
				m.ActiveScreen = ScreenPeerList
				m.Progress = nil
				return m, nil
			}
		}
	}

	var cmd tea.Cmd
	if m.Progress != nil {
		m.Progress, cmd = m.Progress.Update(msg)
	}
	return m, cmd
}

// ── overlay ───────────────────────────────────────────────────────────────────

func (m Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.Overlay == nil {
		return m, nil
	}
	switch msg.String() {
	case "a", "y":
		overlay := m.Overlay
		m.Overlay = nil
		m.ReceiveBusy = true
		// Show receive-progress screen.
		m.Progress = screens.NewProgress(transfer.DirRecv, overlay.Offer.Size, m.Width, m.Height)
		m.ActiveScreen = ScreenProgress
		m.Activity = screens.ActivityReceiving
		// Send accept decision via Cmd (not directly in Update) to preserve the
		// concurrency invariant: channel sends happen in Cmd goroutines only.
		return m, sendDecisionCmd(overlay.Reply, overlay.ReplyAccept())

	case "d", "N", "esc":
		overlay := m.Overlay
		m.Overlay = nil
		return m, sendDecisionCmd(overlay.Reply, overlay.ReplyReject("user declined"))
	}
	return m, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// sendDecisionCmd returns a tea.Cmd that sends the decision on the reply
// channel inside a goroutine - never touching Model fields.
func sendDecisionCmd(reply chan<- protocol.DecisionMessage, dec protocol.DecisionMessage) tea.Cmd {
	return func() tea.Msg {
		reply <- dec
		return nil
	}
}

// computeSizeCmd kicks off the size+checksum pre-pass in a Cmd goroutine.
func computeSizeCmd(path string, isDir bool) tea.Cmd {
	return func() tea.Msg {
		size, checksum, err := computePayloadMeta(path, isDir)
		return SizeComputedMsg{Size: size, Checksum: checksum, Err: err}
	}
}

func computePayloadMeta(path string, isDir bool) (int64, string, error) {
	h := sha256.New()
	var size int64
	if isDir {
		cw := &countWriter{w: h}
		if err := tarFolderImpl(path, cw); err != nil {
			return 0, "", err
		}
		size = cw.n
	} else {
		f, err := os.Open(path)
		if err != nil {
			return 0, "", err
		}
		defer f.Close()
		n, err := io.Copy(h, f)
		if err != nil {
			return 0, "", err
		}
		size = n
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (cw *countWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n += int64(n)
	return n, err
}
