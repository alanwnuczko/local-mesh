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
	peer        discovery.Peer
	path        string
	isDir       bool
	size        int64
	checksum    string
	computing   bool
	computeErr  error
	pairingCode string
	fileCount   int
	spinner     spinner.Model
	width       int
	height      int
}

// NewConfirm creates the confirmation screen.
func NewConfirm(peer discovery.Peer, path string, isDir bool, width, height int) *Confirm {
	return newConfirm(peer, path, isDir, "", 0, width, height)
}

// NewConfirmWithMeta creates a confirmation screen with pairing and batch info.
func NewConfirmWithMeta(peer discovery.Peer, path string, isDir bool, pairingCode string, fileCount, width, height int) *Confirm {
	return newConfirm(peer, path, isDir, pairingCode, fileCount, width, height)
}

func newConfirm(peer discovery.Peer, path string, isDir bool, pairingCode string, fileCount, width, height int) *Confirm {
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = lipgloss.NewStyle().Foreground(ui.ColorAccent)

	return &Confirm{
		peer:        peer,
		path:        path,
		isDir:       isDir,
		computing:   true,
		pairingCode: pairingCode,
		fileCount:   fileCount,
		spinner:     sp,
		width:       width,
		height:      height,
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

	kind := "file"
	if c.isDir {
		kind = "folder"
	}

	sb.WriteString(row("Target", ui.StyleAccent.Render(c.peer.Hostname)+"  "+ui.StyleMuted.Render("("+c.peer.ShortID()+")")))
	sb.WriteString(row("Type", ui.StyleValue.Render(kind)))
	if c.fileCount > 1 {
		sb.WriteString(row("Name", ui.StyleValue.Render(fmt.Sprintf("%d files", c.fileCount))))
	} else {
		sb.WriteString(row("Name", ui.StyleValue.Render(filepath.Base(c.path))))
	}
	if c.pairingCode != "" {
		sb.WriteString(row("Code", ui.StyleAccent.Render(c.pairingCode)+"  "+ui.StyleMuted.Render("(must match receiver)")))
	}

	if c.computeErr != nil {
		sb.WriteString("\n  " + ui.StyleDanger.Render("Error: "+c.computeErr.Error()) + "\n")
		sb.WriteString(ui.HelpStyle.Render("  esc back  q quit"))
	} else if c.computing {
		sb.WriteString("\n  " + c.spinner.View() + "  " + ui.StyleMuted.Render("Calculating size and checksum...") + "\n")
	} else {
		sb.WriteString(row("Size", ui.StyleAccent.Render(formatBytes(c.size))))
		if len(c.checksum) > 16 {
			sb.WriteString(row("SHA-256", ui.StyleMuted.Render(c.checksum[:16]+"...")))
		}
		sb.WriteString("\n")
		sb.WriteString("  " + ui.StyleSuccess.Render("y / enter") + ui.StyleMuted.Render("  confirm   "))
		sb.WriteString(ui.StyleDanger.Render("N / esc") + ui.StyleMuted.Render("  cancel") + "\n")
	}
	return wrapInPanel(sb.String(), c.width, c.height)
}

// row renders a label-value pair with consistent column alignment.
func row(label, value string) string {
	return fmt.Sprintf("  %s  %s\n",
		ui.StyleLabel.Render(fmt.Sprintf("%-8s", label+":")),
		value,
	)
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
