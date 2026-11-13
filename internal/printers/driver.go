package printers

import (
	"errors"
	"fmt"
	"image"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/industrial"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/zebra"
)

// Driver speaks one family of printers' language. Drivers are registered by
// name and chosen per printer, so adding a printer family means adding a
// driver, not changing the API.
type Driver interface {
	// Info describes the driver for clients configuring printers.
	Info() DriverInfo
	// Validate checks the model, media and options of a printer definition.
	Validate(s Settings) error
	// Geometry describes the printer as configured, for FitLabel.
	Geometry(s Settings) (Geometry, error)
	// Plan returns how to encode labels that FitLabel found suit the
	// printer as configured.
	Plan(s Settings, f Fit) (Plan, error)
}

// LabelSpec is the physical size of a rendered label, and where it has
// content, for warnings about parts that will not print.
type LabelSpec struct {
	WidthMM, HeightMM float64
	DPI               int
	Content           []RectMM
	// Feed is which way the label goes through the printer when it fits
	// either way: "across" its width across the roll, "along" its width
	// along it, or empty or "auto" for as fits, not turned when that fits.
	Feed string
	// Inks are the inks the label draws in, for a warning when the printer
	// lacks one (labels.Template.Inks).
	Inks []string
}

// PlanLabel checks labels against a printer and plans printing them: the
// driver's plan and the fit, whose warnings are for the person printing.
// Errors wrap ErrDoesNotFit when the labels do not suit the roll loaded.
func PlanLabel(driver string, s Settings, label LabelSpec) (Plan, Fit, error) {
	d, err := Lookup(driver)
	if err != nil {
		return nil, Fit{}, err
	}
	if _, ok := d.(OneShotDriver); ok {
		return nil, Fit{}, errOneShot
	}
	g, err := d.Geometry(s)
	if err != nil {
		return nil, Fit{}, err
	}
	f, err := FitLabel(g, label)
	if err != nil {
		return nil, Fit{}, err
	}
	plan, err := d.Plan(s, f)
	return plan, f, err
}

// Plan encodes labels for one printer.
type Plan interface {
	// Encode turns labels, rendered 1-bit at the planned size and DPI, into
	// one print job.
	Encode(labels []*image.Gray) ([]byte, error)
}

// InkPlan is a Plan for a printer with inks besides black and white
// (docs/labels.md#inks), which takes labels rendered in them as well.
type InkPlan interface {
	Plan
	// Palette is the inks the printer prints in.
	Palette() labels.Palette
	// EncodeInks turns labels, rendered in the palette's inks at the
	// planned size and DPI, each dot an index into it, into one print job.
	EncodeInks(labels []*image.Paletted) ([]byte, error)
}

// ErrDoesNotFit is wrapped when a template cannot print on the loaded media.
var ErrDoesNotFit = errors.New("template does not fit the printer's media")

// DriverInfo lists what a driver supports.
type DriverInfo struct {
	Driver string   `json:"driver"`
	Models []string `json:"models"`
	// ModelDPI is each model's print resolution, which templates must use.
	ModelDPI map[string]int `json:"model_dpi"`
	// ModelPalette is the most inks each model prints in, for the models
	// that print more than black and white (docs/labels.md#inks). A roll
	// may hold it to fewer.
	ModelPalette map[string]labels.Palette `json:"model_palette,omitempty"`
	Media        []MediaInfo               `json:"media"`
	// CustomStock is set when the driver takes custom stock as well.
	CustomStock *CustomStockInfo `json:"custom_stock,omitempty"`
	Options     []OptionInfo     `json:"options,omitempty"`
	// BLE is the driver's Bluetooth LE profile, the default for a ble
	// connection, when its printers connect over Bluetooth LE.
	BLE *BLEProfile `json:"ble,omitempty"`
	// Display is set for a display driver (display.go): the connections
	// its displays take and the images it makes.
	Display *DisplayInfo `json:"display,omitempty"`
	// OneShot is set for a one-shot printer's driver (oneshot.go): each
	// model's work area, materials and what the machine does itself.
	OneShot []OneShotModel `json:"one_shot,omitempty"`
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
	// Models lists the models that take the roll, when not all of the
	// driver's do.
	Models []string `json:"models,omitempty"`
	// Palette is the inks the roll takes, when more than black and white,
	// on models that print them.
	Palette labels.Palette `json:"palette,omitempty"`
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
	for _, d := range []Driver{brotherQL, brotherDriver{brotherql.SeriesTD}, ptouchDriver{}, dymoLabelWriter{}, dymoLabelManager{}, epsonLabelWorks{}, bradyDriver{}, tsplDriver{}, niimbotDriver{}, zebraDriver{zebra.ZPL}, zebraDriver{zebra.EPL2},
		industrialDriver{industrial.DPL}, industrialDriver{industrial.SBPL}, industrialDriver{industrial.EZPL},
		escposDriver{}, phomemoDriver{escposDriver{phomemo: true}},
		genericDisplay, oeplDisplay, trmnlDisplay, paperlessDisplay, solumDisplay} {
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
		info := d.Info()
		if b, ok := d.(BLEDriver); ok {
			p := b.BLE()
			info.BLE = &p
		}
		if dd, ok := d.(displayDriver); ok {
			info.Display = &DisplayInfo{Connections: dd.Connections(), Formats: slices.Clone(dd.formats)}
		}
		if od, ok := d.(OneShotDriver); ok {
			info.OneShot = od.OneShotModels()
		}
		infos = append(infos, info)
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
