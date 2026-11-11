package xtool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// REST V1 (PROTOCOL.md, "REST API family"): JSON over HTTP on port 8080.
// Answers are JSON, sometimes wrapped as {"code": 0, "data": …}; a code
// other than 0, or a result of "fail", is a refusal.

// REST is a machine reached over REST V1, at a base URL such as
// http://192.0.2.7:8080.
type REST struct {
	Base    string
	Dialect string
	Client  *http.Client
}

// restTimeout bounds a request other than an upload.
const restTimeout = 15 * time.Second

func (d *REST) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// do makes a request and reads its answer, unwrapping its data.
func (d *REST) do(req *http.Request) (json.RawMessage, error) {
	resp, err := d.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	path := req.URL.Path
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", path, ErrNotSupported)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	var env struct {
		Code   *int            `json:"code"`
		Result string          `json:"result"`
		Msg    string          `json:"msg"`
		Data   json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &env) != nil {
		// Not an object: a bare answer, such as a version string.
		return body, nil
	}
	if env.Code != nil && *env.Code != 0 {
		return nil, &DeviceError{URL: path, Code: *env.Code, Msg: env.Msg}
	}
	if strings.EqualFold(env.Result, "fail") {
		return nil, fmt.Errorf("%s: the machine refused", path)
	}
	if len(env.Data) > 0 && string(env.Data) != "null" {
		return env.Data, nil
	}
	return body, nil
}

func (d *REST) get(ctx context.Context, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, restTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.Base+path, nil)
	if err != nil {
		return err
	}
	data, err := d.do(req)
	if err != nil || out == nil || len(data) == 0 {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: the machine's answer: %w", path, err)
	}
	return nil
}

func (d *REST) Firmware(ctx context.Context) (string, error) {
	var fw string
	if d.Dialect == DialectCNC {
		var v struct {
			PackageVersion string `json:"package_version"`
		}
		if err := d.get(ctx, "/system?action=version_v2", &v); err != nil {
			return "", err
		}
		fw = v.PackageVersion
	} else {
		var info struct {
			Firmware json.RawMessage `json:"firmware"`
		}
		if err := d.get(ctx, "/device/machineInfo", &info); err != nil {
			return "", err
		}
		fw = firmwareOf(info.Firmware)
	}
	if fw == "" {
		return "", errors.New("the machine did not say which firmware it runs")
	}
	return fw, nil
}

func (d *REST) Status(ctx context.Context) (Status, error) {
	var st Status
	if d.Dialect == DialectCNC {
		var v struct {
			Mode string `json:"mode"`
		}
		if err := d.get(ctx, "/cnc/status", &v); err != nil {
			return Status{}, err
		}
		st.Mode = v.Mode
	} else {
		var v struct {
			CurMode struct {
				Mode string `json:"mode"`
			} `json:"curMode"`
		}
		if err := d.get(ctx, "/device/runningStatus", &v); err != nil {
			return Status{}, err
		}
		st.Mode = v.CurMode.Mode
	}
	var problem string
	st.State, problem = StateOf(st.Mode, false)
	if d.Dialect != DialectCNC && (st.State == StateRunning || st.State == StatePaused) {
		var p struct {
			Progress float64 `json:"progress"`
		}
		if err := d.get(ctx, "/processing/progress", &p); err == nil {
			st.Progress = progressOf(p.Progress)
		}
	}
	if st.State == StateDone {
		st.Progress = 1
	}
	for _, p := range []struct{ reading, path string }{{ReadingLid, "gap"}, {ReadingSafetyKey, "machine_lock"}, {ReadingDrawer, "drawer"}} {
		var v struct {
			State string `json:"state"`
		}
		err := d.get(ctx, "/peripheral/"+p.path+"?action=get", &v)
		if r, ok := readingOf(p.reading, v.State, err); ok {
			st.Readings = append(st.Readings, r)
		}
	}
	st.Readings = append(st.Readings, Reading{Name: ReadingMachine, OK: problem == "", Message: problem})
	return st, nil
}

func (d *REST) SendJob(ctx context.Context, gcode []byte, start bool) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "tmp.gcode")
	if err != nil {
		return err
	}
	if _, err := part.Write(gcode); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	path := "/processing/upload"
	if d.Dialect == DialectCNC {
		path = "/cnc/data?action=upload&zip=false&id=-1"
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute+time.Duration(len(gcode)/(100<<10))*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.Base+path, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if _, err := d.do(req); err != nil {
		return fmt.Errorf("sending the job: %w", err)
	}
	if start {
		return d.Control(ctx, ActionStart)
	}
	return nil
}

// Frame is refused: how a REST V1 machine frames is not published.
func (d *REST) Frame(context.Context, []byte) error {
	return fmt.Errorf("framing over REST V1: %w", ErrNotSupported)
}

func (d *REST) Control(ctx context.Context, action string) error {
	path := "/processing/" + action
	if d.Dialect == DialectCNC {
		// The M1 resumes with start.
		if action == ActionResume {
			action = ActionStart
		}
		path = "/cnc/data?action=" + action
	}
	return d.get(ctx, path, nil)
}
