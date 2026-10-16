package reports

import (
	"encoding/csv"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/fields"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Chart kinds, and what they group by and measure.
const (
	ChartBar   = "bar"
	ChartDonut = "donut"
	ChartLine  = "line"
)

// MaxBars is how many groups a bar or donut chart shows; the rest are added
// up as "Other".
const MaxBars = 20

// Periods is how many weeks or months a line chart covers, up to now.
const Periods = 12

// groupings a bar or donut chart may group by, besides "field:<key>".
var groupings = map[string]bool{"category": true, "condition": true, "borrower": true, "location": true, "unit": true}

// checkChart checks a chart section.
func checkChart(sec Section, where string, fieldKnown func(string) bool) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s: %s", ErrInvalidSpec, where, fmt.Sprintf(format, args...))
	}
	if len(sec.Columns) > 0 || sec.Descending || sec.SortBy != 0 {
		return fail("a chart has no columns or sorting")
	}
	switch sec.Chart {
	case ChartBar, ChartDonut:
		switch {
		case groupings[sec.GroupBy]:
		case strings.HasPrefix(sec.GroupBy, "field:"):
			key := strings.TrimPrefix(sec.GroupBy, "field:")
			if fields.ValidateKey(key) != nil || (fieldKnown != nil && !fieldKnown(key)) {
				return fail("unknown field %q", key)
			}
		default:
			return fail("group_by must be category, condition, borrower, location, unit or field:<key>")
		}
		switch sec.Measure {
		case "assets", "quantity", "loans":
		default:
			return fail("measure must be assets, quantity or loans")
		}
		if sec.Period != "" {
			return fail("only a line chart has a period")
		}
	case ChartLine:
		if sec.GroupBy != "" {
			return fail("a line chart is grouped by time")
		}
		switch sec.Measure {
		case "used", "restocked", "loans":
		default:
			return fail("measure must be used, restocked or loans")
		}
		if sec.Period != "week" && sec.Period != "month" {
			return fail("period must be week or month")
		}
	default:
		return fail("chart must be bar, donut or line")
	}
	return nil
}

// Series is a chart's line, or its bars or slices.
type Series struct {
	Label  string  `json:"label"`
	Points []Point `json:"points"`
}

// Point is one bar, slice or point of a line.
type Point struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// HistoryKinds lists the events line charts in the spec read, and Since
// how far back they reach from now; none means no chart needs history.
func (s *Spec) HistoryKinds(now time.Time) (kinds []string, since time.Time) {
	seen := map[string]bool{}
	for _, sec := range s.Sections {
		if sec.Type != "chart" || sec.Chart != ChartLine {
			continue
		}
		k := models.EventQuantityChanged
		if sec.Measure == "loans" {
			k = models.EventAssetCheckedOut
		}
		if !seen[k] {
			seen[k] = true
			kinds = append(kinds, k)
		}
		if start := periodStarts(sec.Period, now)[0]; since.IsZero() || start.Before(since) {
			since = start
		}
	}
	return kinds, since
}

// periodStarts gives the starts of the last Periods weeks (from Monday) or
// months, in UTC, oldest first, the last one holding now.
func periodStarts(period string, now time.Time) []time.Time {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	starts := make([]time.Time, Periods)
	for i := range starts {
		back := Periods - 1 - i
		if period == "week" {
			monday := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
			starts[i] = monday.AddDate(0, 0, -7*back)
		} else {
			starts[i] = time.Date(now.Year(), now.Month()-time.Month(back), 1, 0, 0, 0, 0, time.UTC)
		}
	}
	return starts
}

func periodLabel(period string, start time.Time) string {
	if period == "week" {
		return start.Format("2 Jan")
	}
	return start.Format("Jan 2006")
}

// runChart lays out a chart for assets, with history for line charts.
func runChart(sec Section, assets []models.AssetWithDetails, data []labels.Data, history []models.Event, now time.Time) []Series {
	if sec.Chart == ChartLine {
		return lineSeries(sec, assets, history, now)
	}
	totals := map[string]int{}
	for i := range assets {
		a := &assets[i]
		amount, unit := 1, ""
		switch sec.Measure {
		case "quantity":
			if a.Asset.Stock == nil {
				continue
			}
			amount, unit = a.Asset.Stock.Quantity, a.Asset.Stock.Unit
		case "loans":
			if a.Loan == nil {
				continue
			}
		}
		for _, g := range groupsOf(sec.GroupBy, a, data[i]) {
			// Quantities in different units never add up.
			if sec.Measure == "quantity" && sec.GroupBy != "unit" && unit != "" {
				g += " (" + unit + ")"
			}
			totals[g] += amount
		}
	}
	points := make([]Point, 0, len(totals))
	for label, v := range totals {
		points = append(points, Point{Label: label, Value: v})
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].Value != points[j].Value {
			return points[i].Value > points[j].Value
		}
		return points[i].Label < points[j].Label
	})
	if len(points) > MaxBars {
		other := Point{Label: "Other"}
		for _, p := range points[MaxBars-1:] {
			other.Value += p.Value
		}
		points = append(points[:MaxBars-1], other)
	}
	return []Series{{Label: measureLabel(sec.Measure), Points: points}}
}

func measureLabel(m string) string {
	switch m {
	case "quantity":
		return "Quantity"
	case "loans":
		return "On loan"
	case "used":
		return "Used"
	case "restocked":
		return "Restocked"
	}
	return "Assets"
}

// groupsOf names the groups an asset counts in: several for its
// categories, one otherwise.
func groupsOf(by string, a *models.AssetWithDetails, d labels.Data) []string {
	or := func(s, none string) []string {
		if s == "" {
			return []string{none}
		}
		return []string{s}
	}
	switch by {
	case "category":
		names := make([]string, 0, len(a.Categories))
		for _, c := range a.Categories {
			names = append(names, c.Name)
		}
		return names
	case "condition":
		return []string{d.Condition}
	case "borrower":
		return or(d.Borrower, "Not lent")
	case "location":
		return or(d.Location, "No location")
	case "unit":
		if a.Asset.Stock == nil {
			return []string{"Not counted"}
		}
		return or(a.Asset.Stock.Unit, "No unit")
	}
	return or(a.FieldValues[strings.TrimPrefix(by, "field:")], "(none)")
}

// lineSeries totals history per period for the assets: stock used or
// restocked, a line per unit, or loans made.
func lineSeries(sec Section, assets []models.AssetWithDetails, history []models.Event, now time.Time) []Series {
	in := make(map[string]bool, len(assets))
	for i := range assets {
		in[assets[i].Asset.ID] = true
	}
	starts := periodStarts(sec.Period, now)
	index := func(t time.Time) int {
		for i := len(starts) - 1; i >= 0; i-- {
			if !t.Before(starts[i]) {
				return i
			}
		}
		return -1
	}
	byUnit := map[string][]int{}
	var units []string
	add := func(unit string, at time.Time, v int) {
		i := index(at)
		if i < 0 {
			return
		}
		if byUnit[unit] == nil {
			byUnit[unit] = make([]int, len(starts))
			units = append(units, unit)
		}
		byUnit[unit][i] += v
	}
	for _, e := range history {
		if e.AssetID == nil || !in[*e.AssetID] {
			continue
		}
		switch {
		case sec.Measure == "loans" && e.Kind == models.EventAssetCheckedOut:
			add("", e.CreatedAt, 1)
		case e.Kind == models.EventQuantityChanged && e.Payload.Change != nil:
			change := *e.Payload.Change
			if sec.Measure == "used" && change < 0 {
				add(e.Payload.Unit, e.CreatedAt, -change)
			} else if sec.Measure == "restocked" && change > 0 {
				add(e.Payload.Unit, e.CreatedAt, change)
			}
		}
	}
	if len(units) == 0 {
		// An empty line still shows the periods.
		byUnit[""], units = make([]int, len(starts)), []string{""}
	}
	sort.Strings(units)
	series := make([]Series, 0, len(units))
	for _, u := range units {
		s := Series{Label: measureLabel(sec.Measure)}
		if u != "" {
			s.Label += " (" + u + ")"
		}
		for i, start := range starts {
			s.Points = append(s.Points, Point{Label: periodLabel(sec.Period, start), Value: byUnit[u][i]})
		}
		series = append(series, s)
	}
	return series
}

// writeChartCSV writes a chart's data: a row per group or period, a column
// per series.
func writeChartCSV(cw *csv.Writer, sec SectionResult) error {
	first := "Group"
	if sec.Chart == ChartLine {
		first = "Period"
	}
	head := []string{first}
	for _, s := range sec.Series {
		head = append(head, s.Label)
	}
	if err := cw.Write(safeCells(head)); err != nil {
		return err
	}
	if len(sec.Series) > 0 {
		for i, p := range sec.Series[0].Points {
			row := []string{p.Label}
			for _, s := range sec.Series {
				row = append(row, strconv.Itoa(s.Points[i].Value))
			}
			if err := cw.Write(safeCells(row)); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}
