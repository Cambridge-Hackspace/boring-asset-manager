package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientAddresses sets each request's RemoteAddr to its client's address
// (#246). A request from a trusted proxy is from the rightmost address in
// its X-Forwarded-For, or failing that its Forwarded header, that is not
// itself a trusted proxy: the addresses before it are whatever the client
// chose to send. A request from anywhere else is from its own address, and
// its headers are ignored. Everything after, from sign-in throttling to the
// live connections list, then sees the same address.
func ClientAddresses(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if client := clientAddress(r, trusted); client.IsValid() {
				r.RemoteAddr = netip.AddrPortFrom(client, 0).String()
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientAddress is r's client, as ClientAddresses describes; the zero
// address when RemoteAddr is not an address, as in some tests.
func clientAddress(r *http.Request, trusted []netip.Prefix) netip.Addr {
	peer := peerAddress(r.RemoteAddr)
	if !peer.IsValid() || !isTrusted(peer, trusted) {
		return peer
	}
	hops := forwardedFor(r.Header.Values("X-Forwarded-For"))
	if len(hops) == 0 {
		hops = forwardedHeader(r.Header.Values("Forwarded"))
	}
	for i := len(hops) - 1; i >= 0; i-- {
		addr := hops[i]
		if !addr.IsValid() {
			// Something unreadable stands between: nothing before it can
			// be trusted, and the last proxy is the client as far as is known.
			return peer
		}
		if !isTrusted(addr, trusted) {
			return addr
		}
		peer = addr
	}
	return peer
}

// ClientAddress is the address of the client a request came from, without
// its port: what ClientAddresses set.
func ClientAddress(r *http.Request) string {
	if a := peerAddress(r.RemoteAddr); a.IsValid() {
		return a.String()
	}
	return ""
}

func peerAddress(remote string) netip.Addr {
	if ap, err := netip.ParseAddrPort(remote); err == nil {
		return ap.Addr().Unmap()
	}
	if a, err := netip.ParseAddr(remote); err == nil {
		return a.Unmap()
	}
	return netip.Addr{}
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// forwardedFor reads X-Forwarded-For headers, in order: each a
// comma-separated list of addresses, possibly with ports. An entry that is
// not an address reads as the zero address.
func forwardedFor(values []string) []netip.Addr {
	var out []netip.Addr
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, hopAddress(part))
			}
		}
	}
	return out
}

// forwardedHeader reads the for= of each element of Forwarded headers
// (RFC 7239), in order.
func forwardedHeader(values []string) []netip.Addr {
	var out []netip.Addr
	for _, v := range values {
		for _, element := range strings.Split(v, ",") {
			for _, pair := range strings.Split(element, ";") {
				name, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
				if ok && strings.EqualFold(name, "for") {
					out = append(out, hopAddress(strings.Trim(value, `"`)))
				}
			}
		}
	}
	return out
}

// hopAddress reads one hop: an address, an address and port, or an IPv6
// address in brackets with or without a port.
func hopAddress(s string) netip.Addr {
	if a := peerAddress(s); a.IsValid() {
		return a
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		return peerAddress(host)
	}
	return peerAddress(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"))
}
