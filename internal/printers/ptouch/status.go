package ptouch

import (
	"errors"
	"fmt"
)

// Status is the printer's 32-byte reply to a status information request
// (ESC i S), or a notification it sends while printing (section 4, "Status
// information request", in every reference).
type Status struct {
	// Model is the model code: "h" (68h) for the PT-P750W, "p" (70h) for
	// the PT-P950NW and so on.
	Model byte
	// Error1 and Error2 are the error information bytes.
	Error1, Error2 byte
	// WidthMM is the loaded tape's width as the printer reports it: 4 for
	// 3.5 mm tape, 0 with none loaded.
	WidthMM byte
	// MediaType is what is loaded: 0 for nothing, 01h laminated tape, 03h
	// non-laminated tape, 11h and 17h heat-shrink tube, FFh incompatible.
	MediaType byte
	// StatusType says why the status was sent: 00h in reply to a request,
	// 01h when printing completed, 02h on an error.
	StatusType byte
}

// Error information bits that stop printing.
const (
	errNoMedia    = 0x01 // error information 1
	errCutterJam  = 0x04
	errWrongMedia = 0x01 // error information 2: "replace media" or wrong media
	errCoverOpen  = 0x10
	errOverheated = 0x20
)

// ErrStatus is wrapped by errors about a status reply.
var ErrStatus = errors.New("ptouch: status")

// ParseStatus reads a status reply.
func ParseStatus(b []byte) (Status, error) {
	if len(b) != 32 || b[0] != 0x80 || b[1] != 0x20 || b[2] != 'B' {
		return Status{}, fmt.Errorf("%w: not a 32-byte status reply", ErrStatus)
	}
	return Status{Model: b[4], Error1: b[8], Error2: b[9], WidthMM: b[10], MediaType: b[11], StatusType: b[18]}, nil
}

// Check reports whether a printer in this status can print on the cassette
// the layout is for: it has no error, and the cassette loaded is that one's
// width and kind.
func (s Status) Check(m Media) error {
	switch {
	case s.Error1&errNoMedia != 0 || s.MediaType == 0:
		return fmt.Errorf("%w: no tape is loaded", ErrStatus)
	case s.Error1&errCutterJam != 0:
		return fmt.Errorf("%w: the cutter is jammed", ErrStatus)
	case s.Error2&errCoverOpen != 0:
		return fmt.Errorf("%w: the cover is open", ErrStatus)
	case s.Error2&errOverheated != 0:
		return fmt.Errorf("%w: the printer is overheated", ErrStatus)
	case s.Error2&errWrongMedia != 0:
		return fmt.Errorf("%w: the printer reports the wrong cassette loaded", ErrStatus)
	case s.MediaType == 0xFF:
		return fmt.Errorf("%w: the cassette loaded is not one the printer takes", ErrStatus)
	}
	tube := s.MediaType == typeHS21 || s.MediaType == typeHS31
	switch m.Kind {
	case TZe:
		if tube {
			return fmt.Errorf("%w: heat-shrink tube is loaded, not %s", ErrStatus, m.Name)
		}
	default:
		if s.MediaType != m.mediaType() {
			return fmt.Errorf("%w: the cassette loaded is not %s", ErrStatus, m.Name)
		}
	}
	if m.Kind != HS31 && int(s.WidthMM) != m.infoWidth {
		return fmt.Errorf("%w: %d mm %s is loaded, not %s", ErrStatus, s.WidthMM, loadedKind(tube), m.Name)
	}
	return nil
}

func loadedKind(tube bool) string {
	if tube {
		return "tube"
	}
	return "tape"
}

// LoadedMedia finds the cassette a status reports, for the fit check: none
// when nothing it knows is loaded, or a 3:1 tube, whose width the
// references do not give.
func (s Status) LoadedMedia(mo Model) (Media, bool) {
	for _, id := range MediaIDs() {
		m := catalog[id]
		if mo.Takes(m) != nil || m.Kind == HS31 {
			continue
		}
		if s.Check(m) == nil {
			return m, true
		}
	}
	return Media{}, false
}
