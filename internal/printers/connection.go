package printers

import (
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
)

// ConnectionTypes lists the connection types.
func ConnectionTypes() []string { return []string{ConnTCP, ConnDevice, ConnIPP, ConnWindows, ConnBLE} }

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
	default:
		return bad("type must be one of %s", strings.Join(ConnectionTypes(), ", "))
	}
	return c, nil
}
