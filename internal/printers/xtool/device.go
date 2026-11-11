package xtool

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Device is an xTool machine, reached over its protocol.
type Device interface {
	// Firmware is the version of the machine's firmware.
	Firmware(ctx context.Context) (string, error)
	// Status is what the machine reports now.
	Status(ctx context.Context) (Status, error)
	// SendJob sends a job's G-code, leaving it ready for the machine's
	// button, or starting it when start is set.
	SendJob(ctx context.Context, gcode []byte, start bool) error
	// Frame sends G-code that traces a job's outline with the laser off,
	// and runs it.
	Frame(ctx context.Context, gcode []byte) error
	// Control starts (or resumes), pauses or stops the job.
	Control(ctx context.Context, action string) error
}

// Control actions.
const (
	ActionStart = "start"
	ActionPause = "pause"
	ActionStop  = "stop"
	// ActionResume resumes a paused job: start, on machines with no verb
	// of its own.
	ActionResume = "resume"
)

// States a machine is in, named as printers.Machine* names them.
const (
	StateIdle    = "idle"
	StateFraming = "framing"
	StateReady   = "ready"
	StateRunning = "running"
	StatePaused  = "paused"
	StateDone    = "done"
	StateFailed  = "failed"
)

// Readings, named as printers.Interlock* names them.
const (
	ReadingLid       = "lid"
	ReadingSafetyKey = "safety_key"
	ReadingDrawer    = "drawer"
	ReadingFlame     = "flame"
	ReadingMachine   = "machine"
)

// Status is what a machine reports.
type Status struct {
	// Mode is the machine's own word for its state, such as P_WORKING.
	Mode string
	// State is Mode in BAM!'s terms.
	State string
	// Progress is how much of the job is done, from 0 to 1.
	Progress float64
	// Readings are its interlocks, as it reports them.
	Readings []Reading
}

// Reading is one interlock as a machine reports it.
type Reading struct {
	Name    string
	OK      bool
	Message string
}

// ErrNotSupported is wrapped when a machine says it has no such thing.
var ErrNotSupported = errors.New("not supported by this machine")

// DeviceError is a machine's refusal of a request, with its code.
type DeviceError struct {
	URL  string
	Code int
	Msg  string
}

func (e *DeviceError) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("%s: code %d: %s", e.URL, e.Code, e.Msg)
	}
	return fmt.Sprintf("%s: code %d", e.URL, e.Code)
}

// Unwrap is ErrNotSupported for the codes WS-V2 firmware answers for a
// peripheral it has not got: -3 (no such action) and 10 (device not
// support).
func (e *DeviceError) Unwrap() error {
	if e.Code == -3 || e.Code == 10 {
		return ErrNotSupported
	}
	return nil
}

// StateOf is a machine's mode in BAM!'s terms, and the problem it means for
// starting a job, if any. framing is set when the job the machine is
// working on is a frame. Modes are WS-V2's and REST V1's P_* modes, and the
// M1's words.
func StateOf(mode string, framing bool) (state, problem string) {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "P_IDLE", "P_SLEEP", "P_WORK", "IDLE":
		return StateIdle, ""
	case "P_READY", "P_ONLINE_READY_WORK", "P_OFFLINE_READY_WORK":
		return StateReady, ""
	case "P_WORKING", "RUNNING", "PROCESSING":
		if framing {
			return StateFraming, ""
		}
		return StateRunning, ""
	case "P_FRAMING", "P_FRAME_READY", "FRAMING":
		return StateFraming, ""
	case "P_PAUSE", "PAUSE", "PAUSED":
		return StatePaused, ""
	case "P_WORK_DONE", "P_FINISH", "FINISH", "FINISHED":
		return StateDone, ""
	case "P_ERROR", "ALARM", "ERROR":
		return StateFailed, "The machine reports an error"
	case "P_EMERGENCY_STOP":
		return StateFailed, "The emergency stop is pressed"
	case "P_BOOT":
		return StateIdle, "The machine is starting up"
	case "P_UPGRADE", "UPGRADING":
		return StateIdle, "The machine is updating its firmware"
	case "P_MEASURE":
		return StateIdle, "The machine is measuring"
	case "HOMING":
		return StateIdle, "The machine is homing"
	case "":
		return StateIdle, "The machine did not say what it is doing"
	}
	return StateIdle, fmt.Sprintf("The machine reports %q, which BAM! does not know", mode)
}

// readingOf is a peripheral's on or off as an interlock: on is closed, or
// inserted, for the lid, the safety key and the drawer alike.
func readingOf(name, state string, err error) (Reading, bool) {
	what := map[string]string{ReadingLid: "The lid", ReadingSafetyKey: "The safety key", ReadingDrawer: "The drawer"}[name]
	switch {
	case errors.Is(err, ErrNotSupported):
		return Reading{}, false
	case err != nil:
		return Reading{Name: name, Message: fmt.Sprintf("%s could not be read: %v", what, err)}, true
	}
	switch strings.ToLower(state) {
	case "on":
		return Reading{Name: name, OK: true}, true
	case "off":
		msg := map[string]string{ReadingLid: "The lid is open", ReadingSafetyKey: "The safety key is out", ReadingDrawer: "The drawer is open"}[name]
		return Reading{Name: name, Message: msg}, true
	}
	return Reading{Name: name, Message: fmt.Sprintf("%s reports %q, which BAM! does not know", what, state)}, true
}

// progressOf is a machine's progress from 0 to 1: it reports a percentage.
func progressOf(v float64) float64 {
	return min(max(v/100, 0), 1)
}
