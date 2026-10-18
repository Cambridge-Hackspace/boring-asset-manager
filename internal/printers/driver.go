package printers

import (
	"errors"
	"fmt"
	"image"
	"sort"
	"strings"
	"sync"
)

// Driver speaks one family of printers' language. Drivers are registered by
// name and chosen per printer, so adding a printer family means adding a
// driver, not changing the API.
type Driver interface {
	// Info describes the driver for clients configuring printers.
	Info() DriverInfo
	// Validate checks the model, media and options of a printer definition.
	Validate(s Settings) error
	// Plan checks that labels of the given size suit the printer as
	// configured, and returns how to encode them. Errors wrap ErrDoesNotFit
	// when the label and the loaded media disagree.
	Plan(s Settings, label LabelSpec) (Plan, error)
}

// LabelSpec is the physical size of a rendered label.
type LabelSpec struct {
	WidthMM, HeightMM float64
	DPI               int
}

// Plan encodes labels for one printer.
type Plan interface {
	// Encode turns labels, rendered 1-bit at the planned size and DPI, into
	// one print job.
	Encode(labels []*image.Gray) ([]byte, error)
}

// ErrDoesNotFit is wrapped when a template cannot print on the loaded media.
var ErrDoesNotFit = errors.New("template does not fit the printer's media")

// DriverInfo lists what a driver supports.
type DriverInfo struct {
	Driver string   `json:"driver"`
	Models []string `json:"models"`
	// ModelDPI is each model's print resolution, which templates must use.
	ModelDPI map[string]int `json:"model_dpi"`
	Media    []MediaInfo    `json:"media"`
	// CustomStock is set when the driver takes custom stock as well.
	CustomStock *CustomStockInfo `json:"custom_stock,omitempty"`
	Options     []OptionInfo     `json:"options,omitempty"`
}

// MediaInfo describes a roll for clients choosing what is loaded.
type MediaInfo struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	WidthMM  float64 `json:"width_mm"`
	LengthMM float64 `json:"length_mm,omitempty"`
	DieCut   bool    `json:"die_cut"`
	// Custom is set for custom stock rather than a built-in roll.
	Custom bool `json:"custom,omitempty"`
}

// OptionInfo describes one driver option.
type OptionInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default any    `json:"default"`
	// Values lists the allowed values of a string option.
	Values      []string `json:"values,omitempty"`
	Description string   `json:"description"`
}

var (
	registryMu sync.RWMutex
	registry   = map[string]Driver{}
)

func init() {
	for _, d := range []Driver{brotherQL{}, dymoLabelWriter{}, tsplDriver{}} {
		if err := Register(d); err != nil {
			panic(err)
		}
	}
}

// Register adds a driver under the name in its Info.
func Register(d Driver) error {
	name := d.Info().Driver
	if name == "" {
		return errors.New("printers: driver has no name")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[name]; dup {
		return fmt.Errorf("printers: driver %q is already registered", name)
	}
	registry[name] = d
	return nil
}

// Lookup returns a registered driver.
func Lookup(name string) (Driver, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := registry[name]
	if !ok {
		names := make([]string, 0, len(registry))
		for n := range registry {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("%w: driver must be one of %s", ErrInvalidPrinter, strings.Join(names, ", "))
	}
	return d, nil
}

// Drivers describes every registered driver, by name.
func Drivers() []DriverInfo {
	registryMu.RLock()
	defer registryMu.RUnlock()
	infos := make([]DriverInfo, 0, len(registry))
	for _, d := range registry {
		infos = append(infos, d.Info())
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Driver < infos[j].Driver })
	return infos
}

// sameDPI maps every model to one resolution.
func sameDPI(models []string, dpi int) map[string]int {
	out := make(map[string]int, len(models))
	for _, m := range models {
		out[m] = dpi
	}
	return out
}
