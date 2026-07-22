package screens

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/alanwnuczko/local-mesh/internal/discovery"
	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Confirm is the transfer confirmation screen (§4.3).
type Confirm struct {
	peer       discovery.Peer
	path       string
	isDir      bool
	size       int64
	checksum   string
	computing  bool
	computeErr error
	spinner    spinner.Model
	width      int
	height     int
}

// NewConfirm creates the confirmation screen.
func NewConfirm(peer discovery.Peer, path string, isDir bool, width, height int) *Confirm {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(ui.ColorAccent)

	return &Confirm{
		peer:      peer,
		path:      path,
		isDir:     isDir,
		computing: true,
		spinner:   sp,
		width:     width,
		height:    height,
	}
}

// SetComputed sets the pre-computed size and checksum.
func (c *Confirm) SetComputed(size int64, checksum string, err error) {
	c.computing = false
	c.size = size
	c.checksum = checksum
	c.computeErr = err
}

func (c *Confirm) Init() tea.Cmd { return c.spinner.Tick }

func (c *Confirm) Update(msg tea.Msg) (*Confirm, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
	case spinner.TickMsg:
		var cmd tea.Cmd
		c.spinner, cmd = c.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}
	return c, tea.Batch(cmds...)
}

func (c *Confirm) View() string {
	var sb strings.Builder
	sb.WriteString(ui.TitleStyle.Render("Confirm Transfer") + "\n\n")

	sb.WriteString(fmt.Sprintf("  Target:  %s  %s\n",
		ui.HighlightStyle.Render(c.peer.Hostname),
		ui.MutedStyle.Render("("+c.peer.ShortID()+")")))

	kind := "file"
	if c.isDir {
		kind = "folder"
	}
	sb.WriteString(fmt.Sprintf("  Type:    %s\n", kind))
	sb.WriteString(fmt.Sprintf("  Path:    %s\n", ui.MutedStyle.Render(filepath.Base(c.path))))

	if c.computeErr != nil {
		sb.WriteString("\n  " + ui.ErrorStyle.Render("Error: "+c.computeErr.Error()) + "\n")
		sb.WriteString(ui.HelpStyle.Render("  esc back • q quit"))
	} else if c.computing {
		sb.WriteString(fmt.Sprintf("\n  Size:    %s %s\n",
			c.spinner.View(), ui.MutedStyle.Render("calculating…")))
	} else {
		sb.WriteString(fmt.Sprintf("\n  Size:    %s\n", ui.HighlightStyle.Render(formatBytes(c.size))))
		if len(c.checksum) > 16 {
			sb.WriteString(fmt.Sprintf("  SHA-256: %s\n", ui.MutedStyle.Render(c.checksum[:16]+"…")))
		}
		sb.WriteString("\n" + ui.HelpStyle.Render("  y/enter confirm • N/esc cancel • q quit"))
	}
	return sb.String()
}

// IsReady returns true when the compute pre-pass has finished without error.
func (c *Confirm) IsReady() bool { return !c.computing && c.computeErr == nil }

// Size returns the computed payload size in bytes.
func (c *Confirm) Size() int64 { return c.size }

// formatBytes is shared by confirm and progress screens.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
