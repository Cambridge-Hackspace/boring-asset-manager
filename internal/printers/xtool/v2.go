package xtool

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// WS-V2 (PROTOCOL.md, "WS-V2 protocol"): the machine serves TLS WebSockets
// at /websocket on port 28900, with a certificate of its own that cannot be
// checked. A client opens an instruction channel and, to move files, a
// file_stream channel, both with the same id, and frames every message in
// the CRC envelope (frame.go). On the instruction channel it asks first for
// /v1/user/parity, then makes requests, each answered with its transaction
// ID, and pings every few seconds; the machine also pushes events there.
const (
	v2Path = "/websocket"
	// v2UserID and v2UserKey are what xTool Studio sends a machine for a
	// guest: "mk-guest", and "makeblock-xtool" in base64.
	v2UserID  = "mk-guest"
	v2UserKey = "bWFrZWJsb2NrLXh0b29s"
	// v2PingID is the transaction ID every ping carries, and v2MaxID the
	// highest a request carries, so the two never meet.
	v2PingID = 65510
	v2MaxID  = 65499
	// v2PacketSize is the most bytes this side sends in one file data
	// packet; the machine may ask for fewer.
	v2PacketSize = 64 << 10
	// v2FileGcode is a G-code file's type in a transfer.
	v2FileGcode = 1
)

// V2Timing is how long WS-V2 waits; tests shorten it.
type V2Timing struct {
	Request   time.Duration
	Heartbeat time.Duration
	// PongTimeout closes a connection the machine stops answering pings on.
	PongTimeout time.Duration
	// Idle closes a connection nothing has used for this long.
	Idle time.Duration
}

// DefaultV2Timing is xTool Studio's: a ping every 3 s, closed after 11 s
// without an answer.
var DefaultV2Timing = V2Timing{Request: 15 * time.Second, Heartbeat: 3 * time.Second, PongTimeout: 11 * time.Second, Idle: 2 * time.Minute}

// V2 is a machine reached over WS-V2 at an address, host:port. Its
// connection is kept and shared by everyone reaching that address, and
// closed when unused.
type V2 struct {
	Addr   string
	Timing V2Timing
	// FlameHold is how long after the machine reports a flame its flame
	// interlock stays wrong: the machine does not say when one is out.
	FlameHold time.Duration
}

// DefaultFlameHold is FlameHold unless one is set.
const DefaultFlameHold = 5 * time.Minute

func (d *V2) flameHold() time.Duration {
	if d.FlameHold > 0 {
		return d.FlameHold
	}
	return DefaultFlameHold
}

var (
	v2SessionsMu sync.Mutex
	v2Sessions   = map[string]*v2Session{}
)

// CloseV2 closes every WS-V2 connection, for tests.
func CloseV2() {
	v2SessionsMu.Lock()
	sessions := v2Sessions
	v2Sessions = map[string]*v2Session{}
	v2SessionsMu.Unlock()
	for _, s := range sessions {
		s.close(errors.New("closed"))
	}
}

// session is the open connection to the address, opened if there is none.
func (d *V2) session(ctx context.Context) (*v2Session, error) {
	open := func() *v2Session {
		if s, ok := v2Sessions[d.Addr]; ok {
			select {
			case <-s.done:
			default:
				s.touch()
				return s
			}
		}
		return nil
	}
	v2SessionsMu.Lock()
	s := open()
	v2SessionsMu.Unlock()
	if s != nil {
		return s, nil
	}
	timing := d.Timing
	if timing == (V2Timing{}) {
		timing = DefaultV2Timing
	}
	s, err := dialV2(ctx, d.Addr, timing)
	if err != nil {
		return nil, err
	}
	v2SessionsMu.Lock()
	defer v2SessionsMu.Unlock()
	if other := open(); other != nil {
		// Another caller connected first: use theirs.
		go s.close(errors.New("not needed"))
		return other, nil
	}
	v2Sessions[d.Addr] = s
	return s, nil
}

// v2Session is one connection to a machine.
type v2Session struct {
	addr   string
	id     string
	timing V2Timing
	client *http.Client
	conn   *websocket.Conn

	writeMu sync.Mutex
	// transfer is held through a file transfer: one at a time.
	transfer sync.Mutex

	mu       sync.Mutex
	nextID   int
	channel  byte
	pending  map[int]chan v2Response
	lastPong time.Time
	lastUsed time.Time
	// flameAt is when the machine last pushed a flame alarm; estop is set
	// while its emergency stop is pressed, and framing while the job it was
	// sent is a frame.
	flameAt        time.Time
	estop, framing bool
	firmware       string

	done chan struct{}
	err  error
}

type v2Response struct {
	Type          string          `json:"type"`
	Code          *int            `json:"code"`
	TransactionID *int            `json:"transactionId"`
	Data          json.RawMessage `json:"data"`
	Msg           string          `json:"msg"`
	URL           string          `json:"url"`
}

// v2Push is an event the machine pushes.
type v2Push struct {
	URL  string `json:"url"`
	Data struct {
		Module string          `json:"module"`
		Type   string          `json:"type"`
		Info   json.RawMessage `json:"info"`
	} `json:"data"`
}

func dialV2(ctx context.Context, addr string, timing V2Timing) (*v2Session, error) {
	s := &v2Session{
		addr: addr, id: strconv.FormatInt(time.Now().UnixMilli(), 10), timing: timing,
		// The machine's certificate is its own, signed by no one: it is
		// taken as it is (docs/printing.md#xtool).
		client:  &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}}}, //nolint:gosec // see above
		pending: map[int]chan v2Response{}, done: make(chan struct{}),
		lastPong: time.Now(), lastUsed: time.Now(),
	}
	conn, err := s.dial(ctx, "instruction")
	if err != nil {
		return nil, err
	}
	s.conn = conn
	go s.read()
	var parity struct{}
	if err := s.request(ctx, "GET", "/v1/user/parity", nil, map[string]any{"userID": v2UserID, "userKey": v2UserKey, "timezone": "UTC"}, &parity); err != nil {
		s.close(err)
		return nil, fmt.Errorf("the machine refused the connection: %w", err)
	}
	go s.heartbeat()
	return s, nil
}

// dial opens one of the session's channels.
func (s *v2Session) dial(ctx context.Context, function string) (*websocket.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timing.Request)
	defer cancel()
	u := url.URL{Scheme: "wss", Host: s.addr, Path: v2Path, RawQuery: url.Values{"id": {s.id}, "function": {function}}.Encode()}
	conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{HTTPClient: s.client})
	if err != nil {
		return nil, fmt.Errorf("could not connect to the machine's %s channel: %w", function, err)
	}
	conn.SetReadLimit(MaxFramePayload + frameHeader)
	return conn, nil
}

func (s *v2Session) touch() {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

// close ends the session, failing every request waiting on it.
func (s *v2Session) close(err error) {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return
	default:
	}
	s.err = err
	close(s.done)
	s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close(websocket.StatusNormalClosure, "")
	}
	v2SessionsMu.Lock()
	if v2Sessions[s.addr] == s {
		delete(v2Sessions, s.addr)
	}
	v2SessionsMu.Unlock()
}

// read takes frames from the instruction channel until it closes.
func (s *v2Session) read() {
	var frames FrameReader
	for {
		_, data, err := s.conn.Read(context.Background())
		if err != nil {
			s.close(fmt.Errorf("the connection closed: %w", err))
			return
		}
		frames.Write(data)
		for {
			f, ok := frames.Next()
			if !ok {
				break
			}
			if f.Protocol == ProtocolJSON {
				s.handle(f.Payload)
			}
		}
	}
}

// handle takes one JSON message: an answer, or a push.
func (s *v2Session) handle(payload []byte) {
	var r v2Response
	if json.Unmarshal(payload, &r) != nil {
		return
	}
	id := r.TransactionID
	if id == nil && len(r.Data) > 0 {
		var inner struct {
			TransactionID *int `json:"transactionId"`
		}
		if json.Unmarshal(r.Data, &inner) == nil {
			id = inner.TransactionID
		}
	}
	if r.Type == "response" && id != nil {
		s.mu.Lock()
		if *id == v2PingID {
			s.lastPong = time.Now()
		} else if ch, ok := s.pending[*id]; ok {
			delete(s.pending, *id)
			ch <- r
		}
		s.mu.Unlock()
		return
	}
	var p v2Push
	if json.Unmarshal(payload, &p) == nil {
		s.push(p)
	}
}

// push takes an event the machine pushed.
func (s *v2Session) push(p v2Push) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch p.URL {
	case "/fire/alarm":
		s.flameAt = time.Now()
	case "/emergency_stop/status", "/emergency/status":
		switch p.Data.Type {
		case "VOLTAGE_TRIGGER":
			s.estop = true
		case "RESUME":
			s.estop = false
		}
	case "/work/mode":
		var info struct {
			Mode string `json:"mode"`
		}
		if json.Unmarshal(p.Data.Info, &info) == nil {
			s.modeSeen(info.Mode)
		}
	case "/device/status":
		var info string
		_ = json.Unmarshal(p.Data.Info, &info)
		switch p.Data.Type {
		case "WORK_PREPARED", "WORK_STARTED":
			if info == "framing" {
				s.framing = true
			}
		case "WORK_FINISHED":
			s.framing = false
		}
	case "/device/info":
		var info struct {
			Firmware struct {
				PackageVersion string `json:"package_version"`
			} `json:"firmware"`
		}
		if json.Unmarshal(p.Data.Info, &info) == nil && info.Firmware.PackageVersion != "" && s.firmware == "" {
			s.firmware = info.Firmware.PackageVersion
		}
	}
}

// modeSeen notes the machine's mode, held with mu: back to idle ends a
// frame.
func (s *v2Session) modeSeen(mode string) {
	switch mode {
	case "P_IDLE":
		s.framing = false
	case "P_EMERGENCY_STOP":
		s.estop = true
	}
}

// heartbeat pings the machine, and closes the session when it stops
// answering or nothing has used it for a while.
func (s *v2Session) heartbeat() {
	t := time.NewTicker(s.timing.Heartbeat)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
		}
		s.mu.Lock()
		pong, used := s.lastPong, s.lastUsed
		s.mu.Unlock()
		switch {
		case time.Since(pong) > s.timing.PongTimeout:
			s.close(errors.New("the machine stopped answering pings"))
			return
		case time.Since(used) > s.timing.Idle:
			s.close(errors.New("unused"))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), s.timing.Request)
		err := s.send(ctx, v2PingID, "GET", "/v1/user/ping", nil, nil)
		cancel()
		if err != nil {
			s.close(err)
			return
		}
	}
}

// send writes one request.
func (s *v2Session) send(ctx context.Context, id int, method, path string, params, data any) error {
	if params == nil {
		params = map[string]any{}
	}
	if data == nil {
		data = map[string]any{}
	}
	body, err := json.Marshal(map[string]any{
		"type": "request", "method": method, "url": path, "params": params, "data": data,
		"timestamp": time.Now().UnixMilli(), "transactionId": id,
	})
	if err != nil {
		return err
	}
	frame, err := EncodeFrame(ProtocolJSON, body, false)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.Write(ctx, websocket.MessageBinary, frame)
}

// request makes a request and waits for its answer, decoding its data into
// out. A code other than 0 is a *DeviceError.
func (s *v2Session) request(ctx context.Context, method, path string, params, data, out any) error {
	ctx, cancel := context.WithTimeout(ctx, s.timing.Request)
	defer cancel()
	ch := make(chan v2Response, 1)
	s.mu.Lock()
	s.nextID++
	if s.nextID > v2MaxID {
		s.nextID = 1
	}
	id := s.nextID
	s.pending[id] = ch
	s.lastUsed = time.Now()
	s.mu.Unlock()
	forget := func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}
	if err := s.send(ctx, id, method, path, params, data); err != nil {
		forget()
		return fmt.Errorf("%s: %w", path, err)
	}
	select {
	case r := <-ch:
		if r.Code != nil && *r.Code != 0 {
			return &DeviceError{URL: path, Code: *r.Code, Msg: r.Msg}
		}
		if out != nil && len(r.Data) > 0 && string(r.Data) != "null" {
			if err := json.Unmarshal(r.Data, out); err != nil {
				return fmt.Errorf("%s: the machine's answer: %w", path, err)
			}
		}
		return nil
	case <-ctx.Done():
		forget()
		return fmt.Errorf("%s: the machine did not answer: %w", path, ctx.Err())
	case <-s.done:
		return fmt.Errorf("%s: %w", path, s.err)
	}
}

// Request makes a request of the machine.
func (d *V2) Request(ctx context.Context, method, path string, params, data, out any) error {
	s, err := d.session(ctx)
	if err != nil {
		return err
	}
	return s.request(ctx, method, path, params, data, out)
}

func (d *V2) Firmware(ctx context.Context) (string, error) {
	s, err := d.session(ctx)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	fw := s.firmware
	s.mu.Unlock()
	if fw != "" {
		return fw, nil
	}
	var info struct {
		Firmware json.RawMessage `json:"firmware"`
	}
	if err := s.request(ctx, "GET", "/v1/device/machineInfo", nil, nil, &info); err != nil {
		return "", err
	}
	fw = firmwareOf(info.Firmware)
	s.mu.Lock()
	if fw == "" {
		fw = s.firmware
	}
	s.firmware = fw
	s.mu.Unlock()
	if fw == "" {
		return "", errors.New("the machine did not say which firmware it runs")
	}
	return fw, nil
}

// firmwareOf reads the firmware version from a machine's info: an object
// with its package_version, a string, or a list of parts with versions.
func firmwareOf(raw json.RawMessage) string {
	var obj struct {
		PackageVersion string `json:"package_version"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.PackageVersion != "" {
		return obj.PackageVersion
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &list) == nil {
		for _, p := range list {
			if p.Version != "" {
				return p.Version
			}
		}
	}
	return ""
}

func (d *V2) Status(ctx context.Context) (Status, error) {
	s, err := d.session(ctx)
	if err != nil {
		return Status{}, err
	}
	var rt struct {
		CurMode struct {
			Mode string `json:"mode"`
		} `json:"curMode"`
	}
	if err := s.request(ctx, "GET", "/v1/device/runtime-infos", nil, nil, &rt); err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	s.modeSeen(rt.CurMode.Mode)
	framing := s.framing
	s.mu.Unlock()
	st := Status{Mode: rt.CurMode.Mode}
	var problem string
	st.State, problem = StateOf(rt.CurMode.Mode, framing)
	if st.State == StateRunning || st.State == StateFraming || st.State == StatePaused {
		var p struct {
			Progress float64 `json:"progress"`
		}
		if err := s.request(ctx, "GET", "/v1/processing/progress", nil, nil, &p); err == nil {
			st.Progress = progressOf(p.Progress)
		}
	}
	if st.State == StateDone {
		st.Progress = 1
	}
	for _, p := range []struct{ reading, kind string }{{ReadingLid, "gap"}, {ReadingSafetyKey, "machine_lock"}, {ReadingDrawer, "drawer"}} {
		var v struct {
			State string `json:"state"`
		}
		err := s.request(ctx, "GET", "/v1/peripheral/param", map[string]any{"type": p.kind}, nil, &v)
		if r, ok := readingOf(p.reading, v.State, err); ok {
			st.Readings = append(st.Readings, r)
		}
	}
	s.mu.Lock()
	flameAt, estop := s.flameAt, s.estop
	s.mu.Unlock()
	if hold := d.flameHold(); !flameAt.IsZero() && time.Since(flameAt) < hold {
		st.Readings = append(st.Readings, Reading{Name: ReadingFlame, Message: fmt.Sprintf("The machine reported a flame at %s; BAM! waits %s after one", flameAt.Format("15:04:05"), hold)})
	} else {
		st.Readings = append(st.Readings, Reading{Name: ReadingFlame, OK: true})
	}
	if estop && problem == "" {
		problem = "The emergency stop is pressed"
	}
	st.Readings = append(st.Readings, Reading{Name: ReadingMachine, OK: problem == "", Message: problem})
	return st, nil
}

func (d *V2) SendJob(ctx context.Context, gcode []byte, start bool) error {
	s, err := d.session(ctx)
	if err != nil {
		return err
	}
	if err := s.upload(ctx, v2FileGcode, "tmp.gcode", gcode); err != nil {
		return err
	}
	if err := s.request(ctx, "PUT", "/v1/processing/upload/config", nil, map[string]any{"fileType": "txt", "autoStart": 0}, nil); err != nil {
		return err
	}
	s.mu.Lock()
	s.framing = false
	s.mu.Unlock()
	if start {
		return s.control(ctx, ActionStart)
	}
	return nil
}

func (d *V2) Frame(ctx context.Context, gcode []byte) error {
	s, err := d.session(ctx)
	if err != nil {
		return err
	}
	if err := s.upload(ctx, v2FileGcode, "tmpFrame.gcode", gcode); err != nil {
		return err
	}
	if err := s.request(ctx, "PUT", "/v1/processing/upload/config", nil, map[string]any{"fileType": "txt", "autoStart": 0}, nil); err != nil {
		return err
	}
	s.mu.Lock()
	s.framing = true
	s.mu.Unlock()
	return s.control(ctx, ActionStart)
}

func (d *V2) Control(ctx context.Context, action string) error {
	s, err := d.session(ctx)
	if err != nil {
		return err
	}
	return s.control(ctx, action)
}

// control drives the job: WS-V2 resumes with start, and cancels with stop.
func (s *v2Session) control(ctx context.Context, action string) error {
	if action == ActionResume {
		action = ActionStart
	}
	return s.request(ctx, "PUT", "/v1/processing/state", map[string]any{"action": action}, nil, nil)
}

// upload sends a file to the machine (PROTOCOL.md, "File transfer"): the
// file is announced on the instruction channel with its size, MD5 and a
// channel number; the machine asks for it a window at a time on the
// file_stream channel, and is sent it in packets of the size it agreed to;
// then the transfer is finished on the instruction channel.
func (s *v2Session) upload(ctx context.Context, fileType int, name string, content []byte) error {
	s.transfer.Lock()
	defer s.transfer.Unlock()
	// A minute, and a second more for every 100 KB.
	ctx, cancel := context.WithTimeout(ctx, time.Minute+time.Duration(len(content)/(100<<10))*time.Second)
	defer cancel()
	conn, err := s.dial(ctx, "file_stream")
	if err != nil {
		return err
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
	sum := md5.Sum(content)
	var agreed struct {
		PacketSize int `json:"packetsize"`
	}
	if err := s.request(ctx, "PUT", "/v1/filetransfer/upload", nil, map[string]any{
		"filetype": fileType, "filename": name, "filesize": len(content),
		"digesttype": 1, "digestdata": hex.EncodeToString(sum[:]), "channel": channel, "packetsize": v2PacketSize,
	}, &agreed); err != nil {
		return err
	}
	packet := v2PacketSize
	if agreed.PacketSize > 0 && agreed.PacketSize < packet {
		packet = agreed.PacketSize
	}

	sent := int64(0)
	var frames FrameReader
	for sent < int64(len(content)) {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("sending %s: %w", name, err)
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
			if err != nil || p.Op != FileRequest || p.Channel != channel {
				continue
			}
			if p.Offset < 0 || p.Offset > int64(len(content)) || p.Window <= 0 {
				return fmt.Errorf("sending %s: the machine asked for %d bytes at %d of %d", name, p.Window, p.Offset, len(content))
			}
			end := min(p.Offset+int64(p.Window), int64(len(content)))
			for at := p.Offset; at < end; at += int64(packet) {
				chunk := content[at:min(at+int64(packet), end)]
				frame, err := EncodeFrame(ProtocolFileTransfer, EncodeFilePacket(FilePacket{Op: FileData, Channel: channel, Offset: at, Data: chunk}), true)
				if err != nil {
					return err
				}
				if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
					return fmt.Errorf("sending %s: %w", name, err)
				}
			}
			sent = max(sent, end)
		}
	}
	if err := s.request(ctx, "PUT", "/v1/filetransfer/finish", nil, map[string]any{"code": 0, "message": "file transfer finish", "channel": channel}, nil); err != nil {
		return err
	}
	conn.Close(websocket.StatusNormalClosure, "")
	return nil
}
