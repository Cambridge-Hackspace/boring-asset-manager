package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// Webhook posts events as signed JSON to any URL (docs/notifications.md).
// A URL that resolves to a private network is refused unless the channel
// allows it, and the request goes to the very address that was checked,
// so DNS cannot point it elsewhere in between.
type Webhook struct {
	// Resolve and Dial are replaced by tests.
	Resolve func(ctx context.Context, host string) ([]netip.Addr, error)
	Dial    func(ctx context.Context, network, address string) (net.Conn, error)
	// Now stamps BAM-Timestamp; tests replace it.
	Now func() time.Time
}

// WebhookVersion is the version of the standard body.
const WebhookVersion = 1

func (Webhook) Info() DriverInfo {
	return DriverInfo{
		Driver: "webhook",
		Label:  "Webhook",
		Options: []OptionInfo{
			{Name: "url", Label: "URL", Required: true,
				Description: "Where to post each event: https://, or http:// to send without encryption."},
			{Name: "secret", Label: "Signing secret", Secret: true,
				Description: "Signs each request in BAM-Signature, for the receiver to check it came from BAM!. Left empty, requests are not signed."},
			{Name: "headers", Label: "Extra headers", Multiline: true, Secret: true,
				Description: "Name: value, one per line, such as an Authorization the receiver asks for."},
			{Name: "body", Label: "Body template", Multiline: true,
				Description: "Leave empty for BAM!'s own JSON. Otherwise a Go text template of the same data, such as {\"text\": {{json .event.kind}}}."},
			{Name: "content_type", Label: "Content type",
				Description: "application/json unless given; a body template must then make valid JSON."},
			{Name: "allow_private_networks", Label: "Allow private networks",
				Description: "true to post to an address on a private network, such as 192.168.1.20, or this machine. Off, they are refused, so the server cannot be pointed at what is behind it."},
		},
	}
}

// webhookSettings are a channel's options, checked.
type webhookSettings struct {
	url         *url.URL
	secret      string
	headers     http.Header
	body        *template.Template
	contentType string
	private     bool
}

// reserved are headers a channel may not set itself.
var reserved = map[string]bool{
	"Host": true, "Content-Length": true, "Content-Type": true, "Transfer-Encoding": true, "Connection": true,
	"Bam-Event": true, "Bam-Delivery": true, "Bam-Timestamp": true, "Bam-Signature": true, "Bam-Display": true,
}

func webhookSettingsOf(options map[string]string) (*webhookSettings, error) {
	u, err := url.Parse(strings.TrimSpace(options["url"]))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("the URL must be https://host/… or http://host/…, without a user name")
	}
	s := &webhookSettings{url: u, secret: options["secret"], headers: http.Header{}, contentType: strings.TrimSpace(options["content_type"])}
	switch strings.ToLower(strings.TrimSpace(options["allow_private_networks"])) {
	case "", "false":
	case "true":
		s.private = true
	default:
		return nil, errors.New("allow private networks is true or false")
	}
	for _, line := range strings.Split(options["headers"], "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		name, value = http.CanonicalHeaderKey(strings.TrimSpace(name)), strings.TrimSpace(value)
		if !ok || !validHeaderName(name) || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("extra headers are Name: value, one per line, not %q", line)
		}
		if reserved[name] {
			return nil, fmt.Errorf("the %s header is BAM!'s to set", name)
		}
		s.headers.Add(name, value)
	}
	if text := options["body"]; strings.TrimSpace(text) != "" {
		t, err := template.New("body").Funcs(template.FuncMap{"json": jsonText}).Parse(text)
		if err != nil {
			return nil, fmt.Errorf("the body template: %v", err)
		}
		s.body = t
	}
	if s.contentType != "" && strings.ContainsAny(s.contentType, "\r\n") {
		return nil, errors.New("the content type must be one line")
	}
	return s, nil
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

// jsonText writes a value as JSON, for templates.
func jsonText(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func (w Webhook) Validate(options map[string]string) error {
	// A body template is parsed here; one that fails on an event fails
	// that delivery, saying why, as the test button shows.
	s, err := webhookSettingsOf(options)
	if err != nil {
		return err
	}
	// An address written as such is refused at once; a name is checked as
	// each request resolves it.
	if a, err := netip.ParseAddr(strings.Trim(s.url.Hostname(), "[]")); err == nil && !s.private && PrivateAddress(a) {
		return errors.New("the URL is on a private network; allow private networks to post to it")
	}
	return nil
}

// render makes a request's body and content type.
func (s *webhookSettings) render(n Notification) ([]byte, string, error) {
	data := map[string]any{"version": WebhookVersion, "id": n.DeliveryID, "event": outbound(n), "instance": strings.TrimRight(n.BaseURL, "/")}
	if s.body == nil {
		b, err := json.Marshal(data)
		return b, "application/json", err
	}
	// The template sees the same data as plain JSON values.
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, "", err
	}
	var plain map[string]any
	if err := json.Unmarshal(raw, &plain); err != nil {
		return nil, "", err
	}
	var b bytes.Buffer
	if err := s.body.Execute(&b, plain); err != nil {
		return nil, "", fmt.Errorf("the body template: %v", err)
	}
	contentType := s.contentType
	if contentType == "" {
		contentType = "application/json"
		if !json.Valid(b.Bytes()) {
			return nil, "", errors.New("the body template does not make valid JSON; give a content type to send something else")
		}
	}
	return b.Bytes(), contentType, nil
}

// Signature is what BAM-Signature carries for a body sent at timestamp:
// v1= and the hex HMAC-SHA256, keyed by the secret, of "<timestamp>.<body>".
func Signature(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func (w Webhook) Send(ctx context.Context, options map[string]string, n Notification) error {
	s, err := webhookSettingsOf(options)
	if err != nil {
		return err
	}
	body, contentType, err := s.render(n)
	if err != nil {
		return err
	}
	kind := n.Event.Kind
	if n.Test {
		kind = "test"
	}
	return w.post(ctx, s, body, contentType, map[string]string{"BAM-Event": kind, "BAM-Delivery": n.DeliveryID}, n.Report)
}

// DisplayImageEvent is what BAM-Event says of a display's image posted
// through a webhook (docs/printing.md#webhook).
const DisplayImageEvent = "display.image"

// SendImage posts a display's image through a webhook channel: the image
// itself as the body, with its media type, signed as events are, BAM-Event
// display.image and BAM-Display the display's ID. A body template is for
// events, and is not used.
func (w Webhook) SendImage(ctx context.Context, options map[string]string, img []byte, contentType, displayID, deliveryID string) error {
	s, err := webhookSettingsOf(options)
	if err != nil {
		return err
	}
	return w.post(ctx, s, img, contentType, map[string]string{"BAM-Event": DisplayImageEvent, "BAM-Delivery": deliveryID, "BAM-Display": displayID}, nil)
}

// post sends a body to the channel's URL with BAM!'s headers, signed when
// the channel has a secret.
func (w Webhook) post(ctx context.Context, s *webhookSettings, body []byte, contentType string, bam map[string]string, report func(int, string)) error {
	addrs, err := w.resolve(ctx, s)
	if err != nil {
		return err
	}
	port := s.url.Port()
	if port == "" {
		port = "443"
		if s.url.Scheme == "http" {
			port = "80"
		}
	}
	dial := w.Dial
	if dial == nil {
		dial = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	transport := &http.Transport{
		// The addresses checked, whatever DNS says by now.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var last error
			for _, a := range addrs {
				conn, err := dial(ctx, network, net.JoinHostPort(a.String(), port))
				if err == nil {
					return conn, nil
				}
				last = err
			}
			return nil, last
		},
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		Proxy:                 nil,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport,
		// A redirect is not followed: it could lead anywhere.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	for name, values := range s.headers {
		req.Header[name] = values
	}
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	ts := strconv.FormatInt(now().Unix(), 10)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "BAM!")
	for name, value := range bam {
		req.Header.Set(name, value)
	}
	req.Header.Set("BAM-Timestamp", ts)
	if s.secret != "" {
		req.Header.Set("BAM-Signature", Signature(s.secret, ts, body))
	}
	resp, err := client.Do(req)
	if err != nil {
		// Never report the URL: its query may hold a token.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return fmt.Errorf("%w: posting to %s: %v", ErrTemporary, s.url.Hostname(), err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, responseLimit))
	if report != nil {
		report(resp.StatusCode, string(answer))
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return fmt.Errorf("HTTP %d: the URL redirects, which is not followed; give the URL it leads to", resp.StatusCode)
	}
	return HTTPError(resp, string(answer[:min(len(answer), 300)]))
}

// resolve finds the URL's addresses, refusing private ones unless the
// channel allows them.
func (w Webhook) resolve(ctx context.Context, s *webhookSettings) ([]netip.Addr, error) {
	host := strings.Trim(s.url.Hostname(), "[]")
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		addrs = []netip.Addr{a}
	} else {
		resolve := w.Resolve
		if resolve == nil {
			resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
				return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			}
		}
		found, err := resolve(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("%w: looking up %s: %v", ErrTemporary, host, err)
		}
		addrs = found
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: %s has no address", ErrTemporary, host)
	}
	if !s.private {
		for _, a := range addrs {
			if PrivateAddress(a) {
				return nil, fmt.Errorf("%s resolves to %s, on a private network; allow private networks to post to it", host, a.Unmap())
			}
		}
	}
	return addrs, nil
}

// privateRanges are the networks a webhook may not reach unless allowed:
// shared address space (CGNAT), benchmarking, and the IETF's own, beside
// what netip recognizes.
var privateRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
}

// PrivateAddress reports whether a is loopback, private, link-local (the
// cloud metadata address among them), multicast, unspecified, or in
// another range not on the public internet, IPv4-mapped forms included.
func PrivateAddress(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsInterfaceLocalMulticast() || a.IsMulticast() || a.IsUnspecified() {
		return true
	}
	for _, p := range privateRanges {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
