// Package cron reads the five-field cron expressions hooks are scheduled
// with (docs/hooks.md): minute, hour, day of the month, month and day of
// the week, each a *, a number, a range a-b, a list, or any of these with a
// step /n. Days of the week run 0-6 from Sunday, and 7 is Sunday too. As in
// cron, when both the day of the month and the day of the week are
// restricted, a time matching either matches.
package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	// Time zones are named by IANA name; the binary carries the database,
	// so they work where the system has none.
	_ "time/tzdata"
)

// Schedule is a parsed expression.
type Schedule struct {
	minute, hour, dom, month, dow uint64
	// domAny and dowAny are set when the field was *, so that only the
	// other day field restricts.
	domAny, dowAny bool
}

var fieldRanges = [5]struct {
	name     string
	min, max int
}{
	{"minute", 0, 59}, {"hour", 0, 23}, {"day of the month", 1, 31}, {"month", 1, 12}, {"day of the week", 0, 7},
}

// Parse reads an expression of five fields separated by spaces.
func Parse(expr string) (*Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, errors.New("a schedule has five fields: minute, hour, day of the month, month and day of the week")
	}
	var bits [5]uint64
	for i, f := range fields {
		b, err := parseField(f, fieldRanges[i].min, fieldRanges[i].max)
		if err != nil {
			return nil, fmt.Errorf("%s %q: %v", fieldRanges[i].name, f, err)
		}
		bits[i] = b
	}
	// Sunday is 0 or 7.
	if bits[4]&(1<<7) != 0 {
		bits[4] |= 1
	}
	return &Schedule{
		minute: bits[0], hour: bits[1], dom: bits[2], month: bits[3], dow: bits[4],
		domAny: fields[2] == "*", dowAny: fields[4] == "*",
	}, nil
}

func parseField(f string, lo, hi int) (uint64, error) {
	var bits uint64
	for _, part := range strings.Split(f, ",") {
		rng, stepText, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return 0, errors.New("a step must be a whole number of at least 1")
			}
			step = n
		}
		from, to := lo, hi
		switch {
		case rng == "*":
		case strings.Contains(rng, "-"):
			a, b, _ := strings.Cut(rng, "-")
			var err1, err2 error
			from, err1 = strconv.Atoi(a)
			to, err2 = strconv.Atoi(b)
			if err1 != nil || err2 != nil {
				return 0, errors.New("a range is two numbers, a-b")
			}
		default:
			n, err := strconv.Atoi(rng)
			if err != nil {
				return 0, errors.New("not a number, a range or *")
			}
			from, to = n, n
			if hasStep {
				to = hi
			}
		}
		if from < lo || to > hi || from > to {
			return 0, fmt.Errorf("must be within %d-%d", lo, hi)
		}
		for v := from; v <= to; v += step {
			bits |= 1 << uint(v)
		}
	}
	return bits, nil
}

// Matches reports whether the schedule runs in the minute t falls in, read
// in t's location.
func (s *Schedule) Matches(t time.Time) bool {
	if s.minute&(1<<uint(t.Minute())) == 0 || s.hour&(1<<uint(t.Hour())) == 0 || s.month&(1<<uint(t.Month())) == 0 {
		return false
	}
	dom := s.dom&(1<<uint(t.Day())) != 0
	dow := s.dow&(1<<uint(t.Weekday())) != 0
	switch {
	case s.domAny && s.dowAny:
		return true
	case s.domAny:
		return dow
	case s.dowAny:
		return dom
	default:
		return dom || dow
	}
}

// Latest is the latest minute the schedule ran in after after and at or
// before upTo, read in loc, looking back at most a week: false when there
// was none.
func (s *Schedule) Latest(after, upTo time.Time, loc *time.Location) (time.Time, bool) {
	if limit := upTo.Add(-7 * 24 * time.Hour); after.Before(limit) {
		after = limit
	}
	for t := upTo.In(loc).Truncate(time.Minute); t.After(after); t = t.Add(-time.Minute) {
		if s.Matches(t) {
			return t, true
		}
	}
	return time.Time{}, false
}
