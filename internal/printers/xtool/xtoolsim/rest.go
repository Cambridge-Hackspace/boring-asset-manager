package xtoolsim

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/xtool"
)

// REST is a simulated REST V1 machine, in one of the dialects.
type REST struct {
	Server  *httptest.Server
	Dialect string

	mu sync.Mutex
	// Firmware is the version it reports.
	Firmware string
	// Mode is its P_* mode; the M1's dialect reports it in its own words.
	Mode string
	// Peripherals are the states of its peripherals by name (gap,
	// machine_lock, drawer); one missing answers 404.
	Peripherals map[string]string
	Progress    float64

	files   map[string][]byte
	loaded  bool
	actions []string
}

// NewREST starts a simulated machine, idle with every peripheral fine.
func NewREST(dialect, firmware string) *REST {
	s := &REST{Dialect: dialect, Firmware: firmware, Mode: "P_IDLE", Peripherals: map[string]string{"gap": "on", "machine_lock": "on", "drawer": "on"}, files: map[string][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Addr is its host:port.
func (s *REST) Addr() string {
	return strings.TrimPrefix(s.Server.URL, "http://")
}

// Close stops it.
func (s *REST) Close() { s.Server.Close() }

// Set changes it under its lock.
func (s *REST) Set(f func(s *REST)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

// File is a file it was sent.
func (s *REST) File(name string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.files[name]
}

// Actions are the job actions it took, in order.
func (s *REST) Actions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.actions)
}

// Finish ends the job.
func (s *REST) Finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Mode, s.Progress, s.loaded = "P_FINISH", 1, false
}

// cncModes are the M1's words for the P_* modes.
var cncModes = map[string]string{"P_IDLE": "idle", "P_WORKING": "running", "P_PAUSE": "pause", "P_FINISH": "finish", "P_ERROR": "alarm"}

func (s *REST) serve(w http.ResponseWriter, r *http.Request) {
	reply := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	wrapped := func(data any) { reply(map[string]any{"code": 0, "msg": "ok", "data": data}) }
	s.mu.Lock()
	defer s.mu.Unlock()
	cnc := s.Dialect == xtool.DialectCNC
	q := r.URL.Query()
	action := func(name string) {
		ok := false
		switch name {
		case "start":
			if s.loaded && (s.Mode == "P_IDLE" || s.Mode == "P_FINISH" || s.Mode == "P_ONLINE_READY_WORK") {
				s.Mode, s.Progress, ok = "P_WORKING", 0, true
			} else if s.Mode == "P_PAUSE" && cnc {
				s.Mode, ok = "P_WORKING", true
			}
		case "resume":
			if s.Mode == "P_PAUSE" {
				s.Mode, ok = "P_WORKING", true
			}
		case "pause":
			if s.Mode == "P_WORKING" {
				s.Mode, ok = "P_PAUSE", true
			}
		case "stop":
			s.Mode, s.loaded, ok = "P_IDLE", false, true
		}
		if !ok {
			reply(map[string]any{"result": "fail"})
			return
		}
		s.actions = append(s.actions, name)
		reply(map[string]any{"result": "ok"})
	}
	upload := func() {
		f, h, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "no file", http.StatusBadRequest)
			return
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		s.files[h.Filename], s.loaded = b, true
		reply(map[string]any{"result": "ok"})
	}
	switch {
	case !cnc && r.URL.Path == "/device/machineInfo":
		wrapped(map[string]any{"deviceName": "Simulated", "firmware": map[string]any{"package_version": s.Firmware}})
	case cnc && r.URL.Path == "/system" && q.Get("action") == "version_v2":
		reply(map[string]any{"package_version": s.Firmware})
	case !cnc && r.URL.Path == "/device/runningStatus":
		wrapped(map[string]any{"curMode": map[string]any{"mode": s.Mode}})
	case cnc && r.URL.Path == "/cnc/status":
		mode, ok := cncModes[s.Mode]
		if !ok {
			mode = strings.ToLower(s.Mode)
		}
		reply(map[string]any{"mode": mode})
	case !cnc && r.URL.Path == "/processing/progress":
		wrapped(map[string]any{"progress": s.Progress * 100, "workingTime": 1})
	case strings.HasPrefix(r.URL.Path, "/peripheral/") && q.Get("action") == "get":
		state, ok := s.Peripherals[strings.TrimPrefix(r.URL.Path, "/peripheral/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		wrapped(map[string]any{"state": state})
	case !cnc && r.Method == http.MethodPost && r.URL.Path == "/processing/upload":
		upload()
	case cnc && r.Method == http.MethodPost && r.URL.Path == "/cnc/data" && q.Get("action") == "upload":
		upload()
	case !cnc && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/processing/"):
		action(strings.TrimPrefix(r.URL.Path, "/processing/"))
	case cnc && r.Method == http.MethodGet && r.URL.Path == "/cnc/data":
		action(q.Get("action"))
	default:
		http.NotFound(w, r)
	}
}
