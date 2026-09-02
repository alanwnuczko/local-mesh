package app

import "github.com/charmbracelet/bubbles/key"

// GlobalKeyMap holds the keybindings active on every screen.
type GlobalKeyMap struct {
	Quit    key.Binding
	Help    key.Binding
	Refresh key.Binding
}

// GlobalKeys is the singleton global key map.
var GlobalKeys = GlobalKeyMap{
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Help: key.NewBinding(
		key.WithKeys("?"),
		key.WithHelp("?", "toggle help"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh peers"),
	),
}

// helpLines is the full keybinding reference shown when help is toggled open.
var helpLines = []struct {
	keys string
	desc string
}{
	{"↑/↓ / j/k", "navigate lists"},
	{"enter", "select peer / confirm"},
	{"r", "refresh peer list"},
	{"s", "select current folder"},
	{"space", "toggle file in batch"},
	{"esc", "go back / dismiss"},
	{"y / a", "accept incoming transfer"},
	{"code", "first-time peer pairing code"},
	{"N / d / esc", "reject incoming transfer"},
	{"c", "cancel active transfer"},
	{"?", "toggle this help"},
	{"q / ctrl+c", "quit"},
}
