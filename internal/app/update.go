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

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		if m.Footer != nil {
			m.Footer.SetWidth(msg.Width)
		}
		var peerCmd, pickerCmd, confirmCmd, progressCmd tea.Cmd
		m.PeerList, peerCmd = m.PeerList.Update(msg)
		m.Picker, pickerCmd = m.Picker.Update(msg)
		if m.Confirm != nil {
			m.Confirm, confirmCmd = m.Confirm.Update(msg)
		}
		if m.Progress != nil {
			m.Progress, progressCmd = m.Progress.Update(msg)
		}
		return m, tea.Batch(peerCmd, pickerCmd, confirmCmd, progressCmd)

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			m.abortActive()
			return m, tea.Quit
		}
		if m.Overlay != nil {
			return m.handleOverlayKey(msg)
		}

		if key.Matches(msg, GlobalKeys.Help) {
			m.ShowHelp = !m.ShowHelp
			return m, nil
		}

		if m.ShowHelp {
			if msg.String() == "esc" {
				m.ShowHelp = false
			}
			return m, nil
		}

	case PeerFoundMsg:
		cmd := m.PeerList.UpsertPeer(msg.Peer)
		return m, cmd

	case PeerLostMsg:
		m.PeerList.RemovePeer(msg.Peer.ID)
		return m, nil

	case IncomingOfferMsg:
		if m.isBusy() {
			return m, sendDecisionCmd(msg.Reply, protocol.DecisionMessage{
				TransferID: msg.Offer.TransferID,
				Accepted:   false,
				Reason:     protocol.ErrBusy,
			})
		}
		m.Overlay = &screens.OverlayState{Offer: msg.Offer, Reply: msg.Reply}
		m.xferAbort = msg.Handle
		return m, nil

	case SizeComputedMsg:
		if m.Confirm != nil && msg.Path == m.SelectedPath {
			m.Confirm.SetComputed(msg.Size, msg.Checksum, msg.Err)
			m.folderPlan = msg.Plan
			m.payloadSize = msg.Size
			m.payloadChecksum = msg.Checksum
		}
		return m, nil

	case SendProgressMsg:
		if !m.matchesActive(msg.Event.TransferID, transfer.DirSend) {
			return m, nil
		}
		if m.Progress != nil {
			m.Progress.ApplyProgress(msg.Event)
		}
		return m, nil

	case RecvProgressMsg:
		if !m.matchesActive(msg.Event.TransferID, transfer.DirRecv) {
			return m, nil
		}
		if m.Progress != nil {
			m.Progress.ApplyProgress(msg.Event)
		}
		return m, nil

	case RecvDoneMsg:
		if !m.matchesActive(msg.Event.TransferID, transfer.DirRecv) {
			return m, nil
		}
		m.finishActive(msg.Event.Err, msg.Event.SavedPath)
		return m, nil

	case SendDoneMsg:
		if !m.matchesActive(msg.Event.TransferID, transfer.DirSend) {
			return m, nil
		}
		m.finishActive(msg.Event.Err, msg.Event.SavedPath)
		return m, nil

	case TransferErrorMsg:
		// Dial failed before a session existed; the progress screen (if any)
		// belongs to the send we just tried to start.
		m.finishActive(msg.Err, "")
		return m, nil
	}

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

func (m Model) isBusy() bool {
	if m.ReceiveBusy || m.Overlay != nil {
		return true
	}
	if m.activeTransferID != "" {
		return true
	}
	switch m.Activity {
	case screens.ActivityTransferring, screens.ActivityReceiving:
		return true
	}
	return false
}

func (m Model) matchesActive(id string, dir transfer.Direction) bool {
	if m.activeTransferID == "" || id == "" {
		return false
	}
	if m.activeTransferID != id {
		return false
	}
	return m.activeDirection == dir
}

func (m *Model) finishActive(err error, savedPath string) {
	m.ReceiveBusy = false
	m.Activity = screens.ActivityIdle
	m.activeTransferID = ""
	m.activeDirection = 0
	m.xferAbort = nil
	if m.Progress != nil {
		m.Progress.SetDone(err, savedPath)
	}
}

func (m *Model) abortActive() {
	if m.xferAbort != nil {
		m.xferAbort.Abort()
	}
}

// ── peer list ─────────────────────────────────────────────────────────────────

func (m Model) updatePeerList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case msg.String() == "enter":
			if m.isBusy() {
				return m, nil
			}
			if peer, ok := m.PeerList.SelectedPeer(); ok {
				m.SelectedPeer = peer
				m.Picker.Reset()
				m.ActiveScreen = ScreenPicker
				return m, m.Picker.Init()
			}
		case key.Matches(msg, GlobalKeys.Refresh):
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
		m.folderPlan = nil
		m.payloadSize = 0
		m.payloadChecksum = ""
		m.Picker.Reset()
		m.Confirm = screens.NewConfirm(m.SelectedPeer, path, isDir, m.Width, m.Height)
		m.ActiveScreen = ScreenConfirm
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
			if m.isBusy() {
				return m, nil
			}
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
	if m.isBusy() {
		return m, nil
	}

	sendProgress := make(chan transfer.ProgressEvent, 32)
	sendDone := make(chan transfer.DoneEvent, 1)
	m.SendProgress = sendProgress
	m.SendDone = sendDone

	if m.OnStartSend != nil {
		m.OnStartSend(sendProgress, sendDone)
	}

	id := transfer.NewTransferID()
	handle := transfer.NewHandle()
	m.activeTransferID = id
	m.activeDirection = transfer.DirSend
	m.xferAbort = handle
	m.Progress = screens.NewProgress(transfer.DirSend, m.Confirm.Size(), m.Width, m.Height)
	m.ActiveScreen = ScreenProgress
	m.Activity = screens.ActivityTransferring

	addrs := m.SelectedPeer.DialAddrs()
	path := m.SelectedPath
	isDir := m.SelectedIsDir
	selfID := m.SelfID
	selfHost := m.SelfHost
	plan := m.folderPlan
	size := m.payloadSize
	checksum := m.payloadChecksum

	cmd := func() tea.Msg {
		if err := transfer.StartSend(addrs, transfer.SendConfig{
			Handle:     handle,
			TransferID: id,
			SenderID:   selfID,
			SenderHost: selfHost,
			Path:       path,
			IsDir:      isDir,
			Size:       size,
			Checksum:   checksum,
			Plan:       plan,
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
			if m.Progress != nil && !m.Progress.IsDone() {
				m.abortActive()
				return m, nil
			}
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
		m.activeTransferID = overlay.Offer.TransferID
		m.activeDirection = transfer.DirRecv
		m.Progress = screens.NewProgress(transfer.DirRecv, overlay.Offer.Size, m.Width, m.Height)
		m.ActiveScreen = ScreenProgress
		m.Activity = screens.ActivityReceiving
		return m, sendDecisionCmd(overlay.Reply, overlay.ReplyAccept())

	case "d", "N", "esc":
		overlay := m.Overlay
		m.Overlay = nil
		m.xferAbort = nil
		return m, sendDecisionCmd(overlay.Reply, overlay.ReplyReject("user declined"))
	}
	return m, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func sendDecisionCmd(reply chan<- protocol.DecisionMessage, dec protocol.DecisionMessage) tea.Cmd {
	return func() tea.Msg {
		if reply != nil {
			reply <- dec
		}
		return nil
	}
}

func computeSizeCmd(path string, isDir bool) tea.Cmd {
	return func() tea.Msg {
		if isDir {
			plan, err := transfer.PlanFolder(path)
			if err != nil {
				return SizeComputedMsg{Path: path, Err: err}
			}
			return SizeComputedMsg{
				Path:     path,
				Size:     plan.Size,
				Checksum: plan.Checksum,
				Plan:     plan,
			}
		}
		size, checksum, err := computePayloadMeta(path, false)
		return SizeComputedMsg{Path: path, Size: size, Checksum: checksum, Err: err}
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
