package email

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
)

// fakeSMTP is an SMTP server for tests: it offers STARTTLS (unless
// noSTARTTLS) or speaks TLS from the start (implicit), takes AUTH PLAIN,
// answers RCPT with rcptCode, and keeps what it was sent.
type fakeSMTP struct {
	cert       tls.Certificate
	caPEM      string
	implicit   bool
	noSTARTTLS bool
	rcptCode   string

	mu       sync.Mutex
	from     string
	rcpts    []string
	data     string
	authed   bool
	usedTLS  bool
	listener net.Listener
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mail.test"},
		DNSNames: []string{"mail.test"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, IsCA: true, BasicConstraintsValid: true,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSMTP{
		cert:     tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		caPEM:    string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		rcptCode: "250 OK",
	}
	f.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.listener.Close() })
	go f.serve()
	return f
}

// set changes the server's settings for the connections that follow.
func (f *fakeSMTP) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeSMTP) serve() {
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeSMTP) handle(conn net.Conn) {
	defer conn.Close()
	// The settings as they are when the connection opens; tests change
	// them between connections, under the lock (set).
	f.mu.Lock()
	implicit, noSTARTTLS, rcptCode := f.implicit, f.noSTARTTLS, f.rcptCode
	f.mu.Unlock()
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{f.cert}}
	if implicit {
		conn = tls.Server(conn, tlsConfig)
		f.mu.Lock()
		f.usedTLS = true
		f.mu.Unlock()
	}
	r, w := bufio.NewReader(conn), conn
	say := func(line string) { io.WriteString(w, line+"\r\n") }
	say("220 mail.test ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(cmd, " ", 2)[0])
		switch verb {
		case "EHLO", "HELO":
			io.WriteString(w, "250-mail.test\r\n")
			if !noSTARTTLS && !implicit {
				io.WriteString(w, "250-STARTTLS\r\n")
			}
			say("250 AUTH PLAIN")
		case "STARTTLS":
			say("220 go ahead")
			tc := tls.Server(conn, tlsConfig)
			if err := tc.Handshake(); err != nil {
				return
			}
			conn, r, w = tc, bufio.NewReader(tc), tc
			f.mu.Lock()
			f.usedTLS = true
			f.mu.Unlock()
		case "AUTH":
			f.mu.Lock()
			f.authed = true
			f.mu.Unlock()
			say("235 authenticated")
		case "MAIL":
			f.mu.Lock()
			f.from = cmd
			f.mu.Unlock()
			say("250 OK")
		case "RCPT":
			if !strings.HasPrefix(rcptCode, "2") {
				say(rcptCode)
				continue
			}
			f.mu.Lock()
			f.rcpts = append(f.rcpts, cmd)
			f.mu.Unlock()
			say("250 OK")
		case "DATA":
			say("354 send it")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			f.mu.Lock()
			f.data = b.String()
			f.mu.Unlock()
			say("250 queued")
		case "QUIT":
			say("221 bye")
			return
		default:
			say("250 OK")
		}
	}
}

// sender is a Sender for f, dialing it whatever host it names.
func (f *fakeSMTP) sender(mode string) *Sender {
	cfg := config.EmailConfig{Host: "mail.test", TLS: mode, Username: "bam", Password: "secret", From: "BAM! <bam@example.org>", CA: f.caPEM}
	return &Sender{Config: cfg, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, f.listener.Addr().String())
	}}
}

func TestSendGivesHTMLAndTextOverSTARTTLS(t *testing.T) {
	f := newFakeSMTP(t)
	html, text, err := Render(Content{Title: "Running low: <Screws>", Paragraphs: []string{"Down to 3."}, Link: "https://bam.example/categories/c1", Accent: "#f59e0b", Footer: "Why"})
	if err != nil {
		t.Fatal(err)
	}
	err = f.sender(config.EmailSTARTTLS).Send(context.Background(), Message{To: []string{"ada@example.org"}, Subject: "Running low: Screws\r\nBcc: evil@example.org", Text: text, HTML: html})
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.usedTLS || !f.authed {
		t.Errorf("TLS %v, authenticated %v; want both", f.usedTLS, f.authed)
	}
	if len(f.rcpts) != 1 || !strings.Contains(f.rcpts[0], "<ada@example.org>") || !strings.Contains(f.from, "<bam@example.org>") {
		t.Errorf("from %q to %v", f.from, f.rcpts)
	}
	msg, err := mail.ReadMessage(strings.NewReader(f.data))
	if err != nil {
		t.Fatal(err)
	}
	if got := msg.Header.Get("To"); got != "ada@example.org" {
		t.Errorf("To %q", got)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Error("a line break in the subject added a header")
	}
	subject, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if subject != "Running low: Screws  Bcc: evil@example.org" {
		t.Errorf("subject %q", subject)
	}
	if msg.Header.Get("Auto-Submitted") != "auto-generated" || msg.Header.Get("Message-ID") == "" {
		t.Errorf("headers %v", msg.Header)
	}
	_, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(quotedprintable.NewReader(p))
		kind, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		parts[kind] = string(body)
	}
	if !strings.Contains(parts["text/plain"], "Down to 3.") || !strings.Contains(parts["text/plain"], "Open in BAM!: https://bam.example/categories/c1") {
		t.Errorf("text part %q", parts["text/plain"])
	}
	if !strings.Contains(parts["text/html"], "Running low: &lt;Screws&gt;") || !strings.Contains(parts["text/html"], `href="https://bam.example/categories/c1"`) ||
		!strings.Contains(parts["text/html"], "#f59e0b") {
		t.Errorf("html part %q", parts["text/html"])
	}
}

func TestSeveralRecipientsDoNotSeeEachOther(t *testing.T) {
	f := newFakeSMTP(t)
	if err := f.sender(config.EmailImplicitTLS).Send(context.Background(), Message{To: []string{"a@example.org", "b@example.org"}, Subject: "Hi", Text: "Hello"}); err == nil {
		t.Fatal("implicit TLS to a server expecting STARTTLS worked")
	}
	f.set(func() { f.implicit = true })
	if err := f.sender(config.EmailImplicitTLS).Send(context.Background(), Message{To: []string{"a@example.org", "b@example.org"}, Subject: "Hi", Text: "Hello"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	msg, err := mail.ReadMessage(strings.NewReader(f.data))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.rcpts) != 2 || msg.Header.Get("To") != "undisclosed-recipients:;" {
		t.Errorf("rcpts %v, To %q", f.rcpts, msg.Header.Get("To"))
	}
	if !strings.HasPrefix(msg.Header.Get("Content-Type"), "text/plain") {
		t.Errorf("a text-only message is %q", msg.Header.Get("Content-Type"))
	}
}

func TestSMTPRepliesDecideWhetherToRetry(t *testing.T) {
	f := newFakeSMTP(t)
	send := func() error {
		return f.sender(config.EmailSTARTTLS).Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "Hi", Text: "Hello"})
	}
	f.set(func() { f.rcptCode = "451 try again later" })
	if err := send(); !errors.Is(err, ErrTemporary) {
		t.Errorf("4xx: %v, want temporary", err)
	}
	f.set(func() { f.rcptCode = "550 no such mailbox" })
	if err := send(); err == nil || errors.Is(err, ErrTemporary) {
		t.Errorf("5xx: %v, want a permanent failure", err)
	}
	f.listener.Close()
	if err := send(); !errors.Is(err, ErrTemporary) {
		t.Errorf("server gone: %v, want temporary", err)
	}
}

func TestSTARTTLSIsRequiredUnlessTurnedOff(t *testing.T) {
	f := newFakeSMTP(t)
	f.set(func() { f.noSTARTTLS = true })
	err := f.sender(config.EmailSTARTTLS).Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "Hi", Text: "Hello"})
	if err == nil || !strings.Contains(err.Error(), "does not offer STARTTLS") {
		t.Errorf("no STARTTLS offered: %v", err)
	}
	// A server whose certificate is not trusted is refused too.
	f.set(func() { f.noSTARTTLS = false })
	s := f.sender(config.EmailSTARTTLS)
	s.Config.CA = ""
	if err := s.Send(context.Background(), Message{To: []string{"a@example.org"}, Subject: "Hi", Text: "Hello"}); err == nil {
		t.Error("an untrusted certificate was accepted")
	}
}

func TestSendRefusesWhatCannotBeSent(t *testing.T) {
	if err := (*Sender)(nil).Send(context.Background(), Message{To: []string{"a@example.org"}}); !errors.Is(err, ErrNotSetUp) {
		t.Errorf("no sender: %v", err)
	}
	if New(config.EmailConfig{}) != nil {
		t.Error("a sender without a host")
	}
	s := &Sender{Config: config.EmailConfig{Host: "mail.test", From: "bam@example.org"}}
	if err := s.Send(context.Background(), Message{To: []string{"not an address"}}); err == nil {
		t.Error("a bad address was accepted")
	}
}

func TestRenderEscapesAndDefaults(t *testing.T) {
	html, text, err := Render(Content{Title: `<script>alert(1)</script>`, Accent: "red; background:url(x)", Link: "javascript:alert(1)"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>") || strings.Contains(html, "url(x)") || strings.Contains(html, "javascript:") {
		t.Errorf("unsafe HTML: %s", html)
	}
	if !strings.Contains(html, "#6b7280") || !strings.Contains(text, "<script>alert(1)</script>") {
		t.Errorf("defaults: %s / %s", html, text)
	}
}
