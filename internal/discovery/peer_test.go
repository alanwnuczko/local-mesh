package discovery

import (
	"net"
	"testing"
)

func TestMergeIPsDedupes(t *testing.T) {
	a := net.ParseIP("192.168.1.5")
	b := net.ParseIP("10.0.0.2")
	got := mergeIPs([]net.IP{a}, []net.IP{a, b})
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %v", len(got), got)
	}
}

func TestRegistryUpsertMergesAddrs(t *testing.T) {
	r := NewRegistry()
	r.Upsert(Peer{ID: "abc", Hostname: "h", Addrs: []net.IP{net.ParseIP("192.168.1.5")}, Port: 9})
	r.Upsert(Peer{ID: "abc", Hostname: "h", Addrs: []net.IP{net.ParseIP("10.0.0.2")}, Port: 9})
	p, ok := r.Get("abc")
	if !ok {
		t.Fatal("missing")
	}
	if len(p.Addrs) != 2 {
		t.Fatalf("addrs=%v want 2", p.Addrs)
	}
}

func TestDialAddrsPrefersIPv4(t *testing.T) {
	p := Peer{
		ID:   "x",
		Port: 1234,
		Addrs: []net.IP{
			net.ParseIP("fe80::1"),
			net.ParseIP("192.0.2.8"),
		},
	}
	addrs := p.DialAddrs()
	if len(addrs) < 1 {
		t.Fatal("no addrs")
	}
	if addrs[0] != "192.0.2.8:1234" {
		t.Fatalf("first=%q want ipv4", addrs[0])
	}
}

func TestInvalidateLocalNets(t *testing.T) {
	_ = localIPv4Nets()
	InvalidateLocalNets()
	// Must not panic and must re-enumerate.
	_ = localIPv4Nets()
}
