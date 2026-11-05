package relay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Version is the relay protocol's version. A message of another version is
// refused with a nak saying which this node speaks.
const Version = 1

// The types of message. Ack and nak are the protocol's own; the rest are
// what relays are for.
const (
	// TypeAck says a message arrived: re names it.
	TypeAck = "ack"
	// TypeNak says a message was refused, and why: re names it.
	TypeNak = "nak"
	// TypeJob asks a relay to deliver bytes to a local destination. It is
	// answered with a job.result.
	TypeJob = "job"
	// TypeJobResult answers a job: accepted, or failed with a reason.
	TypeJobResult = "job.result"
	// TypeStatus is a relay's status: its version, what its allowlist
	// covers, and whether its local destinations are reachable. A relay
	// sends it unasked, and answers a status sent to it with one.
	TypeStatus = "status"
	// TypeEvent is an event, in either direction.
	TypeEvent = "event"
)

const (
	// PartBytes is the most of a message's body one part carries. A part,
	// signed for BoneMesh, fits one BoneMesh segment.
	PartBytes = 16 << 10
	// MaxParts is the most parts one message is split into, so a message's
	// body is at most 16 MiB.
	MaxParts = 1024
	// MaxMessage bounds one message's whole body.
	MaxMessage = PartBytes * MaxParts
	// maxFrame bounds one message as a transport carries it: a part, its
	// envelope, and its escaping, which makes a character as many as six.
	maxFrame = 8 * PartBytes
)

// Message is one message on the bus, as JSON.
type Message struct {
	// V is the protocol's version.
	V int `json:"v"`
	// ID names the message, so it is delivered once and acknowledged.
	ID   string `json:"id"`
	Type string `json:"type"`
	From string `json:"from"`
	To   string `json:"to"`
	// Re names the message this one answers: what an ack or a nak is for,
	// or the request a reply answers.
	Re string `json:"re,omitempty"`
	// Reason says why a nak refused.
	Reason string `json:"reason,omitempty"`
	// Part is set on a part of a message split for being large: Seg
	// carries that part of its body.
	Part *Part  `json:"part,omitempty"`
	Seg  string `json:"seg,omitempty"`
	// Body is what the message carries, by its type.
	Body json.RawMessage `json:"body,omitempty"`
}

// Part says which part of which message a part is.
type Part struct {
	// Of is the whole message's ID.
	Of string `json:"of"`
	I  int    `json:"i"`
	N  int    `json:"n"`
}

// Decode reads a body into v.
func (m Message) Decode(v any) error {
	if len(m.Body) == 0 {
		return errors.New("the message has no body")
	}
	return json.Unmarshal(m.Body, v)
}

// Job asks a relay to deliver bytes to a local destination.
type Job struct {
	Destination Destination `json:"destination"`
	// Data is the bytes to deliver, as base64.
	Data []byte `json:"data"`
	// TimeoutMS bounds the delivery at the relay; 0 leaves it to the relay.
	TimeoutMS int `json:"timeout_ms,omitempty"`
}

// Destination is somewhere local to a relay, named as a printer's
// connection names it: tcp with an address, device with a path, ipp with a
// URL, or windows with a printer's name.
type Destination struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

// JobResult answers a job: accepted, or failed with a reason.
type JobResult struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// Status is a relay's status.
type Status struct {
	// Version is the relay's BAM! version.
	Version string `json:"version"`
	// Protocol is the relay protocol's version it speaks.
	Protocol int `json:"protocol"`
	// Allowlist summarizes what the relay may reach: its patterns.
	Allowlist []string `json:"allowlist"`
	// Destinations says whether each local destination it was asked about
	// is reachable.
	Destinations []Reachability `json:"destinations,omitempty"`
}

// Reachability is whether a local destination can be reached, and if not,
// why.
type Reachability struct {
	Destination Destination `json:"destination"`
	Reachable   bool        `json:"reachable"`
	Reason      string      `json:"reason,omitempty"`
}

// newID is a fresh message ID: 128 random bits as hex.
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// check says what is wrong with a message as it arrived, if anything.
func (m Message) check() error {
	switch {
	case m.ID == "" || len(m.ID) > 64:
		return errors.New("a message needs an id of at most 64 characters")
	case m.Type == "":
		return errors.New("a message needs a type")
	case m.From == "" || m.To == "":
		return errors.New("a message needs from and to")
	case (m.Type == TypeAck || m.Type == TypeNak) && m.Re == "":
		return errors.New("an ack or a nak names the message it is for in re")
	}
	if m.Part == nil && len(m.Body) > maxFrame {
		return fmt.Errorf("a message's body over %d bytes comes in parts", PartBytes)
	}
	if p := m.Part; p != nil {
		if p.Of == "" || p.N < 2 || p.N > MaxParts || p.I < 0 || p.I >= p.N {
			return fmt.Errorf("a part is one of 2 to %d parts of a message", MaxParts)
		}
		if len(m.Body) > 0 || len(m.Seg) > PartBytes {
			return fmt.Errorf("a part carries at most %d bytes in seg, and no body", PartBytes)
		}
	}
	return nil
}

// split cuts a message whose body is larger than PartBytes into parts,
// each with an ID of its own, cutting its body on UTF-8 boundaries. A
// message that fits is returned as it is.
func split(m Message) ([]Message, error) {
	if len(m.Body) <= PartBytes {
		return []Message{m}, nil
	}
	if len(m.Body) > MaxMessage {
		return nil, fmt.Errorf("a message's body is at most %d bytes", MaxMessage)
	}
	var segs []string
	body := m.Body
	for len(body) > 0 {
		end := min(PartBytes, len(body))
		for end < len(body) && end > 0 && !utf8.RuneStart(body[end]) {
			end--
		}
		segs = append(segs, string(body[:end]))
		body = body[end:]
	}
	if len(segs) > MaxParts {
		return nil, fmt.Errorf("a message's body is at most %d bytes", MaxMessage)
	}
	parts := make([]Message, len(segs))
	for i, seg := range segs {
		parts[i] = Message{V: Version, ID: newID(), Type: m.Type, From: m.From, To: m.To, Re: m.Re,
			Part: &Part{Of: m.ID, I: i, N: len(segs)}, Seg: seg}
	}
	return parts, nil
}

// partial is a message whose parts are arriving.
type partial struct {
	segs    []string
	seen    []bool
	got     int
	bytes   int
	started int64
}

// reassembler puts split messages back together. Its bounds are checked
// before anything is kept, so a peer claiming many parts costs nothing
// until it sends them.
type reassembler struct {
	partials map[string]*partial
	bytes    int
}

const (
	// maxReassembling bounds the bytes of all messages arriving in parts.
	maxReassembling = 4 * MaxMessage
	// maxPartials bounds the messages arriving in parts at once.
	maxPartials = 256
	// partialTimeoutMS is how long a message may take to arrive whole.
	partialTimeoutMS = 2 * 60 * 1000
)

func newReassembler() *reassembler {
	return &reassembler{partials: map[string]*partial{}}
}

// offer takes a part. Once the last of a message's parts arrives it
// returns the whole message, true.
func (r *reassembler) offer(m Message, nowMS int64) (Message, bool, error) {
	for of, p := range r.partials {
		if nowMS-p.started > partialTimeoutMS {
			r.bytes -= p.bytes
			delete(r.partials, of)
		}
	}
	key := m.From + "\x00" + m.Part.Of
	p := r.partials[key]
	if p == nil {
		if len(r.partials) >= maxPartials {
			return Message{}, false, errors.New("too many messages are arriving in parts")
		}
		p = &partial{segs: make([]string, m.Part.N), seen: make([]bool, m.Part.N), started: nowMS}
		r.partials[key] = p
	}
	if len(p.segs) != m.Part.N {
		return Message{}, false, errors.New("the parts of a message disagree on how many there are")
	}
	if p.seen[m.Part.I] {
		return Message{}, false, nil
	}
	if r.bytes+len(m.Seg) > maxReassembling {
		return Message{}, false, errors.New("too many bytes are arriving in parts")
	}
	p.segs[m.Part.I], p.seen[m.Part.I] = m.Seg, true
	p.got++
	p.bytes += len(m.Seg)
	r.bytes += len(m.Seg)
	if p.got < m.Part.N {
		return Message{}, false, nil
	}
	delete(r.partials, key)
	r.bytes -= p.bytes
	body := make([]byte, 0, p.bytes)
	for _, s := range p.segs {
		body = append(body, s...)
	}
	if !json.Valid(body) {
		return Message{}, false, errors.New("a message's parts do not make JSON")
	}
	return Message{V: m.V, ID: m.Part.Of, Type: m.Type, From: m.From, To: m.To, Re: m.Re, Body: body}, true, nil
}
