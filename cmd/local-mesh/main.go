// Command local-mesh is a zero-config LAN file-transfer TUI.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/alanwnuczko/local-mesh/internal/app"
	"github.com/alanwnuczko/local-mesh/internal/bus"
	"github.com/alanwnuczko/local-mesh/internal/config"
	"github.com/alanwnuczko/local-mesh/internal/discovery"
	"github.com/alanwnuczko/local-mesh/internal/transfer"
	tea "github.com/charmbracelet/bubbletea"
)

const maxLogBytes = 5 << 20 // 5 MiB — rotate when larger

func main() {
	setupLogging()

	var firewallWarn string
	if runtime.GOOS == "windows" {
		firewallWarn = ensureWindowsFirewallRule()
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
	model.OnRefresh = func() {
		svc.Refresh()
	}

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

	go server.Serve(ctx)

	uiCtx, uiCancel := context.WithCancel(ctx)
	defer uiCancel()

	// Surface non-fatal network problems once the TUI is running.
	go func() {
		timer := time.NewTimer(400 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-uiCtx.Done():
			return
		case <-timer.C:
		}
		if uiCtx.Err() != nil {
			return
		}
		warn := firewallWarn
		if w := svc.Warning(); w != "" {
			if warn != "" {
				warn = warn + " · " + w
			} else {
				warn = w
			}
		}
		if warn != "" {
			p.Send(app.NetworkWarningMsg{Text: warn})
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "bubbletea: %v\n", err)
		os.Exit(1)
	}
	uiCancel()
	close(sendRegCh)
}

func setupLogging() {
	logPath := "local-mesh.log"
	if cfgDir, err := config.ConfigDir(); err == nil {
		logPath = filepath.Join(cfgDir, "local-mesh.log")
	}

	level := slog.LevelInfo
	if os.Getenv("LOCAL_MESH_DEBUG") != "" {
		level = slog.LevelDebug
	}

	rotateLogIfLarge(logPath)

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
		return
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
		Level: level,
	})))
}

func rotateLogIfLarge(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < maxLogBytes {
		return
	}
	_ = os.Rename(path, path+".old")
}

// ensureWindowsFirewallRule adds inbound rules if missing. Returns a short
// warning for the TUI when rules could not be installed (typically missing
// Administrator privileges).
func ensureWindowsFirewallRule() string {
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
				`program="` + exePath + `"`,
			},
		},
	}

	failed := 0
	for _, r := range rules {
		check := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+r.name)
		if err := check.Run(); err == nil {
			continue
		}
		add := exec.Command("netsh", r.args...)
		if out, err := add.CombinedOutput(); err != nil {
			failed++
			slog.Warn("firewall rule not added (run as Administrator to fix)",
				"rule", r.name, "err", err, "out", string(out))
		} else {
			slog.Info("windows firewall: added rule", "rule", r.name)
		}
	}
	if failed > 0 {
		return "Firewall rules missing — run as Administrator once so peers can discover you"
	}
	return ""
}

type sendChanPair struct {
	progress chan transfer.ProgressEvent
	done     chan transfer.DoneEvent
}
