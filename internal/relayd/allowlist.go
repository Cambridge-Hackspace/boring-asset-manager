package relayd

import (
	"fmt"
	"net"
	"net/netip"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

// Allowlist is what a relay may reach locally, as its owner set it in the
// relay's own configuration. The main node cannot widen it: a job for
// anything outside it is refused, whoever asks.
//
// Entries are, by their form:
//
//   - host:port, or a host alone for port 9100, for TCP printers. The host
//     is an address, a CIDR range such as 192.168.1.0/24, a host name, or a
//     pattern of names such as *.lan; the port is a number or *. A range
//     matches only addresses: a name is matched as a name, whatever it
//     resolves to.
//   - an ipp://, ipps://, http:// or https:// URL, for IPP queues, in which
//   - matches anything, slashes included.
//   - windows: and a printer's name, in which * matches anything, for the
//     Windows spooler.
//
// Device paths are [printers] devices, and Bluetooth printers [printers]
// bluetooth, as on the main node.
type Allowlist struct {
	entries   []string
	tcp       []tcpRule
	urls      []string
	windows   []string
	devices   []string
	bluetooth []string
}

// tcpRule is one TCP entry: a range, an address, or a name pattern, and a
// port, 0 for any.
type tcpRule struct {
	prefix netip.Prefix
	name   string
	port   int
}

// ParseAllowlist reads a relay's allowlist entries, the device path
// patterns it may write to, and the Bluetooth address patterns it may
// reach.
func ParseAllowlist(entries, devices, bluetooth []string) (*Allowlist, error) {
	a := &Allowlist{devices: devices, bluetooth: bluetooth}
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		a.entries = append(a.entries, e)
		lower := strings.ToLower(e)
		switch {
		case strings.HasPrefix(lower, "windows:"):
			name := strings.TrimSpace(e[len("windows:"):])
			if name == "" {
				return nil, fmt.Errorf("allowlist entry %q names no Windows printer", e)
			}
			a.windows = append(a.windows, strings.ToLower(name))
		case strings.Contains(lower, "://"):
			scheme, _, _ := strings.Cut(lower, "://")
			switch scheme {
			case "ipp", "ipps", "http", "https":
			default:
				return nil, fmt.Errorf("allowlist entry %q: an IPP queue's URL starts ipp://, ipps://, http:// or https://", e)
			}
			a.urls = append(a.urls, e)
		case strings.HasPrefix(e, "/"):
			return nil, fmt.Errorf("allowlist entry %q is a device: list device paths under [printers] devices", e)
		case bluetoothAddress.MatchString(e):
			return nil, fmt.Errorf("allowlist entry %q is a Bluetooth address: list Bluetooth printers under [printers] bluetooth", e)
		default:
			r, err := parseTCP(e)
			if err != nil {
				return nil, err
			}
			a.tcp = append(a.tcp, r)
		}
	}
	for _, d := range devices {
		if _, err := path.Match(d, ""); err != nil || !strings.HasPrefix(d, "/") {
			return nil, fmt.Errorf("device pattern %q must be an absolute path pattern", d)
		}
	}
	for _, b := range bluetooth {
		if err := printers.CheckBluetoothPattern(b); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// bluetoothAddress is a Bluetooth address, or a pattern of them, as six
// parts: never a TCP printer, though it would read as an IPv6 one.
var bluetoothAddress = regexp.MustCompile(`^([0-9A-Fa-f*?]{1,2}:){5}[0-9A-Fa-f*?]{1,2}$`)

// parseTCP reads a TCP entry.
func parseTCP(e string) (tcpRule, error) {
	bad := func() (tcpRule, error) {
		return tcpRule{}, fmt.Errorf("allowlist entry %q: a TCP printer is host:port, a range such as 192.168.1.0/24:9100, or a name such as *.lan:*", e)
	}
	host, port := e, strconv.Itoa(printers.DefaultPort)
	switch {
	case strings.HasPrefix(e, "["):
		end := strings.Index(e, "]")
		if end < 0 {
			return bad()
		}
		host = e[1:end]
		if rest := e[end+1:]; rest != "" {
			if !strings.HasPrefix(rest, ":") {
				return bad()
			}
			port = rest[1:]
		}
	case strings.Count(e, ":") == 1:
		host, port, _ = strings.Cut(e, ":")
	case strings.Count(e, ":") > 1:
		// A bare IPv6 address or range, on port 9100.
	}
	r := tcpRule{}
	if port != "*" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return bad()
		}
		r.port = n
	}
	if p, err := netip.ParsePrefix(host); err == nil {
		r.prefix = p.Masked()
		return r, nil
	}
	if a, err := netip.ParseAddr(host); err == nil {
		a = a.Unmap()
		r.prefix = netip.PrefixFrom(a, a.BitLen())
		return r, nil
	}
	host = strings.ToLower(host)
	if host == "" {
		return bad()
	}
	if _, err := path.Match(host, ""); err != nil || strings.ContainsAny(host, "/[") {
		return bad()
	}
	r.name = host
	return r, nil
}

// Allows says whether the relay may reach a normalized connection, and if
// not, why.
func (a *Allowlist) Allows(c printers.Connection) error {
	refuse := func() error {
		return fmt.Errorf("%s is outside this relay's allowlist", c.Address)
	}
	switch c.Type {
	case printers.ConnTCP:
		host, portText, err := net.SplitHostPort(c.Address)
		if err != nil {
			return refuse()
		}
		port, _ := strconv.Atoi(portText)
		addr, notAddr := netip.ParseAddr(host)
		isAddr := notAddr == nil
		for _, r := range a.tcp {
			if r.port != 0 && r.port != port {
				continue
			}
			switch {
			case r.name == "*":
				return nil
			case r.name != "":
				if ok, _ := path.Match(r.name, strings.ToLower(host)); ok && !isAddr {
					return nil
				}
			case isAddr && r.prefix.Contains(addr.Unmap()):
				return nil
			}
		}
	case printers.ConnIPP:
		for _, pattern := range a.urls {
			if wildcard(strings.ToLower(pattern), strings.ToLower(c.Address)) {
				return nil
			}
		}
	case printers.ConnWindows:
		for _, pattern := range a.windows {
			if wildcard(pattern, strings.ToLower(c.Address)) {
				return nil
			}
		}
	case printers.ConnDevice:
		for _, pattern := range a.devices {
			if ok, _ := path.Match(pattern, c.Address); ok {
				return nil
			}
		}
	case printers.ConnBLE:
		addr, err := printers.ParseBLEAddress(c.Address)
		if err == nil && printers.MatchBluetooth(a.bluetooth, addr.MAC) {
			return nil
		}
		if err == nil {
			return fmt.Errorf("%s is outside this relay's allowlist", addr.MAC)
		}
	}
	return refuse()
}

// Summary is the allowlist as the relay reports it: its entries, its
// device patterns, and its Bluetooth ones.
func (a *Allowlist) Summary() []string {
	out := append([]string{}, a.entries...)
	for _, d := range a.devices {
		out = append(out, "device "+d)
	}
	for _, b := range a.bluetooth {
		out = append(out, "bluetooth "+b)
	}
	return out
}

// wildcard reports whether s matches pattern, in which * matches any run
// of characters, slashes included.
func wildcard(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return strings.HasSuffix(s, parts[len(parts)-1])
}
