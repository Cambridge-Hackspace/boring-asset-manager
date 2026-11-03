package cron

import (
	"testing"
	"time"
)

func TestParseRefusesWhatIsNotASchedule(t *testing.T) {
	for _, bad := range []string{"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8",
		"*/0 * * * *", "5-2 * * * *", "a * * * *", "1-x * * * *"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestMatches(t *testing.T) {
	at := func(s string) time.Time {
		tm, err := time.Parse("2006-01-02 15:04", s)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	for _, c := range []struct {
		expr, when string
		want       bool
	}{
		{"* * * * *", "2026-11-02 12:34", true},
		{"0 9 * * 1", "2026-11-02 09:00", true}, // a Monday
		{"0 9 * * 1", "2026-11-03 09:00", false},
		{"0 9 * * 1", "2026-11-02 09:01", false},
		{"*/15 * * * *", "2026-11-02 12:45", true},
		{"*/15 * * * *", "2026-11-02 12:46", false},
		{"0 8-17/3 * * *", "2026-11-02 14:00", true},
		{"0 8-17/3 * * *", "2026-11-02 15:00", false},
		{"0 0 1,15 * *", "2026-11-15 00:00", true},
		{"0 0 * * 7", "2026-11-08 00:00", true}, // Sunday as 7
		{"0 0 * * 0", "2026-11-08 00:00", true},
		// Both days restricted: either matches.
		{"0 0 1 * 1", "2026-11-02 00:00", true},
		{"0 0 1 * 1", "2026-11-01 00:00", true},
		{"0 0 1 * 1", "2026-11-03 00:00", false},
		{"30 2 * 12 *", "2026-12-24 02:30", true},
		{"30 2 * 12 *", "2026-11-24 02:30", false},
	} {
		s, err := Parse(c.expr)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if got := s.Matches(at(c.when)); got != c.want {
			t.Errorf("%q at %s: %v, want %v", c.expr, c.when, got, c.want)
		}
	}
}

func TestLatestReadsTheScheduleInItsTimeZone(t *testing.T) {
	s, err := Parse("0 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 9:00 in New York on November 2nd is 14:00 UTC.
	upTo := time.Date(2026, 11, 2, 14, 30, 0, 0, time.UTC)
	got, ok := s.Latest(upTo.Add(-time.Hour), upTo, ny)
	if !ok || !got.Equal(time.Date(2026, 11, 2, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("latest %v %v, want 14:00 UTC", got, ok)
	}
	if _, ok := s.Latest(upTo.Add(-20*time.Minute), upTo, ny); ok {
		t.Error("a run found since 14:10")
	}
	// Long ago is looked back on a week at most.
	if got, ok := s.Latest(time.Time{}, upTo, time.UTC); !ok || !got.Equal(time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("from the beginning of time: %v %v", got, ok)
	}
}
