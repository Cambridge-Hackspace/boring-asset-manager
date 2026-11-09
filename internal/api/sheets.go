package api

import (
	"errors"
	"fmt"
	"image"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/rbac"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Sheets (docs/reports.md#asset-sheets): an asset's page, for insurance
// records, handovers and a machine's binder, and a stocktake checklist to
// walk the rooms with.

// maxSheets is the most assets one PDF of sheets has: each draws its photo,
// codes and label.
const maxSheets = 500

// sheetHistory is how many of an asset's latest events its sheet lists.
const sheetHistory = 10

// handleAssetSheet makes an asset's sheet: GET /api/v1/assets/{id}/sheet.pdf.
func (s *Server) handleAssetSheet(w http.ResponseWriter, r *http.Request) {
	a, ok := s.assetFor(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	req, ok := s.documentOptions(w, r, a.Asset.Name)
	if !ok {
		return
	}
	d, err := render.NewDocument(req.opts)
	if err != nil {
		writeServerError(w, err, "start the PDF")
		return
	}
	if err := s.drawSheet(r, d, s.shown(r, a), s.picturer(r, documentRequest{photos: true, labels: true})); err != nil {
		writeServerError(w, err, "draw the sheet")
		return
	}
	writeDocument(w, d, nameStem(a.Asset.Name, "asset"))
}

// handleAssetSheets makes the sheets of the asset list, one page each:
// GET /api/v1/assets/sheets.pdf, with the list's filters.
func (s *Server) handleAssetSheets(w http.ResponseWriter, r *http.Request) {
	assets, _, _, ok := s.exportedAssets(w, r)
	if !ok {
		return
	}
	if len(assets) > maxSheets {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Sheets are made for at most %d assets at once, and the list has %d: narrow the list", maxSheets, len(assets)))
		return
	}
	req, ok := s.documentOptions(w, r, "Asset sheets")
	if !ok {
		return
	}
	d, err := render.NewDocument(req.opts)
	if err != nil {
		writeServerError(w, err, "start the PDF")
		return
	}
	pics := s.picturer(r, documentRequest{photos: true, labels: true})
	for i := range assets {
		d.NewPage()
		if err := s.drawSheet(r, d, &assets[i], pics); err != nil {
			writeServerError(w, err, "draw the sheets")
			return
		}
	}
	if len(assets) == 0 {
		d.Paragraph("No assets match.", render.Gray25)
	}
	writeDocument(w, d, fmt.Sprintf("asset-sheets-%s", time.Now().UTC().Format("20060102")))
}

// drawSheet draws an asset's sheet: its pictures, what it is, where, and
// what happened to it lately.
func (s *Server) drawSheet(r *http.Request, d *render.Document, a *models.AssetWithDetails, pics *picturer) error {
	d.Heading(a.Asset.Name)

	// Its photo, its codes drawn, and its label.
	var figs []render.Figure
	drawn := pics.of(a)
	if drawn[0] != nil {
		figs = append(figs, render.Figure{Image: drawn[0], Caption: "Photo", WidthPt: 144, HeightPt: 108})
	}
	for _, k := range a.GeneratorKeys {
		if img := codeImage(k); img != nil {
			w, h := 72.0, 72.0
			if k.GeneratorType == generators.TypeCode128 {
				w, h = 180, 48
			}
			figs = append(figs, render.Figure{Image: img, Caption: k.AssignedCode, WidthPt: w, HeightPt: h})
		}
	}
	if drawn[1] != nil {
		figs = append(figs, render.Figure{Image: drawn[1], Caption: "Label", WidthPt: 144, HeightPt: 72})
	}
	d.Figures(figs)

	facts := []render.Fact{}
	add := func(label, value string) {
		if value != "" {
			facts = append(facts, render.Fact{Label: label, Value: value})
		}
	}
	var cats []string
	for _, c := range a.Categories {
		cats = append(cats, c.Name)
	}
	add("Categories", strings.Join(cats, ", "))
	add("Where", locationOf(a))
	if st := a.Asset.Stock; st != nil {
		add("Quantity", strings.TrimSpace(strconv.Itoa(st.Quantity)+" "+st.Unit))
	}
	add("Condition", conditionOf(a.Asset))
	add("Loan", loanOf(a.Loan))
	if a.Asset.ArchivedAt != nil {
		add("Archived", a.Asset.ArchivedAt.UTC().Format("2006-01-02"))
	}
	var codes []string
	for _, k := range a.GeneratorKeys {
		codes = append(codes, codeName(k.GeneratorType)+": "+k.AssignedCode)
	}
	add("Codes", strings.Join(codes, "; "))
	for _, f := range a.EffectiveFields {
		add(f.Field.Label, a.FieldText(f.Field.Key))
	}
	// Where it came from, as far as the request may see.
	p := a.Purchase
	add("Vendor", p.VendorName)
	add("Ordered", p.OrderDate)
	add("Received", p.ReceivedDate)
	if p.Price != "" {
		add("Price", p.Price+" "+p.Currency)
	}
	add("Order number", p.OrderNumber)
	add("Warranty ends", p.WarrantyEnd)
	// The contracts covering it that the request sees.
	for _, c := range a.Contracts {
		line := c.Name + " (" + c.Kind + ", " + c.VendorName
		if c.EndDate != "" {
			line += ", until " + c.EndDate
		}
		add("Contract", line+")")
	}
	for _, l := range a.Links {
		add(capitalize(l.Label), l.Asset.Name)
	}
	d.Facts(facts)

	// What happened to it lately, as its history shows it.
	events, _, err := s.store.ListAssetEvents(a.Asset.ID, "", sheetHistory)
	if err != nil && !errors.Is(err, store.ErrEventNotFound) {
		return err
	}
	if len(events) > 0 {
		track := Allows(r.Context(), rbac.PermAssetsTrack, a.Scopes)
		prices := Allows(r.Context(), rbac.PermPurchasesRead, a.Scopes)
		table := render.Table{Columns: []string{"When", "Who", "What"}}
		group := render.TableGroup{}
		for _, e := range events {
			e = redactPriceChanges(redactEvent(e, track), prices)
			group.Rows = append(group.Rows, render.TableRow{Cells: []string{
				e.CreatedAt.UTC().Format("2006-01-02 15:04"), eventActor(e), eventText(e),
			}})
		}
		table.Groups = []render.TableGroup{group}
		d.Heading("Recent history")
		d.Table(table)
	}
	return nil
}

// codeImage draws a code as its symbol with its quiet zone, a linear
// code's one row stretched to bars; nil when it cannot be encoded. Each
// module is a block of pixels, so a viewer scaling it up keeps it sharp.
func codeImage(k models.AssetGeneratorKey) image.Image {
	g, err := generators.Lookup(k.GeneratorType)
	if err != nil {
		return nil
	}
	sym, err := g.Encode(k.AssignedCode, "")
	if err != nil || sym.Cols == 0 {
		return nil
	}
	rows := sym.Rows
	if rows == 1 {
		rows = sym.Cols / 4
	}
	const scale = 4
	q := sym.QuietZone
	img := image.NewGray(image.Rect(0, 0, (sym.Cols+2*q)*scale, (rows+2*q)*scale))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	for y := range rows * scale {
		sy := min(y/scale, sym.Rows-1)
		for x := range sym.Cols * scale {
			if sym.Dark[sy*sym.Cols+x/scale] {
				img.Pix[(y+q*scale)*img.Stride+x+q*scale] = 0
			}
		}
	}
	return img
}

func codeName(genType string) string {
	for _, g := range generators.Describe() {
		if g.Type == genType {
			return g.Name
		}
	}
	return genType
}

// locationOf is where an asset is, outermost first.
func locationOf(a *models.AssetWithDetails) string {
	var names []string
	for _, l := range a.Location {
		names = append(names, l.Name)
	}
	return strings.Join(names, " › ")
}

func conditionOf(a models.Asset) string {
	out := ""
	switch a.Condition {
	case models.ConditionNeedsMaintenance:
		out = "Needs maintenance"
	case models.ConditionOutOfService:
		out = "Out of service"
	default:
		return ""
	}
	if a.ConditionNote != "" {
		out += ": " + a.ConditionNote
	}
	return out
}

func loanOf(l *models.Loan) string {
	if l == nil {
		return ""
	}
	who := l.Borrower.Name
	if who == "" {
		who = l.Borrower.Email
	}
	out := "Lent"
	if who != "" {
		out += " to " + who
	}
	out += " on " + l.CheckedOutAt.UTC().Format("2006-01-02")
	if l.DueAt != nil {
		out += ", due " + l.DueAt.UTC().Format("2006-01-02")
	}
	if l.Overdue {
		out += " (overdue)"
	}
	return out
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// eventActor is who caused an event, as far as it may be shown.
func eventActor(e models.Event) string {
	a := e.Payload.Actor
	if a == nil {
		return ""
	}
	if a.Name != "" {
		return a.Name
	}
	return a.Email
}

// eventText says what an event did, as a sheet's history lists it.
func eventText(e models.Event) string {
	p := e.Payload
	switch e.Kind {
	case models.EventAssetCreated:
		return "Added"
	case models.EventAssetEdited:
		return "Edited"
	case models.EventAssetArchived:
		return "Archived"
	case models.EventAssetRestored:
		return "Restored"
	case models.EventQuantityChanged:
		if p.Quantity != nil {
			return "Quantity changed to " + strconv.Itoa(*p.Quantity)
		}
		return "Quantity changed"
	case models.EventAssetRunningLow:
		return "Ran low"
	case models.EventAssetRestocked:
		return "Restocked"
	case models.EventAssetConditionChanged:
		return "Condition changed"
	case models.EventAssetCheckedOut:
		return "Lent"
	case models.EventAssetCheckedIn:
		return "Returned"
	case models.EventAssetOverdue:
		return "Loan overdue"
	case models.EventAssetLabelPrinted:
		return "Label printed"
	case models.EventAssetHook:
		return "Hook fired"
	case models.EventFlowOverdue:
		return "A flow passed its date"
	case models.EventWarrantyExpiring:
		return "Warranty ending"
	}
	return e.Kind
}

// handleStocktake makes a stocktake checklist of the asset list:
// GET /api/v1/assets/stocktake.pdf, with the list's filters. Assets are
// grouped by where they are, outermost container first, in order, so the
// rooms can be walked through, each with a box to tick, what is expected,
// and a blank for what was counted.
func (s *Server) handleStocktake(w http.ResponseWriter, r *http.Request) {
	assets, _, _, ok := s.exportedAssets(w, r)
	if !ok {
		return
	}
	req, ok := s.documentOptions(w, r, "Stocktake")
	if !ok {
		return
	}
	d, err := render.NewDocument(req.opts)
	if err != nil {
		writeServerError(w, err, "start the PDF")
		return
	}
	d.Heading("Stocktake")
	d.Paragraph(plural(len(assets), "asset", "assets")+", by where they are. Tick each one found, and write down how many where it is counted.", render.Gray25)
	d.Table(stocktakeTable(assets))
	writeDocument(w, d, fmt.Sprintf("stocktake-%s", time.Now().UTC().Format("20060102")))
}

// stocktakeTable groups assets by where they are, places in order of their
// path, outermost first, with those in no place last, and each place's
// assets by name.
func stocktakeTable(assets []models.AssetWithDetails) render.Table {
	type place struct {
		path  []string
		title string
		rows  []models.AssetWithDetails
	}
	byTitle := map[string]*place{}
	var places []*place
	for _, a := range assets {
		var path []string
		for _, l := range a.Location {
			path = append(path, l.Name)
		}
		title := strings.Join(path, " › ")
		if title == "" {
			title = "Not in a place"
		}
		p := byTitle[title]
		if p == nil {
			p = &place{path: path, title: title}
			byTitle[title] = p
			places = append(places, p)
		}
		p.rows = append(p.rows, a)
	}
	sort.SliceStable(places, func(i, j int) bool {
		a, b := places[i].path, places[j].path
		if (len(a) == 0) != (len(b) == 0) {
			return len(b) == 0
		}
		for k := 0; k < len(a) && k < len(b); k++ {
			if x, y := strings.ToLower(a[k]), strings.ToLower(b[k]); x != y {
				return x < y
			}
		}
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		// Places named alike but for case, in a fixed order.
		return places[i].title < places[j].title
	})
	t := render.Table{Columns: []string{"Asset", "Codes", "Expected", "Counted"}, Checkbox: true}
	for _, p := range places {
		sort.SliceStable(p.rows, func(i, j int) bool {
			return strings.ToLower(p.rows[i].Asset.Name) < strings.ToLower(p.rows[j].Asset.Name)
		})
		g := render.TableGroup{Title: p.title}
		for _, a := range p.rows {
			var codes []string
			for _, k := range a.GeneratorKeys {
				codes = append(codes, k.AssignedCode)
			}
			expected := "1"
			if st := a.Asset.Stock; st != nil {
				expected = strings.TrimSpace(strconv.Itoa(st.Quantity) + " " + st.Unit)
			}
			g.Rows = append(g.Rows, render.TableRow{Cells: []string{a.Asset.Name, strings.Join(codes, ", "), expected, ""}})
		}
		t.Groups = append(t.Groups, g)
	}
	return t
}
