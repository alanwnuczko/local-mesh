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

// logoArt is the exact art from ASCII.md — box-drawing block characters that
// form "LOCALMESH". Each row must be the same rune-width for the gradient to
// be applied uniformly across all 6 lines.
var logoArt = [6]string{
	`██╗      ██████╗  ██████╗ █████╗ ██╗     ███╗   ███╗███████╗███████╗██╗  ██╗`,
	`██║     ██╔═══██╗██╔════╝██╔══██╗██║     ████╗ ████║██╔════╝██╔════╝██║  ██║`,
	`██║     ██║   ██║██║     ███████║██║     ██╔████╔██║█████╗  ███████╗███████║`,
	`██║     ██║   ██║██║     ██╔══██║██║     ██║╚██╔╝██║██╔══╝  ╚════██║██╔══██║`,
	`███████╗╚██████╔╝╚██████╗██║  ██║███████╗██║ ╚═╝ ██║███████╗███████║██║  ██║`,
	`╚══════╝ ╚═════╝  ╚═════╝╚═╝  ╚═╝╚══════╝╚═╝     ╚═╝╚══════╝╚══════╝╚═╝  ╚═╝`,
}

// renderLogo applies the left-to-right violet-to-cyan gradient (matching
// image.png) per character column and returns the coloured logo string.
// If the terminal panel is narrower than the logo, falls back to a single
// bold gradient title line.
func renderLogo(panelInnerW int) string {
	// Find max rune width across all rows (the last row is slightly wider).
	maxW := 0
	for _, row := range logoArt {
		if w := len([]rune(row)); w > maxW {
			maxW = w
		}
	}

	if panelInnerW < maxW {
		return renderGradientLine("LOCALMESH")
	}

	rows := make([]string, len(logoArt))
	for ri, line := range logoArt {
		var sb strings.Builder
		for ci, r := range []rune(line) {
			t := 0.0
			if maxW > 1 {
				t = float64(ci) / float64(maxW-1)
			}
			sb.WriteString(lipgloss.NewStyle().
				Foreground(gradientColor(t)).
				Render(string(r)))
		}
		rows[ri] = sb.String()
	}
	return strings.Join(rows, "\n")
}

// renderGradientLine applies a per-character violet-to-cyan gradient to a single line.
func renderGradientLine(line string) string {
	runes := []rune(line)
	n := len(runes)
	var sb strings.Builder
	for i, r := range runes {
		t := 0.0
		if n > 1 {
			t = float64(i) / float64(n-1)
		}
		sb.WriteString(lipgloss.NewStyle().
			Foreground(gradientColor(t)).
			Bold(true).
			Render(string(r)))
	}
	return sb.String()
}

// gradientColor interpolates from pink (#EC4899) to orange (#F97316).
func gradientColor(t float64) lipgloss.Color {
	r := lerp(0xEC, 0xF9, t)
	g := lerp(0x48, 0x73, t)
	b := lerp(0x99, 0x16, t)
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", r, g, b))
}

func lerp(a, b int, t float64) int {
	v := float64(a) + t*float64(b-a)
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return int(v)
}

// logoLineCount is the number of text rows the logo occupies.
const logoLineCount = 6

// peerItem wraps a discovery.Peer so it satisfies list.Item.
type peerItem struct {
	peer discovery.Peer
}

func (p peerItem) Title() string {
	return fmt.Sprintf("%s  %s", p.peer.Hostname, ui.StyleMuted.Render("("+p.peer.ShortID()+")"))
}

func (p peerItem) Description() string {
	return fmt.Sprintf("%s/%s  %s", p.peer.OS, p.peer.Arch, ui.StyleMuted.Render(p.peer.Addr()))
}

func (p peerItem) FilterValue() string { return p.peer.Hostname + " " + p.peer.ID }

// PeerList is the default screen: a live list of discovered peers (§4.1).
type PeerList struct {
	list     list.Model
	spinner  spinner.Model
	selfID   string
	selfHost string
	width    int
	height   int
}

// NewPeerList creates the peer-list screen.
func NewPeerList(selfID, selfHost string, width, height int) *PeerList {
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(ui.ColorAccent).
		BorderLeftForeground(ui.ColorPrimary).
		UnsetBackground()
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(ui.ColorMuted).
		BorderLeftForeground(ui.ColorPrimary).
		UnsetBackground()

	innerW, innerH := panelInnerSize(width, height)
	listH := innerH - logoLineCount - 3
	if listH < 2 {
		listH = 2
	}
	l := list.New(nil, delegate, innerW, listH)
	l.Title = ""
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.KeyMap.Quit.SetEnabled(false)

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
		innerW, innerH := panelInnerSize(msg.Width, msg.Height)
		listH := innerH - logoLineCount - 3
		if listH < 2 {
			listH = 2
		}
		pl.list.SetSize(innerW, listH)

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
	innerW, _ := panelInnerSize(pl.width, pl.height)

	var sb strings.Builder
	sb.WriteString(renderLogo(innerW))
	sb.WriteString("\n")

	if len(pl.list.Items()) == 0 {
		sb.WriteString(fmt.Sprintf("\n  %s %s\n",
			pl.spinner.View(),
			ui.StyleMuted.Render("Searching for peers on your network...")))
		sb.WriteString(ui.HelpStyle.Render("\n  q quit  ?  help"))
	} else {
		sb.WriteString(pl.list.View())
		sb.WriteString("\n" + ui.HelpStyle.Render("  enter select  r refresh  q quit  ? help"))
	}

	return wrapInPanel(sb.String(), pl.width, pl.height)
}

// UpsertPeer adds or refreshes a peer entry. Called from the root Update only.
func (pl *PeerList) UpsertPeer(p discovery.Peer) tea.Cmd {
	items := pl.list.Items()
	for i, it := range items {
		if it.(peerItem).peer.ID == p.ID {
			return pl.list.SetItem(i, peerItem{peer: p})
		}
	}
	return pl.list.InsertItem(len(items), peerItem{peer: p})
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
