//go:build linux

package printers

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// On Linux, Bluetooth LE goes through BlueZ, over the system D-Bus
// (BlueZ's doc/org.bluez.*.rst): in pure Go, so BAM! still builds without
// cgo, and with the permissions BlueZ gives the user BAM! runs as.

// systemBLE is the system's Bluetooth LE: BlueZ.
var systemBLE BLEDialer = dialBlueZ

const (
	bluez         = "org.bluez"
	bluezAdapter  = "org.bluez.Adapter1"
	bluezDevice   = "org.bluez.Device1"
	bluezService  = "org.bluez.GattService1"
	bluezChar     = "org.bluez.GattCharacteristic1"
	dbusProps     = "org.freedesktop.DBus.Properties"
	dbusObjectMgr = "org.freedesktop.DBus.ObjectManager"
	// bluezPoll is how often BlueZ is asked whether a printer has been
	// found, or its services read.
	bluezPoll = 250 * time.Millisecond
	// bluezSettle is how long a printer written to without responses has
	// before it is disconnected: BlueZ queues such writes, and
	// disconnecting at once could lose the end of the job.
	bluezSettle = time.Second
)

// bluezObjects is what BlueZ's object manager lists: each object's
// interfaces and their properties.
type bluezObjects = map[dbus.ObjectPath]map[string]map[string]dbus.Variant

// bluezPeripheral is a printer connected through BlueZ.
type bluezPeripheral struct {
	conn   *dbus.Conn
	device dbus.BusObject
	write  dbus.BusObject
	notify dbus.BusObject
	// command is set when the characteristic takes writes without
	// response.
	command bool
	mtu     int
	// connected is set when BAM! connected the printer, and so
	// disconnects it.
	connected bool
	signals   chan *dbus.Signal
	notes     chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func dialBlueZ(ctx context.Context, mac string, profile BLEProfile) (BLEPeripheral, error) {
	conn, err := dbus.ConnectSystemBus(dbus.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("reaching BlueZ on the system D-Bus: %v", err)
	}
	p := &bluezPeripheral{conn: conn, mtu: bleMinMTU, done: make(chan struct{})}
	if err := p.open(ctx, mac, profile); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}

// objects lists BlueZ's objects.
func (p *bluezPeripheral) objects(ctx context.Context) (bluezObjects, error) {
	var objs bluezObjects
	if err := p.conn.Object(bluez, "/").CallWithContext(ctx, dbusObjectMgr+".GetManagedObjects", 0).Store(&objs); err != nil {
		return nil, fmt.Errorf("asking BlueZ for Bluetooth devices: %v", err)
	}
	return objs, nil
}

// property reads one property of an object.
func property(ctx context.Context, o dbus.BusObject, iface, name string) (dbus.Variant, error) {
	var v dbus.Variant
	err := o.CallWithContext(ctx, dbusProps+".Get", 0, iface, name).Store(&v)
	return v, err
}

// find returns the object with an interface whose properties match, if
// any, the first by path.
func find(objs bluezObjects, iface string, match func(props map[string]dbus.Variant) bool) (dbus.ObjectPath, bool) {
	paths := make([]string, 0, len(objs))
	for path := range objs {
		paths = append(paths, string(path))
	}
	sort.Strings(paths)
	for _, path := range paths {
		if props, ok := objs[dbus.ObjectPath(path)][iface]; ok && match(props) {
			return dbus.ObjectPath(path), true
		}
	}
	return "", false
}

// is reports whether a property holds a value.
func is[T comparable](props map[string]dbus.Variant, name string, want T) bool {
	v, ok := props[name].Value().(T)
	return ok && v == want
}

// open finds the printer, connects, and finds its service and
// characteristics.
func (p *bluezPeripheral) open(ctx context.Context, mac string, profile BLEProfile) error {
	objs, err := p.objects(ctx)
	if err != nil {
		return err
	}
	isPrinter := func(props map[string]dbus.Variant) bool {
		addr, _ := props["Address"].Value().(string)
		return strings.EqualFold(addr, mac)
	}
	device, ok := find(objs, bluezDevice, isPrinter)
	if !ok {
		if device, err = p.discover(ctx, objs, isPrinter); err != nil {
			return err
		}
	}
	p.device = p.conn.Object(bluez, device)
	if v, _ := property(ctx, p.device, bluezDevice, "Connected"); v.Value() != true {
		if err := p.device.CallWithContext(ctx, bluezDevice+".Connect", 0).Err; err != nil {
			return fmt.Errorf("connecting: %v", err)
		}
		p.connected = true
	}
	for {
		if v, _ := property(ctx, p.device, bluezDevice, "ServicesResolved"); v.Value() == true {
			break
		}
		select {
		case <-ctx.Done():
			return errors.New("its services were not read in time")
		case <-time.After(bluezPoll):
		}
	}

	if objs, err = p.objects(ctx); err != nil {
		return err
	}
	service, ok := find(objs, bluezService, func(props map[string]dbus.Variant) bool {
		return is(props, "Device", device) && is(props, "UUID", profile.Service)
	})
	if !ok {
		return fmt.Errorf("the printer has no service %s", profile.Service)
	}
	characteristic := func(uuid string) (dbus.ObjectPath, bool) {
		return find(objs, bluezChar, func(props map[string]dbus.Variant) bool {
			return is(props, "Service", service) && is(props, "UUID", uuid)
		})
	}
	write, ok := characteristic(profile.Write)
	if !ok {
		return fmt.Errorf("the printer's service has no characteristic %s", profile.Write)
	}
	props := objs[write][bluezChar]
	flags, _ := props["Flags"].Value().([]string)
	switch {
	case slices.Contains(flags, "write-without-response"):
		p.command = true
	case !slices.Contains(flags, "write"):
		return fmt.Errorf("the printer's characteristic %s cannot be written", profile.Write)
	}
	if mtu, ok := props["MTU"].Value().(uint16); ok && int(mtu) > bleMinMTU {
		p.mtu = int(mtu)
	}
	p.write = p.conn.Object(bluez, write)

	if profile.Notify == "" {
		return nil
	}
	notify, ok := characteristic(profile.Notify)
	if !ok {
		return fmt.Errorf("the printer's service has no characteristic %s", profile.Notify)
	}
	if err := p.conn.AddMatchSignalContext(ctx, dbus.WithMatchObjectPath(notify), dbus.WithMatchInterface(dbusProps), dbus.WithMatchMember("PropertiesChanged")); err != nil {
		return fmt.Errorf("listening for the printer's notifications: %v", err)
	}
	p.signals, p.notes = make(chan *dbus.Signal, 16), make(chan []byte, 16)
	p.conn.Signal(p.signals)
	go p.listen(notify)
	p.notify = p.conn.Object(bluez, notify)
	if err := p.notify.CallWithContext(ctx, bluezChar+".StartNotify", 0).Err; err != nil {
		p.notify = nil
		return fmt.Errorf("subscribing to the printer's notifications: %v", err)
	}
	return nil
}

// discover searches for a printer BlueZ does not know yet, until it is
// found or ctx ends.
func (p *bluezPeripheral) discover(ctx context.Context, objs bluezObjects, isPrinter func(map[string]dbus.Variant) bool) (dbus.ObjectPath, error) {
	path, ok := find(objs, bluezAdapter, func(map[string]dbus.Variant) bool { return true })
	if !ok {
		return "", errors.New("this system has no Bluetooth adapter that BlueZ knows of")
	}
	adapter := p.conn.Object(bluez, path)
	// Only LE devices; an older BlueZ without filters searches for all.
	_ = adapter.CallWithContext(ctx, bluezAdapter+".SetDiscoveryFilter", 0, map[string]dbus.Variant{"Transport": dbus.MakeVariant("le")}).Err
	if err := adapter.CallWithContext(ctx, bluezAdapter+".StartDiscovery", 0).Err; err != nil {
		return "", fmt.Errorf("searching for Bluetooth devices (is the adapter powered?): %v", err)
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = adapter.CallWithContext(stop, bluezAdapter+".StopDiscovery", 0).Err
	}()
	for {
		select {
		case <-ctx.Done():
			return "", errors.New("the printer was not found")
		case <-time.After(bluezPoll):
		}
		objs, err := p.objects(ctx)
		if err != nil {
			return "", err
		}
		if device, ok := find(objs, bluezDevice, isPrinter); ok {
			return device, nil
		}
	}
}

// listen passes on the values the notify characteristic changes to.
func (p *bluezPeripheral) listen(path dbus.ObjectPath) {
	defer close(p.notes)
	for {
		var s *dbus.Signal
		select {
		case s = <-p.signals:
		case <-p.done:
			return
		}
		if s == nil {
			return
		}
		if s.Path != path || len(s.Body) < 2 || s.Body[0] != bluezChar {
			continue
		}
		changed, _ := s.Body[1].(map[string]dbus.Variant)
		value, ok := changed["Value"].Value().([]byte)
		if !ok {
			continue
		}
		select {
		case p.notes <- value:
		case <-p.done:
			return
		}
	}
}

func (p *bluezPeripheral) MTU() int { return p.mtu }

func (p *bluezPeripheral) Write(ctx context.Context, chunk []byte) error {
	kind := "request"
	if p.command {
		kind = "command"
	}
	return p.write.CallWithContext(ctx, bluezChar+".WriteValue", 0, chunk, map[string]dbus.Variant{"type": dbus.MakeVariant(kind)}).Err
}

func (p *bluezPeripheral) Notifications() <-chan []byte {
	if p.notify == nil {
		return nil
	}
	return p.notes
}

func (p *bluezPeripheral) Close() error {
	p.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if p.notify != nil {
			_ = p.notify.CallWithContext(ctx, bluezChar+".StopNotify", 0).Err
		}
		if p.connected {
			if p.command {
				time.Sleep(bluezSettle)
			}
			_ = p.device.CallWithContext(ctx, bluezDevice+".Disconnect", 0).Err
		}
		close(p.done)
	})
	return p.conn.Close()
}
