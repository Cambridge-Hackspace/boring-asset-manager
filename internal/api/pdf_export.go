package api

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // photos' thumbnails
	_ "image/png"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/assetcsv"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/generators"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/render"
)

// Instance settings for documents (docs/reports.md#pdf): the name at the
// top of every page, and the page size a document is made in unless one
// is asked for.
const (
	InstanceNameSetting = "instance.name"
	PageSizeSetting     = "documents.page_size"
	defaultInstanceName = "BAM!"
)

// maxPictureRows is the most rows a document draws photos or labels for:
// each is an image read or a label laid out.
const maxPictureRows = 500

// documentRequest is what a document is asked for with: its page, and
// whether rows have their asset's photo and label.
type documentRequest struct {
	opts           render.DocumentOptions
	photos, labels bool
}

// documentOptions reads a document's options from the query: page_size
// (letter or a4; the instance's when not given), orientation (portrait or
// landscape), photos and labels. title heads every page.
func (s *Server) documentOptions(w http.ResponseWriter, r *http.Request, title string) (documentRequest, bool) {
	q := r.URL.Query()
	sizeName := q.Get("page_size")
	if sizeName == "" {
		var err error
		if sizeName, err = s.store.Setting(PageSizeSetting, render.Letter.Name); err != nil {
			writeServerError(w, err, "read the page size")
			return documentRequest{}, false
		}
	}
	size, ok := render.PageSizeNamed(sizeName)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, `page_size must be "letter" or "a4"`)
		return documentRequest{}, false
	}
	landscape := false
	switch q.Get("orientation") {
	case "", "portrait":
	case "landscape":
		landscape = true
	default:
		writeJSONError(w, http.StatusBadRequest, `orientation must be "portrait" or "landscape"`)
		return documentRequest{}, false
	}
	instance, err := s.store.Setting(InstanceNameSetting, defaultInstanceName)
	if err != nil {
		writeServerError(w, err, "read the instance's name")
		return documentRequest{}, false
	}
	made := "Made " + time.Now().UTC().Format("2006-01-02 15:04") + " UTC"
	if u := CurrentUser(r.Context()); u != nil {
		who := u.Name
		if who == "" {
			who = u.Email
		}
		made += " by " + who
	} else if t := CurrentToken(r.Context()); t != nil {
		made += " by the token " + t.Name
	}
	truthy := func(v string) bool { return v == "1" || v == "true" }
	return documentRequest{
		opts:   render.DocumentOptions{Size: size, Landscape: landscape, Instance: instance, Title: title, Made: made},
		photos: truthy(q.Get("photos")), labels: truthy(q.Get("labels")),
	}, true
}

// pictureColumns are the picture columns' headings, as asked for.
func (req documentRequest) pictureColumns() []string {
	var cols []string
	if req.photos {
		cols = append(cols, "Photo")
	}
	if req.labels {
		cols = append(cols, "Label")
	}
	return cols
}

// pictures are an asset's photo's thumbnail and its label, drawn, as req
// asks for them, each nil where it has none or it cannot be drawn.
type picturer struct {
	s         *Server
	r         *http.Request
	req       documentRequest
	templates map[string]*labels.Template
	images    render.ImageSource
}

func (s *Server) picturer(r *http.Request, req documentRequest) *picturer {
	return &picturer{s: s, r: r, req: req, templates: map[string]*labels.Template{}, images: s.newLabelImages(r.Context())}
}

func (p *picturer) of(a *models.AssetWithDetails) []image.Image {
	var out []image.Image
	if p.req.photos {
		var photo image.Image
		if len(a.Photos) > 0 && a.Photos[0].ThumbID != "" {
			if data, err := p.s.photos.Read(p.r.Context(), a.Photos[0].ThumbID); err == nil {
				photo, _, _ = image.Decode(bytes.NewReader(data))
			}
		}
		out = append(out, photo)
	}
	if p.req.labels {
		var label image.Image
		if id := a.LabelTemplate.TemplateID; id != "" {
			t, seen := p.templates[id]
			if !seen {
				t, _ = p.s.store.GetTemplate(id)
				p.templates[id] = t
			}
			if t != nil {
				if scene, problem, err := p.s.layoutAsset(p.r, t, a.Asset.ID, p.images); err == nil && problem == nil {
					label = scene.Raster(false)
				}
			}
		}
		out = append(out, label)
	}
	return out
}

// writeDocument sends a document's pages as a PDF to download as name.
func writeDocument(w http.ResponseWriter, d *render.Document, name string) {
	var buf bytes.Buffer
	if err := render.WritePDF(&buf, d.Pages()...); err != nil {
		writeServerError(w, err, "write the PDF")
		return
	}
	setDownloadHeaders(w, "application/pdf", name+".pdf", "attachment")
	_, _ = w.Write(buf.Bytes())
}

// handleReportPDF makes a report a PDF: GET /api/v1/reports/{id}/export.pdf
// (docs/reports.md#pdf). Tables are drawn with their groups, charts as
// vectors.
func (s *Server) handleReportPDF(w http.ResponseWriter, r *http.Request) {
	report, result, ok := s.savedReport(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	req, ok := s.documentOptions(w, r, report.Name)
	if !ok {
		return
	}
	rows := 0
	for _, sec := range result.Sections {
		for _, g := range sec.Groups {
			rows += len(g.Rows)
		}
	}
	if (req.photos || req.labels) && rows > maxPictureRows {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Photos and labels are drawn for at most %d rows, and the report has %d: leave them out, or narrow the report", maxPictureRows, rows))
		return
	}
	d, err := render.NewDocument(req.opts)
	if err != nil {
		writeServerError(w, err, "start the PDF")
		return
	}
	d.Heading(report.Name)
	d.Paragraph(plural(result.Assets, "asset matches", "assets match")+".", render.Gray25)
	pics := s.picturer(r, req)
	for i, sec := range result.Sections {
		switch sec.Type {
		case "table":
			if sec.Title != "" {
				d.Heading(sec.Title)
			}
			table := render.Table{Columns: sec.Columns, PictureColumns: req.pictureColumns()}
			for _, g := range sec.Groups {
				group := render.TableGroup{Title: g.Label()}
				for _, row := range g.Rows {
					tr := render.TableRow{Cells: row.Cells}
					if len(table.PictureColumns) > 0 {
						if a, err := s.store.GetAssetByID(row.AssetID); err == nil {
							tr.Pictures = pics.of(a)
						}
					}
					group.Rows = append(group.Rows, tr)
				}
				table.Groups = append(table.Groups, group)
			}
			d.Table(table)
		case "chart":
			c := render.Chart{Title: sec.Title, Kind: sec.Chart}
			if c.Title == "" {
				c.Title = "Chart " + strconv.Itoa(i+1)
			}
			for _, series := range sec.Series {
				cs := render.ChartSeries{Label: series.Label}
				for _, p := range series.Points {
					cs.Points = append(cs.Points, render.ChartPoint{Label: p.Label, Value: p.Value})
				}
				c.Series = append(c.Series, cs)
			}
			if err := d.Chart(c); err != nil {
				writeServerError(w, err, "draw the chart")
				return
			}
		}
	}
	writeDocument(w, d, nameStem(report.Name, "report"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// defaultAssetColumns are those an asset list's PDF has when none are
// asked for: quantity only where any asset is counted.
var defaultAssetColumns = []string{assetcsv.ColumnName, assetcsv.ColumnCategories, assetcsv.ColumnQuantity}

// handleExportAssetsPDF makes the asset list a PDF:
// GET /api/v1/assets/export.pdf, with the CSV export's filters, and
// columns, the export's columns to show (docs/reports.md#pdf).
func (s *Server) handleExportAssetsPDF(w http.ResponseWriter, r *http.Request) {
	assets, cats, refs, ok := s.exportedAssets(w, r)
	if !ok {
		return
	}
	title := "Assets"
	if q := strings.TrimSpace(r.URL.Query().Get("search")); q != "" {
		title += ` matching "` + q + `"`
	}
	req, ok := s.documentOptions(w, r, title)
	if !ok {
		return
	}
	if (req.photos || req.labels) && len(assets) > maxPictureRows {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Photos and labels are drawn for at most %d assets, and the list has %d: leave them out, or narrow the list", maxPictureRows, len(assets)))
		return
	}
	names, rows := assetcsv.Table(assets, cats, generators.AllowedGeneratorTypes(), refs)
	index := map[string]int{}
	for i, n := range names {
		index[n] = i
	}
	var chosen []string
	if raw := r.URL.Query().Get("columns"); raw != "" {
		for _, c := range strings.Split(raw, ",") {
			if c = strings.TrimSpace(c); c != "" {
				chosen = append(chosen, c)
			}
		}
	} else {
		for _, c := range defaultAssetColumns {
			if _, ok := index[c]; ok {
				chosen = append(chosen, c)
			}
		}
	}
	headings, err := s.columnHeadings(chosen)
	if err != nil {
		writeServerError(w, err, "list fields")
		return
	}
	cols := make([]int, len(chosen))
	for i, c := range chosen {
		at, known := index[c]
		if !known && !strings.HasPrefix(c, assetcsv.FieldPrefix) {
			writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("%s is not a column; the columns are %s, and field.<key> for each field", c, strings.Join(names, ", ")))
			return
		}
		// A field no asset listed has a value for is an empty column.
		cols[i] = -1
		if known {
			cols[i] = at
		}
	}

	d, err := render.NewDocument(req.opts)
	if err != nil {
		writeServerError(w, err, "start the PDF")
		return
	}
	d.Heading(title)
	d.Paragraph(plural(len(assets), "asset", "assets")+".", render.Gray25)
	table := render.Table{Columns: headings, PictureColumns: req.pictureColumns()}
	group := render.TableGroup{}
	pics := s.picturer(r, req)
	for i, row := range rows {
		cells := make([]string, len(cols))
		for j, at := range cols {
			if at >= 0 {
				cells[j] = row[at]
			}
		}
		tr := render.TableRow{Cells: cells}
		if len(table.PictureColumns) > 0 {
			tr.Pictures = pics.of(&assets[i])
		}
		group.Rows = append(group.Rows, tr)
	}
	table.Groups = []render.TableGroup{group}
	d.Table(table)
	writeDocument(w, d, fmt.Sprintf("assets-%s", time.Now().UTC().Format("20060102")))
}

// columnHeadings are how a document heads the export's columns: by name,
// a code by its symbology, a field by its label.
func (s *Server) columnHeadings(columns []string) ([]string, error) {
	fields, err := s.store.ListFields()
	if err != nil {
		return nil, err
	}
	labelOf := map[string]string{}
	for _, f := range fields {
		labelOf[f.Key] = f.Label
	}
	codeNames := map[string]string{}
	for _, g := range generators.Describe() {
		codeNames[g.Type] = g.Name
	}
	fixed := map[string]string{
		assetcsv.ColumnID: "ID", assetcsv.ColumnName: "Name", assetcsv.ColumnCategories: "Categories",
		assetcsv.ColumnQuantity: "Quantity", assetcsv.ColumnUnit: "Unit", assetcsv.ColumnThreshold: "Low at",
		assetcsv.ColumnIcon: "Icon", assetcsv.ColumnLinks: "Links",
	}
	out := make([]string, len(columns))
	for i, c := range columns {
		switch {
		case fixed[c] != "":
			out[i] = fixed[c]
		case strings.HasPrefix(c, assetcsv.CodePrefix):
			out[i] = codeNames[strings.TrimPrefix(c, assetcsv.CodePrefix)]
		case strings.HasPrefix(c, assetcsv.FieldPrefix):
			out[i] = labelOf[strings.TrimPrefix(c, assetcsv.FieldPrefix)]
		}
		if out[i] == "" {
			out[i] = c
		}
	}
	return out, nil
}

// DocumentSettingsRequest sets the instance's name on documents, and their
// page size.
type DocumentSettingsRequest struct {
	InstanceName string `json:"instance_name"`
	PageSize     string `json:"page_size"`
}

// handleDocumentSettings reads them: GET /api/v1/settings/documents.
func (s *Server) handleDocumentSettings(w http.ResponseWriter, r *http.Request) {
	name, err := s.store.Setting(InstanceNameSetting, defaultInstanceName)
	if err == nil {
		var size string
		if size, err = s.store.Setting(PageSizeSetting, render.Letter.Name); err == nil {
			writeJSON(w, http.StatusOK, DocumentSettingsRequest{InstanceName: name, PageSize: size})
			return
		}
	}
	writeServerError(w, err, "read the document settings")
}

// handleSetDocumentSettings sets them: PUT /api/v1/settings/documents.
func (s *Server) handleSetDocumentSettings(w http.ResponseWriter, r *http.Request) {
	var req DocumentSettingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.InstanceName = strings.TrimSpace(req.InstanceName)
	if req.InstanceName == "" || len([]rune(req.InstanceName)) > 100 {
		writeJSONError(w, http.StatusBadRequest, "The instance's name is required, and at most 100 characters")
		return
	}
	size, ok := render.PageSizeNamed(req.PageSize)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, `page_size must be "letter" or "a4"`)
		return
	}
	for key, value := range map[string]string{InstanceNameSetting: req.InstanceName, PageSizeSetting: size.Name} {
		if err := s.store.SetSetting(key, value); err != nil {
			writeServerError(w, err, "save the document settings")
			return
		}
	}
	writeJSON(w, http.StatusOK, DocumentSettingsRequest{InstanceName: req.InstanceName, PageSize: size.Name})
}
