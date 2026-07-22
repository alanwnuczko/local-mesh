// Package bus adapts subsystem channels into Bubbletea messages.
//
// Background goroutines (discovery, transfer server) emit events on Go channels.
// The bus owns forwarder goroutines that range over those channels and call
// program.Send(msg) for each event. This is the officially safe way to inject
// messages into Bubbletea's single-threaded Update loop from another goroutine.
//
// CONCURRENCY INVARIANT (§3.2, §3.5): The bus never reads or writes Model
// fields. It only calls program.Send with typed tea.Msg values.
package bus

import (
	"github.com/alanwnuczko/local-mesh/internal/app"
	"github.com/alanwnuczko/local-mesh/internal/discovery"
	"github.com/alanwnuczko/local-mesh/internal/transfer"
	tea "github.com/charmbracelet/bubbletea"
)

// Bus holds the Bubbletea program reference and owns the forwarder goroutines.
type Bus struct {
	program *tea.Program
}

// New creates a Bus connected to the given Bubbletea program.
func New(p *tea.Program) *Bus {
	return &Bus{program: p}
}

// ForwardDiscovery ranges over the discovery event channel and forwards each
// event to the Bubbletea program as a PeerFoundMsg or PeerLostMsg.
// Runs until the channel is closed (i.e., discovery shutdown).
func (b *Bus) ForwardDiscovery(events <-chan discovery.Event) {
	go func() {
		for ev := range events {
			switch ev.Kind {
			case discovery.EventPeerFound:
				b.program.Send(app.PeerFoundMsg{Peer: ev.Peer})
			case discovery.EventPeerLost:
				b.program.Send(app.PeerLostMsg{Peer: ev.Peer})
			}
		}
	}()
}

// ForwardSendProgress ranges over sendProgress and forwards each event.
func (b *Bus) ForwardSendProgress(ch <-chan transfer.ProgressEvent) {
	go func() {
		for ev := range ch {
			b.program.Send(app.SendProgressMsg{Event: ev})
		}
	}()
}

// ForwardSendDone ranges over sendDone and forwards each event.
func (b *Bus) ForwardSendDone(ch <-chan transfer.DoneEvent) {
	go func() {
		for ev := range ch {
			b.program.Send(app.SendDoneMsg{Event: ev})
		}
	}()
}

// ForwardRecvProgress ranges over recvProgress and forwards each event.
func (b *Bus) ForwardRecvProgress(ch <-chan transfer.ProgressEvent) {
	go func() {
		for ev := range ch {
			b.program.Send(app.RecvProgressMsg{Event: ev})
		}
	}()
}

// ForwardRecvDone ranges over recvDone and forwards each event.
func (b *Bus) ForwardRecvDone(ch <-chan transfer.DoneEvent) {
	go func() {
		for ev := range ch {
			b.program.Send(app.RecvDoneMsg{Event: ev})
		}
	}()
}

// ForwardOffers ranges over the incoming-offer channel and forwards each offer
// as an IncomingOfferMsg carrying the reply channel for the UI to respond on.
func (b *Bus) ForwardOffers(ch <-chan transfer.OfferWithReply) {
	go func() {
		for owr := range ch {
			b.program.Send(app.IncomingOfferMsg{
				Offer: owr.Offer,
				Reply: owr.Reply,
			})
		}
	}()
}
