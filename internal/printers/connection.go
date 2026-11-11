package printers

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Connection types.
const (
	// ConnTCP sends the job to a raw TCP port, host[:port] (default 9100):
	// a network printer, or a print server such as p910nd sharing a USB one.
	ConnTCP = "tcp"
	// ConnDevice writes the job to a device on the BAM server, such as
	// /dev/usb/lp0. Paths must match the server's printers.devices setting.
	ConnDevice = "device"
	// ConnIPP submits the job to an IPP queue, such as a raw CUPS queue:
	// ipp://host[:port]/printers/name (also ipps, http and https).
	ConnIPP = "ipp"
	// ConnWindows prints RAW through the Windows spooler to a local printer
	// or a \\host\share; only when BAM itself runs on Windows.
	ConnWindows = "windows"
	// ConnBLE writes the job to a Bluetooth LE printer's GATT
	// characteristic: its Bluetooth address, with the service and
	// characteristics if the driver's are not the ones (ble.go). Addresses
	// must match the server's printers.bluetooth setting.
	ConnBLE = "ble"
	// ConnOpenEPaperLink uploads a display's image to an OpenEPaperLink
	// access point for one of its tags (display.go): the access point's
	// host[:port] or http(s)://host[:port], then ?tag= and the tag's MAC
	// address.
	ConnOpenEPaperLink = "openepaperlink"
	// ConnPull keeps a display's image for the display to fetch with its
	// own token: no address, or for a display that names itself by it,
	// such as a TRMNL, its MAC address.
	ConnPull = "pull"
	// ConnWebhook posts a display's image through a webhook notification
	// channel: the channel's ID.
	ConnWebhook = "webhook"
)

// ConnectionTypes lists the connection types.
func ConnectionTypes() []string {
	return []string{ConnTCP, ConnDevice, ConnIPP, ConnWindows, ConnBLE, ConnOpenEPaperLink, ConnPull, ConnWebhook}
}

// Connection says how BAM reaches a printer.
type Connection struct {
	Type    string `json:"type"`
	Address string `json:"address"`
	// Relay, when set, names the relay the printer is reached through
	// (docs/relays.md): the address is on the relay's network, and the
	// relay's allowlist, not this server's, decides whether it may be
	// reached.
	Relay string `json:"relay,omitempty"`
}

// maxAddress is the longest address stored.
const maxAddress = 1024

// relayName is a relay's name, as the relay bus has it.
var relayName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// NormalizeConnection checks a connection's address for its type and returns
// it in canonical form: TCP addresses always carry a port, device paths are
// cleaned, Bluetooth addresses are upper case. Errors wrap ErrInvalidPrinter.
func NormalizeConnection(c Connection) (Connection, error) {
	bad := func(format string, args ...any) (Connection, error) {
		return c, fmt.Errorf("%w: connection: %s", ErrInvalidPrinter, fmt.Sprintf(format, args...))
	}
	c.Type = strings.TrimSpace(c.Type)
	c.Address = strings.TrimSpace(c.Address)
	c.Relay = strings.TrimSpace(c.Relay)
	if c.Relay != "" && (c.Relay == "main" || !relayName.MatchString(c.Relay)) {
		return bad("relay must name a relay: lowercase letters, digits and hyphens")
	}
	if c.Type == ConnPull || c.Type == ConnWebhook {
		if c.Relay != "" {
			return bad("a %s connection is this server's own, never through a relay", c.Type)
		}
	}
	if c.Type == ConnPull {
		if c.Address == "" {
			return c, nil
		}
		mac, ok := normalizeMAC(c.Address, 6)
		if !ok {
			return bad("a pull connection has no address, or the display's MAC address, such as AA:BB:CC:DD:EE:FF")
		}
		c.Address = mac
		return c, nil
	}
	if c.Address == "" || len(c.Address) > maxAddress {
		return bad("address must be 1-%d characters", maxAddress)
	}
	if strings.IndexFunc(c.Address, unicode.IsControl) >= 0 {
		return bad("address must not contain control characters")
	}
	switch c.Type {
	case ConnTCP:
		host, port := c.Address, DefaultPort
		if h, p, err := net.SplitHostPort(c.Address); err == nil {
			n, err := strconv.Atoi(p)
			if err != nil || n < 1 || n > 65535 {
				return bad("port must be 1-65535")
			}
			host, port = h, n
		}
		if net.ParseIP(host) == nil && (len(host) > 253 || !hostnameRegex.MatchString(host)) {
			return bad("tcp address must be host or host:port, with an IP address or hostname")
		}
		c.Address = net.JoinHostPort(host, strconv.Itoa(port))
	case ConnDevice:
		// Printer devices are Unix paths; on Windows use ConnWindows.
		if !strings.HasPrefix(c.Address, "/") {
			return bad("device address must be an absolute path such as /dev/usb/lp0")
		}
		c.Address = path.Clean(c.Address)
	case ConnIPP:
		u, err := url.Parse(c.Address)
		if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
			return bad("ipp address must be a URI such as ipp://cups.lan/printers/labels")
		}
		switch u.Scheme {
		case "ipp", "ipps", "http", "https":
		default:
			return bad("ipp address must use ipp, ipps, http or https")
		}
	case ConnWindows:
		if len(c.Address) > 255 {
			return bad("windows printer name must be at most 255 characters")
		}
	case ConnBLE:
		a, err := ParseBLEAddress(c.Address)
		if err != nil {
			return bad("%v", err)
		}
		c.Address = a.String()
	case ConnOpenEPaperLink:
		a, err := ParseOEPLAddress(c.Address)
		if err != nil {
			return bad("%v", err)
		}
		c.Address = a.String()
	case ConnWebhook:
		if len(c.Address) > 64 || strings.ContainsAny(c.Address, " /?#") {
			return bad("a webhook connection's address is the ID of a webhook notification channel")
		}
	default:
		return bad("type must be one of %s", strings.Join(ConnectionTypes(), ", "))
	}
	return c, nil
}

// OEPLAddress is an OpenEPaperLink connection's address: the access point,
// and the tag on it.
type OEPLAddress struct {
	// Base is the access point's URL, http(s)://host[:port].
	Base string
	// Tag is the tag's MAC address, 8 bytes in hexadecimal, upper case,
	// as the access point writes it.
	Tag string
}

// String writes the address in canonical form, base?tag=MAC.
func (a OEPLAddress) String() string { return a.Base + "?tag=" + a.Tag }

// ParseOEPLAddress reads an OpenEPaperLink connection's address: the access
// point's host, host:port or http(s)://host[:port], then ?tag= and the
// tag's MAC address, 6 or 8 bytes, as 0000021E3D6F1234 or with colons.
func ParseOEPLAddress(s string) (OEPLAddress, error) {
	where, query, _ := strings.Cut(strings.TrimSpace(s), "?")
	if !strings.Contains(where, "://") {
		where = "http://" + where
	}
	u, err := url.Parse(where)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.Trim(u.Path, "/") != "" || u.Fragment != "" {
		return OEPLAddress{}, errors.New("openepaperlink address must be the access point, such as 192.168.1.50 or http://ap.lan, then ?tag= and the tag's MAC address")
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return OEPLAddress{}, errors.New("openepaperlink address: port must be 1-65535")
		}
	}
	values, err := url.ParseQuery(query)
	if err != nil || len(values["tag"]) != 1 || len(values) != 1 {
		return OEPLAddress{}, errors.New("openepaperlink address: after the access point comes ?tag= and the tag's MAC address, such as ?tag=0000021E3D6F1234")
	}
	mac, ok := normalizeMAC(values["tag"][0], 8)
	if !ok {
		return OEPLAddress{}, errors.New("openepaperlink address: the tag is its MAC address, 6 or 8 bytes in hexadecimal, such as 0000021E3D6F1234")
	}
	return OEPLAddress{Base: u.Scheme + "://" + u.Host, Tag: strings.ReplaceAll(mac, ":", "")}, nil
}

// normalizeMAC reads a MAC address of 6, or with size 8 also of 8, bytes,
// in hexadecimal with or without colons or hyphens between them, and
// writes it upper case with colons: of 6 bytes as given, and with size 8
// widened to 8 with leading zeros as OpenEPaperLink writes them.
func normalizeMAC(s string, size int) (string, bool) {
	digits := strings.NewReplacer(":", "", "-", "").Replace(strings.TrimSpace(s))
	raw, err := hex.DecodeString(digits)
	if err != nil || (len(raw) != 6 && (size != 8 || len(raw) != 8)) {
		return "", false
	}
	if size == 8 && len(raw) == 6 {
		raw = append([]byte{0, 0}, raw...)
	}
	parts := make([]string, len(raw))
	for i, b := range raw {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":"), true
}
