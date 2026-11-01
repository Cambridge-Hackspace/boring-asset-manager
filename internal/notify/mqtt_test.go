package notify

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// fakeBroker is an MQTT 3.1.1 broker enough for tests: it checks the user
// name and password, records what is published, and acknowledges it.
type fakeBroker struct {
	ln             net.Listener
	user, password string
	mu             sync.Mutex
	got            []mqttMessage
	clients        []string
}

func newFakeBroker(t *testing.T, tlsConfig *tls.Config, user, password string) *fakeBroker {
	t.Helper()
	var ln net.Listener
	var err error
	if tlsConfig != nil {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	b := &fakeBroker{ln: ln, user: user, password: password}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go b.serve(conn)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return b
}

func (b *fakeBroker) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	kind, body, err := readMQTTPacket(r)
	if err != nil || kind != mqttConnect {
		return
	}
	// "MQTT", level, flags, keep-alive, then the payload's strings.
	str := func(p []byte) (string, []byte) {
		n := int(binary.BigEndian.Uint16(p))
		return string(p[2 : 2+n]), p[2+n:]
	}
	name, rest := str(body)
	flags := rest[1]
	clientID, rest := str(rest[4:])
	var user, password string
	if flags&0x80 != 0 {
		user, rest = str(rest)
	}
	if flags&0x40 != 0 {
		password, _ = str(rest)
	}
	code := byte(0)
	switch {
	case name != "MQTT" || body[6] != 4:
		code = 1
	case user != b.user || password != b.password:
		code = 4
	}
	b.mu.Lock()
	b.clients = append(b.clients, clientID)
	b.mu.Unlock()
	_, _ = conn.Write([]byte{mqttConnack << 4, 2, 0, code})
	if code != 0 {
		return
	}
	kind, body, err = readMQTTPacket(r)
	if err != nil || kind != mqttPublish {
		return
	}
	topic, rest := str(body)
	id := rest[:2]
	b.mu.Lock()
	b.got = append(b.got, mqttMessage{Topic: topic, Payload: append([]byte(nil), rest[2:]...)})
	b.mu.Unlock()
	_, _ = conn.Write([]byte{mqttPuback << 4, 2, id[0], id[1]})
	_, _, _ = readMQTTPacket(r) // DISCONNECT
}

func (b *fakeBroker) address() string { return b.ln.Addr().String() }

func (b *fakeBroker) messages() []mqttMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]mqttMessage(nil), b.got...)
}

// testCA makes a certificate authority, and certificates it signs.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  string
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return &testCA{cert: cert, key: key, pem: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

// issue signs a certificate for a server at 127.0.0.1, or a client.
func (ca *testCA) issue(t *testing.T, server bool) (certPEM, keyPEM string, pair tls.Certificate) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "bam"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	if server {
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	pair, err = tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	return certPEM, keyPEM, pair
}

func hookNotification() Notification {
	asset := "a1"
	return Notification{BaseURL: "https://bam.example/", Event: models.Event{
		ID: "e1", Kind: models.EventAssetHook, AssetID: &asset, CreatedAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
		Payload: models.EventPayload{
			Asset:   &models.EventAsset{ID: "a1", Name: "Drawer A3"},
			Actor:   &models.EventActor{ID: "u1", Email: "ada@hackspace.org", Name: "Ada"},
			Trigger: models.TriggerManual,
			Hook:    &models.EventHook{Key: "identify", Label: "Identify"},
		},
	}}
}

func TestMQTTPublishes(t *testing.T) {
	b := newFakeBroker(t, nil, "bam", "s3cret")
	opts := map[string]string{"broker": "mqtt://" + b.address(), "topic_prefix": "/hackspace/bam/", "username": "bam", "password": "s3cret", "client_id": "bam-test"}
	if err := (MQTT{}).Validate(opts); err != nil {
		t.Fatal(err)
	}
	if err := (MQTT{}).Send(context.Background(), opts, hookNotification()); err != nil {
		t.Fatal(err)
	}
	got := b.messages()
	if len(got) != 1 || got[0].Topic != "hackspace/bam/asset.hook/a1" {
		t.Fatalf("published: %+v", got)
	}
	var e mqttEvent
	if err := json.Unmarshal(got[0].Payload, &e); err != nil {
		t.Fatal(err)
	}
	if e.Kind != models.EventAssetHook || e.URL != "https://bam.example/assets/a1" || e.Payload.Hook.Key != "identify" || e.Payload.Actor.Name != "Ada" {
		t.Errorf("the message: %+v", e)
	}
	if strings.Contains(string(got[0].Payload), "ada@hackspace.org") {
		t.Error("an email went out")
	}
	if b.clients[0] != "bam-test" {
		t.Errorf("client ID: %v", b.clients)
	}

	// A test message has a topic of its own.
	test := hookNotification()
	test.Test = true
	if err := (MQTT{}).Send(context.Background(), opts, test); err != nil {
		t.Fatal(err)
	}
	if got := b.messages(); got[1].Topic != "hackspace/bam/test" {
		t.Errorf("the test: %+v", got[1])
	}

	// Refused credentials are final; a broker not there is worth retrying.
	opts["password"] = "wrong"
	err := (MQTT{}).Send(context.Background(), opts, hookNotification())
	if err == nil || errors.Is(err, ErrTemporary) || !strings.Contains(err.Error(), "refused the user name or password") {
		t.Errorf("wrong password: %v", err)
	}
	gone, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := gone.Addr().String()
	gone.Close()
	opts["broker"] = "mqtt://" + addr
	if err := (MQTT{}).Send(context.Background(), opts, hookNotification()); !errors.Is(err, ErrTemporary) {
		t.Errorf("no broker: %v", err)
	}
}

func TestMQTTOverTLS(t *testing.T) {
	ca := newTestCA(t)
	_, _, serverPair := ca.issue(t, true)
	clientCert, clientKey, _ := ca.issue(t, false)
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	// A broker that knows its clients by certificate.
	b := newFakeBroker(t, &tls.Config{Certificates: []tls.Certificate{serverPair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}, "", "")
	opts := map[string]string{"broker": "mqtts://" + b.address(), "topic_prefix": "bam", "ca_cert": ca.pem, "client_cert": clientCert, "client_key": clientKey}
	if err := (MQTT{}).Send(context.Background(), opts, hookNotification()); err != nil {
		t.Fatal(err)
	}
	if got := b.messages(); len(got) != 1 {
		t.Fatalf("published over TLS: %+v", got)
	}
	// Without the CA, the broker's certificate is not trusted, and trying
	// again will not help.
	delete(opts, "ca_cert")
	err := (MQTT{}).Send(context.Background(), opts, hookNotification())
	if err == nil || errors.Is(err, ErrTemporary) || !strings.Contains(err.Error(), "not trusted") {
		t.Errorf("an untrusted broker: %v", err)
	}
}

func TestMQTTOptions(t *testing.T) {
	ca := newTestCA(t)
	cert, key, _ := ca.issue(t, false)
	ok := map[string]string{"broker": "mqtts://broker.lan", "topic_prefix": "bam"}
	for name, change := range map[string]map[string]string{
		"no scheme":          {"broker": "broker.lan"},
		"http":               {"broker": "http://broker.lan"},
		"a path":             {"broker": "mqtts://broker.lan/x"},
		"credentials in it":  {"broker": "mqtts://u:p@broker.lan"},
		"a wildcard prefix":  {"topic_prefix": "bam/#"},
		"an empty prefix":    {"topic_prefix": "/"},
		"a long client ID":   {"client_id": strings.Repeat("x", 24)},
		"a password alone":   {"password": "p"},
		"a bad CA":           {"ca_cert": "not pem"},
		"a cert without key": {"client_cert": cert},
		"a mismatched key":   {"client_cert": cert, "client_key": strings.Replace(key, "A", "B", 3)},
		"certificates plain": {"broker": "mqtt://broker.lan", "ca_cert": ca.pem},
	} {
		opts := map[string]string{}
		for k, v := range ok {
			opts[k] = v
		}
		for k, v := range change {
			opts[k] = v
		}
		if err := (MQTT{}).Validate(opts); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	good := map[string]string{"broker": "mqtts://broker.lan:9883", "topic_prefix": "bam", "username": "u", "password": "p", "ca_cert": ca.pem, "client_cert": cert, "client_key": key}
	if err := (MQTT{}).Validate(good); err != nil {
		t.Errorf("a full set: %v", err)
	}
	s, _ := mqttSettingsOf(map[string]string{"broker": "mqtt://broker.lan", "topic_prefix": "bam"})
	if s.dialer.Address != "broker.lan:1883" || s.dialer.TLS != nil || s.dialer.ClientID != "bam" {
		t.Errorf("plain defaults: %+v", s.dialer)
	}
	if s, _ := mqttSettingsOf(ok); s.dialer.Address != "broker.lan:8883" || s.dialer.TLS == nil {
		t.Errorf("TLS defaults: %+v", s.dialer)
	}
	// Multiline options are said so, for the form.
	for _, o := range (MQTT{}).Info().Options {
		if (o.Name == "ca_cert" || o.Name == "client_cert" || o.Name == "client_key") != o.Multiline {
			t.Errorf("%s multiline: %v", o.Name, o.Multiline)
		}
	}
}

func TestMQTTPackets(t *testing.T) {
	// A remaining length past 127 takes two bytes.
	p := mqttPacket(mqttPublish<<4, make([]byte, 200))
	if p[1] != 0xC8 || p[2] != 0x01 || len(p) != 203 {
		t.Errorf("framing 200 bytes: % x", p[:3])
	}
	kind, body, err := readMQTTPacket(bufio.NewReader(strings.NewReader(string(p))))
	if err != nil || kind != mqttPublish || len(body) != 200 {
		t.Errorf("reading it back: %d %d %v", kind, len(body), err)
	}
	if _, _, err := readMQTTPacket(bufio.NewReader(strings.NewReader("\x30\xff\xff\xff\xff\x01"))); err == nil {
		t.Error("a five-byte length was read")
	}
	connect := mqttDialer{ClientID: "bam", Username: "u", Password: "p"}.connectPacket()
	if connect[0] != 0x10 || !strings.Contains(string(connect), "MQTT\x04\xc2\x00\x3c") {
		t.Errorf("CONNECT: % x", connect)
	}
}
