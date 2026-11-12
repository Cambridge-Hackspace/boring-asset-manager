// Package niimbot encodes print jobs for Niimbot's thermal label printers,
// and holds the conversation the printers need to print them: each
// command waits for the printer's answer, and the end of a page for it to
// finish printing.
//
// Niimbot publishes no reference. It follows the community's descriptions
// of the protocol, NiimBlue's wiki and the niimprint and niimbluelib
// projects (docs/printing.md#niimbot), implementing it as they describe
// it and copying none of their code. Like package brotherql it works on
// 1-bit images and physical sizes only.
package niimbot

import (
	"errors"
	"fmt"
)

// A packet is framed as 55 55, its command, the length of its data, the
// data, a checksum, and AA AA. The checksum is the XOR of the command, the
// length and every byte of data.
const (
	head = 0x55
	tail = 0xAA
	// connectPrefix comes before the connect command, and no other.
	connectPrefix = 0x03
	// maxData is the most data a packet's one length byte allows.
	maxData = 255
)

// Commands BAM! sends, and the printer's own.
const (
	CmdPrintStart    = 0x01
	CmdPageStart     = 0x03
	CmdSetPageSize   = 0x13
	CmdPrintQuantity = 0x15
	CmdRFIDInfo      = 0x1A
	CmdPrintClear    = 0x20
	CmdSetDensity    = 0x21
	CmdSetLabelType  = 0x23
	CmdEmptyRows     = 0x84
	CmdBitmapRows    = 0x85
	CmdPrintStatus   = 0xA3
	CmdConnect       = 0xC1
	CmdPageEnd       = 0xE3
	CmdPrintEnd      = 0xF3
	// CmdPrintError is the printer's own, sent unasked.
	CmdPrintError = 0xDB
)

// commands names each command BAM! sends, for messages, and the command the
// printer answers it with; 0 for none.
var commands = map[byte]struct {
	name  string
	reply byte
}{
	CmdConnect:       {"connect", 0xC2},
	CmdRFIDInfo:      {"the roll's RFID tag", 0x1B},
	CmdSetDensity:    {"the density", 0x31},
	CmdSetLabelType:  {"the label type", 0x33},
	CmdPrintStart:    {"print start", 0x02},
	CmdPrintClear:    {"print clear", 0x30},
	CmdPageStart:     {"page start", 0x04},
	CmdSetPageSize:   {"the page size", 0x14},
	CmdPrintQuantity: {"the quantity", 0x16},
	CmdEmptyRows:     {"empty rows", 0},
	CmdBitmapRows:    {"bitmap rows", 0},
	CmdPageEnd:       {"page end", 0xE4},
	CmdPrintStatus:   {"the print status", 0xB3},
	CmdPrintEnd:      {"print end", 0xF4},
}

// Packet is one command and its data.
type Packet struct {
	Cmd  byte
	Data []byte
}

// ErrPacket is wrapped by errors about a malformed packet.
var ErrPacket = errors.New("niimbot: packet")

// checksum is the XOR of a packet's command, length and data.
func checksum(cmd byte, data []byte) byte {
	sum := cmd ^ byte(len(data))
	for _, b := range data {
		sum ^= b
	}
	return sum
}

// Bytes frames the packet, with the prefix the connect command takes.
func (p Packet) Bytes() []byte {
	if len(p.Data) > maxData {
		panic(fmt.Sprintf("niimbot: %d bytes of data in one packet", len(p.Data)))
	}
	out := make([]byte, 0, len(p.Data)+8)
	if p.Cmd == CmdConnect {
		out = append(out, connectPrefix)
	}
	out = append(out, head, head, p.Cmd, byte(len(p.Data)))
	out = append(out, p.Data...)
	return append(out, checksum(p.Cmd, p.Data), tail, tail)
}

// name names a command for messages.
func name(cmd byte) string {
	if c, ok := commands[cmd]; ok {
		return c.name
	}
	return fmt.Sprintf("command %02X", cmd)
}

// parse reads one framed packet from the start of b, returning it and how
// many bytes it took, or 0 and no error when b holds only the start of one.
func parse(b []byte) (Packet, int, error) {
	if len(b) < 4 {
		return Packet{}, 0, nil
	}
	if b[0] != head || b[1] != head {
		return Packet{}, 0, fmt.Errorf("%w: does not start 55 55", ErrPacket)
	}
	n := int(b[3])
	size := n + 7
	if len(b) < size {
		return Packet{}, 0, nil
	}
	p := Packet{Cmd: b[2], Data: append([]byte(nil), b[4:4+n]...)}
	if b[4+n] != checksum(p.Cmd, p.Data) {
		return p, size, fmt.Errorf("%w: %s fails its checksum", ErrPacket, name(p.Cmd))
	}
	if b[5+n] != tail || b[6+n] != tail {
		return p, size, fmt.Errorf("%w: %s does not end AA AA", ErrPacket, name(p.Cmd))
	}
	return p, size, nil
}

// Split cuts a job into its packets, each as framed, with the prefix the
// connect command takes. Errors wrap ErrPacket.
func Split(job []byte) ([][]byte, error) {
	var out [][]byte
	for len(job) > 0 {
		start := job
		if job[0] == connectPrefix {
			job = job[1:]
		}
		p, n, err := parse(job)
		switch {
		case err != nil:
			return nil, err
		case n == 0:
			return nil, fmt.Errorf("%w: the job ends part way through one", ErrPacket)
		case len(start) != len(job) && p.Cmd != CmdConnect:
			return nil, fmt.Errorf("%w: %s has the connect command's prefix", ErrPacket, name(p.Cmd))
		}
		size := n + len(start) - len(job)
		out = append(out, start[:size])
		job = start[size:]
	}
	return out, nil
}

// Reader gathers packets from what a printer sends, which may come in
// pieces (a Bluetooth notification or serial read may hold part of one,
// or several), skipping anything between them that is not one.
type Reader struct {
	buf []byte
}

// Add takes bytes as they arrive and returns the whole packets they
// complete. A packet that fails its checksum is dropped.
func (r *Reader) Add(b []byte) []Packet {
	r.buf = append(r.buf, b...)
	var out []Packet
	for {
		i := 0
		for i+1 < len(r.buf) && (r.buf[i] != head || r.buf[i+1] != head) {
			i++
		}
		r.buf = r.buf[i:]
		p, n, err := parse(r.buf)
		if n == 0 {
			return out
		}
		if err != nil {
			// Not a packet after all: look for one from the next byte.
			r.buf = r.buf[1:]
			continue
		}
		r.buf = r.buf[n:]
		out = append(out, p)
	}
}
