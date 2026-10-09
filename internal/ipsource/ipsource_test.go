package ipsource

import (
	"context"
	"net"
	"testing"

	"github.com/echoface/ozddns/internal/domain"
)

// SRC-01/03: addresses are extracted from arbitrary HTTP / command text.
func TestExtractIP(t *testing.T) {
	cases := []struct {
		text, family, want string
	}{
		{"203.0.113.9", "v4", "203.0.113.9"},
		{"Your IP is 203.0.113.9\n", "v4", "203.0.113.9"},
		{`{"ip":"198.51.100.7"}`, "v4", "198.51.100.7"},
		{"2001:db8::1\n", "v6", "2001:db8::1"},
		{"inet 100.64.0.4/32", "v4", "100.64.0.4"},
		{"no address here", "v4", ""},
		{"203.0.113.9", "v6", ""}, // v4 text is not a valid v6 answer
	}
	for _, c := range cases {
		got, err := extractIP(c.text, c.family)
		if c.want == "" {
			if err == nil {
				t.Errorf("extractIP(%q,%s) = %v, want error", c.text, c.family, got)
			}
			continue
		}
		if err != nil || got.String() != c.want {
			t.Errorf("extractIP(%q,%s) = %v, %v; want %s", c.text, c.family, got, err, c.want)
		}
	}
}

// SRC-04: static sources validate their literal address.
func TestStaticSource(t *testing.T) {
	f := staticFactory{}
	if _, err := f.New("203.0.113.9"); err != nil {
		t.Fatalf("valid static rejected: %v", err)
	}
	if _, err := f.New("not-an-ip"); err == nil {
		t.Fatal("invalid static address accepted")
	}
	src, _ := f.New("2001:db8::5")
	addr, err := src.Get(context.Background(), "v6")
	if err != nil || addr.String() != "2001:db8::5" {
		t.Fatalf("Get = %v, %v", addr, err)
	}
}

// SRC-02: interface sources resolve against a NIC name and fail cleanly.
func TestInterfaceSource(t *testing.T) {
	if _, err := (ifaceFactory{}).New(""); err == nil {
		t.Fatal("empty NIC name accepted")
	}
	only := func() ([]net.Interface, error) { return []net.Interface{{Name: "lo0"}}, nil }
	missing := &ifaceSource{name: "en0", lister: only}
	if _, err := missing.Get(context.Background(), "v4"); err == nil {
		t.Fatal("expected a not-found error for a missing interface")
	}
	// The real NIC table is readable and every returned interface has a name.
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skipf("interfaces unavailable: %v", err)
	}
	for _, ifc := range ifaces {
		if ifc.Name == "" {
			t.Fatal("interface without a name")
		}
	}
}

// SRC-05/registry: all four built-in kinds are registered and buildable.
func TestRegistryBuiltins(t *testing.T) {
	r := NewRegistry()
	metas := r.Metas()
	if len(metas) != 4 {
		t.Fatalf("expected 4 source kinds, got %d", len(metas))
	}
	for _, s := range []domain.IPSource{
		{Type: domain.IPHTTP, Family: "v4", Target: "https://api.ipify.org"},
		{Type: domain.IPStatic, Family: "v4", Target: "203.0.113.1"},
		{Type: domain.IPCommand, Family: "v4", Target: "echo hi"},
	} {
		if _, err := r.Build(s); err != nil {
			t.Errorf("Build(%s): %v", s.Type, err)
		}
	}
	if _, err := r.Build(domain.IPSource{Type: "nope"}); err == nil {
		t.Fatal("unknown source type should not build")
	}
}

// SRC-06: the target grammar understands an optional per-family index.
func TestParseIfaceTarget(t *testing.T) {
	cases := []struct {
		in    string
		name  string
		index int
	}{
		{"en0", "en0", -1},
		{" en0 ", "en0", -1},
		{"en0#1", "en0", 1},
		{"en0#0", "en0", 0},
		{"en0#", "en0#", -1},   // malformed -> treated as a plain name
		{"en0#x", "en0#x", -1}, // malformed -> treated as a plain name
	}
	for _, c := range cases {
		name, idx := parseIfaceTarget(c.in)
		if name != c.name || idx != c.index {
			t.Errorf("parseIfaceTarget(%q) = %q,%d; want %q,%d", c.in, name, idx, c.name, c.index)
		}
	}
}

// ifaceWith builds a fake lister + address table for one interface.
func ifaceWith(name string, cidrs ...string) (ifaceLister, ifaceAddrs) {
	lister := func() ([]net.Interface, error) {
		return []net.Interface{{Name: name, Flags: net.FlagUp}}, nil
	}
	addrs := func(i net.Interface) ([]net.Addr, error) {
		if i.Name != name {
			return nil, nil
		}
		var out []net.Addr
		for _, c := range cidrs {
			out = append(out, ipnet(c))
		}
		return out, nil
	}
	return lister, addrs
}

// SRC-07: an interface with several addresses honours the #index selector.
func TestInterfaceIndexSelection(t *testing.T) {
	lister, addrs := ifaceWith("en0", "192.168.1.5/24", "192.168.1.6/24", "2001:db8::5/64", "fe80::1/64")
	first := &ifaceSource{name: "en0", index: -1, lister: lister, addrs: addrs}
	second := &ifaceSource{name: "en0", index: 1, lister: lister, addrs: addrs}

	a, err := first.Get(context.Background(), "v4")
	if err != nil || a.String() != "192.168.1.5" {
		t.Fatalf("first v4 = %v, %v; want 192.168.1.5", a, err)
	}
	b, err := second.Get(context.Background(), "v4")
	if err != nil || b.String() != "192.168.1.6" {
		t.Fatalf("second v4 = %v, %v; want 192.168.1.6", b, err)
	}
	// v6 indexes independently, and the link-local address is skipped.
	v6, err := first.Get(context.Background(), "v6")
	if err != nil || !v6.Is6() || v6.String() != "2001:db8::5" {
		t.Fatalf("first v6 = %v, %v; want 2001:db8::5", v6, err)
	}
	if _, err := second.Get(context.Background(), "v6"); err == nil {
		t.Fatal("expected an error for a missing second v6 address")
	}
}

// SRC-08: the listing skips loopback/link-local and labels later addrs.
func TestListInterfaces(t *testing.T) {
	lo := net.Interface{Name: "lo0", Flags: net.FlagLoopback | net.FlagUp}
	lister := func() ([]net.Interface, error) {
		return []net.Interface{lo, {Name: "en0", Flags: net.FlagUp}}, nil
	}
	addrs := func(i net.Interface) ([]net.Addr, error) {
		return []net.Addr{
			ipnet("192.168.1.5/24"), ipnet("192.168.1.6/24"),
			ipnet("2001:db8::5/64"), ipnet("fe80::1/64"),
		}, nil
	}
	list, err := listInterfaces(lister, addrs)
	if err != nil {
		t.Fatalf("listInterfaces: %v", err)
	}
	if len(list) != 1 || list[0].Name != "en0" {
		t.Fatalf("expected only en0, got %+v", list)
	}
	var targets []string
	for _, a := range list[0].Addresses {
		targets = append(targets, a.Target)
	}
	want := []string{"en0", "en0#1", "en0"}
	if len(targets) != len(want) {
		t.Fatalf("targets = %v; want %v", targets, want)
	}
	for i := range want {
		if targets[i] != want[i] {
			t.Fatalf("targets = %v; want %v", targets, want)
		}
	}

	// Every listed target resolves back to the address it advertised.
	for _, a := range list[0].Addresses {
		name, idx := parseIfaceTarget(a.Target)
		src := &ifaceSource{name: name, index: idx, lister: lister, addrs: addrs}
		if got, err := src.Get(context.Background(), a.Family); err != nil || got.String() != a.Addr {
			t.Fatalf("%s resolved to %v, %v; want %s", a.Target, got, err, a.Addr)
		}
	}
}

func ipnet(cidr string) *net.IPNet {
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	n.IP = ip
	return n
}
