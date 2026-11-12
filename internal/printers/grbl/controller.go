package grbl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GRBL takes G-code a line at a time into a receive buffer, 128 bytes on
// an Arduino, and answers each line it has taken off it with ok or
// error:N. A sender counts the characters of the lines it has sent and not
// had answered, and sends the next only while it fits: character-counting
// flow control. Real-time commands, single bytes, are acted on as they
// arrive, outside the buffer: ? for a status report, ! to hold, ~ to go on,
// and Ctrl-X to reset.

// Real-time commands.
const (
	realtimeStatus = '?'
	realtimeHold   = '!'
	realtimeResume = '~'
	realtimeReset  = 0x18
)

// DefaultRXBuffer is GRBL's receive buffer on an Arduino.
const DefaultRXBuffer = 128

// Timing is how long the controller is waited for.
type Timing struct {
	// Boot is how long connecting waits for the controller's welcome: one
	// on an Arduino restarts when its port is opened.
	Boot time.Duration
	// Answer is how long a status report, or a command's ok, may take.
	Answer time.Duration
	// Hold is how long cancelling waits for the head to stop before it
	// resets the controller.
	Hold time.Duration
}

// DefaultTiming is the timing unless one is given.
var DefaultTiming = Timing{Boot: 2500 * time.Millisecond, Answer: 3 * time.Second, Hold: 5 * time.Second}

// Report is a status report, such as <Idle|MPos:0.000,0.000,0.000|FS:0,0>.
type Report struct {
	// State is the machine state, with its substate: Idle, Run, Hold:0,
	// Jog, Alarm, Door:1, Check, Home or Sleep.
	State string
	// Fields are the rest, by name, such as MPos and Pn.
	Fields map[string]string
}

// Base is the state without its substate: Hold, not Hold:0.
func (r Report) Base() string {
	base, _, _ := strings.Cut(r.State, ":")
	return base
}

// parseReport reads a status report.
func parseReport(s string) (Report, bool) {
	if !strings.HasPrefix(s, "<") || !strings.HasSuffix(s, ">") {
		return Report{}, false
	}
	parts := strings.Split(s[1:len(s)-1], "|")
	r := Report{State: parts[0], Fields: map[string]string{}}
	for _, p := range parts[1:] {
		k, v, _ := strings.Cut(p, ":")
		r.Fields[k] = v
	}
	return r, r.State != ""
}

// Job is a stream's progress, as Controller.Job tells it.
type Job struct {
	Frame bool
	// Lines is how many lines it has, and Acked how many the controller
	// has answered ok.
	Lines, Acked int
	// Err is why it failed, if it did: an error:N, an alarm, or the
	// connection lost.
	Err string
	// Stopped is set when it was cancelled.
	Stopped bool
}

// Finished reports whether every line was answered ok.
func (j Job) Finished() bool { return j.Err == "" && !j.Stopped && j.Acked == j.Lines }

// Active reports whether lines are still to be sent or answered.
func (j Job) Active() bool { return j.Err == "" && !j.Stopped && j.Acked < j.Lines }

type stream struct {
	frame   bool
	lines   []string
	acked   int
	err     string
	stopped bool
}

func (s *stream) active() bool { return s.err == "" && !s.stopped && s.acked < len(s.lines) }

// entry is a line sent and not yet answered.
type entry struct {
	n    int
	line string
	// job and index are the stream it is from, and where; reply takes a
	// command's answer.
	job   *stream
	index int
	reply chan string
}

// Controller is a GRBL controller on the other end of a byte stream: a
// serial port, or a TCP connection.
type Controller struct {
	rw     io.ReadWriteCloser
	rx     int
	timing Timing

	// writeMu keeps lines whole and in the order they are counted.
	writeMu sync.Mutex

	mu      sync.Mutex
	changed chan struct{}
	pending []entry
	used    int
	report  Report
	reports int
	// settings are the controller's $ settings, as $$ last listed them;
	// collecting gathers them while it lists them.
	settings   map[int]string
	collecting map[int]string
	job        *stream
	banner     string
	message    string
	lastUsed   time.Time
	err        error
	done       chan struct{}
}

// New starts talking to a controller over rw, whose receive buffer holds
// rx bytes.
func New(rw io.ReadWriteCloser, rx int, timing Timing) *Controller {
	if rx < 1 {
		rx = DefaultRXBuffer
	}
	if timing == (Timing{}) {
		timing = DefaultTiming
	}
	c := &Controller{rw: rw, rx: rx, timing: timing, changed: make(chan struct{}), done: make(chan struct{}), lastUsed: time.Now()}
	go c.read()
	return c
}

// errTimeout is a wait that ran out.
var errTimeout = errors.New("timed out")

// errStopped is a stream stopped while a line waited to be sent.
var errStopped = errors.New("stopped")

// broadcast wakes everything waiting, held with mu.
func (c *Controller) broadcast() {
	close(c.changed)
	c.changed = make(chan struct{})
}

// waitFor waits until ok, checked with mu held, is true.
func (c *Controller) waitFor(ctx context.Context, timeout time.Duration, ok func() bool) error {
	var timer <-chan time.Time
	if timeout > 0 {
		t := time.NewTimer(timeout)
		defer t.Stop()
		timer = t.C
	}
	for {
		c.mu.Lock()
		if ok() {
			c.mu.Unlock()
			return nil
		}
		ch, err := c.changed, c.err
		c.mu.Unlock()
		if err != nil {
			return err
		}
		select {
		case <-ch:
		case <-timer:
			return errTimeout
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Connect waits for the controller to start, if it is starting, and to
// answer a status request, and reads its settings.
func (c *Controller) Connect(ctx context.Context) error {
	_ = c.waitFor(ctx, c.timing.Boot, func() bool { return c.banner != "" || c.reports > 0 })
	if _, err := c.Status(ctx); err != nil {
		return err
	}
	_, err := c.Settings(ctx)
	return err
}

// Close closes the connection.
func (c *Controller) Close() error {
	c.fail(errors.New("closed"))
	return nil
}

// Done is closed when the connection is.
func (c *Controller) Done() <-chan struct{} { return c.done }

// Err is why the connection closed.
func (c *Controller) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// fail closes the connection for err, failing whatever waited on it.
func (c *Controller) fail(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	if c.job != nil && c.job.active() {
		c.job.err = "the connection to the controller was lost: " + err.Error()
	}
	c.dropPending("lost")
	close(c.done)
	c.broadcast()
	c.mu.Unlock()
	c.rw.Close()
}

// dropPending forgets every line sent and not answered, as a controller
// that resets or raises an alarm forgets them, held with mu.
func (c *Controller) dropPending(why string) {
	for _, e := range c.pending {
		if e.reply != nil {
			e.reply <- why
		}
	}
	c.pending, c.used = nil, 0
}

func (c *Controller) read() {
	sc := bufio.NewScanner(c.rw)
	sc.Buffer(make([]byte, 0, 256), 4096)
	for sc.Scan() {
		c.handle(strings.TrimSpace(sc.Text()))
	}
	err := sc.Err()
	if err == nil {
		err = io.EOF
	}
	c.fail(err)
}

// handle takes one line from the controller.
func (c *Controller) handle(line string) {
	switch {
	case line == "":
	case line == "ok", strings.HasPrefix(line, "error:"):
		c.answered(line)
	case strings.HasPrefix(line, "ALARM:"):
		c.mu.Lock()
		c.message = line
		if c.job != nil && c.job.active() {
			c.job.err = "the controller raised " + line
		}
		c.dropPending(line)
		c.broadcast()
		c.mu.Unlock()
	case strings.HasPrefix(line, "<"):
		if r, ok := parseReport(line); ok {
			c.mu.Lock()
			c.report = r
			c.reports++
			c.broadcast()
			c.mu.Unlock()
		}
	case strings.HasPrefix(line, "Grbl"):
		// The welcome: the controller has (re)started, forgetting what
		// it was sent.
		c.mu.Lock()
		c.banner = line
		if c.job != nil && c.job.active() {
			c.job.err = "the controller restarted"
		}
		c.dropPending("restarted")
		c.broadcast()
		c.mu.Unlock()
	case strings.HasPrefix(line, "$") && strings.Contains(line, "="):
		k, v, _ := strings.Cut(line[1:], "=")
		n, err := strconv.Atoi(k)
		c.mu.Lock()
		if err == nil && c.collecting != nil {
			// grblHAL and FluidNC may follow a value with a comment.
			v, _, _ = strings.Cut(v, " ")
			c.collecting[n] = strings.TrimSpace(v)
		}
		c.mu.Unlock()
	case strings.HasPrefix(line, "[MSG:"):
		c.mu.Lock()
		c.message = strings.TrimSuffix(strings.TrimPrefix(line, "[MSG:"), "]")
		c.mu.Unlock()
	}
}

// answered takes an ok or error:N for the oldest line not yet answered.
func (c *Controller) answered(reply string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) == 0 {
		return
	}
	e := c.pending[0]
	c.pending = c.pending[1:]
	c.used -= e.n
	c.broadcast()
	if e.reply != nil {
		e.reply <- reply
		return
	}
	if e.job != c.job || !e.job.active() {
		return
	}
	if reply == "ok" {
		e.job.acked++
		return
	}
	// A line refused: what follows it would go wrong, so the job stops.
	e.job.err = fmt.Sprintf("the controller answered %s to line %d, %s", reply, e.index+1, e.line)
	go c.stopMachine(context.Background())
}

// send sends a line once there is room for it in the controller's buffer,
// unless stop, checked with mu held, says not to.
func (c *Controller) send(ctx context.Context, e entry, stop func() bool) error {
	e.n = len(e.line) + 1
	if e.n > c.rx {
		return fmt.Errorf("the line %q is longer than the controller's %d-byte buffer", e.line, c.rx)
	}
	for {
		c.writeMu.Lock()
		c.mu.Lock()
		if c.err != nil {
			err := c.err
			c.mu.Unlock()
			c.writeMu.Unlock()
			return err
		}
		if stop != nil && stop() {
			c.mu.Unlock()
			c.writeMu.Unlock()
			return errStopped
		}
		if c.used+e.n <= c.rx {
			c.pending = append(c.pending, e)
			c.used += e.n
			c.lastUsed = time.Now()
			c.mu.Unlock()
			_, err := c.rw.Write([]byte(e.line + "\n"))
			c.writeMu.Unlock()
			if err != nil {
				c.fail(err)
				return err
			}
			return nil
		}
		ch := c.changed
		c.mu.Unlock()
		c.writeMu.Unlock()
		select {
		case <-ch:
		case <-c.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// realtime sends a real-time command.
func (c *Controller) realtime(b byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	err := c.err
	c.lastUsed = time.Now()
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if _, err := c.rw.Write([]byte{b}); err != nil {
		c.fail(err)
		return err
	}
	return nil
}

// Command sends a line and waits for its answer: ok, or why not.
func (c *Controller) Command(ctx context.Context, line string) error {
	e := entry{line: line, reply: make(chan string, 1)}
	if err := c.send(ctx, e, nil); err != nil {
		return err
	}
	t := time.NewTimer(c.timing.Answer)
	defer t.Stop()
	select {
	case reply := <-e.reply:
		if reply != "ok" {
			return fmt.Errorf("the controller answered %s to %s", reply, line)
		}
		return nil
	case <-t.C:
		return fmt.Errorf("the controller did not answer %s", line)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Settings reads the controller's $ settings with $$.
func (c *Controller) Settings(ctx context.Context) (map[int]string, error) {
	c.mu.Lock()
	c.collecting = map[int]string{}
	c.mu.Unlock()
	err := c.Command(ctx, "$$")
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.settings = c.collecting
	}
	c.collecting = nil
	return c.settings, err
}

// Setting is one of the settings $$ last listed, and whether it did.
func (c *Controller) Setting(n int) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.settings[n]
	return v, ok
}

// Status asks the controller for a status report.
func (c *Controller) Status(ctx context.Context) (Report, error) {
	c.mu.Lock()
	seen := c.reports
	c.mu.Unlock()
	if err := c.realtime(realtimeStatus); err != nil {
		return Report{}, err
	}
	var r Report
	err := c.waitFor(ctx, c.timing.Answer, func() bool {
		r = c.report
		return c.reports > seen
	})
	if err == errTimeout {
		return Report{}, errors.New("the controller did not answer a status request")
	}
	return r, err
}

// Message is the last message the controller sent, such as an alarm.
func (c *Controller) Message() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.message
}

// Job is the last stream's progress, and false when there has been none.
func (c *Controller) Job() (Job, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job == nil {
		return Job{}, false
	}
	j := c.job
	return Job{Frame: j.frame, Lines: len(j.lines), Acked: j.acked, Err: j.err, Stopped: j.stopped}, true
}

// Idle reports whether the controller has had nothing to do for a while.
func (c *Controller) Idle(d time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return (c.job == nil || !c.job.active()) && time.Since(c.lastUsed) > d
}

// Stream sends G-code to the controller, a line at a time as it has room,
// and returns once the stream has begun; Job follows it. A frame is a
// stream as a job is, told apart for its status.
func (c *Controller) Stream(gcode []byte, frame bool) error {
	var lines []string
	for _, l := range strings.Split(string(gcode), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if len(l)+1 > c.rx {
				return fmt.Errorf("the line %q is longer than the controller's %d-byte buffer", l, c.rx)
			}
			lines = append(lines, l)
		}
	}
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	if c.job != nil && c.job.active() {
		c.mu.Unlock()
		return errors.New("the controller is busy with another job")
	}
	s := &stream{frame: frame, lines: lines}
	c.job = s
	c.broadcast()
	c.mu.Unlock()
	go func() {
		for i, line := range lines {
			err := c.send(context.Background(), entry{line: line, job: s, index: i}, func() bool { return s.err != "" || s.stopped })
			if err != nil {
				c.mu.Lock()
				if s.err == "" && !s.stopped {
					s.err = err.Error()
				}
				c.mu.Unlock()
				return
			}
		}
	}()
	return nil
}

// Pause holds the job: the head slows to a stop, and the laser is off
// while it stands.
func (c *Controller) Pause() error { return c.realtime(realtimeHold) }

// Resume goes on from a hold.
func (c *Controller) Resume() error { return c.realtime(realtimeResume) }

// Cancel stops the job: nothing more is sent, the head is held to a stop
// and the controller reset, which forgets what it had in its buffers.
func (c *Controller) Cancel(ctx context.Context) error {
	c.mu.Lock()
	if c.job != nil && c.job.active() {
		c.job.stopped = true
	}
	c.broadcast()
	c.mu.Unlock()
	return c.stopMachine(ctx)
}

// stopMachine holds the head, waits for it to stop, so the controller
// keeps its position, and resets the controller.
func (c *Controller) stopMachine(ctx context.Context) error {
	if err := c.realtime(realtimeHold); err != nil {
		return err
	}
	deadline := time.Now().Add(c.timing.Hold)
	for time.Now().Before(deadline) {
		r, err := c.Status(ctx)
		if err != nil {
			break
		}
		if s := r.State; s == "Hold:0" || s == "Idle" || r.Base() == "Alarm" || r.Base() == "Door" {
			break
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := c.realtime(realtimeReset); err != nil {
		return err
	}
	c.mu.Lock()
	c.dropPending("reset")
	c.broadcast()
	c.mu.Unlock()
	return nil
}
