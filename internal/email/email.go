// Package email sends email through an SMTP server (docs/notifications.md):
// each message HTML built from templates, with a plain-text alternative.
// No provider's API is used, so any server that speaks SMTP will do.
package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

// ErrNotSetUp is returned when the configuration has no [email] server.
var ErrNotSetUp = errors.New("email is not set up: add an [email] server to config.toml (docs/configuration.md)")

// ErrTemporary is wrapped by failures worth retrying: the server could not
// be reached, or answered with a 4xx reply.
var ErrTemporary = errors.New("temporary email failure")

// Message is one email. Text is its plain-text part; HTML, when set, its
// HTML part, sent as an alternative.
type Message struct {
	To      []string
	Subject string
	Text    string
	HTML    string
}

// Sender sends messages through the configured server.
type Sender struct {
	Config config.EmailConfig
	// Dial connects to the server; tests replace it. It defaults to a
	// net.Dialer bounded by the context.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// Now stamps the Date header; tests replace it.
	Now func() time.Time
}

// New returns a Sender for cfg, or nil when email is not set up.
func New(cfg config.EmailConfig) *Sender {
	if !cfg.Enabled() {
		return nil
	}
	return &Sender{Config: cfg}
}

// Send delivers m to every address in m.To in one SMTP transaction. A
// reply of 4xx, or a server out of reach, wraps ErrTemporary; a 5xx reply,
// such as an address refused, fails for good.
func (s *Sender) Send(ctx context.Context, m Message) error {
	if s == nil || !s.Config.Enabled() {
		return ErrNotSetUp
	}
	if len(m.To) == 0 {
		return errors.New("no one to send to")
	}
	from, err := mail.ParseAddress(s.Config.From)
	if err != nil {
		return fmt.Errorf("email.from: %w", err)
	}
	to := make([]string, 0, len(m.To))
	for _, a := range m.To {
		addr, err := mail.ParseAddress(a)
		if err != nil {
			return fmt.Errorf("%q is not an email address", a)
		}
		to = append(to, addr.Address)
	}
	body, err := s.compose(from, to, m)
	if err != nil {
		return err
	}
	return classify(s.transact(ctx, from.Address, to, body))
}

// transact runs one SMTP conversation.
func (s *Sender) transact(ctx context.Context, from string, to []string, body []byte) error {
	dial := s.Dial
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	address := s.Config.Address()
	conn, err := dial(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("%w: connecting to %s: %v", ErrTemporary, address, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	tlsConfig, err := s.tlsConfig()
	if err != nil {
		conn.Close()
		return err
	}
	if s.Config.TLS == config.EmailImplicitTLS {
		conn = tls.Client(conn, tlsConfig)
	}
	c, err := smtp.NewClient(conn, s.Config.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Hello(helloName()); err != nil {
		return err
	}
	if s.Config.TLS == config.EmailSTARTTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s does not offer STARTTLS; set email.tls to \"implicit\" or, to send in the clear, \"none\"", address)
		}
		if err := c.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if s.Config.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Config.Username, s.Config.Password, s.Config.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func (s *Sender) tlsConfig() (*tls.Config, error) {
	cfg := &tls.Config{ServerName: s.Config.Host, MinVersion: tls.VersionTLS12}
	if s.Config.CA != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(s.Config.CA)) {
			return nil, errors.New("email.ca holds no certificate")
		}
		cfg.RootCAs = pool
	}
	return cfg, nil
}

// helloName is how BAM! names itself to the server: this machine's name,
// which some servers check, or localhost when it has none fit to send.
func helloName() string {
	name, err := os.Hostname()
	if err != nil || name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		return "localhost"
	}
	return name
}

// classify marks a failure temporary when SMTP says so with a 4xx reply,
// or when the conversation broke off; a 5xx reply is permanent.
func classify(err error) error {
	if err == nil || errors.Is(err, ErrTemporary) {
		return err
	}
	var reply *textproto.Error
	if errors.As(err, &reply) {
		if reply.Code >= 400 && reply.Code < 500 {
			return fmt.Errorf("%w: %v", ErrTemporary, err)
		}
		return err
	}
	var nerr net.Error
	if errors.As(err, &nerr) || errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("%w: %v", ErrTemporary, err)
	}
	return err
}

// compose writes the message: headers, then a multipart/alternative body
// of text and HTML, or the text alone.
func (s *Sender) compose(from *mail.Address, to []string, m Message) ([]byte, error) {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	var b bytes.Buffer
	header := func(name, value string) {
		fmt.Fprintf(&b, "%s: %s\r\n", name, oneLine(value))
	}
	header("From", from.String())
	// Several recipients are sent one message, and do not see each other's
	// addresses.
	if len(to) == 1 {
		header("To", to[0])
	} else {
		header("To", "undisclosed-recipients:;")
	}
	if s.Config.ReplyTo != "" {
		if rt, err := mail.ParseAddress(s.Config.ReplyTo); err == nil {
			header("Reply-To", rt.String())
		}
	}
	header("Subject", mime.QEncoding.Encode("utf-8", oneLine(m.Subject)))
	header("Date", now().Format(time.RFC1123Z))
	id, err := messageID(from.Address)
	if err != nil {
		return nil, err
	}
	header("Message-ID", id)
	header("MIME-Version", "1.0")
	// Automated: auto-responders should not answer it (RFC 3834).
	header("Auto-Submitted", "auto-generated")

	if m.HTML == "" {
		header("Content-Type", "text/plain; charset=utf-8")
		header("Content-Transfer-Encoding", "quoted-printable")
		b.WriteString("\r\n")
		if err := writeQP(&b, m.Text); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	boundary, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	header("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, part := range []struct{ kind, body string }{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", boundary, part.kind)
		if err := writeQP(&b, part.body); err != nil {
			return nil, err
		}
		b.WriteString("\r\n")
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}

// oneLine puts a header value on one line. Values come from people
// (subjects name things), and no line break in one may start a header of
// its own.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, s)
}

func writeQP(b *bytes.Buffer, s string) error {
	w := quotedprintable.NewWriter(b)
	if _, err := w.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n"))); err != nil {
		return err
	}
	return w.Close()
}

func messageID(from string) (string, error) {
	r, err := randomHex(16)
	if err != nil {
		return "", err
	}
	domain := "bam.invalid"
	if at := strings.LastIndex(from, "@"); at >= 0 {
		domain = from[at+1:]
	}
	return "<" + r + "@" + domain + ">", nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
