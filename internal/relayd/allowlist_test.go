package relayd

import (
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
)

func TestAllowlist(t *testing.T) {
	a, err := ParseAllowlist([]string{
		"192.168.1.40",   // one printer, on 9100
		"10.0.0.0/24:*",  // a range, on any port
		"*.lan:9101",     // names
		"[fd00::40]:515", // an IPv6 printer
		"ipp://cups.lan/printers/*",
		"windows:Zebra*",
	}, []string{"/dev/usb/lp*"}, []string{"7C:2F:80:*"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		conn printers.Connection
		ok   bool
	}{
		{printers.Connection{Type: printers.ConnTCP, Address: "192.168.1.40:9100"}, true},
		{printers.Connection{Type: printers.ConnTCP, Address: "192.168.1.40:22"}, false},
		{printers.Connection{Type: printers.ConnTCP, Address: "192.168.1.41:9100"}, false},
		{printers.Connection{Type: printers.ConnTCP, Address: "10.0.0.7:631"}, true},
		{printers.Connection{Type: printers.ConnTCP, Address: "10.0.1.7:9100"}, false},
		{printers.Connection{Type: printers.ConnTCP, Address: "ql.lan:9101"}, true},
		{printers.Connection{Type: printers.ConnTCP, Address: "QL.LAN:9101"}, true},
		{printers.Connection{Type: printers.ConnTCP, Address: "ql.lan:9100"}, false},
		{printers.Connection{Type: printers.ConnTCP, Address: "evil.example:9101"}, false},
		{printers.Connection{Type: printers.ConnTCP, Address: "[fd00::40]:515"}, true},
		{printers.Connection{Type: printers.ConnIPP, Address: "ipp://cups.lan/printers/dymo"}, true},
		{printers.Connection{Type: printers.ConnIPP, Address: "ipp://cups.lan/admin"}, false},
		{printers.Connection{Type: printers.ConnWindows, Address: "Zebra ZD421"}, true},
		{printers.Connection{Type: printers.ConnWindows, Address: "DYMO"}, false},
		{printers.Connection{Type: printers.ConnDevice, Address: "/dev/usb/lp0"}, true},
		{printers.Connection{Type: printers.ConnDevice, Address: "/etc/passwd"}, false},
		// A protocol after the path is the same device.
		{printers.Connection{Type: printers.ConnDevice, Address: "/dev/usb/lp0?protocol=niimbot"}, true},
		{printers.Connection{Type: printers.ConnDevice, Address: "/etc/passwd?protocol=niimbot"}, false},
		{printers.Connection{Type: printers.ConnBLE, Address: "7C:2F:80:A1:B2:C3"}, true},
		{printers.Connection{Type: printers.ConnBLE, Address: "7C:2F:80:A1:B2:C3?service=0000fff0-0000-1000-8000-00805f9b34fb"}, true},
		{printers.Connection{Type: printers.ConnBLE, Address: "00:11:22:33:44:55"}, false},
	} {
		if err := a.Allows(c.conn); (err == nil) != c.ok {
			t.Errorf("%+v: %v, want allowed %v", c.conn, err, c.ok)
		}
	}
	if s := a.Summary(); len(s) != 8 || s[6] != "device /dev/usb/lp*" || s[7] != "bluetooth 7C:2F:80:*" {
		t.Fatalf("summary: %q", s)
	}

	// Empty reaches nothing.
	none, _ := ParseAllowlist(nil, nil, nil)
	if err := none.Allows(printers.Connection{Type: printers.ConnTCP, Address: "192.168.1.40:9100"}); err == nil {
		t.Fatal("an empty allowlist allowed a printer")
	}
	if err := none.Allows(printers.Connection{Type: printers.ConnBLE, Address: "7C:2F:80:A1:B2:C3"}); err == nil {
		t.Fatal("an empty allowlist allowed a Bluetooth printer")
	}
	// A Bluetooth address belongs under [printers] bluetooth, not [relay]
	// allow, where it would read as an IPv6 host.
	for _, bad := range []string{"ftp://x/y", "/dev/usb/lp0", "printer.lan:http", "10.0.0.0/24:70000", "windows:", "[", "7C:2F:80:A1:B2:C3", "7C:2F:80:*:*:*"} {
		if _, err := ParseAllowlist([]string{bad}, nil, nil); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
	if _, err := ParseAllowlist(nil, []string{"dev/lp0"}, nil); err == nil {
		t.Error("a relative device pattern was taken")
	}
	if _, err := ParseAllowlist(nil, nil, []string{"printer.lan"}); err == nil {
		t.Error("a host name was taken as a Bluetooth pattern")
	}
}

func TestAllowlistReachesOpenEPaperLinkAccessPointsByTheirURL(t *testing.T) {
	a, err := ParseAllowlist([]string{"http://192.168.1.50", "http://*.ap.lan/"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		address string
		ok      bool
	}{
		{"192.168.1.50?tag=0000021E3D6F1234", true},
		{"http://192.168.1.50?tag=0000021E3D6F1234", true},
		{"http://192.168.1.51?tag=0000021E3D6F1234", false},
		{"https://192.168.1.50?tag=0000021E3D6F1234", false},
		{"http://shelf.ap.lan?tag=0197E5CB3B38", true},
		{"http://shelf.ap.lan:8080?tag=0197E5CB3B38", false},
		{"http://ap.example?tag=0197E5CB3B38", false},
		{"not an address", false},
	} {
		conn := printers.Connection{Type: printers.ConnOpenEPaperLink, Address: c.address}
		if n, err := printers.NormalizeConnection(conn); err == nil {
			conn = n
		}
		if err := a.Allows(conn); (err == nil) != c.ok {
			t.Errorf("%s: %v, want allowed %v", c.address, err, c.ok)
		}
	}
}

func TestWildcard(t *testing.T) {
	for _, c := range []struct {
		pattern, s string
		ok         bool
	}{
		{"a*", "abc", true}, {"*c", "abc", true}, {"a*c", "abc", true}, {"a*c", "ab", false},
		{"ipp://h/*", "ipp://h/p/q", true}, {"abc", "abc", true}, {"abc", "abd", false}, {"a*a", "a", false},
	} {
		if wildcard(c.pattern, c.s) != c.ok {
			t.Errorf("wildcard(%q, %q) = %v", c.pattern, c.s, !c.ok)
		}
	}
}
