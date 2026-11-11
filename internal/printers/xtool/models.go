package xtool

import (
	"fmt"
	"strconv"
	"strings"
)

// Protocol is the family of protocol a machine's firmware speaks.
type Protocol string

const (
	// WSV2 is the protocol of xTool's newer firmware: JSON requests and
	// file transfers over TLS WebSockets on port 28900.
	WSV2 Protocol = "ws-v2"
	// RESTV1 is the protocol of their older firmware: HTTP on port 8080.
	RESTV1 Protocol = "rest-v1"
)

// Each protocol's standard port.
const (
	PortWSV2   = 28900
	PortRESTV1 = 8080
)

// REST V1 dialects: most models take jobs at /processing/*, and report
// their state at /device/runningStatus; the M1 takes them at /cnc/data and
// reports at /cnc/status.
const (
	DialectProcessing = "processing"
	DialectCNC        = "cnc"
)

// Model is one xTool model on one protocol.
type Model struct {
	ID       string
	Protocol Protocol
	// Dialect is the REST V1 dialect.
	Dialect string
	// WorkWidthMM and WorkHeightMM are the work area, as xTool publishes it.
	WorkWidthMM, WorkHeightMM float64
	// MirrorX is set when the machine's X axis runs right to left as you
	// face it, so a job's X is turned around for it.
	MirrorX bool
	// StartButton is set when jobs are sent ready for the machine's own
	// button; Frames when it traces a job's outline.
	StartButton, Frames bool
	// Firmware is the firmware BAM! knows for it on this protocol; any
	// other is refused unless the printer allows it.
	Firmware []FirmwareRange
	// Camera is how its camera over the bed is asked for a photo: by name
	// over WS-V2, by stream over REST V1; empty where PROTOCOL.md gives no
	// way to.
	Camera string
}

// Model IDs.
const (
	ModelF1          = "F1"
	ModelF1Ultra     = "F1 Ultra"
	ModelF1V1        = "F1 (V1 firmware)"
	ModelF1UltraV1   = "F1 Ultra (V1 firmware)"
	ModelP2V1        = "P2 (V1 firmware)"
	ModelM1V1        = "M1 (V1 firmware)"
	firmwareF1       = "40.51"
	firmwareF1Ultra  = "40.52"
	thresholdF1      = "40.51.020.04"
	thresholdF1Ultra = "40.52.016.05"
)

// Models are the models BAM! drives. The thresholds between V1 and V2
// firmware are those PROTOCOL.md gives, from xTool's release notes for the
// "communication framework upgrade"; nothing here has run on a real machine
// (docs/printing.md#xtool).
func Models() []Model {
	return []Model{
		{ID: ModelF1, Protocol: WSV2, WorkWidthMM: 115, WorkHeightMM: 115, StartButton: true, Frames: true,
			Firmware: []FirmwareRange{{Line: firmwareF1, Min: thresholdF1}}},
		{ID: ModelF1Ultra, Protocol: WSV2, WorkWidthMM: 220, WorkHeightMM: 220, MirrorX: true, StartButton: true, Frames: true,
			Firmware: []FirmwareRange{{Line: firmwareF1Ultra, Min: thresholdF1Ultra}}, Camera: "main"},
		// PROTOCOL.md names the overview camera's stream 0 for the F1, the
		// F1 Ultra and the P2 on REST V1, and no snapshot route for the F1
		// on WS-V2, or for the M1.
		{ID: ModelF1V1, Protocol: RESTV1, Dialect: DialectProcessing, WorkWidthMM: 115, WorkHeightMM: 115,
			Firmware: []FirmwareRange{{Line: firmwareF1, Below: thresholdF1}}, Camera: "0"},
		{ID: ModelF1UltraV1, Protocol: RESTV1, Dialect: DialectProcessing, WorkWidthMM: 220, WorkHeightMM: 220, MirrorX: true,
			Firmware: []FirmwareRange{{Line: firmwareF1Ultra, Below: thresholdF1Ultra}}, Camera: "0"},
		// xTool has not published the P2's or the M1's firmware lines, so
		// no firmware is known for them: each must be allowed.
		{ID: ModelP2V1, Protocol: RESTV1, Dialect: DialectProcessing, WorkWidthMM: 600, WorkHeightMM: 305, Camera: "0"},
		{ID: ModelM1V1, Protocol: RESTV1, Dialect: DialectCNC, WorkWidthMM: 385, WorkHeightMM: 300},
	}
}

// LookupModel finds a model by ID.
func LookupModel(id string) (Model, bool) {
	for _, m := range Models() {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// FirmwareRange is firmware on one line (its first two numbers, which name
// the model), from Min, when set, and before Below, when set.
type FirmwareRange struct {
	Line  string
	Min   string
	Below string
}

// Contains reports whether a firmware version is in the range.
func (r FirmwareRange) Contains(version string) bool {
	v, ok := ParseVersion(version)
	if !ok {
		return false
	}
	line, _ := ParseVersion(r.Line)
	if len(v) < len(line) || compareVersions(v[:len(line)], line) != 0 {
		return false
	}
	if r.Min != "" {
		min, _ := ParseVersion(r.Min)
		if compareVersions(v, min) < 0 {
			return false
		}
	}
	if r.Below != "" {
		below, _ := ParseVersion(r.Below)
		if compareVersions(v, below) >= 0 {
			return false
		}
	}
	return true
}

// String is the range in words: "40.51.020.04 and later on 40.51".
func (r FirmwareRange) String() string {
	switch {
	case r.Min != "" && r.Below != "":
		return fmt.Sprintf("%s up to %s on %s", r.Min, r.Below, r.Line)
	case r.Min != "":
		return fmt.Sprintf("%s and later on %s", r.Min, r.Line)
	case r.Below != "":
		return fmt.Sprintf("before %s on %s", r.Below, r.Line)
	}
	return "any on " + r.Line
}

// KnownFirmware reports whether a model's firmware is one BAM! knows.
func (m Model) KnownFirmware(version string) bool {
	for _, r := range m.Firmware {
		if r.Contains(version) {
			return true
		}
	}
	return false
}

// KnownFirmwareText is the firmware BAM! knows for a model, in words.
func (m Model) KnownFirmwareText() string {
	if len(m.Firmware) == 0 {
		return "none"
	}
	parts := make([]string, len(m.Firmware))
	for i, r := range m.Firmware {
		parts[i] = r.String()
	}
	return strings.Join(parts, "; ")
}

// ParseVersion reads a firmware version's numbers: "V40.52.016.2020.01.ht5"
// is 40, 52, 16, 2020, 1, stopping at the first part not a number. It needs
// at least two.
func ParseVersion(s string) ([]int, bool) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "V"), "v")
	var out []int
	for part := range strings.SplitSeq(s, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			break
		}
		out = append(out, n)
	}
	return out, len(out) >= 2
}

// compareVersions compares two versions number by number, a missing number
// counting as zero.
func compareVersions(a, b []int) int {
	for i := range max(len(a), len(b)) {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
