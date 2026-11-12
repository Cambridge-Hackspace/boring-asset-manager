package printers

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"slices"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
)

// Displays (docs/printing.md#displays) are e-paper screens driven as
// printers: a driver reports each model's panel, its pixels, resolution and
// inks, so the fit check and the template editor treat it as a die-cut
// label of the panel's size, and "printing" a label makes an image the
// display shows. A display shows one label at a time: the last of a job.

// Display drivers.
const (
	// DriverDisplay drives any e-paper panel by its size, pulled by the
	// display or pushed through a webhook channel.
	DriverDisplay = "display"
	// DriverOpenEPaperLink drives OpenEPaperLink tags, pushed to their
	// access point.
	DriverOpenEPaperLink = "openepaperlink"
	// DriverTRMNL drives TRMNL displays, which pull through TRMNL's "bring
	// your own server" API.
	DriverTRMNL = "trmnl"
	// DriverPaperlessPaper drives paperlesspaper's OpenPaper frames, which
	// pull on its offline firmware or are pushed through its cloud.
	DriverPaperlessPaper = "paperlesspaper"
	// DriverSolum drives SoluM's electronic shelf labels, pushed through
	// SoluM's AIMS.
	DriverSolum = "solum"
)

// Image formats a display takes.
const (
	FormatPNG  = "png"
	FormatBMP  = "bmp"
	FormatJPEG = "jpeg"
	// FormatOpenPaper is the 4-bit bitmap an OpenPaper frame shows as it
	// is: each dot its panel's code for the ink (paperlesspaper.go).
	FormatOpenPaper = "openpaper"
)

// Pull refresh intervals a display may be given, in seconds: a minute to
// a day.
const (
	MinDisplayRefresh = 60
	MaxDisplayRefresh = 24 * 60 * 60
)

// DisplayDriver is a Driver for displays: what its plans encode is an image
// file the display shows, not a print job, and its displays take only the
// connections it names.
type DisplayDriver interface {
	Driver
	// Connections are the connection types its displays take.
	Connections() []string
	// CheckConnection checks a normalized connection of one of those types
	// suits the driver, beyond its type.
	CheckConnection(c Connection) error
}

// DisplayInfo describes a display driver for clients configuring displays.
type DisplayInfo struct {
	// Connections are the connection types its displays take.
	Connections []string `json:"connections"`
	// Formats are the image formats it makes, the default first.
	Formats []string `json:"formats"`
}

// IsDisplay reports whether a driver drives displays.
func IsDisplay(driver string) bool {
	d, err := Lookup(driver)
	if err != nil {
		return false
	}
	_, ok := d.(DisplayDriver)
	return ok
}

// isDisplayConnection reports whether a connection type reaches only
// displays.
func isDisplayConnection(t string) bool {
	return t == ConnPull || t == ConnWebhook || t == ConnOpenEPaperLink || t == ConnPaperlessPaper || t == ConnSolum
}

// checkDisplayConnection checks a driver takes a connection: a display
// driver one of its own, any other driver one that is not a display's.
func checkDisplayConnection(d Driver, c Connection) error {
	dd, ok := d.(DisplayDriver)
	if !ok {
		if isDisplayConnection(c.Type) {
			return fmt.Errorf("%w: connection: a %s connection is for displays, and %s drives printers", ErrInvalidPrinter, c.Type, d.Info().Driver)
		}
		return nil
	}
	if !slices.Contains(dd.Connections(), c.Type) {
		return fmt.Errorf("%w: connection: %s displays take a %s connection", ErrInvalidPrinter, d.Info().Driver, joinOr(dd.Connections()))
	}
	if err := dd.CheckConnection(c); err != nil {
		return fmt.Errorf("%w: connection: %v", ErrInvalidPrinter, err)
	}
	return nil
}

// joinOr joins words as alternatives in a sentence: "a", "a or b", "a, b or c".
func joinOr(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}

// Panel is an e-paper panel: its pixels, the resolution templates are laid
// out at for it, and its inks. Its size in millimeters is its pixels at
// that resolution, so a template of its size renders to its pixels.
type Panel struct {
	ID            string
	Name          string
	Width, Height int
	DPI           int
	Palette       labels.Palette
}

// WidthMM and HeightMM are the panel's size at its resolution.
func (p Panel) WidthMM() float64  { return float64(p.Width) * 25.4 / float64(p.DPI) }
func (p Panel) HeightMM() float64 { return float64(p.Height) * 25.4 / float64(p.DPI) }

// Inks is the panel's palette: black and white when it declares none.
func (p Panel) Inks() labels.Palette {
	if p.Palette.Valid() {
		return p.Palette
	}
	return labels.PaletteBW
}

// The resolutions are each panel's own, near enough: its diagonal in
// pixels over its diagonal in inches, rounded.
var (
	genericPanels = []Panel{
		{ID: "1.54in-152x152", Name: "1.54-inch, 152 × 152", Width: 152, Height: 152, DPI: 140},
		{ID: "1.54in-152x152-bwr", Name: "1.54-inch, 152 × 152, red", Width: 152, Height: 152, DPI: 140, Palette: labels.PaletteBWR},
		{ID: "2.13in-250x122", Name: "2.13-inch, 250 × 122", Width: 250, Height: 122, DPI: 130},
		{ID: "2.13in-250x122-bwr", Name: "2.13-inch, 250 × 122, red", Width: 250, Height: 122, DPI: 130, Palette: labels.PaletteBWR},
		{ID: "2.9in-296x128", Name: "2.9-inch, 296 × 128", Width: 296, Height: 128, DPI: 112},
		{ID: "2.9in-296x128-bwr", Name: "2.9-inch, 296 × 128, red", Width: 296, Height: 128, DPI: 112, Palette: labels.PaletteBWR},
		{ID: "4.2in-400x300", Name: "4.2-inch, 400 × 300", Width: 400, Height: 300, DPI: 120},
		{ID: "4.2in-400x300-bwr", Name: "4.2-inch, 400 × 300, red", Width: 400, Height: 300, DPI: 120, Palette: labels.PaletteBWR},
		{ID: "4.2in-400x300-bwry", Name: "4.2-inch, 400 × 300, red and yellow", Width: 400, Height: 300, DPI: 120, Palette: labels.PaletteBWRY},
		{ID: "5.65in-600x448-acep", Name: "5.65-inch, 600 × 448, seven colors", Width: 600, Height: 448, DPI: 133, Palette: labels.PaletteBWRYGBO},
		{ID: "7.3in-800x480-spectra6", Name: "7.3-inch, 800 × 480, Spectra 6", Width: 800, Height: 480, DPI: 128, Palette: labels.PaletteBWRYGB},
		{ID: "7.5in-800x480", Name: "7.5-inch, 800 × 480", Width: 800, Height: 480, DPI: 125},
		{ID: "7.5in-800x480-bwr", Name: "7.5-inch, 800 × 480, red", Width: 800, Height: 480, DPI: 125, Palette: labels.PaletteBWR},
	}
	// OpenEPaperLink's tags: second-hand shelf labels, nearly all with red.
	oeplPanels = []Panel{
		{ID: "1.54in", Name: "1.54-inch tag, 152 × 152", Width: 152, Height: 152, DPI: 140, Palette: labels.PaletteBWR},
		{ID: "2.6in", Name: "2.6-inch tag, 296 × 152", Width: 296, Height: 152, DPI: 128, Palette: labels.PaletteBWR},
		{ID: "2.9in", Name: "2.9-inch tag, 296 × 128", Width: 296, Height: 128, DPI: 112, Palette: labels.PaletteBWR},
		{ID: "4.2in", Name: "4.2-inch tag, 400 × 300", Width: 400, Height: 300, DPI: 120, Palette: labels.PaletteBWR},
		{ID: "7.4in", Name: "7.4-inch tag, 640 × 384", Width: 640, Height: 384, DPI: 101, Palette: labels.PaletteBWR},
	}
	trmnlPanels = []Panel{
		{ID: "og", Name: "TRMNL, 800 × 480", Width: 800, Height: 480, DPI: 125},
		{ID: "x", Name: "TRMNL X, 1872 × 1404", Width: 1872, Height: 1404, DPI: 227},
	}
	// paperlesspaper's frames, in Spectra 6, each as its offline firmware
	// takes its image: the 7-inch landscape, the 13-inch portrait.
	paperlessPanels = []Panel{
		{ID: "epd7", Name: "OpenPaper 7, 800 × 480", Width: 800, Height: 480, DPI: 128, Palette: labels.PaletteBWRYGB},
		{ID: "openpaper13", Name: "OpenPaper L, 1200 × 1600", Width: 1200, Height: 1600, DPI: 150, Palette: labels.PaletteBWRYGB},
	}
	// SoluM's labels, by their screens' sizes and inks, as AIMS reports
	// each label's: a push checks the label is its model's.
	solumPanels = func() []Panel {
		var out []Panel
		for _, s := range []struct {
			inch          string
			width, height int
			dpi           int
		}{
			{"1.6", 152, 152, 134}, {"2.2", 212, 104, 107}, {"2.13", 250, 122, 130}, {"2.6", 296, 152, 128}, {"2.9", 296, 128, 111},
			{"4.2", 400, 300, 119}, {"7.4", 640, 384, 101}, {"7.5", 800, 480, 124},
		} {
			for _, inks := range []struct {
				id, name string
				palette  labels.Palette
			}{{"bw", "", labels.PaletteBW}, {"bwr", ", red", labels.PaletteBWR}, {"bwy", ", yellow", labels.PaletteBWY}, {"bwry", ", red and yellow", labels.PaletteBWRY}} {
				out = append(out, Panel{ID: fmt.Sprintf("%sin-%dx%d-%s", s.inch, s.width, s.height, inks.id), Name: fmt.Sprintf("%s-inch, %d × %d%s", s.inch, s.width, s.height, inks.name),
					Width: s.width, Height: s.height, DPI: s.dpi, Palette: inks.palette})
			}
		}
		return out
	}()
)

// displayDriver drives a family of displays, each model a panel.
type displayDriver struct {
	name   string
	panels []Panel
	// connections are the connection types its displays take.
	connections []string
	// formats are the image formats it makes, the default first.
	formats []string
	// byMAC is set when its pull displays name themselves by their MAC
	// address, which the connection's address must then be.
	byMAC bool
}

var (
	genericDisplay   = displayDriver{name: DriverDisplay, panels: genericPanels, connections: []string{ConnPull, ConnWebhook}, formats: []string{FormatPNG, FormatBMP}}
	oeplDisplay      = displayDriver{name: DriverOpenEPaperLink, panels: oeplPanels, connections: []string{ConnOpenEPaperLink}, formats: []string{FormatJPEG}}
	trmnlDisplay     = displayDriver{name: DriverTRMNL, panels: trmnlPanels, connections: []string{ConnPull}, formats: []string{FormatPNG, FormatBMP}, byMAC: true}
	paperlessDisplay = displayDriver{name: DriverPaperlessPaper, panels: paperlessPanels, connections: []string{ConnPull, ConnPaperlessPaper}, formats: []string{FormatOpenPaper}}
	solumDisplay     = displayDriver{name: DriverSolum, panels: solumPanels, connections: []string{ConnSolum}, formats: []string{FormatPNG}}
)

// displayOptions are a display driver's options.
type displayOptions struct {
	Format         string `json:"format,omitempty"`
	Rotate180      bool   `json:"rotate_180,omitempty"`
	RefreshSeconds *int   `json:"refresh_seconds,omitempty"`
}

func (d displayDriver) pulls() bool { return slices.Contains(d.connections, ConnPull) }

func (d displayDriver) Info() DriverInfo {
	info := DriverInfo{Driver: d.name, ModelDPI: map[string]int{}}
	for _, p := range d.panels {
		info.Models = append(info.Models, p.ID)
		info.ModelDPI[p.ID] = p.DPI
		m := MediaInfo{ID: p.ID, Name: fmt.Sprintf("Screen, %d × %d", p.Width, p.Height), WidthMM: p.WidthMM(), LengthMM: p.HeightMM(), DieCut: true, Models: []string{p.ID}}
		if p.Inks() != labels.PaletteBW {
			if info.ModelPalette == nil {
				info.ModelPalette = map[string]labels.Palette{}
			}
			info.ModelPalette[p.ID] = p.Inks()
			m.Palette = p.Inks()
		}
		info.Media = append(info.Media, m)
	}
	if len(d.formats) > 1 {
		info.Options = append(info.Options, OptionInfo{Name: "format", Type: "string", Default: d.formats[0], Values: d.formats,
			Description: "The image file the display is given."})
	}
	info.Options = append(info.Options, OptionInfo{Name: "rotate_180", Type: "bool", Default: false,
		Description: "Show labels the other way up, for a display mounted upside down."})
	if d.pulls() {
		info.Options = append(info.Options, OptionInfo{Name: "refresh_seconds", Type: "int", Default: nil,
			Description: fmt.Sprintf("How long a display that pulls waits before asking again, %d to %d seconds; unset, the instance's default.", MinDisplayRefresh, MaxDisplayRefresh)})
	}
	return info
}

// Connections implements DisplayDriver.
func (d displayDriver) Connections() []string { return slices.Clone(d.connections) }

// CheckConnection implements DisplayDriver: a TRMNL names itself by its MAC
// address, which its pull connection's address must be, and other pull
// displays by the URL alone.
func (d displayDriver) CheckConnection(c Connection) error {
	if c.Type != ConnPull {
		return nil
	}
	if d.byMAC && c.Address == "" {
		return fmt.Errorf("a %s display's address is its MAC address, such as AA:BB:CC:DD:EE:FF, which it gives when it sets up", d.name)
	}
	if !d.byMAC && c.Address != "" {
		return fmt.Errorf("a %s display that pulls has no address: it fetches its image from its URL with its own token", d.name)
	}
	return nil
}

// DisplayPanel is a display driver's model's panel.
func DisplayPanel(driver, model string) (Panel, bool) {
	d, err := Lookup(driver)
	if err != nil {
		return Panel{}, false
	}
	dd, ok := d.(displayDriver)
	if !ok {
		return Panel{}, false
	}
	return dd.panel(model)
}

func (d displayDriver) panel(model string) (Panel, bool) {
	for _, p := range d.panels {
		if p.ID == model {
			return p, true
		}
	}
	return Panel{}, false
}

func (d displayDriver) settings(s Settings) (Panel, displayOptions, error) {
	p, ok := d.panel(s.Model)
	if !ok {
		ids := make([]string, len(d.panels))
		for i, p := range d.panels {
			ids[i] = p.ID
		}
		return p, displayOptions{}, fmt.Errorf("model must be one of %s", strings.Join(ids, ", "))
	}
	if s.Stock != nil || s.Media != p.ID {
		return p, displayOptions{}, fmt.Errorf("a display's media is its own screen, %s", p.ID)
	}
	var o displayOptions
	if len(s.Options) > 0 && string(s.Options) != "null" {
		dec := json.NewDecoder(bytes.NewReader(s.Options))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			return p, o, fmt.Errorf("options: %v", err)
		}
	}
	switch {
	case o.Format == "":
		o.Format = d.formats[0]
	case !slices.Contains(d.formats, o.Format) || len(d.formats) == 1:
		return p, o, fmt.Errorf("options: format must be one of %s", strings.Join(d.formats, ", "))
	}
	if o.RefreshSeconds != nil && (!d.pulls() || *o.RefreshSeconds < MinDisplayRefresh || *o.RefreshSeconds > MaxDisplayRefresh) {
		if !d.pulls() {
			return p, o, fmt.Errorf("options: refresh_seconds is for displays that pull")
		}
		return p, o, fmt.Errorf("options: refresh_seconds must be %d-%d", MinDisplayRefresh, MaxDisplayRefresh)
	}
	return p, o, nil
}

func (d displayDriver) Validate(s Settings) error {
	_, _, err := d.settings(s)
	return err
}

func (d displayDriver) Geometry(s Settings) (Geometry, error) {
	p, _, err := d.settings(s)
	if err != nil {
		return Geometry{}, err
	}
	return Geometry{DPI: p.DPI, Prints: "the " + p.Name + " panel draws", Roll: p.Name, WidthMM: p.WidthMM(), LengthMM: p.HeightMM(), Screen: true, Palette: p.Palette}, nil
}

func (d displayDriver) Plan(s Settings, f Fit) (Plan, error) {
	p, o, err := d.settings(s)
	if err != nil {
		return nil, err
	}
	return displayPlan{panel: p, rotate: f.Rotate, upsideDown: o.Rotate180, format: o.Format}, nil
}

// DisplayRefresh is the refresh interval a display's options give, in
// seconds, if they give one.
func DisplayRefresh(s Settings) (int, bool) {
	var o displayOptions
	if len(s.Options) == 0 || json.Unmarshal(s.Options, &o) != nil || o.RefreshSeconds == nil {
		return 0, false
	}
	return *o.RefreshSeconds, true
}

// BlankDisplay is the image a display shows before it is given a label: its
// screen, white, in its format.
func BlankDisplay(driver string, s Settings) ([]byte, error) {
	d, err := Lookup(driver)
	if err != nil {
		return nil, err
	}
	if _, ok := d.(DisplayDriver); !ok {
		return nil, fmt.Errorf("%w: %s drives printers, not displays", ErrInvalidPrinter, driver)
	}
	plan, err := d.Plan(s, Fit{})
	if err != nil {
		return nil, err
	}
	return plan.Encode(nil)
}

// DisplayContentType is the media type of an image a display plan made,
// from its first bytes.
func DisplayContentType(img []byte) string {
	switch {
	case bytes.HasPrefix(img, []byte("\x89PNG")):
		return "image/png"
	case bytes.HasPrefix(img, []byte("BM")):
		return "image/bmp"
	case bytes.HasPrefix(img, []byte{0xFF, 0xD8}):
		return "image/jpeg"
	}
	return "application/octet-stream"
}

// displayPlan makes the image for one display.
type displayPlan struct {
	panel      Panel
	rotate     bool
	upsideDown bool
	format     string
}

// Palette implements InkPlan.
func (p displayPlan) Palette() labels.Palette { return p.panel.Inks() }

// Encode implements Plan, for black-and-white panels: the last label, as
// the screen's image. None is a blank screen.
func (p displayPlan) Encode(labels []*image.Gray) ([]byte, error) {
	var last *image.Paletted
	if len(labels) > 0 {
		g := labels[len(labels)-1]
		last = image.NewPaletted(g.Bounds(), color.Palette{color.Black, color.White})
		for i, v := range g.Pix {
			if v >= 0x80 {
				last.Pix[i] = 1
			}
		}
	}
	return p.encode(last)
}

// EncodeInks implements InkPlan: the last label, drawn in the panel's
// inks, as the screen's image.
func (p displayPlan) EncodeInks(labels []*image.Paletted) ([]byte, error) {
	if len(labels) == 0 {
		return p.encode(nil)
	}
	return p.encode(labels[len(labels)-1])
}

func (p displayPlan) encode(label *image.Paletted) ([]byte, error) {
	screen := p.compose(label)
	var buf bytes.Buffer
	var err error
	switch p.format {
	case FormatBMP:
		err = writeBMP(&buf, screen, p.panel.DPI)
	case FormatJPEG:
		err = writeJPEG(&buf, screen)
	case FormatOpenPaper:
		err = writeOpenPaper(&buf, screen)
	default:
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, screen)
	}
	if err != nil {
		return nil, fmt.Errorf("encoding the display's image: %v", err)
	}
	return buf.Bytes(), nil
}

// compose lays a label on the panel's screen: turned as the fit says, the
// other way up if asked, centered, and cut to the screen. White is where
// it does not reach.
func (p displayPlan) compose(label *image.Paletted) *image.Paletted {
	inks := p.panel.Inks().Colors()
	palette := make(color.Palette, len(inks))
	for i, c := range inks {
		palette[i] = c
	}
	// White is every palette's second ink.
	const white = 1
	screen := image.NewPaletted(image.Rect(0, 0, p.panel.Width, p.panel.Height), palette)
	for i := range screen.Pix {
		screen.Pix[i] = white
	}
	if label == nil {
		return screen
	}
	// The label's dots, as indexes into the screen's palette.
	index := make([]uint8, len(label.Palette))
	for i, c := range label.Palette {
		index[i] = uint8(palette.Index(c))
	}
	b := label.Bounds()
	w, h := b.Dx(), b.Dy()
	at := func(x, y int) uint8 { return index[label.Pix[y*label.Stride+x]] }
	if p.rotate {
		// Turned 90° counterclockwise: its top right corner to the top left.
		w, h = h, w
		at0 := at
		at = func(x, y int) uint8 { return at0(b.Dx()-1-y, x) }
	}
	if p.upsideDown {
		at1 := at
		at = func(x, y int) uint8 { return at1(w-1-x, h-1-y) }
	}
	ox, oy := (p.panel.Width-w)/2, (p.panel.Height-h)/2
	for y := 0; y < h; y++ {
		sy := y + oy
		if sy < 0 || sy >= p.panel.Height {
			continue
		}
		for x := 0; x < w; x++ {
			sx := x + ox
			if sx < 0 || sx >= p.panel.Width {
				continue
			}
			screen.Pix[sy*screen.Stride+sx] = at(x, y)
		}
	}
	return screen
}

// writeJPEG writes a screen as a baseline JPEG at the highest quality, in
// gray when it is black and white.
func writeJPEG(w io.Writer, screen *image.Paletted) error {
	opts := &jpeg.Options{Quality: 100}
	if len(screen.Palette) == 2 {
		g := image.NewGray(screen.Bounds())
		for i, v := range screen.Pix {
			g.Pix[i] = color.GrayModel.Convert(screen.Palette[v]).(color.Gray).Y
		}
		return jpeg.Encode(w, g, opts)
	}
	rgba := image.NewRGBA(screen.Bounds())
	for y := 0; y < screen.Rect.Dy(); y++ {
		for x := 0; x < screen.Rect.Dx(); x++ {
			rgba.Set(x, y, screen.Palette[screen.Pix[y*screen.Stride+x]])
		}
	}
	return jpeg.Encode(w, rgba, opts)
}

// writeBMP writes a screen as an uncompressed Windows bitmap with a
// palette, at 1 bit a dot for black and white, 4 for up to 16 inks: the
// 1-bit bitmap TRMNL and many microcontroller libraries read.
func writeBMP(w io.Writer, screen *image.Paletted, dpi int) error {
	bpp := 8
	switch n := len(screen.Palette); {
	case n <= 2:
		bpp = 1
	case n <= 16:
		bpp = 4
	}
	width, height := screen.Rect.Dx(), screen.Rect.Dy()
	row := (width*bpp + 31) / 32 * 4
	colors := len(screen.Palette)
	offset := 14 + 40 + 4*colors
	size := offset + row*height
	ppm := uint32(float64(dpi)/0.0254 + 0.5)
	var head bytes.Buffer
	head.WriteString("BM")
	for _, v := range []any{uint32(size), uint32(0), uint32(offset),
		uint32(40), int32(width), int32(height), uint16(1), uint16(bpp), uint32(0), uint32(row * height), ppm, ppm, uint32(colors), uint32(0)} {
		_ = binary.Write(&head, binary.LittleEndian, v)
	}
	for _, c := range screen.Palette {
		r, g, b, _ := c.RGBA()
		head.Write([]byte{byte(b >> 8), byte(g >> 8), byte(r >> 8), 0})
	}
	if _, err := w.Write(head.Bytes()); err != nil {
		return err
	}
	// Rows bottom up, each padded to four bytes.
	line := make([]byte, row)
	for y := height - 1; y >= 0; y-- {
		clear(line)
		for x := 0; x < width; x++ {
			v := screen.Pix[y*screen.Stride+x]
			switch bpp {
			case 1:
				line[x/8] |= v << (7 - x%8)
			case 4:
				line[x/2] |= v << (4 * (1 - x%2))
			default:
				line[x] = v
			}
		}
		if _, err := w.Write(line); err != nil {
			return err
		}
	}
	return nil
}
