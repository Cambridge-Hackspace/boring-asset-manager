package printers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// Bluetooth LE printers (docs/printing.md#bluetooth): a connection of type
// ble names the printer's Bluetooth address, and the GATT service and
// characteristics its driver writes to. The job is written to the write
// characteristic in chunks as large as the link allows, and where the
// driver wants it, each chunk waits for the printer to acknowledge it with
// a notification. A driver whose printers answer each command instead
// holds a conversation with them (Protocol), which the job is sent in.

// BLEProfile is how a family of printers takes jobs over Bluetooth LE:
// the service, the characteristic jobs are written to, and the one the
// printer notifies on, if any.
type BLEProfile struct {
	Service string `json:"service"`
	Write   string `json:"write"`
	Notify  string `json:"notify,omitempty"`
	// Ack is set when the printer acknowledges each write with a
	// notification, and the next write must wait for it.
	Ack bool `json:"ack,omitempty"`
	// Protocol, when set, is the conversation the job is sent in, which
	// reads the printer's answers on the notify characteristic
	// (protocol.go): ProtocolNiimbot.
	Protocol string `json:"protocol,omitempty"`
}

// BLEDriver is a driver whose printers connect over Bluetooth LE. Its
// profile is the default for a ble connection, which may override any of
// it.
type BLEDriver interface {
	BLE() BLEProfile
}

// BLEAddress is a ble connection's address, parsed: the printer's
// Bluetooth address, and whatever of the profile the connection sets.
type BLEAddress struct {
	// MAC is the printer's address, such as 7C:2F:80:A1:B2:C3.
	MAC     string
	Service string
	Write   string
	Notify  string
	// Ack is nil when the connection leaves it to the driver.
	Ack *bool
	// Protocol is empty when the connection leaves it to the driver.
	Protocol string
}

var (
	// bleMAC is a Bluetooth address, with colons or hyphens.
	bleMAC = regexp.MustCompile(`^[0-9A-Fa-f]{2}([:-][0-9A-Fa-f]{2}){5}$`)
	// bleUUID is a 128-bit UUID, with or without hyphens, or a 16- or
	// 32-bit one from the Bluetooth base UUID.
	bleUUID = regexp.MustCompile(`^([0-9a-f]{4}|[0-9a-f]{8}|[0-9a-f]{32}|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$`)
	// blePattern is a pattern of Bluetooth addresses, as allowlists take.
	blePattern = regexp.MustCompile(`^[0-9A-Fa-f:*?]+$`)
)

// bleBaseUUID completes a 16- or 32-bit UUID.
const bleBaseUUID = "-0000-1000-8000-00805f9b34fb"

// normalizeUUID returns a UUID in its full, lowercase form.
func normalizeUUID(s string) (string, bool) {
	s = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "0x"))
	if !bleUUID.MatchString(s) {
		return "", false
	}
	switch len(s) {
	case 4:
		return "0000" + s + bleBaseUUID, true
	case 8:
		return s + bleBaseUUID, true
	case 32:
		return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], true
	}
	return s, true
}

// ParseBLEAddress reads a ble connection's address: the printer's
// Bluetooth address, then optionally, as a query, service, write and
// notify UUIDs, ack (true or false) and protocol, such as
// 7C:2F:80:A1:B2:C3?service=fff0&write=fff2&notify=fff1.
func ParseBLEAddress(s string) (BLEAddress, error) {
	mac, query, _ := strings.Cut(strings.TrimSpace(s), "?")
	if !bleMAC.MatchString(mac) {
		return BLEAddress{}, errors.New("ble address must be the printer's Bluetooth address, such as 7C:2F:80:A1:B2:C3")
	}
	a := BLEAddress{MAC: strings.ToUpper(strings.ReplaceAll(mac, "-", ":"))}
	values, err := url.ParseQuery(query)
	if err != nil {
		return a, errors.New("ble address: after the Bluetooth address come service=, write=, notify=, ack= and protocol=, joined with &")
	}
	for key, v := range values {
		if len(v) != 1 {
			return a, fmt.Errorf("ble address gives %s more than once", key)
		}
		switch key {
		case "service", "write", "notify":
			uuid, ok := normalizeUUID(v[0])
			if !ok {
				return a, fmt.Errorf("ble address: %s must be a UUID, such as 0000fff0-0000-1000-8000-00805f9b34fb or fff0", key)
			}
			switch key {
			case "service":
				a.Service = uuid
			case "write":
				a.Write = uuid
			default:
				a.Notify = uuid
			}
		case "ack":
			on := v[0] == "true"
			if !on && v[0] != "false" {
				return a, errors.New("ble address: ack must be true or false")
			}
			a.Ack = &on
		case "protocol":
			if err := checkProtocol(v[0]); err != nil {
				return a, fmt.Errorf("ble address: %v", err)
			}
			a.Protocol = v[0]
		default:
			return a, fmt.Errorf("ble address: %q is not one of service, write, notify, ack and protocol", key)
		}
	}
	return a, nil
}

// String is the address in canonical form.
func (a BLEAddress) String() string {
	var q []string
	for _, kv := range [][2]string{{"service", a.Service}, {"write", a.Write}, {"notify", a.Notify}} {
		if kv[1] != "" {
			q = append(q, kv[0]+"="+kv[1])
		}
	}
	if a.Ack != nil {
		q = append(q, fmt.Sprintf("ack=%t", *a.Ack))
	}
	if a.Protocol != "" {
		q = append(q, "protocol="+a.Protocol)
	}
	if len(q) == 0 {
		return a.MAC
	}
	return a.MAC + "?" + strings.Join(q, "&")
}

// Profile is the address's profile over the driver's: what it sets, and
// the driver's defaults for the rest.
func (a BLEAddress) Profile(defaults BLEProfile) BLEProfile {
	p := defaults
	if a.Service != "" {
		p.Service = a.Service
	}
	if a.Write != "" {
		p.Write = a.Write
	}
	if a.Notify != "" {
		p.Notify = a.Notify
	}
	if a.Ack != nil {
		p.Ack = *a.Ack
	}
	if a.Protocol != "" {
		p.Protocol = a.Protocol
	}
	return p
}

// checkProfile says what a profile lacks for a job to be written.
func checkProfile(p BLEProfile) error {
	switch {
	case p.Service == "" || p.Write == "":
		return errors.New("ble connection needs the printer's service and write characteristic: its driver gives none, so set them on the connection")
	case p.Ack && p.Notify == "":
		return errors.New("ble connection acknowledges writes, so it needs the characteristic the printer notifies on")
	case p.Protocol != "" && p.Notify == "":
		return errors.New("ble connection reads the printer's answers, so it needs the characteristic the printer notifies on")
	}
	return nil
}

// ConnectionFor is a printer's connection as it is sent on: for ble, its
// address with the driver's profile filled in where the connection sets
// none, and for a device, the driver's protocol, so that a relay
// delivering it needs no drivers. Errors wrap ErrInvalidPrinter.
func ConnectionFor(driver string, c Connection) (Connection, error) {
	if c.Type != ConnBLE && c.Type != ConnDevice {
		return c, nil
	}
	d, err := Lookup(driver)
	if err != nil {
		return c, err
	}
	return connectionFor(d, c)
}

func connectionFor(d Driver, c Connection) (Connection, error) {
	if c.Type == ConnDevice {
		return deviceFor(d, c)
	}
	a, err := ParseBLEAddress(c.Address)
	if err != nil {
		return c, fmt.Errorf("%w: connection: %v", ErrInvalidPrinter, err)
	}
	var defaults BLEProfile
	if b, ok := d.(BLEDriver); ok {
		defaults = b.BLE()
	}
	p := a.Profile(defaults)
	if err := checkProfile(p); err != nil {
		return c, fmt.Errorf("%w: connection: %v", ErrInvalidPrinter, err)
	}
	full := BLEAddress{MAC: a.MAC, Service: p.Service, Write: p.Write, Notify: p.Notify, Protocol: p.Protocol}
	if p.Ack {
		full.Ack = &p.Ack
	}
	c.Address = full.String()
	return c, nil
}

// CheckBluetoothPattern checks a pattern of Bluetooth addresses, as
// [printers] bluetooth lists them: an address, or a path.Match pattern of
// them such as 7C:2F:80:*, or * for any.
func CheckBluetoothPattern(pattern string) error {
	if !blePattern.MatchString(pattern) {
		return fmt.Errorf("Bluetooth pattern %q must be an address such as 7C:2F:80:A1:B2:C3, with * or ? for any part", pattern)
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return fmt.Errorf("Bluetooth pattern %q: %v", pattern, err)
	}
	return nil
}

// MatchBluetooth says whether a printer's Bluetooth address is among the
// patterns.
func MatchBluetooth(patterns []string, mac string) bool {
	for _, pattern := range patterns {
		if ok, _ := path.Match(strings.ToUpper(pattern), strings.ToUpper(mac)); ok {
			return true
		}
	}
	return false
}

// BLEPeripheral is a printer connected over Bluetooth LE, with its
// service and characteristics found. BlueZ's, on Linux, is the real one;
// tests use a simulated one.
type BLEPeripheral interface {
	// MTU is the ATT MTU agreed with the printer: a write carries at most
	// MTU-3 bytes.
	MTU() int
	// Write writes one chunk to the write characteristic.
	Write(ctx context.Context, chunk []byte) error
	// Notifications are the values the printer notifies, nil without a
	// notify characteristic.
	Notifications() <-chan []byte
	// Close disconnects.
	Close() error
}

// BLEDialer connects to a printer over Bluetooth LE, finding the service
// and characteristics the profile names.
type BLEDialer func(ctx context.Context, mac string, p BLEProfile) (BLEPeripheral, error)

// ErrBLEUnsupported is wrapped when this system cannot reach Bluetooth LE
// printers.
var ErrBLEUnsupported = errors.New("Bluetooth LE isn't supported on this system")

const (
	// bleMinMTU is the ATT MTU every link has.
	bleMinMTU = 23
	// bleMaxWrite is the most a characteristic's value may hold.
	bleMaxWrite = 512
)

// dialerBLE is the dialer in use: the dispatcher's own, or the system's,
// nil when there is none.
func (d *Dispatcher) dialerBLE() BLEDialer {
	if d.DialBLE != nil {
		return d.DialBLE
	}
	return systemBLE
}

// checkBLE says whether this server may reach a ble connection.
func (d *Dispatcher) checkBLE(c Connection) error {
	a, err := ParseBLEAddress(c.Address)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPrinter, err)
	}
	switch {
	case len(d.AllowedBluetooth) == 0:
		return fmt.Errorf("%w: Bluetooth connections are off; list allowed printer addresses in the server's [printers] bluetooth setting", ErrInvalidPrinter)
	case !MatchBluetooth(d.AllowedBluetooth, a.MAC):
		return fmt.Errorf("%w: %s is not among the server's allowed Bluetooth printers (%s)", ErrInvalidPrinter, a.MAC, strings.Join(d.AllowedBluetooth, ", "))
	case d.dialerBLE() == nil:
		return fmt.Errorf("%w: %v; reach the printer through a relay on Linux", ErrInvalidPrinter, ErrBLEUnsupported)
	}
	return nil
}

// sendBLE writes a job to a printer over Bluetooth LE, in chunks the link
// allows. Each chunk has WriteTimeout to be taken, and to be acknowledged
// when the profile says the printer does.
func (d *Dispatcher) sendBLE(ctx context.Context, address string, job []byte) error {
	a, err := ParseBLEAddress(address)
	if err != nil {
		return err
	}
	p := a.Profile(BLEProfile{})
	if err := checkProfile(p); err != nil {
		return err
	}
	dctx, cancel := context.WithTimeout(ctx, d.BLEDialTimeout)
	peripheral, err := d.dialerBLE()(dctx, a.MAC, p)
	cancel()
	if err != nil {
		if ctx.Err() == nil && dctx.Err() != nil {
			return fmt.Errorf("%s could not be connected to in %s: is it on, in range, and free of other connections? (%v)", a.MAC, d.BLEDialTimeout, err)
		}
		return fmt.Errorf("%s: %v", a.MAC, err)
	}
	defer peripheral.Close()

	notes := peripheral.Notifications()
	if p.Protocol != "" {
		if notes == nil {
			return errors.New("the printer's notify characteristic could not be subscribed to")
		}
		return d.converse(ctx, p.Protocol, blePort{peripheral: peripheral, timeout: d.WriteTimeout}, job)
	}
	if p.Ack && notes == nil {
		return errors.New("the printer's notify characteristic could not be subscribed to")
	}
	chunk := min(max(peripheral.MTU(), bleMinMTU)-3, bleMaxWrite)
	for sent := 0; sent < len(job); {
		n := min(chunk, len(job)-sent)
		wctx, cancel := context.WithTimeout(ctx, d.WriteTimeout)
		err := peripheral.Write(wctx, job[sent:sent+n])
		if err == nil && p.Ack {
			select {
			case _, ok := <-notes:
				if !ok {
					err = errors.New("the printer disconnected")
				}
			case <-wctx.Done():
				err = wctx.Err()
				if ctx.Err() == nil {
					err = fmt.Errorf("the printer did not acknowledge a write within %s", d.WriteTimeout)
				}
			}
		} else if err != nil && ctx.Err() == nil && wctx.Err() != nil {
			err = fmt.Errorf("the printer did not take a write within %s", d.WriteTimeout)
		}
		cancel()
		if err != nil {
			return fmt.Errorf("sending job at byte %d of %d: %v", sent, len(job), err)
		}
		sent += n
	}
	return nil
}

// bleSlotKey is the destination a ble connection's jobs take turns at: the
// printer, however its profile is given.
func bleSlotKey(c Connection) Connection {
	if a, err := ParseBLEAddress(c.Address); err == nil {
		c.Address = a.MAC
	}
	c.Relay = ""
	return c
}

// defaultBLEDialTimeout is how long finding and connecting to a Bluetooth
// LE printer may take: longer than a TCP connection, since the printer may
// first need to be found.
const defaultBLEDialTimeout = 20 * time.Second

// blePort is a conversation's port over Bluetooth LE: each packet written
// in chunks the link allows, and the printer's notifications.
type blePort struct {
	peripheral BLEPeripheral
	timeout    time.Duration
}

func (b blePort) Write(ctx context.Context, data []byte) error {
	chunk := min(max(b.peripheral.MTU(), bleMinMTU)-3, bleMaxWrite)
	for sent := 0; sent < len(data); {
		n := min(chunk, len(data)-sent)
		wctx, cancel := context.WithTimeout(ctx, b.timeout)
		err := b.peripheral.Write(wctx, data[sent:sent+n])
		cancel()
		if err != nil {
			if ctx.Err() == nil && wctx.Err() != nil {
				return fmt.Errorf("the printer did not take a write within %s", b.timeout)
			}
			return err
		}
		sent += n
	}
	return nil
}

func (b blePort) Received() <-chan []byte { return b.peripheral.Notifications() }
