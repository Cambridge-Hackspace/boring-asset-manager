package printers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/brotherql"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/dymolm"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/dymolw"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/epsonlw"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/escpos"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/industrial"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/niimbot"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/ptouch"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/tspl"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers/zebra"
)

func TestValidate(t *testing.T) {
	brother := func(model, media, options string) Settings {
		return Settings{Model: model, Media: media, Options: json.RawMessage(options)}
	}
	ok := func(name, driver, host string, port int, s Settings) {
		t.Helper()
		if err := Validate(name, driver, tcp(host, port), s); err != nil {
			t.Errorf("rejected valid printer: %v", err)
		}
	}
	bad := func(why, name, driver, host string, port int, s Settings) {
		t.Helper()
		if err := Validate(name, driver, tcp(host, port), s); !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%s: got %v, want ErrInvalidPrinter", why, err)
		}
	}
	ok("Front desk", DriverBrotherQL, "192.168.1.40", 9100, brother("QL-810W", "dk-11201", ""))
	ok("Workshop", DriverBrotherQL, "ql-workshop.lan", 9100, brother("QL-820NWB", "dk-22205", `{"cut":false}`))
	ok("IPv6", DriverBrotherQL, "fd00::40", 9100, brother("QL-810W", "dk-22210", "null"))
	bad("blank name", " ", DriverBrotherQL, "10.0.0.1", 9100, brother("QL-810W", "dk-11201", ""))
	bad("driver", "P", "zebra", "10.0.0.1", 9100, brother("QL-810W", "dk-11201", ""))
	bad("model", "P", DriverBrotherQL, "10.0.0.1", 9100, brother("TD-2020", "dk-11201", ""))
	ok("QL-700", DriverBrotherQL, "ql.lan", 9100, brother("QL-700", "dk-11201", ""))
	ok("QL-500 without a cutter", DriverBrotherQL, "ql.lan", 9100, brother("QL-500", "dk-22205", ""))
	ok("QL-500 told not to cut", DriverBrotherQL, "ql.lan", 9100, brother("QL-500", "dk-22205", `{"cut":false}`))
	ok("4-inch roll on the QL-1100", DriverBrotherQL, "ql.lan", 9100, brother("QL-1100", "dk-11241", ""))
	ok("4-inch roll on the QL-1050", DriverBrotherQL, "ql.lan", 9100, brother("QL-1050", "dk-22243", ""))
	bad("QL-500 told to cut", "P", DriverBrotherQL, "ql.lan", 9100, brother("QL-500", "dk-22205", `{"cut":true}`))
	bad("4-inch roll on a 62 mm model", "P", DriverBrotherQL, "ql.lan", 9100, brother("QL-810W", "dk-11241", ""))
	bad("103 mm roll on the QL-1115NWB", "P", DriverBrotherQL, "ql.lan", 9100, brother("QL-1115NWB", "dk-22246", ""))
	bad("roll the QL-500 does not list", "P", DriverBrotherQL, "ql.lan", 9100, brother("QL-500", "brother-29x42", ""))
	ok("P-touch", DriverBrotherPTouch, "pt.lan", 9100, brother("PT-P750W", "tze-12", ""))
	ok("P-touch options", DriverBrotherPTouch, "pt.lan", 9100, brother("PT-P950NW", "tze-36",
		`{"cut_after":"last_label","half_cut":true,"chain":true,"mirror":true,"margin_mm":5}`))
	ok("P-touch tube", DriverBrotherPTouch, "pt.lan", 9100, brother("PT-P700", "hse-11.7", ""))
	bad("P-touch model", "P", DriverBrotherPTouch, "pt.lan", 9100, brother("QL-810W", "tze-12", ""))
	bad("36 mm tape on a 24 mm model", "P", DriverBrotherPTouch, "pt.lan", 9100, brother("PT-P750W", "tze-36", ""))
	bad("tube on the PT-P910BT", "P", DriverBrotherPTouch, "pt.lan", 9100, brother("PT-P910BT", "hse-11.7", ""))
	bad("half cut on the PT-P710BT", "P", DriverBrotherPTouch, "pt.lan", 9100, brother("PT-P710BT", "tze-12", `{"half_cut":true}`))
	bad("P-touch margin too small", "P", DriverBrotherPTouch, "pt.lan", 9100, brother("PT-P750W", "tze-12", `{"margin_mm":1}`))
	bad("P-touch cut", "P", DriverBrotherPTouch, "pt.lan", 9100, brother("PT-P750W", "tze-12", `{"cut_after":"never"}`))
	bad("P-touch unknown option", "P", DriverBrotherPTouch, "pt.lan", 9100, brother("PT-P750W", "tze-12", `{"cut":true}`))
	ok("LabelManager", DriverDymoLabelManager, "lm.lan", 9100, brother("LabelManager PnP", "d1-12", ""))
	ok("LabelManager options", DriverDymoLabelManager, "lm.lan", 9100, brother("LabelManager 500TS", "d1-24",
		`{"tape_type":"white_on_black","cut_after":"last_label","margin_mm":20}`))
	bad("LabelManager model", "P", DriverDymoLabelManager, "lm.lan", 9100, brother("PT-P750W", "d1-12", ""))
	bad("19 mm tape on a 12 mm model", "P", DriverDymoLabelManager, "lm.lan", 9100, brother("LabelManager 280", "d1-19", ""))
	bad("cut_after without a cutter", "P", DriverDymoLabelManager, "lm.lan", 9100, brother("LabelManager 420P", "d1-12", `{"cut_after":"each_label"}`))
	bad("LabelManager margin short of the cutter", "P", DriverDymoLabelManager, "lm.lan", 9100, brother("LabelManager PnP", "d1-12", `{"margin_mm":8}`))
	bad("LabelManager tape type", "P", DriverDymoLabelManager, "lm.lan", 9100, brother("LabelManager PnP", "d1-12", `{"tape_type":"gold"}`))
	bad("LabelManager unknown option", "P", DriverDymoLabelManager, "lm.lan", 9100, brother("LabelManager PnP", "d1-12", `{"mirror":true}`))
	ok("LabelWorks", DriverEpsonLabelWorks, "lw.lan", 9100, brother("LW-C410", "lk-12", ""))
	ok("LabelWorks options", DriverEpsonLabelWorks, "lw.lan", 9100, brother("LW-PX400", "lk-24",
		`{"cut_after":"never","density":-5,"margin_mm":0}`))
	bad("LabelWorks model", "P", DriverEpsonLabelWorks, "lw.lan", 9100, brother("LW-700", "lk-12", ""))
	bad("24 mm tape on the LW-C410", "P", DriverEpsonLabelWorks, "lw.lan", 9100, brother("LW-C410", "lk-24", ""))
	bad("4 mm tape on the LW-600P", "P", DriverEpsonLabelWorks, "lw.lan", 9100, brother("LW-600P", "lk-4", ""))
	bad("LabelWorks cut", "P", DriverEpsonLabelWorks, "lw.lan", 9100, brother("LW-600P", "lk-12", `{"cut_after":"half"}`))
	bad("LabelWorks density", "P", DriverEpsonLabelWorks, "lw.lan", 9100, brother("LW-600P", "lk-12", `{"density":6}`))
	bad("LabelWorks margin", "P", DriverEpsonLabelWorks, "lw.lan", 9100, brother("LW-600P", "lk-12", `{"margin_mm":128}`))
	bad("LabelWorks unknown option", "P", DriverEpsonLabelWorks, "lw.lan", 9100, brother("LW-600P", "lk-12", `{"mirror":true}`))
	ok("TD-2", DriverBrotherTD, "td.lan", 9100, brother("TD-2130N", "rd-51x26", ""))
	ok("TD-4 with its cutter", DriverBrotherTD, "td.lan", 9100, brother("TD-4420DN", "rd-102x152", `{"cut":true}`))
	bad("TD model", "P", DriverBrotherTD, "td.lan", 9100, brother("QL-810W", "rd-58", ""))
	bad("DK roll on a TD", "P", DriverBrotherTD, "td.lan", 9100, brother("TD-4420DN", "dk-22205", ""))
	bad("4-inch roll on a TD-2", "P", DriverBrotherTD, "td.lan", 9100, brother("TD-2020", "rd-102", ""))
	bad("TD-2 told to cut", "P", DriverBrotherTD, "td.lan", 9100, brother("TD-2020", "rd-58", `{"cut":true}`))
	bad("media", "P", DriverBrotherQL, "10.0.0.1", 9100, brother("QL-810W", "dk-9999", ""))
	bad("unknown option", "P", DriverBrotherQL, "10.0.0.1", 9100, brother("QL-810W", "dk-11201", `{"cutt":true}`))
	bad("option type", "P", DriverBrotherQL, "10.0.0.1", 9100, brother("QL-810W", "dk-11201", `{"cut":"yes"}`))
	bad("host with scheme", "P", DriverBrotherQL, "tcp://10.0.0.1", 9100, brother("QL-810W", "dk-11201", ""))
	bad("host with port", "P", DriverBrotherQL, "printer:9100", 9100, brother("QL-810W", "dk-11201", ""))
	bad("empty host", "P", DriverBrotherQL, "", 9100, brother("QL-810W", "dk-11201", ""))
	bad("port", "P", DriverBrotherQL, "10.0.0.1", 70000, brother("QL-810W", "dk-11201", ""))

	ok("Dymo", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "30252", ""))
	ok("Dymo dark", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 550", "30334", `{"density":"dark"}`))
	ok("Dymo twin", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450 Twin Turbo", "30252", `{"roll":"right"}`))
	ok("Dymo XL", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 4XL", "1744907", ""))
	bad("Dymo model", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("QL-810W", "30252", ""))
	bad("Dymo media", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "dk-11201", ""))
	bad("4-inch roll on a 56 mm head", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "1744907", ""))
	bad("roll on a single-roll model", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "30252", `{"roll":"left"}`))
	bad("unknown roll", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter Twin Turbo", "30252", `{"roll":"middle"}`))
	bad("unknown density", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "30252", `{"density":"max"}`))
	bad("Dymo unknown option", "P", DriverDymoLabelWriter, "lw.lan", 9100, brother("LabelWriter 450", "30252", `{"cut":true}`))
	ok("Rollo", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1038", "4x6", ""))
	ok("Rollo options", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1040", "2x1", `{"density":10,"rotate_180":true}`))
	bad("TSPL model", "P", DriverTSPL, "rollo.lan", 9100, brother("LabelWriter 450", "4x6", ""))
	bad("TSPL media", "P", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1038", "30252", ""))
	bad("TSPL density", "P", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1038", "4x6", `{"density":16}`))
	bad("TSPL unknown option", "P", DriverTSPL, "rollo.lan", 9100, brother("Rollo X1038", "4x6", `{"speed":4}`))
	ok("Zebra", DriverZPL, "zebra.lan", 9100, brother("Zebra GK420d", "4x6", ""))
	ok("Zebra options", DriverZPL, "zebra.lan", 9100, brother("Zebra ZT411 300 DPI", "4x2",
		`{"media_type":"transfer","darkness":20,"speed":6,"print_mode":"cutter","calibrate":true,"compress":false}`))
	bad("ZPL model", "P", DriverZPL, "zebra.lan", 9100, brother("Zebra LP 2844", "4x6", ""))
	bad("ZPL media", "P", DriverZPL, "zebra.lan", 9100, brother("Zebra GK420d", "30252", ""))
	bad("4-inch roll on a 2-inch head", "P", DriverZPL, "zebra.lan", 9100, brother("Zebra ZD410", "4x6", ""))
	bad("ZPL darkness", "P", DriverZPL, "zebra.lan", 9100, brother("Zebra GK420d", "4x6", `{"darkness":31}`))
	bad("ZPL print mode", "P", DriverZPL, "zebra.lan", 9100, brother("Zebra GK420d", "4x6", `{"print_mode":"rewind"}`))
	bad("ZPL media type", "P", DriverZPL, "zebra.lan", 9100, brother("Zebra GK420d", "4x6", `{"media_type":"ribbon"}`))
	bad("ZPL speed", "P", DriverZPL, "zebra.lan", 9100, brother("Zebra GK420d", "4x6", `{"speed":15}`))
	bad("ZPL unknown option", "P", DriverZPL, "zebra.lan", 9100, brother("Zebra GK420d", "4x6", `{"density":4}`))
	ok("EPL2", DriverEPL2, "lp.lan", 9100, brother("Zebra LP 2844", "4x6", `{"density":8}`))
	bad("EPL2 model", "P", DriverEPL2, "lp.lan", 9100, brother("Zebra GK420d", "4x6", ""))
	bad("EPL2 density", "P", DriverEPL2, "lp.lan", 9100, brother("Zebra LP 2844", "4x6", `{"density":16}`))
	bad("EPL2 unknown option", "P", DriverEPL2, "lp.lan", 9100, brother("Zebra LP 2844", "4x6", `{"darkness":4}`))
	ok("DPL", DriverDPL, "datamax.lan", 9100, brother("Datamax I-Class 300 DPI", "4x6", ""))
	ok("DPL options", DriverDPL, "datamax.lan", 9100, brother("Honeywell PM43 203 DPI", "4x2",
		`{"heat":20,"speed":6,"media_sensor":"mark","image_module":"G"}`))
	bad("DPL model", "P", DriverDPL, "datamax.lan", 9100, brother("SATO WS408", "4x6", ""))
	bad("DPL heat", "P", DriverDPL, "datamax.lan", 9100, brother("Datamax E-Class 203 DPI", "4x6", `{"heat":31}`))
	bad("DPL speed", "P", DriverDPL, "datamax.lan", 9100, brother("Datamax E-Class 203 DPI", "4x6", `{"speed":13}`))
	bad("DPL sensor", "P", DriverDPL, "datamax.lan", 9100, brother("Datamax E-Class 203 DPI", "4x6", `{"media_sensor":"none"}`))
	bad("DPL module", "P", DriverDPL, "datamax.lan", 9100, brother("Datamax E-Class 203 DPI", "4x6", `{"image_module":"dram"}`))
	bad("DPL unknown option", "P", DriverDPL, "datamax.lan", 9100, brother("Datamax E-Class 203 DPI", "4x6", `{"darkness":4}`))
	ok("SBPL", DriverSBPL, "sato.lan", 9100, brother("SATO CL4NX 305 DPI", "4x6", ""))
	ok("SBPL options", DriverSBPL, "sato.lan", 9100, brother("SATO CL6NX 203 DPI", "continuous-4",
		`{"darkness":5,"speed":8,"media_sensor":"none","stx_etx":true}`))
	bad("SBPL darkness", "P", DriverSBPL, "sato.lan", 9100, brother("SATO WS408", "4x6", `{"darkness":0}`))
	bad("SBPL speed", "P", DriverSBPL, "sato.lan", 9100, brother("SATO WS408", "4x6", `{"speed":11}`))
	bad("SBPL sensor", "P", DriverSBPL, "sato.lan", 9100, brother("SATO WS408", "4x6", `{"media_sensor":"continuous"}`))
	bad("SBPL unknown option", "P", DriverSBPL, "sato.lan", 9100, brother("SATO WS408", "4x6", `{"heat":4}`))
	ok("EZPL", DriverEZPL, "godex.lan", 9100, brother("Godex G500", "4x6", ""))
	ok("EZPL options", DriverEZPL, "godex.lan", 9100, brother("Godex RT230", "4x3",
		`{"darkness":12,"speed":5,"media_type":"transfer","media_sensor":"mark"}`))
	bad("EZPL model", "P", DriverEZPL, "godex.lan", 9100, brother("Datamax E-Class 203 DPI", "4x6", ""))
	bad("EZPL darkness", "P", DriverEZPL, "godex.lan", 9100, brother("Godex G500", "4x6", `{"darkness":20}`))
	bad("EZPL speed", "P", DriverEZPL, "godex.lan", 9100, brother("Godex G500", "4x6", `{"speed":8}`))
	bad("EZPL media type", "P", DriverEZPL, "godex.lan", 9100, brother("Godex G500", "4x6", `{"media_type":"ribbon"}`))
	bad("EZPL unknown option", "P", DriverEZPL, "godex.lan", 9100, brother("Godex G500", "4x6", `{"stx_etx":true}`))
}

func TestDriversDescribeEveryRoll(t *testing.T) {
	d := Drivers()
	by := map[string]DriverInfo{}
	for _, info := range d {
		by[info.Driver] = info
	}
	// The test laser is there only once a test has registered it.
	n := len(d)
	if _, ok := by[DriverTestLaser]; ok {
		n--
	}
	if n != 23 || len(by) != len(d) {
		t.Fatalf("Drivers() = %+v", d)
	}
	if b := by[DriverBrotherQL]; len(b.Media) != len(brotherql.MediaIDs()) || len(b.Options) != 1 || b.CustomStock != nil ||
		len(b.Models) != len(brotherql.Models(brotherql.SeriesQL)) || b.ModelDPI["QL-500"] != 300 || b.ModelDPI["QL-1100"] != 300 {
		t.Fatalf("Brother driver info = %+v", b)
	}
	// Each roll says which models take it, when not all do.
	for _, m := range by[DriverBrotherQL].Media {
		switch m.ID {
		case "dk-22205":
			if m.Models != nil {
				t.Errorf("62 mm tape is for %v, not every model", m.Models)
			}
		case "dk-11241":
			if strings.Join(m.Models, ",") != "QL-1050,QL-1060N,QL-1100,QL-1110NWB,QL-1115NWB" {
				t.Errorf("DK-11241 is for %v", m.Models)
			}
		}
	}
	pt := by[DriverBrotherPTouch]
	if len(pt.Media) != len(ptouch.MediaIDs()) || len(pt.Options) != 5 || pt.CustomStock != nil ||
		pt.ModelDPI["PT-P750W"] != 180 || pt.ModelDPI["PT-P950NW"] != 360 || len(pt.Models) != len(ptouch.Models()) {
		t.Fatalf("Brother P-touch driver info = %+v", pt)
	}
	for _, m := range pt.Media {
		if m.ID == "tze-36" && strings.Join(m.Models, ",") != "PT-P900,PT-P900W,PT-P910BT,PT-P950NW" {
			t.Errorf("36 mm tape is for %v", m.Models)
		}
		if m.ID == "tze-12" && m.Models != nil {
			t.Errorf("12 mm tape is for %v, not every model", m.Models)
		}
	}
	td := by[DriverBrotherTD]
	if len(td.Media) != len(brotherql.SeriesMediaIDs(brotherql.SeriesTD)) || len(td.Options) != 1 || td.CustomStock == nil ||
		td.ModelDPI["TD-2020"] != 203 || td.ModelDPI["TD-4550DNWB"] != 300 || td.CustomStock.Note == "" {
		t.Fatalf("Brother TD driver info = %+v", td)
	}
	if w := td.CustomStock.MaxWidthMM; w["TD-2020"] != 63 || w["TD-4420DN"] != 118 {
		t.Errorf("TD custom stock widths = %v", w)
	}
	if c := td.CustomStock; c.MinLengthMM != 12.1 || c.MaxLengthMM < 2999 || c.MaxLengthMM > 3000 {
		t.Errorf("TD custom stock lengths %g-%g mm", c.MinLengthMM, c.MaxLengthMM)
	}
	dymo := by[DriverDymoLabelWriter]
	if len(dymo.Media) != len(dymolw.MediaIDs()) || len(dymo.Options) != 2 || len(dymo.Options[0].Values) != 4 {
		t.Fatalf("Dymo driver info = %+v", dymo)
	}
	lm := by[DriverDymoLabelManager]
	if len(lm.Media) != len(dymolm.MediaIDs()) || len(lm.Options) != 3 || lm.CustomStock != nil || len(lm.Options[0].Values) != 13 ||
		lm.ModelDPI["LabelManager PnP"] != 180 || lm.ModelDPI["LabelManager Wireless PnP"] != 300 || len(lm.Models) != len(dymolm.Models()) {
		t.Fatalf("DYMO LabelManager driver info = %+v", lm)
	}
	for _, m := range lm.Media {
		if m.ID == "d1-24" && strings.Join(m.Models, ",") != "LabelManager 500TS,LabelManager Wireless PnP" {
			t.Errorf("24 mm tape is for %v", m.Models)
		}
		if m.ID == "d1-12" && m.Models != nil {
			t.Errorf("12 mm tape is for %v, not every model", m.Models)
		}
	}
	lw := by[DriverEpsonLabelWorks]
	if len(lw.Media) != len(epsonlw.MediaIDs()) || len(lw.Options) != 3 || lw.CustomStock != nil || len(lw.Options[0].Values) != 3 ||
		lw.ModelDPI["LW-600P"] != 180 || lw.ModelDPI["LW-C410"] != 180 || len(lw.Models) != 3 {
		t.Fatalf("Epson LabelWorks driver info = %+v", lw)
	}
	for _, m := range lw.Media {
		if m.ID == "lk-4" && strings.Join(m.Models, ",") != "LW-C410,LW-PX400" {
			t.Errorf("4 mm tape is for %v", m.Models)
		}
		if m.ID == "lk-24" && strings.Join(m.Models, ",") != "LW-600P,LW-PX400" {
			t.Errorf("24 mm tape is for %v", m.Models)
		}
		if m.ID == "lk-12" && m.Models != nil {
			t.Errorf("12 mm tape is for %v, not every model", m.Models)
		}
	}
	if ts := by[DriverTSPL]; len(ts.Media) != len(tspl.MediaIDs()) || len(ts.Options) != 2 {
		t.Fatalf("TSPL driver info = %+v", ts)
	}
	if e := by[DriverESCPOS]; len(e.Media) != len(escpos.MediaIDs(false)) || len(e.Options) != 3 || len(e.Models) != 2 || e.BLE != nil {
		t.Fatalf("ESC/POS driver info = %+v", e)
	}
	if p := by[DriverPhomemo]; len(p.Media) != len(escpos.MediaIDs(true)) || len(p.Options) != 3 || len(p.Models) != 9 || p.BLE == nil || p.ModelDPI["D30"] != 203 {
		t.Fatalf("Phomemo driver info = %+v", p)
	}
	if nb := by[DriverNiimbot]; len(nb.Media) != len(niimbot.MediaIDs()) || len(nb.Options) != 2 || len(nb.Models) != len(niimbot.Models()) || nb.BLE == nil {
		t.Fatalf("Niimbot driver info = %+v", nb)
	}
	zpl, epl2 := by[DriverZPL], by[DriverEPL2]
	if len(zpl.Media) != len(zebra.MediaIDs()) || len(zpl.Options) != 6 || len(zpl.Models) != len(zebra.Models(zebra.ZPL)) || zpl.ModelDPI["Zebra GX430t"] != 300 {
		t.Fatalf("ZPL driver info = %+v", zpl)
	}
	if len(epl2.Options) != 1 || len(epl2.Models) != len(zebra.Models(zebra.EPL2)) || epl2.ModelDPI["Zebra LP 2824"] != 203 {
		t.Fatalf("EPL2 driver info = %+v", epl2)
	}
	for _, c := range []struct {
		driver  string
		lang    industrial.Language
		options int
		model   string
		dpi     int
	}{
		{DriverDPL, industrial.DPL, 4, "Datamax I-Class 600 DPI", 600},
		{DriverSBPL, industrial.SBPL, 4, "SATO CL4NX 305 DPI", 300},
		{DriverEZPL, industrial.EZPL, 4, "Godex G530", 300},
	} {
		info := by[c.driver]
		if len(info.Media) != len(zebra.MediaIDs()) || len(info.Options) != c.options || len(info.Models) != len(industrial.Models(c.lang)) || info.ModelDPI[c.model] != c.dpi {
			t.Fatalf("%s driver info = %+v", c.driver, info)
		}
	}
	// The drivers that take custom stock say how wide, for every model; a
	// laser has no roll.
	for _, info := range d {
		if info.Driver == DriverBrotherQL || info.Driver == DriverBrotherPTouch || info.Driver == DriverDymoLabelManager || info.Driver == DriverEpsonLabelWorks || info.Display != nil || info.OneShot != nil {
			continue
		}
		c := info.CustomStock
		if c == nil || len(c.MaxWidthMM) != len(info.Models) || c.MinLengthMM <= 0 || c.MaxLengthMM <= c.MinLengthMM {
			t.Errorf("%s custom stock = %+v", info.Driver, c)
		}
	}
	if w := by[DriverTSPL].CustomStock.MaxWidthMM["Rollo X1038"]; w < 105 || w > 106.5 {
		t.Errorf("Rollo takes custom stock up to %g mm", w)
	}
	if w := zpl.CustomStock.MaxWidthMM["Zebra ZD410"]; w < 57 || w > 58.1 {
		t.Errorf("the ZD410 takes custom stock up to %g mm", w)
	}
	if dymo.CustomStock.Note == "" {
		t.Error("the DYMO driver says nothing of the 550 series and genuine labels")
	}
}

func TestValidateStock(t *testing.T) {
	good := []Stock{
		{Name: "Shelf edge", WidthMM: 40, LengthMM: 12, GapMM: 3},
		{Name: "Tape", WidthMM: 50},
		{Name: "Gapless", WidthMM: 50, LengthMM: 25},
	}
	for _, s := range good {
		if err := ValidateStock(s); err != nil {
			t.Errorf("%+v: %v", s, err)
		}
	}
	bad := []Stock{
		{Name: " ", WidthMM: 40},
		{Name: "Thin", WidthMM: 2},
		{Name: "Wide", WidthMM: 400},
		{Name: "Short", WidthMM: 40, LengthMM: 1},
		{Name: "Gap on tape", WidthMM: 40, GapMM: 3},
		{Name: "Big gap", WidthMM: 40, LengthMM: 20, GapMM: 40},
		{Name: "Negative gap", WidthMM: 40, LengthMM: 20, GapMM: -1},
	}
	for _, s := range bad {
		if err := ValidateStock(s); !errors.Is(err, ErrInvalidPrinter) {
			t.Errorf("%+v: err = %v", s, err)
		}
	}
}

func TestCustomStockOnEachDriver(t *testing.T) {
	stock := func(w, l, gap float64) *Stock {
		return &Stock{ID: "11111111-2222-3333-4444-555555555555", Name: "Custom", WidthMM: w, LengthMM: l, GapMM: gap}
	}
	cases := []struct {
		why, driver, model string
		stock              *Stock
		ok                 bool
	}{
		{"Brother takes only its rolls", DriverBrotherQL, "QL-810W", stock(62, 0, 0), false},
		{"P-touch takes only its cassettes", DriverBrotherPTouch, "PT-P750W", stock(12, 0, 0), false},
		{"LabelManager takes only its cassettes", DriverDymoLabelManager, "LabelManager PnP", stock(12, 0, 0), false},
		{"LabelWorks takes only its cartridges", DriverEpsonLabelWorks, "LW-PX400", stock(12, 0, 0), false},
		{"Brother's 4-inch QL takes only its rolls too", DriverBrotherQL, "QL-1100", stock(102, 0, 0), false},
		{"TD-2 die-cut", DriverBrotherTD, "TD-2020", stock(50, 30, 3), true},
		{"TD-2 wider than it takes", DriverBrotherTD, "TD-2130N", stock(76, 0, 0), false},
		{"TD-4 continuous", DriverBrotherTD, "TD-4410D", stock(100, 0, 0), true},
		{"TD-4 at its widest", DriverBrotherTD, "TD-4550DNWB", stock(118, 80, 3), true},
		{"TD-4 wider than it takes", DriverBrotherTD, "TD-4550DNWB", stock(120, 80, 3), false},
		{"TSPL die-cut", DriverTSPL, "Rollo X1038", stock(60, 40, 2.5), true},
		{"TSPL continuous", DriverTSPL, "Rollo X1038", stock(80, 0, 0), true},
		{"TSPL wider than the head", DriverTSPL, "Rollo X1038", stock(120, 40, 3), false},
		{"DYMO die-cut", DriverDymoLabelWriter, "LabelWriter 450", stock(50, 30, 0), true},
		{"DYMO 550 too", DriverDymoLabelWriter, "LabelWriter 550", stock(50, 30, 0), true},
		{"DYMO wide roll on a narrow head", DriverDymoLabelWriter, "LabelWriter 450", stock(80, 30, 0), false},
		{"DYMO wide roll on the 4XL", DriverDymoLabelWriter, "LabelWriter 4XL", stock(100, 150, 0), true},
		{"DYMO wider than the 4XL", DriverDymoLabelWriter, "LabelWriter 4XL", stock(120, 150, 0), false},
		{"ZPL die-cut", DriverZPL, "Zebra ZD421 300 DPI", stock(60, 40, 2.5), true},
		{"ZPL continuous", DriverZPL, "Zebra ZT411 600 DPI", stock(80, 0, 0), true},
		{"ZPL wider than the head", DriverZPL, "Zebra ZD410", stock(80, 40, 3), false},
		{"EPL2 die-cut", DriverEPL2, "Zebra TLP 2844", stock(60, 40, 2.5), true},
		{"ESC/POS continuous", DriverESCPOS, "Generic ESC/POS 80 mm", stock(76, 0, 0), true},
		{"ESC/POS wider than its paper", DriverESCPOS, "Generic ESC/POS 58 mm", stock(60, 0, 0), false},
		{"Phomemo die-cut", DriverPhomemo, "M221", stock(60, 40, 2), true},
		{"Phomemo tape", DriverPhomemo, "D35", stock(12, 50, 3), true},
		{"Phomemo continuous tape", DriverPhomemo, "D35", stock(12, 0, 0), false},
		{"DPL die-cut", DriverDPL, "Datamax M-Class 300 DPI", stock(60, 40, 2.5), true},
		{"DPL continuous", DriverDPL, "Datamax I-Class 600 DPI", stock(100, 0, 0), true},
		{"DPL wider than the head", DriverDPL, "Datamax E-Class 203 DPI", stock(120, 40, 3), false},
		{"SBPL on the 6-inch CL6NX", DriverSBPL, "SATO CL6NX 203 DPI", stock(152, 100, 3), true},
		{"SBPL 6-inch roll on a 4-inch head", DriverSBPL, "SATO CL4NX 203 DPI", stock(152, 100, 3), false},
		{"EZPL die-cut", DriverEZPL, "Godex EZ-1300Plus", stock(60, 40, 2.5), true},
	}
	for _, c := range cases {
		d, _ := Lookup(c.driver)
		s := Settings{Model: c.model, Media: c.stock.ID, Stock: c.stock}
		if err := d.Validate(s); (err == nil) != c.ok {
			t.Errorf("%s: Validate = %v", c.why, err)
		}
		if _, err := planWith(d, s, LabelSpec{WidthMM: c.stock.WidthMM, HeightMM: max(c.stock.LengthMM, 30), DPI: d.Info().ModelDPI[c.model]}); (err == nil) != c.ok {
			t.Errorf("%s: Plan = %v", c.why, err)
		}
	}
}

// TestCustomStockReachesThePrinter checks that a TSPL printer is told the
// custom stock's size and gap, and a LabelWriter its label length.
func TestCustomStockReachesThePrinter(t *testing.T) {
	s := Settings{Model: "Rollo X1038", Media: "x", Stock: &Stock{ID: "x", Name: "Bin labels", WidthMM: 60, LengthMM: 40, GapMM: 2.5}}
	d, _ := Lookup(DriverTSPL)
	plan, err := planWith(d, s, LabelSpec{WidthMM: 60, HeightMM: 40, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := plan.Encode([]*image.Gray{blankLabel(60, 40, 203)})
	if err != nil {
		t.Fatal(err)
	}
	job, err := tspl.DecodeJob(stream)
	if err != nil {
		t.Fatal(err)
	}
	setup := strings.Join(job.Setup, "; ")
	if !strings.Contains(setup, "SIZE 60 mm,40 mm") || !strings.Contains(setup, "GAP 2.5 mm,0 mm") {
		t.Errorf("setup = %s", setup)
	}

	s = Settings{Model: "LabelWriter 450", Media: "y", Stock: &Stock{ID: "y", Name: "Tags", WidthMM: 50, LengthMM: 30}}
	d, _ = Lookup(DriverDymoLabelWriter)
	plan, err = planWith(d, s, LabelSpec{WidthMM: 50, HeightMM: 30, DPI: 300})
	if err != nil {
		t.Fatal(err)
	}
	// Marked on its last line, which would otherwise be left to the form feed.
	tag := blankLabel(50, 30, 300)
	tag.Pix[(tag.Rect.Dy()-1)*tag.Stride] = 0
	stream, err = plan.Encode([]*image.Gray{tag})
	if err != nil {
		t.Fatal(err)
	}
	dj, err := dymolw.DecodeJob(dymolw.ProtocolV1, stream, 672)
	if err != nil {
		t.Fatal(err)
	}
	if len(dj.Pages) != 1 || dj.Pages[0].Image.Bounds().Dy() != 354 {
		t.Errorf("decoded %d pages, the first %v", len(dj.Pages), dj.Pages[0].Image.Bounds())
	}
}

func blankLabel(wMM, hMM float64, dpi int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, int(wMM/25.4*float64(dpi)+0.5), int(hMM/25.4*float64(dpi)+0.5)))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	return img
}

type namedDriver struct {
	brotherDriver
	name string
}

func (n namedDriver) Info() DriverInfo { return DriverInfo{Driver: n.name} }

func TestRegisterRejectsDuplicatesAndBlankNames(t *testing.T) {
	if err := Register(namedDriver{name: DriverBrotherQL}); err == nil {
		t.Error("registered brother_ql twice")
	}
	if err := Register(namedDriver{name: ""}); err == nil {
		t.Error("registered a driver without a name")
	}
}

func TestBrotherPlan(t *testing.T) {
	d, err := Lookup(DriverBrotherQL)
	if err != nil {
		t.Fatal(err)
	}
	settings := Settings{Model: "QL-810W", Media: "dk-11201"}

	if _, err := planWith(d, settings, LabelSpec{WidthMM: 40, HeightMM: 20, DPI: 300}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("wrong size: got %v, want ErrDoesNotFit", err)
	}

	label := image.NewGray(image.Rect(0, 0, 1063, 343)) // 90 × 29 mm, all black
	for _, cut := range []bool{true, false} {
		settings.Options = json.RawMessage(`{"cut":` + strconv.FormatBool(cut) + `}`)
		plan, err := planWith(d, settings, LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300})
		if err != nil {
			t.Fatal(err)
		}
		job, err := plan.Encode([]*image.Gray{label, label})
		if err != nil {
			t.Fatal(err)
		}
		pages, err := brotherql.DecodeJob(job)
		if err != nil {
			t.Fatal(err)
		}
		if len(pages) != 2 || len(pages[0].Lines) != 991 || pages[0].AutoCut != cut {
			t.Errorf("cut=%v: decoded %d pages, %d lines, auto-cut %v", cut, len(pages), len(pages[0].Lines), pages[0].AutoCut)
		}
	}
}

// TestBrotherTDPlan checks that a TD printer prints custom stock at its
// size: placed on the head by the TD references' offsets, and told the
// roll's type and size without asking the printer to check them.
func TestBrotherTDPlan(t *testing.T) {
	d, err := Lookup(DriverBrotherTD)
	if err != nil {
		t.Fatal(err)
	}
	s := Settings{Model: "TD-4420DN", Media: "x", Stock: &Stock{ID: "x", Name: "Bin labels", WidthMM: 60, LengthMM: 40, GapMM: 3}}
	plan, err := planWith(d, s, LabelSpec{WidthMM: 60, HeightMM: 40, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	stream, err := plan.Encode([]*image.Gray{blankLabel(60, 40, 203)})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := brotherql.LookupModel("TD-4420DN")
	pages, err := model.DecodeJob(stream)
	if err != nil {
		t.Fatal(err)
	}
	// 40 mm is 320 lines, less 24 at each end.
	p := pages[0]
	if len(pages) != 1 || p.MediaType != 0x0B || p.WidthMM != 60 || p.LengthMM != 40 || p.InfoFlags != 0x80 || len(p.Lines) != 272 || p.AutoCut {
		t.Errorf("decoded %d pages, the first %+v with %d lines", len(pages), p, len(p.Lines))
	}
	if len(p.Lines[0]) != 104 {
		t.Errorf("%d-byte lines on the 832-pin head", len(p.Lines[0]))
	}

	// Brother's own RD rolls are built in, for the series that takes them.
	s = Settings{Model: "TD-2020", Media: "rd-58"}
	plan, err = planWith(d, s, LabelSpec{WidthMM: 58, HeightMM: 30, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	if stream, err = plan.Encode([]*image.Gray{blankLabel(58, 30, 203)}); err != nil {
		t.Fatal(err)
	}
	model, _ = brotherql.LookupModel("TD-2020")
	if pages, err = model.DecodeJob(stream); err != nil || len(pages[0].Lines[0]) != 56 || pages[0].FeedDots != 24 || pages[0].MediaType != 0x0A {
		t.Errorf("TD-2020 stream: %v", err)
	}
}

// TestQLModelsPlan checks a model of each QL family through the registry.
func TestQLModelsPlan(t *testing.T) {
	d, _ := Lookup(DriverBrotherQL)
	cases := []struct {
		model, media string
		w, h         float64
		bytes        int
		cut          bool
	}{
		{"QL-500", "dk-11201", 29, 90, 90, false},
		{"QL-700", "dk-11201", 29, 90, 90, true},
		{"QL-1060N", "dk-11240", 102, 51, 162, true},
		{"QL-1115NWB", "dk-22243", 102, 60, 162, true},
	}
	for _, c := range cases {
		s := Settings{Model: c.model, Media: c.media}
		plan, err := planWith(d, s, LabelSpec{WidthMM: c.w, HeightMM: c.h, DPI: 300})
		if err != nil {
			t.Fatalf("%s: %v", c.model, err)
		}
		stream, err := plan.Encode([]*image.Gray{blankLabel(c.w, c.h, 300)})
		if err != nil {
			t.Fatalf("%s: %v", c.model, err)
		}
		model, _ := brotherql.LookupModel(c.model)
		pages, err := model.DecodeJob(stream)
		if err != nil || len(pages[0].Lines[0]) != c.bytes || pages[0].AutoCut != c.cut {
			t.Errorf("%s: %v", c.model, err)
		}
	}
}

// fakePrinter records what each connection sent.
type fakePrinter struct {
	ln   net.Listener
	mu   sync.Mutex
	jobs [][]byte
}

func newFakePrinter(t *testing.T) *fakePrinter {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fp := &fakePrinter{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				data, _ := io.ReadAll(conn)
				fp.mu.Lock()
				fp.jobs = append(fp.jobs, data)
				fp.mu.Unlock()
			}()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return fp
}

func (fp *fakePrinter) port() int { return fp.ln.Addr().(*net.TCPAddr).Port }

func TestSendDeliversWholeJob(t *testing.T) {
	fp := newFakePrinter(t)
	job := bytes.Repeat([]byte("raster"), 50000)
	if err := NewDispatcher(nil).Send(context.Background(), local(fp.port()), job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		fp.mu.Lock()
		n := len(fp.jobs)
		fp.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if len(fp.jobs) != 1 || !bytes.Equal(fp.jobs[0], job) {
		t.Fatalf("printer received %d jobs", len(fp.jobs))
	}
}

// countingConn counts itself open until closed, and slows writes so that
// overlapping jobs would be caught.
type countingConn struct {
	net.Conn
	open   *atomic.Int32
	closed atomic.Bool
}

func (c *countingConn) Write(b []byte) (int, error) {
	time.Sleep(10 * time.Millisecond)
	return c.Conn.Write(b)
}

func (c *countingConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		c.open.Add(-1)
	}
	return c.Conn.Close()
}

func TestSendSerializesJobsPerPrinter(t *testing.T) {
	printers := []*fakePrinter{newFakePrinter(t), newFakePrinter(t)}

	var mu sync.Mutex
	open := map[string]*atomic.Int32{} // connections currently open, per address
	peak := map[string]int32{}
	dials := 0

	d := NewDispatcher(nil)
	d.dial = func(ctx context.Context, addr string) (net.Conn, error) {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			return nil, err
		}
		mu.Lock()
		defer mu.Unlock()
		dials++
		if open[addr] == nil {
			open[addr] = &atomic.Int32{}
		}
		if n := open[addr].Add(1); n > peak[addr] {
			peak[addr] = n
		}
		return &countingConn{Conn: conn, open: open[addr]}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		for _, p := range printers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := d.Send(context.Background(), local(p.port()), []byte{byte(i)}); err != nil {
					t.Error(err)
				}
			}()
		}
	}
	wg.Wait()

	if dials != 12 {
		t.Errorf("dialed %d times, want 12", dials)
	}
	for addr, n := range peak {
		if n != 1 {
			t.Errorf("%s had %d connections open at once, want 1", addr, n)
		}
	}
}

// slowPrinter is a printer on the far end of a pipe, which has no buffer:
// it takes a job a little at a time, as a printer does while it prints, and
// stops taking it once stall is closed. What it took is read once the job
// is over.
func slowPrinter(t *testing.T, d *Dispatcher, stall chan struct{}) func() []byte {
	t.Helper()
	got := &bytes.Buffer{}
	done, ended := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(ended) })
	d.dial = func(ctx context.Context, addr string) (net.Conn, error) {
		ours, theirs := net.Pipe()
		go func() {
			defer close(done)
			defer theirs.Close()
			buf := make([]byte, 4<<10)
			for {
				select {
				case <-stall:
					// Holding the line open, taking nothing.
					<-ended
					return
				case <-time.After(5 * time.Millisecond):
				}
				n, err := theirs.Read(buf)
				got.Write(buf[:n])
				if err != nil {
					return
				}
			}
		}()
		return ours, nil
	}
	return func() []byte {
		<-done
		return got.Bytes()
	}
}

func TestSendTakesAsLongAsThePrinterKeepsTakingTheJob(t *testing.T) {
	d := NewDispatcher(nil)
	d.WriteTimeout = 100 * time.Millisecond
	got := slowPrinter(t, d, make(chan struct{}))
	// 64 reads 5ms apart: several times WriteTimeout in all.
	job := bytes.Repeat([]byte("label"), 256<<10/5)
	start := time.Now()
	if err := d.Send(context.Background(), local(1), job); err != nil {
		t.Fatalf("after %v: %v", time.Since(start), err)
	}
	if time.Since(start) < 3*d.WriteTimeout {
		t.Errorf("the job went in %v: the printer was not slow enough to tell", time.Since(start))
	}
	if took := got(); !bytes.Equal(took, job) {
		t.Errorf("the printer took %d of %d bytes", len(took), len(job))
	}
}

func TestSendFailsWhenThePrinterStopsTakingTheJob(t *testing.T) {
	d := NewDispatcher(nil)
	d.WriteTimeout = 50 * time.Millisecond
	stall := make(chan struct{})
	close(stall)
	slowPrinter(t, d, stall)
	if err := d.Send(context.Background(), local(1), bytes.Repeat([]byte("x"), 64<<10)); !errors.Is(err, ErrUnreachable) {
		t.Errorf("got %v, want ErrUnreachable", err)
	}
}

func TestSendReportsUnreachablePrinters(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // nothing listens any more

	err = NewDispatcher(nil).Send(context.Background(), local(port), []byte("job"))
	if !errors.Is(err, ErrUnreachable) {
		t.Errorf("got %v, want ErrUnreachable", err)
	}
}

func TestSendGivesUpWhileWaitingWhenCanceled(t *testing.T) {
	d := NewDispatcher(nil)
	slot := d.slot(local(1))
	slot <- struct{}{} // another job holds the printer
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := d.Send(ctx, local(1), []byte("job")); !errors.Is(err, ErrUnreachable) {
		t.Errorf("got %v, want ErrUnreachable after cancellation", err)
	}
}

func TestDymoPlan(t *testing.T) {
	d, err := Lookup(DriverDymoLabelWriter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planWith(d, Settings{Model: "LabelWriter 450", Media: "30252"}, LabelSpec{WidthMM: 90, HeightMM: 29, DPI: 300}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("Brother-sized label: got %v, want ErrDoesNotFit", err)
	}

	defer func(orig func() uint32) { newJobID = orig }(newJobID)
	newJobID = func() uint32 { return 42 }

	label := image.NewGray(image.Rect(0, 0, 1051, 331)) // 89 × 28 mm, all black
	for _, c := range []struct {
		model, options string
		protocol       dymolw.Protocol
		density, roll  byte
		jobID          uint32
	}{
		{"LabelWriter 450", "", dymolw.ProtocolV1, 'e', 0, 0},
		{"LabelWriter 450 Twin Turbo", `{"roll":"right","density":"light"}`, dymolw.ProtocolV1, 'c', '2', 0},
		{"LabelWriter 550", `{"density":"medium"}`, dymolw.ProtocolV2, 'd', 0, 42},
	} {
		plan, err := planWith(d, Settings{Model: c.model, Media: "30252", Options: json.RawMessage(c.options)}, LabelSpec{WidthMM: 89, HeightMM: 28, DPI: 300})
		if err != nil {
			t.Fatal(err)
		}
		stream, err := plan.Encode([]*image.Gray{label, label})
		if err != nil {
			t.Fatal(err)
		}
		job, err := dymolw.DecodeJob(c.protocol, stream, 331)
		if err != nil {
			t.Fatalf("%s: %v", c.model, err)
		}
		if job.Density != c.density || job.Roll != c.roll || job.JobID != c.jobID || len(job.Pages) != 2 {
			t.Errorf("%s: decoded %+v", c.model, job)
		}
	}
}

func TestTSPLPlan(t *testing.T) {
	d, err := Lookup(DriverTSPL)
	if err != nil {
		t.Fatal(err)
	}
	settings := Settings{Model: "Rollo X1038", Media: "4x6", Options: json.RawMessage(`{"density":11}`)}
	if _, err := planWith(d, settings, LabelSpec{WidthMM: 101.6, HeightMM: 152.4, DPI: 300}); !errors.Is(err, ErrDoesNotFit) {
		t.Errorf("300 DPI template on a 203 DPI printer: got %v, want ErrDoesNotFit", err)
	}
	plan, err := planWith(d, settings, LabelSpec{WidthMM: 152.4, HeightMM: 101.6, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	label := image.NewGray(image.Rect(0, 0, 1219, 812)) // landscape 6 × 4 in, all black
	stream, err := plan.Encode([]*image.Gray{label, label, label})
	if err != nil {
		t.Fatal(err)
	}
	job, err := tspl.DecodeJob(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Labels) != 1 || job.Labels[0].Copies != 3 || job.Labels[0].Image.Rect.Dy() != 1219 {
		t.Errorf("decoded %+v", job.Labels)
	}
	if !strings.Contains(strings.Join(job.Setup, "\n"), "DENSITY 11") {
		t.Errorf("setup %q lacks the density", job.Setup)
	}
}

func TestZebraPlans(t *testing.T) {
	d, err := Lookup(DriverZPL)
	if err != nil {
		t.Fatal(err)
	}
	settings := Settings{Model: "Zebra GK420d", Media: "4x6", Options: json.RawMessage(`{"darkness":22,"print_mode":"peel_off"}`)}
	plan, err := planWith(d, settings, LabelSpec{WidthMM: 152.4, HeightMM: 101.6, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	label := image.NewGray(image.Rect(0, 0, 1219, 812)) // landscape 6 × 4 in, all black
	stream, err := plan.Encode([]*image.Gray{label, label, label})
	if err != nil {
		t.Fatal(err)
	}
	job, err := zebra.DecodeZPL(stream)
	if err != nil {
		t.Fatal(err)
	}
	// Turned to run along the roll, as three copies of one label, as long
	// as the 6 in label: 1,218 dots.
	if len(job.Labels) != 1 || job.Labels[0].Copies != 3 || job.Labels[0].Length != 1218 || job.Labels[0].Width != 812 {
		t.Errorf("decoded %+v", job.Labels)
	}
	if strings.Join(job.Setup, " ") != "~SD22" || !strings.Contains(strings.Join(job.Labels[0].Commands, " "), "^MMP") {
		t.Errorf("setup %q, commands %q", job.Setup, job.Labels[0].Commands)
	}

	d, _ = Lookup(DriverEPL2)
	plan, err = planWith(d, Settings{Model: "Zebra LP 2824", Media: "2x1"}, LabelSpec{WidthMM: 50.8, HeightMM: 25.4, DPI: 203})
	if err != nil {
		t.Fatal(err)
	}
	small := image.NewGray(image.Rect(0, 0, 406, 203))
	stream, err = plan.Encode([]*image.Gray{small})
	if err != nil {
		t.Fatal(err)
	}
	if epl, err := zebra.DecodeEPL2(stream); err != nil || len(epl.Labels) != 1 || epl.Labels[0].Width != 406 {
		t.Errorf("EPL2 job %+v: %v", epl, err)
	}
}

func TestIndustrialPlans(t *testing.T) {
	label := image.NewGray(image.Rect(0, 0, 1219, 812)) // landscape 6 × 4 in, all black
	plan := func(driver, model string, options string) []byte {
		t.Helper()
		d, err := Lookup(driver)
		if err != nil {
			t.Fatal(err)
		}
		p, err := planWith(d, Settings{Model: model, Media: "4x6", Options: json.RawMessage(options)}, LabelSpec{WidthMM: 152.4, HeightMM: 101.6, DPI: 203})
		if err != nil {
			t.Fatal(err)
		}
		stream, err := p.Encode([]*image.Gray{label, label, label})
		if err != nil {
			t.Fatal(err)
		}
		return stream
	}
	// Each turned to run along the roll, as three copies of one label: the
	// 6 in roll is 1,218 dots long, the rendered label 1,219, all of which
	// DPL and EZPL send.
	dpl, err := industrial.DecodeDPL(plan(DriverDPL, "Datamax E-Class 203 DPI", `{"heat":22}`))
	if err != nil || len(dpl.Labels) != 1 || dpl.Labels[0].Copies != 3 || dpl.Labels[0].Image.Rect.Dy() != 1219 || !slices.Contains(dpl.Labels[0].Commands, "H22") {
		t.Errorf("DPL job %+v: %v", dpl, err)
	}
	sbpl, err := industrial.DecodeSBPL(plan(DriverSBPL, "SATO WS408", `{"media_sensor":"gap"}`))
	if err != nil || len(sbpl.Labels) != 1 || sbpl.Labels[0].Copies != 3 || sbpl.Labels[0].Length != 1218 || sbpl.Labels[0].Width != 812 ||
		strings.Join(sbpl.Setup, " ") != "IG1" {
		t.Errorf("SBPL job %+v: %v", sbpl, err)
	}
	ezpl, err := industrial.DecodeEZPL(plan(DriverEZPL, "Godex G500", `{"media_type":"direct"}`))
	if err != nil || len(ezpl.Labels) != 1 || ezpl.Labels[0].Copies != 3 || ezpl.Labels[0].Image.Rect.Dy() != 1219 ||
		!slices.Contains(ezpl.Labels[0].Setup, "^AD") {
		t.Errorf("EZPL job %+v: %v", ezpl, err)
	}
}

// tcp builds a TCP connection the way the old host and port fields did; a
// zero port leaves it to the default.
func tcp(host string, port int) Connection {
	if port == 0 {
		return Connection{Type: ConnTCP, Address: host}
	}
	return Connection{Type: ConnTCP, Address: net.JoinHostPort(host, strconv.Itoa(port))}
}

// local is a normalized TCP connection to this machine.
func local(port int) Connection { return tcp("127.0.0.1", port) }

// planWith checks labels against a driver's printer and plans them, as
// PlanLabel does by the driver's name.
func planWith(d Driver, s Settings, label LabelSpec) (Plan, error) {
	g, err := d.Geometry(s)
	if err != nil {
		return nil, err
	}
	f, err := FitLabel(g, label)
	if err != nil {
		return nil, err
	}
	return d.Plan(s, f)
}

// TestBrotherRedAndBlack prints labels drawn in black, white and red on
// DK-22251 in a QL-800 series printer, which declares those inks for it.
func TestBrotherRedAndBlack(t *testing.T) {
	d, err := Lookup(DriverBrotherQL)
	if err != nil {
		t.Fatal(err)
	}
	info := d.Info()
	if info.ModelPalette["QL-810W"] != labels.PaletteBWR || info.ModelPalette["QL-700"] != "" {
		t.Errorf("model palettes %v", info.ModelPalette)
	}
	for _, m := range info.Media {
		if (m.ID == "dk-22251") != (m.Palette == labels.PaletteBWR) {
			t.Errorf("%s: palette %q", m.ID, m.Palette)
		}
	}
	settings := Settings{Model: "QL-810W", Media: "dk-22251"}
	if l := Loaded(DriverBrotherQL, settings); l == nil || l.Palette != labels.PaletteBWR {
		t.Errorf("loaded %+v", l)
	}
	if l := Loaded(DriverBrotherQL, Settings{Model: "QL-810W", Media: "dk-22205"}); l == nil || l.Palette != "" {
		t.Errorf("black-and-white tape loaded %+v", l)
	}
	spec := LabelSpec{WidthMM: 62, HeightMM: 30, DPI: 300, Inks: []string{labels.InkBlack, labels.InkRed, labels.InkYellow}}
	plan, fit, err := PlanLabel(DriverBrotherQL, settings, spec)
	if err != nil {
		t.Fatal(err)
	}
	if fit.Palette != labels.PaletteBWR || !slices.Contains(fit.Warnings, "The printer prints only black, white and red, so yellow prints black.") {
		t.Errorf("fit %+v", fit)
	}
	inks, ok := plan.(InkPlan)
	if !ok || inks.Palette() != labels.PaletteBWR {
		t.Fatalf("plan %T", plan)
	}
	// 62 × 30 mm at 300 DPI: black on the left, red on the right.
	pal := color.Palette{}
	for _, c := range labels.PaletteBWR.Colors() {
		pal = append(pal, c)
	}
	label := image.NewPaletted(image.Rect(0, 0, 732, 354), pal)
	for y := 0; y < 354; y++ {
		for x := 0; x < 732; x++ {
			switch {
			case x < 300:
				label.Pix[y*label.Stride+x] = 0
			case x < 600:
				label.Pix[y*label.Stride+x] = 2
			default:
				label.Pix[y*label.Stride+x] = 1
			}
		}
	}
	job, err := inks.EncodeInks([]*image.Paletted{label})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := brotherql.DecodeJob(job)
	if err != nil {
		t.Fatal(err)
	}
	if !pages[0].TwoColor || len(pages[0].RedLines) != 354 {
		t.Fatalf("decoded %+v", pages[0].TwoColor)
	}
	m, _ := brotherql.LookupMedia("dk-22251")
	black, red := brotherql.PageImage(m, pages[0]), brotherql.PageImage(m, brotherql.Page{Lines: pages[0].RedLines})
	// The page is the middle 696 dots of the label's 732.
	for _, c := range []struct {
		x          int
		black, red bool
	}{{0, true, false}, {281, true, false}, {282, false, true}, {581, false, true}, {582, false, false}} {
		if (black.Pix[c.x] == 0) != c.black || (red.Pix[c.x] == 0) != c.red {
			t.Errorf("dot %d: black %v red %v", c.x, black.Pix[c.x] == 0, red.Pix[c.x] == 0)
		}
	}

	// On black-and-white tape there is nothing to warn of but the inks.
	plan, fit, err = PlanLabel(DriverBrotherQL, Settings{Model: "QL-810W", Media: "dk-22205"}, spec)
	if err != nil {
		t.Fatal(err)
	}
	if fit.Palette != labels.PaletteBW || !slices.Contains(fit.Warnings, "The printer prints only black and white, so red and yellow print black.") {
		t.Errorf("fit %+v", fit)
	}
	if p, ok := plan.(InkPlan); !ok || p.Palette() != labels.PaletteBW {
		t.Errorf("plan on black-and-white tape %T", plan)
	}
}
