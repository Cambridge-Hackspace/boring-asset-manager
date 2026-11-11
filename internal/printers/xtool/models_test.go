package xtool

import "testing"

func TestParseVersion(t *testing.T) {
	for in, want := range map[string][]int{
		"40.52.016.2020.01.ht5": {40, 52, 16, 2020, 1},
		"V40.31.006.01 B2":      {40, 31, 6, 1},
		" 40.51.020.04 ":        {40, 51, 20, 4},
	} {
		got, ok := ParseVersion(in)
		if !ok || len(got) != len(want) {
			t.Errorf("%q: %v %v", in, got, ok)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%q: %v", in, got)
			}
		}
	}
	for _, bad := range []string{"", "40", "beta", "x.1.2"} {
		if _, ok := ParseVersion(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestKnownFirmware(t *testing.T) {
	f1, _ := LookupModel(ModelF1)
	f1v1, _ := LookupModel(ModelF1V1)
	ultra, _ := LookupModel(ModelF1Ultra)
	p2, _ := LookupModel(ModelP2V1)
	for _, c := range []struct {
		m    Model
		fw   string
		want bool
	}{
		{f1, "40.51.020.04", true}, // the first V2 firmware
		{f1, "40.51.021.00", true},
		{f1, "40.51.020.03", false}, // V1 firmware, on the V2 model
		{f1v1, "40.51.020.03", true},
		{f1v1, "40.51.020.04", false},
		{f1, "40.52.016.05", false}, // the F1 Ultra's, on an F1
		{ultra, "40.52.016.2020.01.ht5", true},
		{ultra, "40.52.016.04", false},
		{ultra, "garbage", false},
		{p2, "40.30.001.00", false}, // nothing is known for the P2
	} {
		if got := c.m.KnownFirmware(c.fw); got != c.want {
			t.Errorf("%s %s: known %v, want %v", c.m.ID, c.fw, got, c.want)
		}
	}
	if got := f1.KnownFirmwareText(); got != "40.51.020.04 and later on 40.51" {
		t.Errorf("text %q", got)
	}
	if got := p2.KnownFirmwareText(); got != "none" {
		t.Errorf("P2 text %q", got)
	}
}

func TestStateOf(t *testing.T) {
	for _, c := range []struct {
		mode    string
		framing bool
		state   string
		problem bool
	}{
		{"P_IDLE", false, StateIdle, false},
		{"P_ONLINE_READY_WORK", false, StateReady, false},
		{"P_OFFLINE_READY_WORK", false, StateReady, false},
		{"P_WORKING", false, StateRunning, false},
		{"P_WORKING", true, StateFraming, false},
		{"P_PAUSE", false, StatePaused, false},
		{"P_FINISH", false, StateDone, false},
		{"P_ERROR", false, StateFailed, true},
		{"P_EMERGENCY_STOP", false, StateFailed, true},
		{"P_UPGRADE", false, StateIdle, true},
		{"running", false, StateRunning, false}, // the M1's words
		{"alarm", false, StateFailed, true},
		{"P_SOMETHING_NEW", false, StateIdle, true},
		{"", false, StateIdle, true},
	} {
		state, problem := StateOf(c.mode, c.framing)
		if state != c.state || (problem != "") != c.problem {
			t.Errorf("%s (framing %v): %s %q", c.mode, c.framing, state, problem)
		}
	}
}
