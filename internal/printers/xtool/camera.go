package xtool

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
)

// The camera (docs/printing.md#camera-alignment), as PROTOCOL.md describes
// it. Over WS-V2 a snapshot is asked for on the instruction channel by the
// camera's name, which answers with a file name, and the photo is fetched
// over the file channel as a file of type 5. Over REST V1 a snapshot is a
// JPEG from port 8329, by stream: 0 is the overview camera. The fill light
// is set before each photo, since some models turn it off after a job.
// None of it has been tried on a real machine.

// PortCamera is REST V1's camera port.
const PortCamera = 8329

// Camera is a machine whose camera BAM! knows how to ask for a photo.
type Camera interface {
	// Snapshot is a photo from the named camera (WS-V2) or stream (REST
	// V1), as the machine sends it, usually a JPEG.
	Snapshot(ctx context.Context, camera string) ([]byte, error)
	// FillLight sets the fill light's brightness, 0 to 255.
	FillLight(ctx context.Context, level int) error
}

// maxPhoto bounds a photo's size.
const maxPhoto = 32 << 20

// v2FilePhoto is a camera snapshot's type in a transfer.
const v2FilePhoto = 5

func (d *V2) Snapshot(ctx context.Context, camera string) ([]byte, error) {
	s, err := d.session(ctx)
	if err != nil {
		return nil, err
	}
	var snap struct {
		Filename string `json:"filename"`
	}
	if err := s.request(ctx, "GET", "/v1/camera/snap", map[string]any{"name": camera}, nil, &snap); err != nil {
		return nil, err
	}
	if snap.Filename == "" {
		return nil, fmt.Errorf("/v1/camera/snap: the machine named no photo")
	}
	return s.download(ctx, v2FilePhoto, snap.Filename)
}

func (d *V2) FillLight(ctx context.Context, level int) error {
	return d.Request(ctx, "PUT", "/v1/peripheral/param", map[string]any{"type": "fill_light"}, map[string]any{"action": "set_bri", "idx": 0, "value": level}, nil)
}

// download fetches a file from the machine: the transfer is asked for on
// the instruction channel, with a channel number, and answered with the
// file's size and MD5; this side asks for it a window at a time on the
// file_stream channel, as the machine does of an upload, and finishes the
// transfer on the instruction channel. The mirror of upload, as
// PROTOCOL.md names the route; unverified.
func (s *v2Session) download(ctx context.Context, fileType int, name string) ([]byte, error) {
	s.transfer.Lock()
	defer s.transfer.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	conn, err := s.dial(ctx, "file_stream")
	if err != nil {
		return nil, err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(MaxFramePayload + frameHeader)

	s.mu.Lock()
	s.channel++
	if s.channel == 0 {
		s.channel = 1
	}
	channel := s.channel
	s.mu.Unlock()
	var offer struct {
		FileSize   int64  `json:"filesize"`
		DigestData string `json:"digestdata"`
		PacketSize int    `json:"packetsize"`
	}
	if err := s.request(ctx, "PUT", "/v1/filetransfer/download", nil, map[string]any{
		"filetype": fileType, "filename": name, "channel": channel, "packetsize": v2PacketSize,
	}, &offer); err != nil {
		return nil, err
	}
	if offer.FileSize <= 0 || offer.FileSize > maxPhoto {
		return nil, fmt.Errorf("fetching %s: the machine offered %d bytes", name, offer.FileSize)
	}
	content := make([]byte, offer.FileSize)
	var frames FrameReader
	for at := int64(0); at < offer.FileSize; {
		window := min(int64(4*v2PacketSize), offer.FileSize-at)
		frame, err := EncodeFrame(ProtocolFileTransfer, EncodeFilePacket(FilePacket{Op: FileRequest, Channel: channel, Offset: at, Window: int(window)}), true)
		if err != nil {
			return nil, err
		}
		if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
			return nil, fmt.Errorf("fetching %s: %w", name, err)
		}
		end := at + window
		for at < end {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return nil, fmt.Errorf("fetching %s: %w", name, err)
			}
			frames.Write(data)
			for {
				f, ok := frames.Next()
				if !ok {
					break
				}
				if f.Protocol != ProtocolFileTransfer {
					continue
				}
				p, err := DecodeFilePacket(f.Payload)
				if err != nil || p.Op != FileData || p.Channel != channel {
					continue
				}
				if p.Offset != at || p.Offset+int64(len(p.Data)) > end {
					return nil, fmt.Errorf("fetching %s: the machine sent %d bytes at %d, expecting them at %d", name, len(p.Data), p.Offset, at)
				}
				copy(content[at:], p.Data)
				at += int64(len(p.Data))
			}
		}
	}
	if offer.DigestData != "" {
		if sum := md5.Sum(content); hex.EncodeToString(sum[:]) != offer.DigestData {
			return nil, fmt.Errorf("fetching %s: its MD5 differs from the machine's", name)
		}
	}
	if err := s.request(ctx, "PUT", "/v1/filetransfer/finish", nil, map[string]any{"code": 0, "message": "file transfer finish", "channel": channel}, nil); err != nil {
		return nil, err
	}
	conn.Close(websocket.StatusNormalClosure, "")
	return content, nil
}

func (d *REST) Snapshot(ctx context.Context, camera string) ([]byte, error) {
	base := d.CameraBase
	if base == "" {
		return nil, fmt.Errorf("the camera's address is not known: %w", ErrNotSupported)
	}
	ctx, cancel := context.WithTimeout(ctx, restTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/camera/snap?"+url.Values{"stream": {camera}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("/camera/snap: %w", ErrNotSupported)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/camera/snap: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxPhoto+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxPhoto {
		return nil, fmt.Errorf("/camera/snap: the photo is larger than %d MB", maxPhoto>>20)
	}
	return b, nil
}

func (d *REST) FillLight(ctx context.Context, level int) error {
	ctx, cancel := context.WithTimeout(ctx, restTimeout)
	defer cancel()
	body, _ := json.Marshal(map[string]any{"action": "set_bri", "idx": 0, "value": level})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Base+"/peripheral/fill_light", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	_, err = d.do(req)
	return err
}
