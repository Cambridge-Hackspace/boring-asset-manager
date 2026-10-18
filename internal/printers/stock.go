package printers

import (
	"fmt"
	"math"
	"strings"
)

// Stock is a roll of labels known by its size alone, kept in the database
// for printers that take any size (README §4.4), as opposed to the rolls a
// driver has built in.
type Stock struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// WidthMM is the label width across the roll.
	WidthMM float64 `json:"width_mm"`
	// LengthMM is the length of a die-cut label; 0 for a continuous roll.
	LengthMM float64 `json:"length_mm"`
	// GapMM is the gap between die-cut labels; 0 on a continuous roll.
	GapMM float64 `json:"gap_mm"`
}

// DieCut reports whether the roll holds pre-cut labels.
func (s Stock) DieCut() bool { return s.LengthMM > 0 }

// Limits on any custom stock; drivers narrow them.
const (
	MinStockMM    = 5.0
	MaxStockWidth = 300.0
	MaxStockLen   = 3000.0
	MaxStockGap   = 25.0
)

// ValidateStock checks a stock's own values, before any printer is
// considered. Errors wrap ErrInvalidPrinter.
func ValidateStock(s Stock) error {
	var problems []string
	if strings.TrimSpace(s.Name) == "" || len(s.Name) > 255 {
		problems = append(problems, "name must be 1-255 characters")
	}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !finite(s.WidthMM) || s.WidthMM < MinStockMM || s.WidthMM > MaxStockWidth {
		problems = append(problems, fmt.Sprintf("width must be %g-%g mm", MinStockMM, MaxStockWidth))
	}
	if !finite(s.LengthMM) || s.LengthMM != 0 && (s.LengthMM < MinStockMM || s.LengthMM > MaxStockLen) {
		problems = append(problems, fmt.Sprintf("length must be %g-%g mm, or none for a continuous roll", MinStockMM, MaxStockLen))
	}
	switch {
	case !finite(s.GapMM) || s.GapMM < 0 || s.GapMM > MaxStockGap:
		problems = append(problems, fmt.Sprintf("gap must be 0-%g mm", MaxStockGap))
	case s.LengthMM == 0 && s.GapMM != 0:
		problems = append(problems, "a continuous roll has no gap")
	}
	if problems != nil {
		return fmt.Errorf("%w: %s", ErrInvalidPrinter, strings.Join(problems, "; "))
	}
	return nil
}

// CustomStockInfo says what custom stock a driver takes.
type CustomStockInfo struct {
	// MaxWidthMM is the widest label each model takes.
	MaxWidthMM map[string]float64 `json:"max_width_mm"`
	// MinLengthMM and MaxLengthMM bound the labels it prints from a
	// continuous roll.
	MinLengthMM float64 `json:"min_length_mm"`
	MaxLengthMM float64 `json:"max_length_mm"`
	// Note is anything else to know, such as models that refuse labels
	// they cannot identify.
	Note string `json:"note,omitempty"`
}

// Loaded describes the roll a printer has loaded: its custom stock, or the
// driver's built-in roll it names. It is nil when neither is known.
func Loaded(driver string, s Settings) *MediaInfo {
	if s.Stock != nil {
		return &MediaInfo{ID: s.Stock.ID, Name: s.Stock.Name, WidthMM: s.Stock.WidthMM, LengthMM: s.Stock.LengthMM, DieCut: s.Stock.DieCut(), Custom: true}
	}
	d, err := Lookup(driver)
	if err != nil {
		return nil
	}
	for _, m := range d.Info().Media {
		if m.ID == s.Media {
			return &m
		}
	}
	return nil
}

// errNoCustomStock refuses custom stock on drivers that know only their own
// rolls.
func errNoCustomStock(what string) error {
	return fmt.Errorf("%s take only their own rolls; choose one of the built-in media", what)
}
