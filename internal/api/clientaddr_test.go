package api

import (
	"net/http/httptest"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

func TestClientAddressTrustsHeadersOnlyFromTrustedProxies(t *testing.T) {
	trusted, err := config.ParseProxies([]string{"10.0.0.0/8", "2001:db8::1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, remote, xff, forwarded, want string
	}{
		{"direct, no headers", "203.0.113.5:4000", "", "", "203.0.113.5"},
		{"direct, forged header ignored", "203.0.113.5:4000", "198.51.100.1", "", "203.0.113.5"},
		{"proxied", "10.0.0.2:80", "198.51.100.1", "", "198.51.100.1"},
		{"proxied, the client's own claims ignored", "10.0.0.2:80", "1.1.1.1, 198.51.100.1", "", "198.51.100.1"},
		{"two proxies", "10.0.0.2:80", "198.51.100.1, 10.0.0.3", "", "198.51.100.1"},
		{"port in the header", "10.0.0.2:80", "198.51.100.1:5555", "", "198.51.100.1"},
		{"IPv6 proxy and client", "[2001:db8::1]:443", "[2001:db8::99]:1", "", "2001:db8::99"},
		{"IPv4-mapped peer", "[::ffff:203.0.113.5]:1", "", "", "203.0.113.5"},
		{"Forwarded header", "10.0.0.2:80", "", `for=198.51.100.7;proto=https, for="10.0.0.4"`, "198.51.100.7"},
		{"Forwarded IPv6 in brackets", "10.0.0.2:80", "", `for="[2001:db8::7]:80"`, "2001:db8::7"},
		{"proxied, no header", "10.0.0.2:80", "", "", "10.0.0.2"},
		{"garbage stops the walk at the last proxy", "10.0.0.2:80", "198.51.100.1, unknown", "", "10.0.0.2"},
		{"only proxies", "10.0.0.2:80", "10.0.0.5", "", "10.0.0.5"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.forwarded != "" {
			r.Header.Set("Forwarded", c.forwarded)
		}
		if got := clientAddress(r, trusted).String(); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestParseProxiesRefusesWhatIsNotAnAddress(t *testing.T) {
	if _, err := config.ParseProxies([]string{"proxy.example.com"}); err == nil {
		t.Error("a host name was accepted")
	}
	if _, err := config.ParseProxies([]string{" 192.0.2.1 ", "192.0.2.0/24"}); err != nil {
		t.Errorf("an address and a range: %v", err)
	}
}
