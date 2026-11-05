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
	}, []string{"/dev/usb/lp*"})
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
	} {
		if err := a.Allows(c.conn); (err == nil) != c.ok {
			t.Errorf("%+v: %v, want allowed %v", c.conn, err, c.ok)
		}
	}
	if s := a.Summary(); len(s) != 7 || s[6] != "device /dev/usb/lp*" {
		t.Fatalf("summary: %q", s)
	}

	// Empty reaches nothing.
	none, _ := ParseAllowlist(nil, nil)
	if err := none.Allows(printers.Connection{Type: printers.ConnTCP, Address: "192.168.1.40:9100"}); err == nil {
		t.Fatal("an empty allowlist allowed a printer")
	}
	for _, bad := range []string{"ftp://x/y", "/dev/usb/lp0", "printer.lan:http", "10.0.0.0/24:70000", "windows:", "["} {
		if _, err := ParseAllowlist([]string{bad}, nil); err == nil {
			t.Errorf("%q was taken", bad)
		}
	}
	if _, err := ParseAllowlist(nil, []string{"dev/lp0"}); err == nil {
		t.Error("a relative device pattern was taken")
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
