package app

import (
	"github.com/alanwnuczko/local-mesh/internal/screens"
)

// View renders the active screen and, if an overlay is active, composites it
// on top of the dimmed base view (§4.5).
//
// CONCURRENCY INVARIANT: View is called by Bubbletea on the same goroutine as
// Update; it must not trigger side effects.
func (m Model) View() string {
	base := m.renderBase()

	if m.Overlay != nil {
		return screens.RenderOverlay(base, m.Overlay, m.Width, m.Height)
	}

	return base
}

func (m Model) renderBase() string {
	switch m.ActiveScreen {
	case ScreenPeerList:
		return m.PeerList.View()

	case ScreenPicker:
		return m.Picker.View()

	case ScreenConfirm:
		if m.Confirm != nil {
			return m.Confirm.View()
		}

	case ScreenProgress:
		if m.Progress != nil {
			return m.Progress.View()
		}
	}
	return ""
}
