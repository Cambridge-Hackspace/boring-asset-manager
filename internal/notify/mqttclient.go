package notify

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// A publisher for MQTT 3.1.1 (OASIS standard, 2014), as much of it as
// sending notifications needs: connect, with a user name and password if
// given, publish at QoS 1 and wait for the broker's acknowledgment, and
// disconnect. Each delivery makes its own short connection, so it never
// needs to keep one alive.

// MQTT control packet types, in the high nibble of the first byte.
const (
	mqttConnect    = 1
	mqttConnack    = 2
	mqttPublish    = 3
	mqttPuback     = 4
	mqttDisconnect = 14
)

// mqttRefusals are CONNACK's return codes that will not change by trying
// again; codes 2 (identifier rejected) and 3 (server unavailable) may.
var mqttRefusals = map[byte]string{
	1: "the broker does not speak MQTT 3.1.1",
	4: "the broker refused the user name or password",
	5: "the broker refused to let this client connect",
}

// mqttMessage is one publication.
type mqttMessage struct {
	Topic   string
	Payload []byte
}

// mqttDialer reaches a broker, over TLS when TLS is set, as ClientID.
type mqttDialer struct {
	Address   string
	TLS       *tls.Config
	ClientID  string
	Username  string
	Password  string
	keepAlive uint16
}

// publish connects, publishes msg at QoS 1, waits for its acknowledgment,
// and disconnects. Network failures wrap ErrTemporary; refusals by the
// broker do not.
func (d mqttDialer) publish(ctx context.Context, msg mqttMessage) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", d.Address)
	if err != nil {
		return fmt.Errorf("%w: reaching the broker: %v", ErrTemporary, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	if d.TLS != nil {
		tc := tls.Client(conn, d.TLS)
		if err := tc.HandshakeContext(ctx); err != nil {
			var verify *tls.CertificateVerificationError
			if errors.As(err, &verify) {
				return fmt.Errorf("the broker's certificate is not trusted: %v", err)
			}
			return fmt.Errorf("%w: TLS with the broker: %v", ErrTemporary, err)
		}
		conn = tc
	}
	r := bufio.NewReader(conn)

	if _, err := conn.Write(d.connectPacket()); err != nil {
		return fmt.Errorf("%w: sending CONNECT: %v", ErrTemporary, err)
	}
	kind, body, err := readMQTTPacket(r)
	if err != nil {
		return fmt.Errorf("%w: reading CONNACK: %v", ErrTemporary, err)
	}
	if kind != mqttConnack || len(body) != 2 {
		return fmt.Errorf("the broker answered CONNECT with packet type %d", kind)
	}
	if code := body[1]; code != 0 {
		if why, permanent := mqttRefusals[code]; permanent {
			return errors.New(why)
		}
		return fmt.Errorf("%w: the broker refused to connect (code %d)", ErrTemporary, code)
	}

	const packetID = 1
	if _, err := conn.Write(publishPacket(msg, packetID)); err != nil {
		return fmt.Errorf("%w: sending PUBLISH: %v", ErrTemporary, err)
	}
	kind, body, err = readMQTTPacket(r)
	if err != nil {
		return fmt.Errorf("%w: waiting for PUBACK: %v", ErrTemporary, err)
	}
	if kind != mqttPuback || len(body) != 2 || binary.BigEndian.Uint16(body) != packetID {
		return fmt.Errorf("%w: the broker answered PUBLISH with packet type %d", ErrTemporary, kind)
	}
	_, _ = conn.Write([]byte{mqttDisconnect << 4, 0})
	return nil
}

// connectPacket is CONNECT: protocol MQTT level 4, a clean session, and the
// user name and password when given.
func (d mqttDialer) connectPacket() []byte {
	var flags byte = 0x02 // clean session
	body := mqttString("MQTT")
	payload := mqttString(d.ClientID)
	if d.Username != "" {
		flags |= 0x80
		payload = append(payload, mqttString(d.Username)...)
		if d.Password != "" {
			flags |= 0x40
			payload = append(payload, mqttString(d.Password)...)
		}
	}
	keepAlive := d.keepAlive
	if keepAlive == 0 {
		keepAlive = 60
	}
	body = append(body, 4, flags, byte(keepAlive>>8), byte(keepAlive))
	return mqttPacket(mqttConnect<<4, append(body, payload...))
}

// publishPacket is PUBLISH at QoS 1, not retained.
func publishPacket(m mqttMessage, packetID uint16) []byte {
	body := mqttString(m.Topic)
	body = append(body, byte(packetID>>8), byte(packetID))
	return mqttPacket(mqttPublish<<4|0x02, append(body, m.Payload...))
}

// mqttString is a length-prefixed UTF-8 string.
func mqttString(s string) []byte {
	return append([]byte{byte(len(s) >> 8), byte(len(s))}, s...)
}

// mqttPacket frames a packet: its first byte, the remaining length in up
// to four 7-bit groups, and the body.
func mqttPacket(first byte, body []byte) []byte {
	out := []byte{first}
	n := len(body)
	for {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 0x80
		}
		out = append(out, b)
		if n == 0 {
			break
		}
	}
	return append(out, body...)
}

// maxMQTTPacket bounds a packet read from the broker; it sends only short
// ones.
const maxMQTTPacket = 1 << 16

// readMQTTPacket reads one packet: its type and body.
func readMQTTPacket(r *bufio.Reader) (kind byte, body []byte, err error) {
	first, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	n, mult := 0, 1
	for i := 0; ; i++ {
		if i == 4 {
			return 0, nil, errors.New("malformed remaining length")
		}
		b, err := r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		n += int(b&0x7f) * mult
		if b&0x80 == 0 {
			break
		}
		mult *= 128
	}
	if n > maxMQTTPacket {
		return 0, nil, fmt.Errorf("a packet of %d bytes", n)
	}
	body = make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return first >> 4, body, nil
}
