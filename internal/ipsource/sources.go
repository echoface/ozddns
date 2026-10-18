package ipsource

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---- shared parsing ------------------------------------------------------

var ipToken = regexp.MustCompile(`[0-9A-Fa-f:.]+`)

// extractIP pulls the first address of the requested family out of arbitrary
// text (an HTTP body or command output).
func extractIP(text, family string) (netip.Addr, error) {
	for _, m := range ipToken.FindAllString(text, -1) {
		addr, err := netip.ParseAddr(strings.Trim(m, ".,;:\"'()[]{}"))
		if err != nil {
			continue
		}
		if family == "v6" {
			if addr.Is6() && !addr.Is4In6() {
				return addr, nil
			}
		} else if addr.Is4() || addr.Is4In6() {
			return addr.Unmap(), nil
		}
	}
	return netip.Addr{}, fmt.Errorf("no %s address found in response", family)
}

// ---- HTTP ----------------------------------------------------------------

type httpFactory struct{}

func (httpFactory) Meta() Meta {
	return Meta{ID: "http", Label: "HTTP lookup", Icon: "globe",
		Desc: `Query a public "what is my IP" endpoint.`}
}
func (httpFactory) New(target string) (Source, error) {
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("http source requires a URL")
	}
	return &httpSource{url: target}, nil
}

type httpSource struct{ url string }

func (s *httpSource) Get(ctx context.Context, family string) (netip.Addr, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return netip.Addr{}, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return netip.Addr{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return netip.Addr{}, fmt.Errorf("HTTP source returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("read HTTP source response: %w", err)
	}
	return extractIP(string(body), family)
}

// ---- Network interface ---------------------------------------------------

// matchFamily reports whether addr is a usable address for the requested
// family. Link-local and loopback addresses are ignored: they are never a
// sensible DNS target.
func matchFamily(addr netip.Addr, family string) bool {
	if family == "v6" {
		return addr.Is6() && !addr.Is4In6() && addr.IsGlobalUnicast()
	}
	return addr.Is4() && addr.IsGlobalUnicast()
}

// InterfaceAddr is one address bound to a local NIC that OzDDNS can follow.
// Target is the value stored on the source ("en0" or, to select the second
// address of the family, "en0#1").
type InterfaceAddr struct {
	Target string `json:"target"`
	Name   string `json:"name"`
	Addr   string `json:"addr"`
	Family string `json:"family"`
	Index  int    `json:"index"`
	Prefix int    `json:"prefix"`
}

// InterfaceInfo describes a local network interface for the panel's picker.
type InterfaceInfo struct {
	Name      string          `json:"name"`
	Up        bool            `json:"up"`
	Addresses []InterfaceAddr `json:"addresses"`
}

// ListInterfaces enumerates the local NICs and every address OzDDNS could
// track, so the wizard can offer a real choice instead of a bare text box.
func ListInterfaces() ([]InterfaceInfo, error) { return listInterfaces(net.Interfaces, defaultAddrs) }

func listInterfaces(lister ifaceLister, addrs ifaceAddrs) ([]InterfaceInfo, error) {
	ifaces, err := lister()
	if err != nil {
		return nil, err
	}
	out := make([]InterfaceInfo, 0, len(ifaces))
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		info := InterfaceInfo{Name: ifc.Name, Up: ifc.Flags&net.FlagUp != 0}
		list, err := addrs(ifc)
		if err != nil {
			continue
		}
		seen := map[string]int{}
		for _, a := range list {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipnet.IP)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			family := "v4"
			if addr.Is6() {
				family = "v6"
			}
			if !matchFamily(addr, family) {
				continue
			}
			idx := seen[family]
			seen[family]++
			// The first address of a family keeps the bare NIC name so existing
			// sources stay valid; later ones are disambiguated with "#n".
			target := ifc.Name
			if idx > 0 {
				target = fmt.Sprintf("%s#%d", ifc.Name, idx)
			}
			ones, _ := ipnet.Mask.Size()
			info.Addresses = append(info.Addresses, InterfaceAddr{
				Target: target, Name: ifc.Name, Addr: addr.String(),
				Family: family, Index: idx, Prefix: ones,
			})
		}
		if len(info.Addresses) == 0 {
			continue
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type ifaceFactory struct{}

func (ifaceFactory) Meta() Meta {
	return Meta{ID: "interface", Label: "Network interface", Icon: "network",
		Desc: "Read the address bound to a local NIC (LAN / WAN)."}
}
func (ifaceFactory) New(target string) (Source, error) {
	name, idx := parseIfaceTarget(target)
	if name == "" {
		return nil, fmt.Errorf("interface source requires a NIC name")
	}
	return &ifaceSource{name: name, index: idx, lister: net.Interfaces, addrs: defaultAddrs}, nil
}

type ifaceLister func() ([]net.Interface, error)

// ifaceAddrs resolves the addresses bound to an interface. It is indirected so
// tests can supply a deterministic table without touching the host's NICs.
type ifaceAddrs func(net.Interface) ([]net.Addr, error)

func defaultAddrs(i net.Interface) ([]net.Addr, error) { return i.Addrs() }

type ifaceSource struct {
	name   string
	index  int // -1 selects the first matching address of the family
	lister ifaceLister
	addrs  ifaceAddrs
}

// parseIfaceTarget understands "en0" and "en0#1" (the second same-family
// address on that interface). A missing or malformed suffix means "first".
func parseIfaceTarget(target string) (string, int) {
	s := strings.TrimSpace(target)
	if i := strings.LastIndex(s, "#"); i >= 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(s[i+1:])); err == nil && n >= 0 {
			return strings.TrimSpace(s[:i]), n
		}
	}
	return s, -1
}

func (s *ifaceSource) Get(ctx context.Context, family string) (netip.Addr, error) {
	ifaces, err := s.lister()
	if err != nil {
		return netip.Addr{}, err
	}
	for _, ifc := range ifaces {
		if ifc.Name != s.name {
			continue
		}
		resolve := s.addrs
		if resolve == nil {
			resolve = defaultAddrs
		}
		addrs, err := resolve(ifc)
		if err != nil {
			return netip.Addr{}, err
		}
		want := 0
		if s.index > 0 {
			want = s.index
		}
		seen := 0
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipnet.IP)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			if !matchFamily(addr, family) {
				continue
			}
			if seen == want {
				return addr, nil
			}
			seen++
		}
		if seen == 0 {
			return netip.Addr{}, fmt.Errorf("interface %q has no %s address", s.name, family)
		}
		return netip.Addr{}, fmt.Errorf("interface %q has no %s address #%d", s.name, family, want+1)
	}
	return netip.Addr{}, fmt.Errorf("interface %q not found", s.name)
}

// ---- Shell command -------------------------------------------------------

type cmdFactory struct{}

func (cmdFactory) Meta() Meta {
	return Meta{ID: "command", Label: "Shell command", Icon: "terminal",
		Desc: "Run a command and extract the first matching IP."}
}
func (cmdFactory) New(target string) (Source, error) {
	if strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("command source requires a command")
	}
	return &cmdSource{command: target}, nil
}

type cmdSource struct{ command string }

func (s *cmdSource) Get(ctx context.Context, family string) (netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sh", "-c", s.command).Output()
	if err != nil {
		return netip.Addr{}, fmt.Errorf("command failed: %w", err)
	}
	return extractIP(string(out), family)
}

// ---- Static --------------------------------------------------------------

type staticFactory struct{}

func (staticFactory) Meta() Meta {
	return Meta{ID: "static", Label: "Static / manual", Icon: "map-pin",
		Desc: "Pin a fixed address (testing or fixed-line)."}
}
func (staticFactory) New(target string) (Source, error) {
	if _, err := netip.ParseAddr(strings.TrimSpace(target)); err != nil {
		return nil, fmt.Errorf("static source requires a literal IP: %w", err)
	}
	return &staticSource{value: strings.TrimSpace(target)}, nil
}

type staticSource struct{ value string }

func (s *staticSource) Get(ctx context.Context, family string) (netip.Addr, error) {
	addr, err := netip.ParseAddr(s.value)
	if err != nil {
		return netip.Addr{}, err
	}
	addr = addr.Unmap()
	if (family == "v6" && !addr.Is6()) || (family == "v4" && !addr.Is4()) {
		return netip.Addr{}, fmt.Errorf("static source contains %s, not a %s address", addr, family)
	}
	return addr, nil
}

// registerBuiltins wires the four built-in source kinds into a registry.
func registerBuiltins(r *Registry) {
	r.Register(httpFactory{})
	r.Register(ifaceFactory{})
	r.Register(cmdFactory{})
	r.Register(staticFactory{})
}
