package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alanwnuczko/local-mesh/internal/config"
	"github.com/alanwnuczko/local-mesh/internal/discovery"
	"github.com/alanwnuczko/local-mesh/internal/transfer"
	"github.com/alanwnuczko/local-mesh/internal/trust"
	"github.com/alanwnuczko/local-mesh/pkg/protocol"
)

func usage() {
	fmt.Fprintf(os.Stderr, `local-mesh — LAN file transfer

Usage:
  local-mesh                 start the interactive TUI
  local-mesh list            discover peers and print them
  local-mesh send --peer <id|host> <path>
  local-mesh recv [--yes]    wait for an incoming transfer
  local-mesh help

Flags:
  --peer string   device ID (prefix ok) or hostname
  --yes           auto-accept incoming transfers (skips pairing prompt)
`)
}

func runCLI(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return -1
	}
	switch args[0] {
	case "help", "-h", "--help":
		usage()
		return 0
	case "list":
		if err := cliList(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "list: %v\n", err)
			return 1
		}
		return 0
	case "send":
		fs := flag.NewFlagSet("send", flag.ContinueOnError)
		peer := fs.String("peer", "", "device ID prefix or hostname")
		fs.SetOutput(os.Stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if *peer == "" || fs.NArg() < 1 {
			fmt.Fprintln(os.Stderr, "usage: local-mesh send --peer <id|host> <path>")
			return 2
		}
		if err := cliSend(ctx, *peer, fs.Arg(0)); err != nil {
			fmt.Fprintf(os.Stderr, "send: %v\n", err)
			return 1
		}
		return 0
	case "recv":
		fs := flag.NewFlagSet("recv", flag.ContinueOnError)
		yes := fs.Bool("yes", false, "auto-accept")
		fs.SetOutput(os.Stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		if err := cliRecv(ctx, *yes); err != nil {
			fmt.Fprintf(os.Stderr, "recv: %v\n", err)
			return 1
		}
		return 0
	default:
		if strings.HasPrefix(args[0], "-") {
			return -1
		}
		fmt.Fprintf(os.Stderr, "unknown command %q\n", args[0])
		usage()
		return 2
	}
}

type meshRuntime struct {
	deviceID string
	hostname string
	svc      *discovery.Service
	server   *transfer.Server
	offers   chan transfer.OfferWithReply
	recvProg chan transfer.ProgressEvent
	recvDone chan transfer.DoneEvent
	trust    *trust.Store
}

func startRuntime(ctx context.Context) (*meshRuntime, error) {
	id, err := discovery.LoadOrCreateDeviceID()
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	offers := make(chan transfer.OfferWithReply, 4)
	prog := make(chan transfer.ProgressEvent, 64)
	done := make(chan transfer.DoneEvent, 4)
	srv, err := transfer.NewServer(":0", offers, prog, done)
	if err != nil {
		return nil, err
	}
	svc, err := discovery.New(id, host, srv.Port())
	if err != nil {
		return nil, err
	}
	svc.Browse(ctx)
	go srv.Serve(ctx)
	st, _ := trust.Load()
	return &meshRuntime{
		deviceID: id,
		hostname: host,
		svc:      svc,
		server:   srv,
		offers:   offers,
		recvProg: prog,
		recvDone: done,
		trust:    st,
	}, nil
}

func (r *meshRuntime) close() {
	if r.svc != nil {
		r.svc.Shutdown()
	}
}

func cliList(ctx context.Context) error {
	rt, err := startRuntime(ctx)
	if err != nil {
		return err
	}
	defer rt.close()
	fmt.Println("searching for peers (3s)...")
	t := time.NewTimer(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			peers := rt.svc.Registry().All()
			if len(peers) == 0 {
				fmt.Println("no peers found")
				return nil
			}
			for _, p := range peers {
				fmt.Printf("%s  %s  %s  %s\n", p.ShortID(), p.Hostname, p.OS+"/"+p.Arch, p.Addr())
			}
			return nil
		case <-rt.svc.Events():
		}
	}
}

func cliSend(ctx context.Context, peerKey, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	rt, err := startRuntime(ctx)
	if err != nil {
		return err
	}
	defer rt.close()

	fmt.Println("searching for peers (3s)...")
	time.Sleep(3 * time.Second)
	peer, ok := findPeer(rt.svc.Registry().All(), peerKey)
	if !ok {
		return fmt.Errorf("peer %q not found", peerKey)
	}

	if rt.trust != nil && !rt.trust.Known(peer.ID) {
		fmt.Printf("pairing code %s  (must match %s)\n", trust.PairingCode(rt.deviceID, peer.ID), peer.Hostname)
		fmt.Print("continue? [y/N] ")
		if !readYes() {
			return fmt.Errorf("aborted")
		}
		rt.trust.Remember(peer.ID)
	}

	abs, _ := filepath.Abs(path)
	progress := make(chan transfer.ProgressEvent, 32)
	done := make(chan transfer.DoneEvent, 1)
	go func() {
		for ev := range progress {
			if ev.BytesTotal > 0 {
				fmt.Fprintf(os.Stderr, "\r%s %s / %s", ev.Phase, human(ev.BytesDone), human(ev.BytesTotal))
			}
		}
	}()
	if err := transfer.StartSend(peer.DialAddrs(), transfer.SendConfig{
		SenderID:   rt.deviceID,
		SenderHost: rt.hostname,
		Path:       abs,
		IsDir:      info.IsDir(),
		Progress:   progress,
		Done:       done,
	}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ev := <-done:
		fmt.Fprintln(os.Stderr)
		close(progress)
		if ev.Err != nil {
			return ev.Err
		}
		fmt.Println("sent")
		return nil
	}
}

func cliRecv(ctx context.Context, auto bool) error {
	rt, err := startRuntime(ctx)
	if err != nil {
		return err
	}
	defer rt.close()
	dir, _ := config.DownloadsDir()
	fmt.Printf("listening as %s (%s) — saving to %s\n", rt.hostname, rt.deviceID[:8], dir)

	go func() {
		for owr := range rt.offers {
			offer := owr.Offer
			dec := protocol.DecisionMessage{TransferID: offer.TransferID}
			if !auto {
				if rt.trust != nil && !rt.trust.Known(offer.SenderID) {
					fmt.Printf("pairing code %s  (must match sender)\n", trust.PairingCode(rt.deviceID, offer.SenderID))
				}
				fmt.Printf("%s wants to send %s (%s). accept? [y/N] ", offer.SenderHost, offer.Name, human(offer.Size))
				if !readYes() {
					dec.Reason = "user declined"
					owr.Reply <- dec
					continue
				}
			}
			dec.Accepted = true
			if rt.trust != nil {
				rt.trust.Remember(offer.SenderID)
			}
			owr.Reply <- dec
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-rt.recvDone:
			if ev.Err != nil {
				fmt.Fprintf(os.Stderr, "recv: %v\n", ev.Err)
				continue
			}
			fmt.Println("saved", ev.SavedPath)
			return nil
		}
	}
}

func findPeer(peers []discovery.Peer, key string) (discovery.Peer, bool) {
	key = strings.ToLower(key)
	for _, p := range peers {
		id := strings.ToLower(p.ID)
		host := strings.ToLower(p.Hostname)
		if id == key || strings.HasPrefix(id, key) || host == key {
			return p, true
		}
	}
	return discovery.Peer{}, false
}

func readYes() bool {
	in := bufio.NewReader(os.Stdin)
	line, _ := in.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

func human(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
