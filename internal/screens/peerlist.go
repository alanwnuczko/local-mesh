package screens

import (
	"fmt"
	"strings"

	"github.com/alanwnuczko/local-mesh/internal/discovery"
	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// peerItem wraps a discovery.Peer so it satisfies list.Item.
type peerItem struct {
	peer discovery.Peer
}

func (p peerItem) Title() string {
	return fmt.Sprintf("%s  %s", p.peer.Hostname, ui.MutedStyle.Render("("+p.peer.ShortID()+")"))
}

func (p peerItem) Description() string {
	return fmt.Sprintf("%s/%s  %s", p.peer.OS, p.peer.Arch, ui.MutedStyle.Render(p.peer.Addr()))
}

func (p peerItem) FilterValue() string { return p.peer.Hostname + " " + p.peer.ID }

// PeerList is the default screen: a live list of discovered peers (§4.1).
type PeerList struct {
	list      list.Model
	spinner   spinner.Model
	selfID    string
	selfHost  string
	width     int
	height    int
}

// NewPeerList creates the peer-list screen.
func NewPeerList(selfID, selfHost string, width, height int) *PeerList {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(ui.ColorAccent).
		BorderLeftForeground(ui.ColorPrimary)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(ui.ColorMuted).
		BorderLeftForeground(ui.ColorPrimary)

	l := list.New(nil, delegate, width, height-4)
	l.Title = "local-mesh"
	l.Styles.Title = ui.TitleStyle
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.KeyMap.Quit.SetEnabled(false) // handled by root

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ui.ColorAccent)

	return &PeerList{
		list:     l,
		spinner:  sp,
		selfID:   selfID,
		selfHost: selfHost,
		width:    width,
		height:   height,
	}
}

func (pl *PeerList) Init() tea.Cmd {
	return pl.spinner.Tick
}

func (pl *PeerList) Update(msg tea.Msg) (*PeerList, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		pl.width = msg.Width
		pl.height = msg.Height
		pl.list.SetSize(msg.Width, msg.Height-4)

	case spinner.TickMsg:
		var cmd tea.Cmd
		pl.spinner, cmd = pl.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	var listCmd tea.Cmd
	pl.list, listCmd = pl.list.Update(msg)
	cmds = append(cmds, listCmd)

	return pl, tea.Batch(cmds...)
}

func (pl *PeerList) View() string {
	var sb strings.Builder

	// Header showing our own identity.
	shortID := pl.selfID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	header := fmt.Sprintf("  %s  %s",
		ui.HighlightStyle.Render(pl.selfHost),
		ui.MutedStyle.Render("id:"+shortID),
	)
	sb.WriteString(header + "\n")

	if len(pl.list.Items()) == 0 {
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("  %s %s\n",
			pl.spinner.View(),
			ui.MutedStyle.Render("Searching for peers on your network…")))
		sb.WriteString(ui.HelpStyle.Render("\n  q quit • ? help"))
		return sb.String()
	}

	sb.WriteString(pl.list.View())
	hints := ui.HelpStyle.Render("  ↑/↓ navigate • enter select • r refresh • q quit • ? help")
	sb.WriteString("\n" + hints)

	return sb.String()
}

// UpsertPeer adds or refreshes a peer entry. Called from the root Update only.
func (pl *PeerList) UpsertPeer(p discovery.Peer) {
	items := pl.list.Items()
	for i, it := range items {
		if it.(peerItem).peer.ID == p.ID {
			items[i] = peerItem{peer: p}
			pl.list.SetItems(items)
			return
		}
	}
	pl.list.InsertItem(len(items), peerItem{peer: p})
}

// RemovePeer removes a peer by device ID. Called from the root Update only.
func (pl *PeerList) RemovePeer(id string) {
	items := pl.list.Items()
	for i, it := range items {
		if it.(peerItem).peer.ID == id {
			pl.list.RemoveItem(i)
			return
		}
	}
}

// SelectedPeer returns the currently highlighted peer, if any.
func (pl *PeerList) SelectedPeer() (discovery.Peer, bool) {
	item := pl.list.SelectedItem()
	if item == nil {
		return discovery.Peer{}, false
	}
	pi, ok := item.(peerItem)
	return pi.peer, ok
}
