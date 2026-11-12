package niimbot

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Port is a connection to a printer, over Bluetooth LE or a serial port.
type Port interface {
	// Write sends bytes to the printer.
	Write(ctx context.Context, b []byte) error
	// Received carries what the printer sends, in whatever pieces it
	// arrives in, and is closed when the printer disconnects.
	Received() <-chan []byte
}

// Timing bounds a conversation.
type Timing struct {
	// Timeout is how long the printer has to answer a command, and to
	// show progress while it prints.
	Timeout time.Duration
	// Poll is how long to wait before asking again whether the printer has
	// finished.
	Poll time.Duration
	// Gap is the pause after each row packet, which the printer does not
	// answer, so as not to outrun it.
	Gap time.Duration
}

// DefaultTiming is the conversation's timing but for the timeout, which
// the caller gives: the printer is asked every 300 ms whether it has
// finished, and given 10 ms after each row packet, as NiimBlue does.
var DefaultTiming = Timing{Poll: 300 * time.Millisecond, Gap: 10 * time.Millisecond}

// ErrPrinter is wrapped when the printer refuses a job or reports an
// error.
var ErrPrinter = errors.New("the printer")

// printErrors names the error codes the printer reports while printing.
var printErrors = map[byte]string{
	0x01: "its cover is open", 0x02: "it is out of labels", 0x03: "its battery is low",
	0x04: "its battery has failed", 0x05: "it was canceled on the printer", 0x06: "it found the data wrong",
	0x07: "it is overheated", 0x08: "labels did not feed out properly", 0x09: "it is busy",
	0x0A: "it has no print head", 0x0B: "it is too cold", 0x0C: "its print head is loose",
	0x0D: "it has no ribbon", 0x0E: "its ribbon is the wrong one", 0x0F: "its ribbon is used up",
	0x10: "its roll is the wrong one", 0x11: "it could not set the label type", 0x12: "it could not set the print mode",
	0x13: "it could not set the density", 0x16: "communication failed", 0x32: "the page was not one it takes",
	0x34: "it waited too long for the data",
}

// Converse sends a job (EncodeJob) to a printer, packet by packet. Each
// command but the rows waits for the printer's answer; page start, page end
// and print end are asked again while the printer answers that it is not
// ready, and a print status request is asked again until the printer has
// printed every page sent so far. Before anything prints, the roll the
// printer reports, if its RFID tag is read, must be of the kind the job is
// for. A printer that does not answer in Timeout, or makes no progress
// printing for as long, fails the job, as does one that reports an error.
func Converse(ctx context.Context, port Port, job []byte, t Timing) error {
	packets, err := Split(job)
	if err != nil {
		return err
	}
	c := conversation{port: port, t: t, reader: &Reader{}}
	labelType, hasType := byte(0), false
	for _, raw := range packets {
		if p, _, _ := parse(trimPrefix(raw)); p.Cmd == CmdSetLabelType && len(p.Data) == 1 {
			labelType, hasType = p.Data[0], true
		}
	}

	started := false
	// copies is the current page's; printed counts the pages ended.
	copies, ended := 1, 0
	for _, raw := range packets {
		p, _, _ := parse(trimPrefix(raw))
		if commands[p.Cmd].reply == 0 {
			if err := c.write(ctx, raw); err != nil {
				return err
			}
			if err := sleep(ctx, t.Gap); err != nil {
				return err
			}
			continue
		}
		switch p.Cmd {
		case CmdPrintStatus:
			err = c.waitPrinted(ctx, raw, ended)
		case CmdPageStart, CmdPageEnd, CmdPrintEnd:
			err = c.untilReady(ctx, raw, p.Cmd)
		default:
			var reply Packet
			reply, err = c.ask(ctx, raw, p.Cmd)
			switch {
			case err != nil:
			case p.Cmd == CmdConnect:
				if len(reply.Data) == 0 || reply.Data[0] == 0 {
					err = fmt.Errorf("%w refused the connection", ErrPrinter)
				}
			case p.Cmd == CmdRFIDInfo:
				if hasType {
					err = checkRoll(reply, labelType)
				}
			case len(reply.Data) == 1 && reply.Data[0] == 0:
				err = fmt.Errorf("%w refused %s", ErrPrinter, name(p.Cmd))
			}
		}
		if err != nil {
			if started {
				// Leave the printer ready for the next job.
				_ = port.Write(context.WithoutCancel(ctx), Packet{Cmd: CmdPrintEnd, Data: one}.Bytes())
			}
			return err
		}
		switch p.Cmd {
		case CmdPrintStart:
			started = true
		case CmdPageStart:
			copies = 1
		case CmdPrintQuantity:
			if len(p.Data) == 2 {
				copies = int(binary.BigEndian.Uint16(p.Data))
			}
		case CmdSetPageSize:
			if len(p.Data) >= 6 {
				copies = int(binary.BigEndian.Uint16(p.Data[4:6]))
			}
		case CmdPageEnd:
			ended += copies
		case CmdPrintEnd:
			started = false
		}
	}
	return nil
}

func trimPrefix(raw []byte) []byte {
	if len(raw) > 0 && raw[0] == connectPrefix {
		return raw[1:]
	}
	return raw
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// conversation is one job's exchange with a printer.
type conversation struct {
	port    Port
	t       Timing
	reader  *Reader
	pending []Packet
}

func (c *conversation) write(ctx context.Context, raw []byte) error {
	if err := c.port.Write(ctx, raw); err != nil {
		return fmt.Errorf("sending %s: %w", name(trimPrefix(raw)[2]), err)
	}
	return nil
}

// ask sends a command and waits for its answer.
func (c *conversation) ask(ctx context.Context, raw []byte, cmd byte) (Packet, error) {
	if err := c.write(ctx, raw); err != nil {
		return Packet{}, err
	}
	return c.await(ctx, cmd)
}

// await waits Timeout for the answer to a command, passing over whatever
// else the printer sends but an error.
func (c *conversation) await(ctx context.Context, cmd byte) (Packet, error) {
	want := commands[cmd].reply
	wctx, cancel := context.WithTimeout(ctx, c.t.Timeout)
	defer cancel()
	for {
		for len(c.pending) > 0 {
			p := c.pending[0]
			c.pending = c.pending[1:]
			switch p.Cmd {
			case want:
				return p, nil
			case CmdPrintError:
				return p, printError(p)
			}
		}
		select {
		case b, ok := <-c.port.Received():
			if !ok {
				return Packet{}, fmt.Errorf("%w disconnected while waiting for an answer to %s", ErrPrinter, name(cmd))
			}
			c.pending = append(c.pending, c.reader.Add(b)...)
		case <-wctx.Done():
			if ctx.Err() != nil {
				return Packet{}, ctx.Err()
			}
			return Packet{}, fmt.Errorf("%w did not answer %s within %s", ErrPrinter, name(cmd), c.t.Timeout)
		}
	}
}

// untilReady sends a command the printer answers 0 to while it is not
// ready, again every Poll until it answers 1, for at most Timeout.
func (c *conversation) untilReady(ctx context.Context, raw []byte, cmd byte) error {
	deadline := time.Now().Add(c.t.Timeout)
	for {
		reply, err := c.ask(ctx, raw, cmd)
		if err != nil {
			return err
		}
		if len(reply.Data) > 0 && reply.Data[0] != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w was still not ready for %s after %s", ErrPrinter, name(cmd), c.t.Timeout)
		}
		if err := sleep(ctx, c.t.Poll); err != nil {
			return err
		}
	}
}

// waitPrinted asks for the print status every Poll until the printer has
// printed the pages, failing when it makes no progress for Timeout.
func (c *conversation) waitPrinted(ctx context.Context, raw []byte, pages int) error {
	last, deadline := -1, time.Now().Add(c.t.Timeout)
	for {
		reply, err := c.ask(ctx, raw, CmdPrintStatus)
		if err != nil {
			return err
		}
		if len(reply.Data) < 4 {
			return fmt.Errorf("%w answered the print status with %d bytes", ErrPrinter, len(reply.Data))
		}
		page := int(binary.BigEndian.Uint16(reply.Data))
		if page >= pages {
			return nil
		}
		// The page and how far it has printed and fed.
		if progress := page<<16 | int(reply.Data[2])<<8 | int(reply.Data[3]); progress != last {
			last, deadline = progress, time.Now().Add(c.t.Timeout)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w printed %d of %d pages and then made no progress for %s", ErrPrinter, page, pages, c.t.Timeout)
		}
		if err := sleep(ctx, c.t.Poll); err != nil {
			return err
		}
	}
}

func printError(p Packet) error {
	if len(p.Data) == 0 {
		return fmt.Errorf("%w reported an error", ErrPrinter)
	}
	if why, ok := printErrors[p.Data[0]]; ok {
		return fmt.Errorf("%w reported an error: %s", ErrPrinter, why)
	}
	return fmt.Errorf("%w reported error %02X", ErrPrinter, p.Data[0])
}

// Roll is what a roll's RFID tag says of it, as the printer reports it.
type Roll struct {
	Barcode, Serial string
	// Total and Used count its labels.
	Total, Used int
	// LabelType is the kind of roll.
	LabelType byte
}

// ParseRoll reads the printer's answer to a request for the roll's RFID
// tag: the roll, and whether a tag was read at all.
func ParseRoll(p Packet) (Roll, bool, error) {
	d := p.Data
	if len(d) <= 1 {
		return Roll{}, false, nil
	}
	bad := fmt.Errorf("%w: the roll's RFID tag is reported in %d bytes, too few", ErrPacket, len(d))
	// The tag's 8-byte ID, then the barcode and serial, each after its
	// length.
	i := 8
	str := func() (string, bool) {
		if i >= len(d) || i+1+int(d[i]) > len(d) {
			return "", false
		}
		s := string(d[i+1 : i+1+int(d[i])])
		i += 1 + int(d[i])
		return s, true
	}
	var r Roll
	var ok bool
	if r.Barcode, ok = str(); !ok {
		return r, true, bad
	}
	if r.Serial, ok = str(); !ok {
		return r, true, bad
	}
	if i+5 > len(d) {
		return r, true, bad
	}
	r.Total = int(binary.BigEndian.Uint16(d[i:]))
	r.Used = int(binary.BigEndian.Uint16(d[i+2:]))
	r.LabelType = d[i+4]
	return r, true, nil
}

// Continuous reports whether the roll is of continuous labels, rather
// than labels with gaps or marks between them.
func (r Roll) Continuous() bool {
	return r.LabelType == LabelContinuous || r.LabelType == LabelTube || r.LabelType == LabelPVCTag
}

// checkRoll refuses a job for die-cut labels on a continuous roll, or the
// other way about. A roll with no tag read, or one reported in a way this
// does not know, is left to the printer.
func checkRoll(reply Packet, labelType byte) error {
	r, ok, err := ParseRoll(reply)
	if err != nil || !ok {
		return nil
	}
	wantContinuous := labelType == LabelContinuous
	switch {
	case r.Continuous() && !wantContinuous:
		return fmt.Errorf("%w has a continuous roll loaded (%s), but is set up for labels with gaps", ErrPrinter, r.Barcode)
	case !r.Continuous() && wantContinuous:
		return fmt.Errorf("%w has a roll of labels with gaps loaded (%s), but is set up for a continuous one", ErrPrinter, r.Barcode)
	}
	return nil
}
