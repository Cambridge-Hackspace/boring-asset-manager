package printers

import (
	"fmt"
	"net"
	"net/url"
	"path"
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
)

// ConnectionTypes lists the connection types.
func ConnectionTypes() []string { return []string{ConnTCP, ConnDevice, ConnIPP, ConnWindows} }

// Connection says how BAM reaches a printer.
type Connection struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

// maxAddress is the longest address stored.
const maxAddress = 1024

// NormalizeConnection checks a connection's address for its type and returns
// it in canonical form: TCP addresses always carry a port, device paths are
// cleaned. Errors wrap ErrInvalidPrinter.
func NormalizeConnection(c Connection) (Connection, error) {
	bad := func(format string, args ...any) (Connection, error) {
		return c, fmt.Errorf("%w: connection: %s", ErrInvalidPrinter, fmt.Sprintf(format, args...))
	}
	c.Type = strings.TrimSpace(c.Type)
	c.Address = strings.TrimSpace(c.Address)
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
	default:
		return bad("type must be one of %s", strings.Join(ConnectionTypes(), ", "))
	}
	return c, nil
}
