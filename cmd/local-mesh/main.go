// Command local-mesh is a zero-config LAN file-transfer TUI.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alanwnuczko/local-mesh/internal/app"
	"github.com/alanwnuczko/local-mesh/internal/bus"
	"github.com/alanwnuczko/local-mesh/internal/discovery"
	"github.com/alanwnuczko/local-mesh/internal/transfer"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Log to a file so output does not interfere with the TUI.
	logFile, err := os.OpenFile("local-mesh.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})))
		defer logFile.Close()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Load or generate the stable per-device ID.
	deviceID, err := discovery.LoadOrCreateDeviceID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "device ID: %v\n", err)
		os.Exit(1)
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	// Channels for inbound transfers (server → bus → Update).
	// These are long-lived; one set for the whole server lifetime.
	offerCh := make(chan transfer.OfferWithReply, 4)
	recvProgress := make(chan transfer.ProgressEvent, 64)
	recvDone := make(chan transfer.DoneEvent, 4)

	server, err := transfer.NewServer(":0", offerCh, recvProgress, recvDone)
	if err != nil {
		fmt.Fprintf(os.Stderr, "transfer server: %v\n", err)
		os.Exit(1)
	}
	port := server.Port()
	slog.Info("transfer server listening", "port", port)

	// Register this instance via mDNS and start browsing for peers.
	svc, err := discovery.New(deviceID, hostname, port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discovery: %v\n", err)
		os.Exit(1)
	}
	defer svc.Shutdown()
	svc.Browse(ctx)

	// Build the initial model.
	model := app.NewModel(deviceID, hostname, 80, 24)

	// sendRegCh receives newly created send-channel pairs when the user
	// confirms a transfer. The bus goroutine below registers forwarders for them.
	sendRegCh := make(chan sendChanPair, 8)
	model.OnStartSend = func(progress chan transfer.ProgressEvent, done chan transfer.DoneEvent) {
		sendRegCh <- sendChanPair{progress: progress, done: done}
	}

	// Create the Bubbletea program.
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Wire the bus. Bus forwarder goroutines call p.Send(msg) — the only safe
	// way for background goroutines to communicate with the Bubbletea Update loop.
	b := bus.New(p)
	b.ForwardDiscovery(svc.Events())
	b.ForwardOffers(offerCh)
	b.ForwardRecvProgress(recvProgress)
	b.ForwardRecvDone(recvDone)

	// Watch for newly created send channel pairs and register bus forwarders.
	go func() {
		for pair := range sendRegCh {
			b.ForwardSendProgress(pair.progress)
			b.ForwardSendDone(pair.done)
		}
	}()

	// Start the TCP Accept loop (goroutine #3).
	server.Serve(ctx)

	// Run the Bubbletea event loop (blocks until the user quits).
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "bubbletea: %v\n", err)
		os.Exit(1)
	}
}

type sendChanPair struct {
	progress chan transfer.ProgressEvent
	done     chan transfer.DoneEvent
}
