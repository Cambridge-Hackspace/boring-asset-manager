// Package xtoolsim simulates xTool lasers for tests: one speaking WS-V2,
// with its envelope, CRCs, channels and file transfers, and one speaking
// REST V1. They behave as PROTOCOL.md describes the machines, and record
// what they were sent. Nothing here burns anything.
package xtoolsim

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool"
)

// V2 is a simulated WS-V2 machine, served over TLS by an httptest server.
type V2 struct {
	Server *httptest.Server

	mu sync.Mutex
	// Firmware is the version it reports.
	Firmware string
	// Mode is its P_* mode.
	Mode string
	// Peripherals are the states of its peripherals, "on" or "off", by
	// type (gap, machine_lock, drawer); one missing answers code 10.
	Peripherals map[string]string
	// Progress is the job's, from 0 to 1.
	Progress float64
	// PacketSize is the largest file data packet it agrees to, and Window
	// how much it asks for at once.
	PacketSize, Window int
	// FileDelay is how long a new file channel takes to be taken, as a
	// busy machine's may: an upload can be announced before then.
	FileDelay time.Duration
	// Photos are what its camera, "main", sends, in turn, the last again
	// and again; none answers code 10. FillLights are the fill light's
	// levels it was set to.
	Photos     [][]byte
	FillLights []int

	files map[string][]byte
	// finished is the file last sent whole, which the next upload
	// config loads; loaded is the one loaded.
	finished string
	loaded   string
	framed   int
	connects int
	requests []string
	actions  []string
	sessions map[string]*v2Conns
	uploads  map[int]*v2Upload
	wake     chan struct{}
	// snaps are photos taken and not yet fetched, by file name;
	// downloads those being fetched, by channel.
	snaps     map[string][]byte
	downloads map[int][]byte
	shots     int
}

type v2Conns struct {
	instruction, file *websocket.Conn
	writeMu           sync.Mutex
}

type v2Upload struct {
	session, name string
	size          int
	digest        string
	buf           []byte
	got           int
	asked         int
	window        int
}

// NewV2 starts a simulated machine, idle with every peripheral fine.
func NewV2(firmware string) *V2 {
	s := &V2{
		Firmware: firmware, Mode: "P_IDLE", Peripherals: map[string]string{"gap": "on", "machine_lock": "on", "drawer": "on"},
		PacketSize: 1024, Window: 4096,
		files: map[string][]byte{}, sessions: map[string]*v2Conns{}, uploads: map[int]*v2Upload{}, wake: make(chan struct{}),
		snaps: map[string][]byte{}, downloads: map[int][]byte{},
	}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	return s
}

// Addr is its host:port.
func (s *V2) Addr() string {
	return strings.TrimPrefix(s.Server.URL, "https://")
}

// Close stops it.
func (s *V2) Close() {
	s.Drop()
	s.Server.Close()
}

// Drop drops every client's connections, as a machine restarting does.
func (s *V2) Drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, c := range s.sessions {
		if c.instruction != nil {
			c.instruction.CloseNow()
		}
		if c.file != nil {
			c.file.CloseNow()
		}
		delete(s.sessions, id)
	}
}

// Set changes it under its lock.
func (s *V2) Set(f func(s *V2)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

// File is a file it was sent, whole and checked.
func (s *V2) File(name string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files[name]
}

// Requests are the URLs it was asked for, in order.
func (s *V2) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// Actions are the job actions it took (start, pause, stop), in order.
func (s *V2) Actions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.actions)
}

// Connects is how many times a client opened an instruction channel.
func (s *V2) Connects() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connects
}

// Framed is how many frames it traced.
func (s *V2) Framed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.framed
}

// PressButton presses its button, starting a job sent ready.
func (s *V2) PressButton() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Mode == "P_ONLINE_READY_WORK" && s.loaded == "tmp.gcode" {
		s.Mode = "P_WORKING"
		s.actions = append(s.actions, "button")
	}
}

// Finish ends what it is working on, and pushes the change of mode.
func (s *V2) Finish() {
	s.mu.Lock()
	framing := s.loaded == "tmpFrame.gcode"
	if framing {
		s.Mode = "P_IDLE"
	} else {
		s.Mode, s.Progress = "P_FINISH", 1
	}
	mode := s.Mode
	s.mu.Unlock()
	if framing {
		s.Push("/device/status", "STATUS_CONTROLLER", "WORK_FINISHED", "framing")
	}
	s.Push("/work/mode", "STATUS_CONTROLLER", "MODE_CHANGE", map[string]string{"mode": mode})
}

// Push sends an event to every client.
func (s *V2) Push(url, module, typ string, info any) {
	body, _ := json.Marshal(map[string]any{"url": url, "data": map[string]any{"module": module, "type": typ, "info": info}, "timestamp": time.Now().UnixMilli()})
	s.mu.Lock()
	conns := make([]*v2Conns, 0, len(s.sessions))
	for _, c := range s.sessions {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		if c.instruction != nil {
			c.send(c.instruction, xtool.ProtocolJSON, body, false)
		}
	}
}

func (c *v2Conns) send(conn *websocket.Conn, protocol byte, payload []byte, noCRC bool) error {
	frame, err := xtool.EncodeFrame(protocol, payload, noCRC)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return conn.Write(ctx, websocket.MessageBinary, frame)
}

// dropFile forgets a file channel that has closed, unless a newer one has
// taken its place.
func (s *V2) dropFile(c *v2Conns, conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.file == conn {
		c.file = nil
	}
}

func (s *V2) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/websocket" {
		http.NotFound(w, r)
		return
	}
	id, function := r.URL.Query().Get("id"), r.URL.Query().Get("function")
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(32 << 20)
	defer conn.CloseNow()
	if function == "file_stream" {
		s.mu.Lock()
		delay := s.FileDelay
		s.mu.Unlock()
		time.Sleep(delay)
	}
	s.mu.Lock()
	c, ok := s.sessions[id]
	switch function {
	case "instruction":
		if !ok {
			c = &v2Conns{}
			s.sessions[id] = c
		}
		c.instruction = conn
		s.connects++
	case "file_stream":
		// A file channel belongs to the client that opened the instruction
		// channel with the same id; any other is turned away.
		if !ok {
			s.mu.Unlock()
			conn.Close(websocket.StatusPolicyViolation, "unknown client")
			return
		}
		c.file = conn
		defer s.dropFile(c, conn)
		close(s.wake)
		s.wake = make(chan struct{})
	default:
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	parity := false
	var frames xtool.FrameReader
	for {
		typ, data, err := conn.Read(context.Background())
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			// The machine drops raw text, as the firmware does.
			continue
		}
		frames.Write(data)
		for {
			f, ok := frames.Next()
			if !ok {
				break
			}
			switch {
			case function == "instruction" && f.Protocol == xtool.ProtocolJSON:
				parity = s.request(c, id, f.Payload, parity)
			case function == "file_stream" && f.Protocol == xtool.ProtocolFileTransfer:
				s.fileData(c, f.Payload)
				s.fileRequest(c, conn, f.Payload)
			}
		}
	}
}

type v2Request struct {
	Type          string          `json:"type"`
	Method        string          `json:"method"`
	URL           string          `json:"url"`
	Params        json.RawMessage `json:"params"`
	Data          json.RawMessage `json:"data"`
	TransactionID int             `json:"transactionId"`
}

// request answers one request, returning whether the client has passed
// parity.
func (s *V2) request(c *v2Conns, session string, payload []byte, parity bool) bool {
	var req v2Request
	if json.Unmarshal(payload, &req) != nil || req.Type != "request" {
		return parity
	}
	answer := func(code int, msg string, data any) {
		body, _ := json.Marshal(map[string]any{"type": "response", "code": code, "msg": msg, "transactionId": req.TransactionID, "data": data})
		c.send(c.instruction, xtool.ProtocolJSON, body, false)
	}
	if req.URL == "/v1/user/ping" {
		answer(0, "ok", map[string]any{})
		return parity
	}
	if req.URL == "/v1/user/parity" {
		var d struct {
			UserKey string `json:"userKey"`
		}
		_ = json.Unmarshal(req.Data, &d)
		if d.UserKey != "bWFrZWJsb2NrLXh0b29s" {
			answer(1, "parity failed", nil)
			return false
		}
		answer(0, "ok", map[string]any{})
		return true
	}
	if !parity {
		answer(-1, "no parity", nil)
		return false
	}
	var params map[string]any
	_ = json.Unmarshal(req.Params, &params)
	str := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }

	s.mu.Lock()
	s.requests = append(s.requests, req.Method+" "+req.URL)
	switch req.Method + " " + req.URL {
	case "GET /v1/device/machineInfo":
		fw := s.Firmware
		s.mu.Unlock()
		answer(0, "ok", map[string]any{"deviceName": "Simulated", "firmware": map[string]any{"package_version": fw}})
	case "GET /v1/device/runtime-infos":
		mode := s.Mode
		s.mu.Unlock()
		answer(0, "ok", map[string]any{"curMode": map[string]any{"mode": mode, "subMode": "LASER_PLANE"}})
	case "GET /v1/processing/progress":
		p := s.Progress * 100
		s.mu.Unlock()
		answer(0, "ok", map[string]any{"progress": p, "workingTime": 1})
	case "GET /v1/peripheral/param":
		state, ok := s.Peripherals[str(params, "type")]
		s.mu.Unlock()
		if !ok {
			answer(10, "device not support", nil)
			return parity
		}
		answer(0, "ok", map[string]any{"state": state})
	case "PUT /v1/filetransfer/upload":
		var d struct {
			FileType   int    `json:"filetype"`
			FileName   string `json:"filename"`
			FileSize   int    `json:"filesize"`
			DigestType int    `json:"digesttype"`
			DigestData string `json:"digestdata"`
			Channel    int    `json:"channel"`
			PacketSize int    `json:"packetsize"`
		}
		_ = json.Unmarshal(req.Data, &d)
		if d.FileType != 1 || d.DigestType != 1 || d.FileSize <= 0 || d.Channel < 1 || d.Channel > 255 {
			s.mu.Unlock()
			answer(1, "bad upload", nil)
			return parity
		}
		up := &v2Upload{session: session, name: d.FileName, size: d.FileSize, digest: d.DigestData, buf: make([]byte, d.FileSize), window: s.Window}
		s.uploads[d.Channel] = up
		packet := min(s.PacketSize, d.PacketSize)
		s.mu.Unlock()
		answer(0, "ok", map[string]any{"packetsize": packet})
		go s.ask(session, byte(d.Channel))
	case "GET /v1/camera/snap":
		if str(params, "name") != "main" || len(s.Photos) == 0 {
			s.mu.Unlock()
			answer(10, "device not support", nil)
			return parity
		}
		photo := s.Photos[min(s.shots, len(s.Photos)-1)]
		s.shots++
		name := fmt.Sprintf("6f1c2e00-0000-4000-8000-%012d", s.shots)
		s.snaps[name] = photo
		s.mu.Unlock()
		answer(0, "ok", map[string]any{"filename": name})
	case "PUT /v1/filetransfer/download":
		var d struct {
			FileType   int    `json:"filetype"`
			FileName   string `json:"filename"`
			Channel    int    `json:"channel"`
			PacketSize int    `json:"packetsize"`
		}
		_ = json.Unmarshal(req.Data, &d)
		photo, ok := s.snaps[d.FileName]
		if d.FileType != 5 || !ok || d.Channel < 1 || d.Channel > 255 {
			s.mu.Unlock()
			answer(1, "no such file", nil)
			return parity
		}
		delete(s.snaps, d.FileName)
		s.downloads[d.Channel] = photo
		sum := md5.Sum(photo)
		packet := min(s.PacketSize, d.PacketSize)
		s.mu.Unlock()
		answer(0, "ok", map[string]any{"filesize": len(photo), "digesttype": 1, "digestdata": hex.EncodeToString(sum[:]), "packetsize": packet})
	case "PUT /v1/peripheral/param":
		var d struct {
			Action string `json:"action"`
			Value  int    `json:"value"`
		}
		_ = json.Unmarshal(req.Data, &d)
		ok := str(params, "type") == "fill_light" && d.Action == "set_bri"
		if ok {
			s.FillLights = append(s.FillLights, d.Value)
		}
		s.mu.Unlock()
		if !ok {
			answer(10, "device not support", nil)
			return parity
		}
		answer(0, "ok", map[string]any{})
	case "PUT /v1/filetransfer/finish":
		var d struct {
			Channel int `json:"channel"`
		}
		_ = json.Unmarshal(req.Data, &d)
		if _, ok := s.downloads[d.Channel]; ok {
			delete(s.downloads, d.Channel)
			s.mu.Unlock()
			answer(0, "ok", map[string]any{})
			return parity
		}
		// Data still on its way over the file channel is waited for.
		for wait := time.Now().Add(2 * time.Second); s.uploads[d.Channel] != nil && s.uploads[d.Channel].got < s.uploads[d.Channel].size && time.Now().Before(wait); {
			s.mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			s.mu.Lock()
		}
		up := s.uploads[d.Channel]
		delete(s.uploads, d.Channel)
		ok := up != nil && up.got >= up.size
		if ok {
			sum := md5.Sum(up.buf)
			ok = hex.EncodeToString(sum[:]) == up.digest
		}
		if ok {
			s.files[up.name], s.finished = up.buf, up.name
		}
		s.mu.Unlock()
		if !ok {
			answer(1, "file transfer incomplete or its MD5 differs", nil)
			return parity
		}
		answer(0, "ok", map[string]any{})
	case "PUT /v1/processing/upload/config":
		var d struct {
			FileType  string `json:"fileType"`
			AutoStart int    `json:"autoStart"`
		}
		_ = json.Unmarshal(req.Data, &d)
		// The file just sent is the one loaded.
		name := s.finished
		s.finished = ""
		if d.FileType != "txt" || name == "" || s.Mode == "P_WORKING" || s.Mode == "P_PAUSE" {
			s.mu.Unlock()
			answer(1, "failed", nil)
			return parity
		}
		s.loaded = name
		s.Mode = "P_ONLINE_READY_WORK"
		s.mu.Unlock()
		answer(0, "ok", map[string]any{"result": "ok"})
	case "PUT /v1/processing/state":
		action := str(params, "action")
		code := 0
		switch {
		case action == "start" && s.Mode == "P_ONLINE_READY_WORK":
			s.Mode, s.Progress = "P_WORKING", 0
			if s.loaded == "tmpFrame.gcode" {
				s.framed++
			}
		case action == "start" && s.Mode == "P_PAUSE":
			s.Mode = "P_WORKING"
		case action == "pause" && s.Mode == "P_WORKING":
			s.Mode = "P_PAUSE"
		case action == "stop":
			s.Mode = "P_IDLE"
		default:
			code = 1
		}
		if code == 0 {
			s.actions = append(s.actions, action)
		}
		framing := s.loaded == "tmpFrame.gcode" && action == "start"
		s.mu.Unlock()
		if code != 0 {
			answer(code, "failed", nil)
			return parity
		}
		answer(0, "ok", map[string]any{})
		if framing {
			s.Push("/device/status", "STATUS_CONTROLLER", "WORK_STARTED", "framing")
		}
	default:
		s.mu.Unlock()
		answer(-2, "invalid request", nil)
	}
	return parity
}

// ask asks for the next window of an upload, once the client's file
// channel is open.
func (s *V2) ask(session string, channel byte) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.mu.Lock()
		c := s.sessions[session]
		up := s.uploads[int(channel)]
		wake := s.wake
		if c == nil || up == nil {
			s.mu.Unlock()
			return
		}
		if file := c.file; file != nil {
			n := min(up.window, up.size-up.asked)
			at := up.asked
			up.asked += n
			s.mu.Unlock()
			if n <= 0 {
				return
			}
			// A channel that closed under it is forgotten, and the
			// window asked for again on the next one.
			if err := c.send(file, xtool.ProtocolFileTransfer, xtool.EncodeFilePacket(xtool.FilePacket{Op: xtool.FileRequest, Channel: channel, Offset: int64(at), Window: n}), true); err != nil {
				s.dropFile(c, file)
				s.mu.Lock()
				up.asked = at
				s.mu.Unlock()
				continue
			}
			return
		}
		s.mu.Unlock()
		select {
		case <-wake:
		case <-time.After(time.Until(deadline)):
			return
		}
	}
}

// fileData takes a data packet of an upload, asking for the next window
// when this one is whole.
func (s *V2) fileData(c *v2Conns, payload []byte) {
	p, err := xtool.DecodeFilePacket(payload)
	if err != nil || p.Op != xtool.FileData {
		return
	}
	s.mu.Lock()
	up := s.uploads[int(p.Channel)]
	if up == nil || p.Offset < 0 || int(p.Offset)+len(p.Data) > up.size || len(p.Data) > s.PacketSize {
		s.mu.Unlock()
		return
	}
	copy(up.buf[p.Offset:], p.Data)
	up.got += len(p.Data)
	more := up.got == up.asked && up.asked < up.size
	session := up.session
	s.mu.Unlock()
	if more {
		s.ask(session, p.Channel)
	}
}

// fileRequest answers a client's request for a window of a file it is
// fetching, a packet at a time, on the file channel it came on.
func (s *V2) fileRequest(c *v2Conns, conn *websocket.Conn, payload []byte) {
	p, err := xtool.DecodeFilePacket(payload)
	if err != nil || p.Op != xtool.FileRequest {
		return
	}
	s.mu.Lock()
	content, ok := s.downloads[int(p.Channel)]
	packet := s.PacketSize
	s.mu.Unlock()
	if !ok || p.Offset < 0 || p.Offset >= int64(len(content)) {
		return
	}
	end := min(p.Offset+int64(p.Window), int64(len(content)))
	for at := p.Offset; at < end; at += int64(packet) {
		chunk := content[at:min(at+int64(packet), end)]
		c.send(conn, xtool.ProtocolFileTransfer, xtool.EncodeFilePacket(xtool.FilePacket{Op: xtool.FileData, Channel: p.Channel, Offset: at, Data: chunk}), true)
	}
}
