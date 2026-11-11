// Package xtool speaks to xTool lasers over their network protocols
// (docs/printing.md#xtool), as ha-xtool's docs/PROTOCOL.md describes them:
// WS-V2, the TLS WebSocket protocol of their newer firmware, and REST V1,
// the HTTP protocol of their older firmware. It also writes jobs as G-code in
// xTool's dialect, and checks firmware against the versions BAM! knows.
package xtool

import (
	"bytes"
	"errors"
	"fmt"
)

// WS-V2 wraps every message in a 10-byte envelope:
//
//	0-1  0xBA 0xBE
//	2-4  the payload's length, big-endian
//	5    the protocol type, with 0x80 set when the payload has no CRC
//	6-7  the payload's CRC-16/ARC, big-endian, or zero
//	8-9  the CRC-16/ARC of bytes 0-7, big-endian
//
// and then the payload.
const (
	frameMagic0, frameMagic1 = 0xBA, 0xBE
	frameHeader              = 10
	frameNoCRC               = 0x80
	// MaxFramePayload bounds a payload this side takes: larger claims are
	// taken as noise and skipped, rather than waited for.
	MaxFramePayload = 8 << 20
)

// Protocol types carried in an envelope.
const (
	ProtocolJSON         = 4
	ProtocolFileTransfer = 33
)

// CRC16 is CRC-16/ARC: polynomial 0x8005 reflected (0xA001), starting at
// zero, with nothing XORed out.
func CRC16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// EncodeFrame wraps a payload in its envelope, with the payload's CRC unless
// noCRC is set.
func EncodeFrame(protocol byte, payload []byte, noCRC bool) ([]byte, error) {
	if len(payload) > 0xFFFFFF {
		return nil, fmt.Errorf("xtool: a %d-byte payload is too long for a frame", len(payload))
	}
	out := make([]byte, frameHeader+len(payload))
	out[0], out[1] = frameMagic0, frameMagic1
	out[2], out[3], out[4] = byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload))
	out[5] = protocol & 0x7F
	if noCRC {
		out[5] |= frameNoCRC
	} else {
		crc := CRC16(payload)
		out[6], out[7] = byte(crc>>8), byte(crc)
	}
	crc := CRC16(out[:8])
	out[8], out[9] = byte(crc>>8), byte(crc)
	copy(out[frameHeader:], payload)
	return out, nil
}

// Frame is one message taken from a stream.
type Frame struct {
	Protocol byte
	Payload  []byte
}

// FrameReader takes frames from a stream of bytes, as they arrive in
// WebSocket messages: a frame may be split across messages, or several may
// share one. A bad CRC skips to the next magic, rather than dropping what
// was buffered.
type FrameReader struct {
	buf []byte
}

// errShortFrame is a frame not yet whole.
var errShortFrame = errors.New("short")

// Write adds bytes received.
func (r *FrameReader) Write(p []byte) {
	r.buf = append(r.buf, p...)
}

// Next returns the next whole frame, or false when none is buffered yet.
func (r *FrameReader) Next() (Frame, bool) {
	for {
		i := bytes.Index(r.buf, []byte{frameMagic0, frameMagic1})
		if i < 0 {
			// Keep a trailing first magic byte, which may begin a frame.
			if n := len(r.buf); n > 0 && r.buf[n-1] == frameMagic0 {
				r.buf = r.buf[n-1:]
			} else {
				r.buf = r.buf[:0]
			}
			return Frame{}, false
		}
		r.buf = r.buf[i:]
		f, n, err := parseFrame(r.buf)
		if err == errShortFrame {
			return Frame{}, false
		}
		if err != nil {
			// Not a frame after all: look again from the next byte.
			r.buf = r.buf[1:]
			continue
		}
		r.buf = r.buf[n:]
		return f, true
	}
}

// parseFrame reads the frame at the start of b, returning how many bytes it
// took.
func parseFrame(b []byte) (Frame, int, error) {
	if len(b) < frameHeader {
		return Frame{}, 0, errShortFrame
	}
	if CRC16(b[:8]) != uint16(b[8])<<8|uint16(b[9]) {
		return Frame{}, 0, errors.New("bad header CRC")
	}
	n := int(b[2])<<16 | int(b[3])<<8 | int(b[4])
	if n > MaxFramePayload {
		return Frame{}, 0, errors.New("payload too long")
	}
	if len(b) < frameHeader+n {
		return Frame{}, 0, errShortFrame
	}
	payload := b[frameHeader : frameHeader+n]
	if b[5]&frameNoCRC == 0 && CRC16(payload) != uint16(b[6])<<8|uint16(b[7]) {
		return Frame{}, 0, errors.New("bad payload CRC")
	}
	return Frame{Protocol: b[5] & 0x7F, Payload: bytes.Clone(payload)}, frameHeader + n, nil
}

// File-transfer packets, carried in ProtocolFileTransfer frames on the
// file_stream channel. The side receiving a file asks for each window of it
// with a request (opcode, channel, a 5-byte offset and a 3-byte window size,
// big-endian); the side sending it answers with data packets (opcode,
// channel, a 5-byte offset, and the bytes).
const (
	FileRequest       = 1
	FileData          = 129
	fileRequestHeader = 10
	fileDataHeader    = 7
)

// FilePacket is one file-transfer packet.
type FilePacket struct {
	Op      byte
	Channel byte
	Offset  int64
	// Window is how many bytes a request asks for.
	Window int
	// Data is what a data packet carries.
	Data []byte
}

// EncodeFilePacket is a packet's bytes.
func EncodeFilePacket(p FilePacket) []byte {
	put40 := func(b []byte, v int64) {
		for i := range 5 {
			b[i] = byte(v >> (8 * (4 - i)))
		}
	}
	if p.Op == FileRequest {
		b := make([]byte, fileRequestHeader)
		b[0], b[1] = p.Op, p.Channel
		put40(b[2:7], p.Offset)
		b[7], b[8], b[9] = byte(p.Window>>16), byte(p.Window>>8), byte(p.Window)
		return b
	}
	b := make([]byte, fileDataHeader+len(p.Data))
	b[0], b[1] = p.Op, p.Channel
	put40(b[2:7], p.Offset)
	copy(b[fileDataHeader:], p.Data)
	return b
}

// DecodeFilePacket reads a packet.
func DecodeFilePacket(b []byte) (FilePacket, error) {
	get40 := func(b []byte) int64 {
		var v int64
		for _, c := range b[:5] {
			v = v<<8 | int64(c)
		}
		return v
	}
	if len(b) < 2 {
		return FilePacket{}, errors.New("xtool: a file packet too short")
	}
	switch b[0] {
	case FileRequest:
		if len(b) < fileRequestHeader {
			return FilePacket{}, errors.New("xtool: a file request too short")
		}
		return FilePacket{Op: b[0], Channel: b[1], Offset: get40(b[2:7]), Window: int(b[7])<<16 | int(b[8])<<8 | int(b[9])}, nil
	case FileData:
		if len(b) < fileDataHeader {
			return FilePacket{}, errors.New("xtool: a file data packet too short")
		}
		return FilePacket{Op: b[0], Channel: b[1], Offset: get40(b[2:7]), Data: bytes.Clone(b[fileDataHeader:])}, nil
	}
	return FilePacket{}, fmt.Errorf("xtool: unknown file packet opcode %d", b[0])
}
