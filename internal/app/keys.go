package app

import "github.com/charmbracelet/bubbles/key"

// GlobalKeyMap holds the keybindings active on every screen.
type GlobalKeyMap struct {
	Quit   key.Binding
	Help   key.Binding
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
}
