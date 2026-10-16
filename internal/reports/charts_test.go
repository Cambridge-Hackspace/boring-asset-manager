package reports

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func chartSpec(t *testing.T, section string) *Spec {
	t.Helper()
	spec, err := Parse([]byte(`{"sections":[`+section+`]}`), known)
	if err != nil {
		t.Fatalf("%s: %v", section, err)
	}
	return spec
}

func TestChartsAreChecked(t *testing.T) {
	for _, good := range []string{
		`{"type":"chart","chart":"bar","group_by":"category","measure":"assets"}`,
		`{"type":"chart","chart":"donut","group_by":"field:serial","measure":"quantity"}`,
		`{"type":"chart","chart":"line","measure":"used","period":"month"}`,
	} {
		chartSpec(t, good)
	}
	for what, bad := range map[string]string{
		"an unknown chart":      `{"type":"chart","chart":"radar","group_by":"category","measure":"assets"}`,
		"an unknown grouping":   `{"type":"chart","chart":"bar","group_by":"colour","measure":"assets"}`,
		"an unknown field":      `{"type":"chart","chart":"bar","group_by":"field:weight","measure":"assets"}`,
		"an unknown measure":    `{"type":"chart","chart":"bar","group_by":"unit","measure":"value"}`,
		"a bar over time":       `{"type":"chart","chart":"bar","group_by":"unit","measure":"assets","period":"week"}`,
		"a grouped line":        `{"type":"chart","chart":"line","group_by":"unit","measure":"used","period":"week"}`,
		"a line without period": `{"type":"chart","chart":"line","measure":"loans"}`,
		"a line of assets":      `{"type":"chart","chart":"line","measure":"assets","period":"week"}`,
		"a chart with columns":  `{"type":"chart","chart":"bar","group_by":"unit","measure":"assets","columns":[{"heading":"A","binding":"x"}]}`,
		"a table with a chart":  `{"type":"table","chart":"bar","columns":[{"heading":"A","binding":"x"}]}`,
	} {
		if _, err := Parse([]byte(`{"sections":[`+bad+`]}`), known); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("%s: %v", what, err)
		}
	}
}

func points(s Series) map[string]int {
	out := map[string]int{}
	for _, p := range s.Points {
		out[p.Label] = p.Value
	}
	return out
}

func TestBarsGroupAndMeasure(t *testing.T) {
	cats, assets := fixtures()
	assets[0].Asset.Stock = &models.AssetStock{Quantity: 1000, Unit: "g"}
	assets[3].Asset.Stock = &models.AssetStock{Quantity: 250, Unit: "g"}
	assets[1].Loan = &models.Loan{}
	assets[1].FieldValues = map[string]string{"serial": "SN-1"}
	data := func(a *models.AssetWithDetails) labels.Data { return labels.Data{Condition: "Working"} }
	run := func(section string) Series {
		t.Helper()
		res := chartSpec(t, section).Run(assets, cats, data, nil, time.Now())
		return res.Sections[0].Series[0]
	}

	// An asset in two categories counts in each.
	if got := points(run(`{"type":"chart","chart":"bar","group_by":"category","measure":"assets"}`)); !reflect.DeepEqual(got, map[string]int{"Filament": 2, "Tools": 3}) {
		t.Errorf("by category: %v", got)
	}
	// Quantities by unit, never adding different units.
	if got := points(run(`{"type":"chart","chart":"bar","group_by":"category","measure":"quantity"}`)); !reflect.DeepEqual(got, map[string]int{"Filament (g)": 1250, "Tools (g)": 250, "Tools (pcs)": 9}) {
		t.Errorf("quantity by category: %v", got)
	}
	if got := points(run(`{"type":"chart","chart":"donut","group_by":"unit","measure":"quantity"}`)); !reflect.DeepEqual(got, map[string]int{"g": 1250, "pcs": 9}) {
		t.Errorf("quantity by unit: %v", got)
	}
	if got := points(run(`{"type":"chart","chart":"bar","group_by":"field:serial","measure":"assets"}`)); !reflect.DeepEqual(got, map[string]int{"SN-1": 1, "(none)": 3}) {
		t.Errorf("by field: %v", got)
	}
	if got := points(run(`{"type":"chart","chart":"bar","group_by":"category","measure":"loans"}`)); !reflect.DeepEqual(got, map[string]int{"Tools": 1}) {
		t.Errorf("loans: %v", got)
	}
	// Largest first.
	if s := run(`{"type":"chart","chart":"bar","group_by":"category","measure":"assets"}`); s.Points[0].Label != "Tools" {
		t.Errorf("order: %+v", s.Points)
	}

	// Past MaxBars groups, the rest are Other.
	var many []models.AssetWithDetails
	for i := 0; i < MaxBars+5; i++ {
		many = append(many, models.AssetWithDetails{Asset: models.Asset{ID: fmt.Sprint(i)}, FieldValues: map[string]string{"serial": fmt.Sprintf("S%02d", i)}})
	}
	res := chartSpec(t, `{"type":"chart","chart":"bar","group_by":"field:serial","measure":"assets"}`).Run(many, nil, data, nil, time.Now())
	if p := res.Sections[0].Series[0].Points; len(p) != MaxBars || p[MaxBars-1] != (Point{Label: "Other", Value: 6}) {
		t.Errorf("many groups: %+v", p)
	}
}

func TestLinesOverTime(t *testing.T) {
	// Thursday 15 October 2026.
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	weeks := periodStarts("week", now)
	if len(weeks) != Periods || !weeks[Periods-1].Equal(time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)) || weeks[0].Weekday() != time.Monday {
		t.Errorf("weeks %v", weeks)
	}
	months := periodStarts("month", now)
	if !months[0].Equal(time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)) || !months[Periods-1].Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("months %v", months)
	}

	_, assets := fixtures()
	id := func(s string) *string { return &s }
	change := func(n int) *int { return &n }
	history := []models.Event{
		{Kind: models.EventQuantityChanged, AssetID: id("pla"), CreatedAt: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), Payload: models.EventPayload{Change: change(-300), Unit: "g"}},
		{Kind: models.EventQuantityChanged, AssetID: id("pla"), CreatedAt: time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC), Payload: models.EventPayload{Change: change(-100), Unit: "g"}},
		{Kind: models.EventQuantityChanged, AssetID: id("screws"), CreatedAt: time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC), Payload: models.EventPayload{Change: change(-4), Unit: "pcs"}},
		{Kind: models.EventQuantityChanged, AssetID: id("pla"), CreatedAt: time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC), Payload: models.EventPayload{Change: change(500), Unit: "g"}},
		// Counting started: no change, so neither use nor restock.
		{Kind: models.EventQuantityChanged, AssetID: id("pla"), CreatedAt: time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC), Payload: models.EventPayload{Unit: "g"}},
		// Not among the report's assets.
		{Kind: models.EventQuantityChanged, AssetID: id("elsewhere"), CreatedAt: time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC), Payload: models.EventPayload{Change: change(-999), Unit: "g"}},
		// Too long ago.
		{Kind: models.EventQuantityChanged, AssetID: id("pla"), CreatedAt: time.Date(2025, 1, 2, 9, 0, 0, 0, time.UTC), Payload: models.EventPayload{Change: change(-7), Unit: "g"}},
		{Kind: models.EventAssetCheckedOut, AssetID: id("drill"), CreatedAt: time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)},
	}
	spec := chartSpec(t, `{"type":"chart","chart":"line","measure":"used","period":"month"}`)
	if kinds, since := spec.HistoryKinds(now); !reflect.DeepEqual(kinds, []string{models.EventQuantityChanged}) || !since.Equal(months[0]) {
		t.Errorf("history %v since %v", kinds, since)
	}
	series := spec.Run(assets, nil, func(*models.AssetWithDetails) labels.Data { return labels.Data{} }, history, now).Sections[0].Series
	if len(series) != 2 || series[0].Label != "Used (g)" || series[1].Label != "Used (pcs)" {
		t.Fatalf("series %+v", series)
	}
	g := series[0].Points
	if len(g) != Periods || g[Periods-1] != (Point{Label: "Oct 2026", Value: 300}) || g[Periods-2].Value != 100 || g[0].Label != "Nov 2025" {
		t.Errorf("grams used: %+v", g)
	}
	if series[1].Points[Periods-1].Value != 4 {
		t.Errorf("pieces used: %+v", series[1].Points)
	}

	restocked := chartSpec(t, `{"type":"chart","chart":"line","measure":"restocked","period":"week"}`).Run(assets, nil, func(*models.AssetWithDetails) labels.Data { return labels.Data{} }, history, now)
	// 4 October is a Sunday, so in the week from Monday 28 September.
	if p := restocked.Sections[0].Series[0].Points; p[Periods-3] != (Point{Label: "28 Sep", Value: 500}) {
		t.Errorf("restocked by week: %+v", p)
	}
	loans := chartSpec(t, `{"type":"chart","chart":"line","measure":"loans","period":"week"}`)
	if kinds, _ := loans.HistoryKinds(now); !reflect.DeepEqual(kinds, []string{models.EventAssetCheckedOut}) {
		t.Errorf("loans read %v", kinds)
	}
	res := loans.Run(assets, nil, func(*models.AssetWithDetails) labels.Data { return labels.Data{} }, history, now)
	if s := res.Sections[0].Series; len(s) != 1 || s[0].Label != "On loan" || s[0].Points[Periods-1].Value != 1 {
		t.Errorf("loans: %+v", s)
	}
	// Nothing at all still draws the periods.
	empty := spec.Run(nil, nil, func(*models.AssetWithDetails) labels.Data { return labels.Data{} }, nil, now)
	if s := empty.Sections[0].Series; len(s) != 1 || len(s[0].Points) != Periods || s[0].Label != "Used" {
		t.Errorf("an empty line: %+v", s)
	}

	var buf bytes.Buffer
	if err := WriteCSV(&buf, SectionResult{Type: "chart", Chart: ChartLine, Series: series}); err != nil {
		t.Fatal(err)
	}
	if want := "\xef\xbb\xbfPeriod,Used (g),Used (pcs)\nNov 2025,0,0\n"; !bytes.HasPrefix(buf.Bytes(), []byte(want)) || !bytes.HasSuffix(buf.Bytes(), []byte("Oct 2026,300,4\n")) {
		t.Errorf("csv %q", buf.String())
	}
}
