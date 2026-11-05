package relay

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitAndReassemble(t *testing.T) {
	// Text of several bytes a character, so parts are cut between
	// characters, never inside one.
	text := strings.Repeat("ÅßÇ€𝄞 label ", 9000)
	body, _ := json.Marshal(map[string]string{"text": text})
	m := Message{V: Version, ID: newID(), Type: TypeEvent, From: "r1", To: Main, Re: "q", Body: body}
	parts, err := split(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 {
		t.Fatalf("%d bytes made %d parts", len(body), len(parts))
	}
	r := newReassembler()
	// Out of order, and one twice before the message is whole. A part
	// arriving again after is the bus's to drop, by the part's own ID.
	order := []int{len(parts) - 1, 0}
	for i := range len(parts) - 1 {
		order = append(order, i)
	}
	var whole Message
	done := false
	for _, i := range order {
		p := parts[i]
		if err := p.check(); err != nil {
			t.Fatalf("part %d: %v", i, err)
		}
		if len(p.Seg) > PartBytes || !utf8.ValidString(p.Seg) {
			t.Fatalf("part %d is %d bytes, cut inside a character or too long", i, len(p.Seg))
		}
		got, ok, err := r.offer(p, 0)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			if done {
				t.Fatal("the message was made whole twice")
			}
			whole, done = got, true
		}
	}
	if !done || whole.ID != m.ID || whole.Re != "q" || !bytes.Equal(whole.Body, body) {
		t.Fatal("the parts did not make the message again")
	}
	if len(r.partials) != 0 || r.bytes != 0 {
		t.Fatal("the reassembler kept what it made whole")
	}
}

func TestSmallMessagesAreNotSplit(t *testing.T) {
	m := Message{V: Version, ID: newID(), Type: TypeStatus, From: "r1", To: Main, Body: json.RawMessage(`{}`)}
	parts, err := split(m)
	if err != nil || len(parts) != 1 || parts[0].Part != nil {
		t.Fatalf("a small message was split: %v, %v", parts, err)
	}
}

func TestTooLargeIsRefused(t *testing.T) {
	m := Message{V: Version, ID: newID(), Type: TypeJob, From: Main, To: "r1", Body: bytes.Repeat([]byte("1"), MaxMessage+1)}
	if _, err := split(m); err == nil {
		t.Fatal("a message over the most was split")
	}
}

func TestReassemblerBounds(t *testing.T) {
	r := newReassembler()
	for i := range maxPartials {
		p := Message{V: Version, ID: newID(), Type: TypeJob, From: "r1", To: Main, Part: &Part{Of: newID(), I: 0, N: MaxParts}, Seg: "x"}
		if _, _, err := r.offer(p, 0); err != nil {
			t.Fatalf("part %d: %v", i, err)
		}
	}
	p := Message{V: Version, ID: newID(), Type: TypeJob, From: "r1", To: Main, Part: &Part{Of: newID(), I: 0, N: 2}, Seg: "x"}
	if _, _, err := r.offer(p, 0); err == nil {
		t.Fatal("more messages than the most were taken in parts")
	}
	// Those left too long are dropped.
	if _, _, err := r.offer(p, partialTimeoutMS+1); err != nil {
		t.Fatalf("after the old ones lapsed: %v", err)
	}
	if len(r.partials) != 1 {
		t.Fatalf("%d messages are arriving in parts", len(r.partials))
	}
	// Parts that disagree are refused.
	q := p
	q.ID, q.Part = newID(), &Part{Of: p.Part.Of, I: 1, N: 3}
	if _, _, err := r.offer(q, partialTimeoutMS+1); err == nil {
		t.Fatal("parts disagreeing on their number were taken")
	}
}

func TestMessageChecks(t *testing.T) {
	ok := Message{V: Version, ID: "1", Type: TypeJob, From: Main, To: "r1"}
	if err := ok.check(); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string]Message{
		"no id": {V: Version, Type: TypeJob, From: Main, To: "r1"},
		"a large body whole": {V: Version, ID: "1", Type: TypeJob, From: Main, To: "r1",
			Body: json.RawMessage(`"` + strings.Repeat("x", maxFrame) + `"`)},
		"no type":           {V: Version, ID: "1", From: Main, To: "r1"},
		"no to":             {V: Version, ID: "1", Type: TypeJob, From: Main},
		"an ack for none":   {V: Version, ID: "1", Type: TypeAck, From: Main, To: "r1"},
		"one part":          {V: Version, ID: "1", Type: TypeJob, From: Main, To: "r1", Part: &Part{Of: "2", I: 0, N: 1}},
		"part past the end": {V: Version, ID: "1", Type: TypeJob, From: Main, To: "r1", Part: &Part{Of: "2", I: 2, N: 2}},
		"a part with a body": {V: Version, ID: "1", Type: TypeJob, From: Main, To: "r1", Part: &Part{Of: "2", I: 0, N: 2},
			Body: json.RawMessage(`{}`)},
	} {
		if err := m.check(); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
}
