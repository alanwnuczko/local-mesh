package screens

import (
	"os"

	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/charmbracelet/bubbles/filepicker"
	tea "github.com/charmbracelet/bubbletea"
)

// Picker is the file/folder selection screen (§4.2).
type Picker struct {
	fp       filepicker.Model
	selected string
	isDir    bool
	width    int
	height   int
}

// NewPicker creates the file picker rooted at the user's home directory.
func NewPicker(width, height int) *Picker {
	home, _ := os.UserHomeDir()

	fp := filepicker.New()
	fp.CurrentDirectory = home
	fp.AllowedTypes = nil
	fp.ShowHidden = false
	fp.Height = height - 6

	return &Picker{fp: fp, width: width, height: height}
}

// FileSelected returns the last selected path, isDir flag, and whether a
// selection has been made.
func (p *Picker) FileSelected() (path string, isDir bool, ok bool) {
	if p.selected == "" {
		return "", false, false
	}
	return p.selected, p.isDir, true
}

// Reset clears any previous selection.
func (p *Picker) Reset() {
	p.selected = ""
	p.isDir = false
}

func (p *Picker) Init() tea.Cmd { return p.fp.Init() }

func (p *Picker) Update(msg tea.Msg) (*Picker, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.fp.Height = msg.Height - 6

	case tea.KeyMsg:
		if msg.String() == "s" {
			// Select current directory as folder transfer.
			dir := p.fp.CurrentDirectory
			info, err := os.Stat(dir)
			if err == nil && info.IsDir() {
				p.selected = dir
				p.isDir = true
				return p, nil
			}
		}
	}

	var cmd tea.Cmd
	p.fp, cmd = p.fp.Update(msg)
	cmds = append(cmds, cmd)

	if didSelect, path := p.fp.DidSelectFile(msg); didSelect {
		info, err := os.Stat(path)
		if err == nil {
			p.selected = path
			p.isDir = info.IsDir()
		}
	}

	return p, tea.Batch(cmds...)
}

func (p *Picker) View() string {
	header := ui.TitleStyle.Render("Select file or folder") + "\n"
	help := ui.HelpStyle.Render("  ↑/↓ navigate • enter select file • s select current folder • esc back • q quit")
	return header + p.fp.View() + "\n" + help
}
