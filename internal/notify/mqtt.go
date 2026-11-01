package notify

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// MQTT publishes events to an MQTT broker (README §5.2), for those running
// one, such as Home Assistant's: each to <prefix>/<kind>/<asset or category
// ID> as JSON.
type MQTT struct{}

func (MQTT) Info() DriverInfo {
	return DriverInfo{
		Driver: "mqtt",
		Label:  "MQTT",
		Options: []OptionInfo{
			{Name: "broker", Label: "Broker", Required: true,
				Description: "mqtts://host for TLS (port 8883 unless given), or mqtt://host to send without encryption (port 1883)."},
			{Name: "topic_prefix", Label: "Topic prefix", Required: true,
				Description: "Events are published to <prefix>/<kind>/<asset or category ID>, such as bam/asset.hook/…"},
			{Name: "client_id", Label: "Client ID",
				Description: "How BAM names itself to the broker; bam unless given."},
			{Name: "username", Label: "User name", Description: "For a broker that asks for one."},
			{Name: "password", Label: "Password", Secret: true, Description: "For a broker that asks for one."},
			{Name: "ca_cert", Label: "CA certificate", Multiline: true,
				Description: "PEM, to trust a broker whose certificate no public authority signed. Without it, the system's authorities are trusted."},
			{Name: "client_cert", Label: "Client certificate", Multiline: true,
				Description: "PEM, for a broker that knows its clients by certificate; give its key too."},
			{Name: "client_key", Label: "Client key", Multiline: true, Secret: true, Description: "PEM, the client certificate's private key."},
		},
	}
}

// mqttSettings are a channel's options, checked.
type mqttSettings struct {
	dialer mqttDialer
	prefix string
}

func mqttSettingsOf(options map[string]string) (*mqttSettings, error) {
	u, err := url.Parse(strings.TrimSpace(options["broker"]))
	if err != nil || u.Host == "" || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" {
		return nil, errors.New("broker must be mqtts://host[:port] or mqtt://host[:port]")
	}
	port := u.Port()
	var tlsConfig *tls.Config
	switch u.Scheme {
	case "mqtts":
		if port == "" {
			port = "8883"
		}
		tlsConfig = &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}
	case "mqtt":
		if port == "" {
			port = "1883"
		}
	default:
		return nil, errors.New("broker must be mqtts://host[:port] or mqtt://host[:port]")
	}
	prefix := strings.Trim(strings.TrimSpace(options["topic_prefix"]), "/")
	if prefix == "" || strings.ContainsAny(prefix, "+#\x00") || !utf8.ValidString(prefix) || len(prefix) > 200 {
		return nil, errors.New("the topic prefix must be 1-200 characters without +, # or NUL")
	}
	clientID := strings.TrimSpace(options["client_id"])
	if clientID == "" {
		clientID = "bam"
	}
	if len(clientID) > 23 {
		return nil, errors.New("the client ID is at most 23 characters, as MQTT 3.1.1 brokers must take")
	}
	username, password := options["username"], options["password"]
	if password != "" && username == "" {
		return nil, errors.New("a password needs a user name")
	}
	ca, cert, key := strings.TrimSpace(options["ca_cert"]), strings.TrimSpace(options["client_cert"]), strings.TrimSpace(options["client_key"])
	if tlsConfig == nil && (ca != "" || cert != "" || key != "") {
		return nil, errors.New("certificates need TLS: use mqtts://")
	}
	if ca != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(ca)) {
			return nil, errors.New("the CA certificate is not a PEM certificate")
		}
		tlsConfig.RootCAs = pool
	}
	if (cert == "") != (key == "") {
		return nil, errors.New("give the client certificate and its key together")
	}
	if cert != "" {
		pair, err := tls.X509KeyPair([]byte(cert), []byte(key))
		if err != nil {
			return nil, fmt.Errorf("the client certificate and key do not make a pair: %v", err)
		}
		tlsConfig.Certificates = []tls.Certificate{pair}
	}
	return &mqttSettings{
		dialer: mqttDialer{Address: net.JoinHostPort(u.Hostname(), port), TLS: tlsConfig, ClientID: clientID, Username: username, Password: password},
		prefix: prefix,
	}, nil
}

func (MQTT) Validate(options map[string]string) error {
	_, err := mqttSettingsOf(options)
	return err
}

// mqttEvent is an event as it is published: people by name only, and a
// link to what it is about.
type mqttEvent struct {
	ID         string              `json:"id"`
	Kind       string              `json:"kind"`
	AssetID    *string             `json:"asset_id,omitempty"`
	CategoryID *string             `json:"category_id,omitempty"`
	URL        string              `json:"url,omitempty"`
	Payload    models.EventPayload `json:"payload"`
	CreatedAt  time.Time           `json:"created_at"`
	Test       bool                `json:"test,omitempty"`
}

// mqttMessageFor is a notification as published.
func mqttMessageFor(prefix string, n Notification) (mqttMessage, error) {
	e := n.Event
	p := e.Payload
	for _, who := range []**models.EventActor{&p.Actor, &p.Borrower} {
		if *who != nil {
			named := **who
			named.Email = ""
			*who = &named
		}
	}
	out := mqttEvent{ID: e.ID, Kind: e.Kind, AssetID: e.AssetID, CategoryID: e.CategoryID, Payload: p, CreatedAt: e.CreatedAt, Test: n.Test}
	topic := prefix + "/" + e.Kind
	base := strings.TrimRight(n.BaseURL, "/")
	switch {
	case n.Test:
		topic = prefix + "/test"
	case e.AssetID != nil:
		topic += "/" + *e.AssetID
		out.URL = base + "/assets/" + *e.AssetID
	case e.CategoryID != nil:
		topic += "/" + *e.CategoryID
		out.URL = base + "/categories/" + *e.CategoryID
	}
	data, err := json.Marshal(out)
	if err != nil {
		return mqttMessage{}, err
	}
	return mqttMessage{Topic: topic, Payload: data}, nil
}

func (MQTT) Send(ctx context.Context, options map[string]string, n Notification) error {
	s, err := mqttSettingsOf(options)
	if err != nil {
		return err
	}
	msg, err := mqttMessageFor(s.prefix, n)
	if err != nil {
		return err
	}
	return s.dialer.publish(ctx, msg)
}
