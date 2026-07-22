package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/alanwnuczko/local-mesh/internal/transfer"
	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

// Progress is the transfer progress screen (§4.4).
// Used for both send and receive; direction controls labels.
type Progress struct {
	bar        progress.Model
	direction  transfer.Direction
	phase      transfer.Phase
	bytesDone  int64
	bytesTotal int64
	bps        float64
	startTime  time.Time
	lastErr    string
	done       bool
	width      int
	height     int
}

// NewProgress creates the progress screen.
func NewProgress(dir transfer.Direction, total int64, width, height int) *Progress {
	bar := progress.New(progress.WithDefaultGradient())
	bar.Width = width - 10
	return &Progress{
		bar:        bar,
		direction:  dir,
		bytesTotal: total,
		startTime:  time.Now(),
		width:      width,
		height:     height,
	}
}

func (p *Progress) Init() tea.Cmd { return nil }

// ApplyProgress updates progress from an event. Called by root Update only.
func (p *Progress) ApplyProgress(ev transfer.ProgressEvent) {
	p.phase = ev.Phase
	if ev.BytesDone > 0 {
		p.bytesDone = ev.BytesDone
	}
	if ev.BytesTotal > 0 {
		p.bytesTotal = ev.BytesTotal
	}
	p.bps = ev.BytesPerSec
}

// SetDone marks the transfer complete (ok or error).
func (p *Progress) SetDone(err error) {
	p.done = true
	if err != nil {
		p.phase = transfer.PhaseFailed
		p.lastErr = err.Error()
	} else {
		p.phase = transfer.PhaseDone
		p.bytesDone = p.bytesTotal
	}
}

func (p *Progress) Update(msg tea.Msg) (*Progress, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.bar.Width = msg.Width - 10
	}
	_, barCmd := p.bar.Update(msg)
	cmds = append(cmds, barCmd)
	return p, tea.Batch(cmds...)
}

func (p *Progress) View() string {
	var sb strings.Builder

	dirLabel := "Sending"
	if p.direction == transfer.DirRecv {
		dirLabel = "Receiving"
	}
	sb.WriteString(ui.TitleStyle.Render(dirLabel) + "\n\n")

	phStyle := ui.HighlightStyle
	switch p.phase {
	case transfer.PhaseDone:
		phStyle = ui.SuccessStyle
	case transfer.PhaseFailed:
		phStyle = ui.ErrorStyle
	}
	sb.WriteString(fmt.Sprintf("  Phase:    %s\n", phStyle.Render(p.phase.String())))

	var pct float64
	if p.bytesTotal > 0 {
		pct = float64(p.bytesDone) / float64(p.bytesTotal)
		if pct > 1 {
			pct = 1
		}
	}
	sb.WriteString("  " + p.bar.ViewAs(pct) + "\n\n")

	sb.WriteString(fmt.Sprintf("  Progress: %s / %s\n",
		formatBytes(p.bytesDone), formatBytes(p.bytesTotal)))
	if p.bps > 0 {
		sb.WriteString(fmt.Sprintf("  Speed:    %s/s\n", formatBytes(int64(p.bps))))
	}
	elapsed := time.Since(p.startTime).Round(time.Second)
	sb.WriteString(fmt.Sprintf("  Elapsed:  %s\n", elapsed))

	if p.lastErr != "" {
		sb.WriteString("\n  " + ui.ErrorStyle.Render("Error: "+p.lastErr) + "\n")
	}
	if p.done {
		if p.phase == transfer.PhaseDone {
			sb.WriteString("\n  " + ui.SuccessStyle.Render("✓ Transfer complete") + "\n")
		}
		sb.WriteString(ui.HelpStyle.Render("\n  enter/esc return to peer list"))
	} else {
		sb.WriteString(ui.HelpStyle.Render("\n  c cancel transfer"))
	}
	return sb.String()
}

// IsDone returns true when the transfer has completed (success or failure).
func (p *Progress) IsDone() bool { return p.done }
