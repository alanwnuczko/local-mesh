package screens

import (
	"fmt"
	"strings"

	"github.com/alanwnuczko/local-mesh/internal/ui"
	"github.com/alanwnuczko/local-mesh/pkg/protocol"
	"github.com/charmbracelet/lipgloss"
)

// OverlayState holds the state for the incoming-request overlay (§4.5).
// It is stored at model.Overlay and rendered on top of the active screen.
// Showing the overlay does NOT change activeScreen and does NOT touch the base
// screen's model — it is purely additive state.
type OverlayState struct {
	Offer protocol.OfferMessage
	// Reply is the channel the receive goroutine is blocked on (§3.3).
	// Decisions are sent on it via a tea.Cmd, never directly in Update.
	Reply chan<- protocol.DecisionMessage
}

// ReplyAccept returns an accepting DecisionMessage.
func (o *OverlayState) ReplyAccept() protocol.DecisionMessage {
	return protocol.DecisionMessage{TransferID: o.Offer.TransferID, Accepted: true}
}

// ReplyReject returns a rejecting DecisionMessage with the given reason.
func (o *OverlayState) ReplyReject(reason string) protocol.DecisionMessage {
	return protocol.DecisionMessage{TransferID: o.Offer.TransferID, Accepted: false, Reason: reason}
}

// RenderOverlay composites the overlay box over a dimmed base view.
func RenderOverlay(base string, overlay *OverlayState, width, height int) string {
	// Dim the base.
	dimStyle := lipgloss.NewStyle().Foreground(ui.ColorMuted)
	lines := strings.Split(base, "\n")
	dimmedLines := make([]string, len(lines))
	for i, l := range lines {
		dimmedLines[i] = dimStyle.Render(l)
	}

	content := renderOverlayContent(overlay)
	boxWidth := width - 8
	if boxWidth > 60 {
		boxWidth = 60
	}
	box := ui.OverlayStyle.Width(boxWidth).Render(content)
	boxLines := strings.Split(box, "\n")

	topPad := (height - len(boxLines)) / 2
	if topPad < 0 {
		topPad = 0
	}

	result := make([]string, 0, height)
	for i, l := range dimmedLines {
		if i == topPad {
			result = append(result, boxLines...)
		}
		if i < topPad || i >= topPad+len(boxLines) {
			result = append(result, l)
		}
	}
	// In case base is shorter than overlay position.
	for len(result) < topPad+len(boxLines) {
		result = append(result, strings.Join(boxLines[len(result)-topPad:], "\n"))
		break
	}

	return strings.Join(result, "\n")
}

func renderOverlayContent(o *OverlayState) string {
	var sb strings.Builder
	sb.WriteString(ui.TitleStyle.Render("Incoming Transfer Request") + "\n\n")

	senderID := o.Offer.SenderID
	if len(senderID) > 8 {
		senderID = senderID[:8]
	}
	sb.WriteString(fmt.Sprintf("  From:  %s  %s\n",
		ui.HighlightStyle.Render(o.Offer.SenderHost),
		ui.MutedStyle.Render("("+senderID+")")))

	kind := "file"
	if o.Offer.IsDir {
		kind = "folder"
	}
	sb.WriteString(fmt.Sprintf("  Name:  %s  (%s)\n",
		ui.HighlightStyle.Render(o.Offer.Name), kind))
	sb.WriteString(fmt.Sprintf("  Size:  %s\n",
		ui.HighlightStyle.Render(formatBytes(o.Offer.Size))))

	sb.WriteString("\n")
	sb.WriteString(ui.SuccessStyle.Render("  a/y") + " accept   ")
	sb.WriteString(ui.ErrorStyle.Render("d/N/esc") + " reject\n")
	return sb.String()
}
