package screens

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/charmbracelet/bubbles/filepicker"
	tea "github.com/charmbracelet/bubbletea"
)

// Picker is the file/folder selection screen (§4.2).
type Picker struct {
	fp       filepicker.Model
	selected string
	isDir    bool
	batch    []string
	marked   map[string]struct{}
	entries  []os.DirEntry
	cursor   int
	listed   string
	termW    int
	termH    int
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
	fp.AutoHeight = false
	_, innerH := panelInnerSize(width, height)
	fp.Height = innerH - 3
	if fp.Height < 3 {
		fp.Height = 3
	}

	p := &Picker{fp: fp, width: width, height: height, termW: width, termH: height, marked: map[string]struct{}{}}
	p.reloadEntries()
	return p
}

// FileSelected returns the last selected path, isDir flag, and whether a
// selection has been made.
func (p *Picker) FileSelected() (path string, isDir bool, ok bool) {
	if p.selected == "" && len(p.batch) == 0 {
		return "", false, false
	}
	if len(p.batch) > 0 {
		return p.batch[0], true, true
	}
	return p.selected, p.isDir, true
}

// SelectedBatch returns the multi-selected files, if any.
func (p *Picker) SelectedBatch() []string {
	return p.batch
}

// Reset clears any previous selection (keeps marks until consumed).
func (p *Picker) Reset() {
	p.selected = ""
	p.isDir = false
	p.batch = nil
	p.marked = map[string]struct{}{}
}

func (p *Picker) Init() tea.Cmd { return p.fp.Init() }

func (p *Picker) Update(msg tea.Msg) (*Picker, tea.Cmd) {
	var cmds []tea.Cmd
	fpMsg := msg

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height
		p.termW = msg.Width
		p.termH = msg.Height
		_, innerH := panelInnerSize(msg.Width, msg.Height)
		p.fp.Height = innerH - 3
		if p.fp.Height < 3 {
			p.fp.Height = 3
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "s":
			dir := p.fp.CurrentDirectory
			info, err := os.Stat(dir)
			if err == nil && info.IsDir() {
				p.selected = dir
				p.isDir = true
				p.batch = nil
				return p, nil
			}
		case " ":
			p.reloadEntries()
			p.toggleMarked()
			return p, nil
		case "enter":
			if len(p.marked) > 0 {
				p.batch = p.markedPaths()
				p.selected = p.batch[0]
				p.isDir = true
				return p, nil
			}
		case "j", "down":
			p.reloadEntries()
			if n := len(p.entries); n > 0 && p.cursor >= n-1 {
				p.cursor = 0
				fpMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}
			} else {
				p.trackCursor("j")
			}
		case "k", "up":
			p.reloadEntries()
			if len(p.entries) > 0 && p.cursor <= 0 {
				p.cursor = len(p.entries) - 1
				fpMsg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")}
			} else {
				p.trackCursor("k")
			}
		default:
			p.trackCursor(msg.String())
		}
	}

	var cmd tea.Cmd
	p.fp, cmd = p.fp.Update(fpMsg)
	cmds = append(cmds, cmd)
	p.reloadEntries()

	if didSelect, path := p.fp.DidSelectFile(msg); didSelect && len(p.marked) == 0 {
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
	if n := len(p.marked); n > 0 {
		header += ui.StyleAccent.Render(fmt.Sprintf("  %d selected", n)) + "\n"
	}
	help := ui.HelpStyle.Render("  enter select  space toggle  s folder  esc back  q quit")
	content := header + p.fp.View() + "\n" + help
	return wrapInPanel(content, p.termW, p.termH)
}

func (p *Picker) reloadEntries() {
	dir := p.fp.CurrentDirectory
	if dir == p.listed && p.entries != nil {
		return
	}
	p.listed = dir
	p.cursor = 0
	ents, err := os.ReadDir(dir)
	if err != nil {
		p.entries = nil
		return
	}
	sort.Slice(ents, func(i, j int) bool {
		if ents[i].IsDir() == ents[j].IsDir() {
			return ents[i].Name() < ents[j].Name()
		}
		return ents[i].IsDir()
	})
	if !p.fp.ShowHidden {
		var vis []os.DirEntry
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			vis = append(vis, e)
		}
		ents = vis
	}
	p.entries = ents
}

func (p *Picker) trackCursor(key string) {
	n := len(p.entries)
	if n == 0 {
		return
	}
	switch key {
	case "j", "down":
		if p.cursor < n-1 {
			p.cursor++
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case "g":
		p.cursor = 0
	case "G":
		p.cursor = n - 1
	}
}

func (p *Picker) toggleMarked() {
	if p.cursor < 0 || p.cursor >= len(p.entries) {
		return
	}
	e := p.entries[p.cursor]
	if e.IsDir() {
		return
	}
	path := filepath.Join(p.fp.CurrentDirectory, e.Name())
	if p.marked == nil {
		p.marked = map[string]struct{}{}
	}
	if _, ok := p.marked[path]; ok {
		delete(p.marked, path)
	} else {
		p.marked[path] = struct{}{}
	}
}

func (p *Picker) markedPaths() []string {
	out := make([]string, 0, len(p.marked))
	for pth := range p.marked {
		out = append(out, pth)
	}
	sort.Strings(out)
	return out
}
