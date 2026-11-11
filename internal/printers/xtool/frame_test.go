package xtool

import (
	"bytes"
	"testing"
)

func TestCRC16IsARC(t *testing.T) {
	// CRC-16/ARC's check value.
	if got := CRC16([]byte("123456789")); got != 0xBB3D {
		t.Fatalf("CRC16 = %#04x, want 0xbb3d", got)
	}
	if got := CRC16(nil); got != 0 {
		t.Fatalf("CRC16 of nothing = %#04x", got)
	}
}

func TestEncodeFrameLaysOutTheEnvelope(t *testing.T) {
	payload := []byte(`{"a":1}`)
	f, err := EncodeFrame(ProtocolJSON, payload, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f[:6], []byte{0xBA, 0xBE, 0, 0, 7, 4}) {
		t.Errorf("header % x", f[:6])
	}
	crc := CRC16(payload)
	if f[6] != byte(crc>>8) || f[7] != byte(crc) {
		t.Errorf("payload CRC % x, want %04x", f[6:8], crc)
	}
	hcrc := CRC16(f[:8])
	if f[8] != byte(hcrc>>8) || f[9] != byte(hcrc) {
		t.Errorf("header CRC % x, want %04x", f[8:10], hcrc)
	}
	if !bytes.Equal(f[10:], payload) {
		t.Errorf("payload %q", f[10:])
	}

	// Without a CRC: bit 7 set and the payload CRC zero.
	f, _ = EncodeFrame(ProtocolFileTransfer, payload, true)
	if f[5] != 33|0x80 || f[6] != 0 || f[7] != 0 {
		t.Errorf("no-CRC header % x", f[:8])
	}
}

func TestFrameReaderTakesSplitAndJoinedFramesAndResyncs(t *testing.T) {
	a, _ := EncodeFrame(ProtocolJSON, []byte("first"), false)
	b, _ := EncodeFrame(ProtocolFileTransfer, []byte("second"), true)
	bad, _ := EncodeFrame(ProtocolJSON, []byte("broken"), false)
	bad[len(bad)-1] ^= 0xFF // the payload no longer matches its CRC

	stream := append(append(append([]byte{0x00, 0xBA}, bad...), a...), b...)
	var r FrameReader
	var got []string
	// One byte at a time, as the worst split there is.
	for _, c := range stream {
		r.Write([]byte{c})
		for {
			f, ok := r.Next()
			if !ok {
				break
			}
			got = append(got, string(f.Payload))
		}
	}
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("frames %q", got)
	}

	// Two in one message.
	r = FrameReader{}
	r.Write(append(append([]byte{}, a...), b...))
	f1, ok1 := r.Next()
	f2, ok2 := r.Next()
	if !ok1 || !ok2 || f1.Protocol != ProtocolJSON || f2.Protocol != ProtocolFileTransfer {
		t.Fatalf("joined: %v %v %+v %+v", ok1, ok2, f1, f2)
	}
	if _, ok := r.Next(); ok {
		t.Fatal("a third frame from nothing")
	}
}

func TestFrameReaderSkipsAnOverlongClaim(t *testing.T) {
	// A header that claims more than MaxFramePayload is skipped, not waited
	// on, so a good frame after it still arrives.
	h := []byte{0xBA, 0xBE, 0xFF, 0xFF, 0xFF, 4, 0, 0}
	crc := CRC16(h)
	h = append(h, byte(crc>>8), byte(crc))
	good, _ := EncodeFrame(ProtocolJSON, []byte("ok"), false)
	var r FrameReader
	r.Write(append(h, good...))
	f, ok := r.Next()
	if !ok || string(f.Payload) != "ok" {
		t.Fatalf("got %+v %v", f, ok)
	}
}

func TestFilePacketsRoundTrip(t *testing.T) {
	req := FilePacket{Op: FileRequest, Channel: 7, Offset: 1<<33 + 5, Window: 5 << 20}
	b := EncodeFilePacket(req)
	if len(b) != 10 || b[0] != 1 || b[1] != 7 {
		t.Fatalf("request % x", b)
	}
	got, err := DecodeFilePacket(b)
	if err != nil || got.Offset != req.Offset || got.Window != req.Window || got.Channel != 7 {
		t.Fatalf("request back %+v %v", got, err)
	}
	data := FilePacket{Op: FileData, Channel: 3, Offset: 4096, Data: []byte("G90\n")}
	b = EncodeFilePacket(data)
	if len(b) != 7+4 || b[0] != 129 {
		t.Fatalf("data % x", b)
	}
	got, err = DecodeFilePacket(b)
	if err != nil || got.Offset != 4096 || string(got.Data) != "G90\n" {
		t.Fatalf("data back %+v %v", got, err)
	}
	if _, err := DecodeFilePacket([]byte{9, 1, 2}); err == nil {
		t.Error("an unknown opcode was taken")
	}
}
