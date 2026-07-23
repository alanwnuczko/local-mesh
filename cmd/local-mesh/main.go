// Command local-mesh is a zero-config LAN file-transfer TUI.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
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

	// On Windows, try to open the firewall for mDNS.
	// Requires Administrator; errors are logged only (best-effort).
	if runtime.GOOS == "windows" {
		ensureWindowsFirewallRule()
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	deviceID, err := discovery.LoadOrCreateDeviceID()
	if err != nil {
		fmt.Fprintf(os.Stderr, "device ID: %v\n", err)
		os.Exit(1)
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

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

	svc, err := discovery.New(deviceID, hostname, port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discovery: %v\n", err)
		os.Exit(1)
	}
	defer svc.Shutdown()
	svc.Browse(ctx)

	model := app.NewModel(deviceID, hostname, 80, 24)

	sendRegCh := make(chan sendChanPair, 8)
	model.OnStartSend = func(progress chan transfer.ProgressEvent, done chan transfer.DoneEvent) {
		sendRegCh <- sendChanPair{progress: progress, done: done}
	}

	// tea.WithAltScreen() gives Bubbletea a dedicated full-screen canvas that
	// is cleared on every render, eliminating leftover lines when the terminal
	// is resized. The first-paint blank-screen issue on Windows CMD/PowerShell
	// is resolved by tea.ClearScreen returned from Init().
	p := tea.NewProgram(model, tea.WithAltScreen())

	b := bus.New(p)
	b.ForwardDiscovery(svc.Events())
	b.ForwardOffers(offerCh)
	b.ForwardRecvProgress(recvProgress)
	b.ForwardRecvDone(recvDone)

	go func() {
		for pair := range sendRegCh {
			b.ForwardSendProgress(pair.progress)
			b.ForwardSendDone(pair.done)
		}
	}()

	// Serve is a blocking Accept loop (spec §3.1 goroutine #3). It must run
	// in its own goroutine so the Bubbletea event loop can start. Without the
	// go keyword, p.Run() is never reached until the context is cancelled
	// (Ctrl+C), which is why the TUI stayed blank until interrupt — and by
	// then discovery's browse context was already cancelled too.
	go server.Serve(ctx)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "bubbletea: %v\n", err)
		os.Exit(1)
	}
}

func ensureWindowsFirewallRule() {
	// mDNS (UDP 5353) must be allowed inbound on every profile. VMware Host-Only
	// adapters are often classified as Public, so profile=any is required.
	// Also allow the process itself for the TCP transfer port (OS-assigned).
	exePath, err := os.Executable()
	if err != nil {
		exePath = os.Args[0]
	}

	rules := []struct {
		name string
		args []string
	}{
		{
			name: "local-mesh mDNS",
			args: []string{
				"advfirewall", "firewall", "add", "rule",
				"name=local-mesh mDNS",
				"protocol=UDP", "dir=in", "localport=5353",
				"action=allow", "profile=any",
			},
		},
		{
			name: "local-mesh Fallback",
			args: []string{
				"advfirewall", "firewall", "add", "rule",
				"name=local-mesh Fallback",
				"protocol=UDP", "dir=in", "localport=53333",
				"action=allow", "profile=any",
			},
		},
		{
			name: "local-mesh TCP",
			args: []string{
				"advfirewall", "firewall", "add", "rule",
				"name=local-mesh TCP",
				"protocol=TCP", "dir=in",
				"action=allow", "profile=any",
				"program=" + exePath,
			},
		},
	}
	for _, r := range rules {
		check := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+r.name)
		if err := check.Run(); err == nil {
			continue // rule already exists
		}
		add := exec.Command("netsh", r.args...)
		if out, err := add.CombinedOutput(); err != nil {
			slog.Warn("firewall rule not added (run as Administrator to fix)",
				"rule", r.name, "err", err, "out", string(out))
		} else {
			slog.Info("windows firewall: added rule", "rule", r.name)
		}
	}
}

type sendChanPair struct {
	progress chan transfer.ProgressEvent
	done     chan transfer.DoneEvent
}
