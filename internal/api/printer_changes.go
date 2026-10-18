package api

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/labels"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/printers"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// PrinterChanges is what changing a printer, such as loading another roll,
// would leave without a printer (README §4.4), so it can be said before
// the change is saved.
type PrinterChanges struct {
	// Stranded are labels waiting in the spool for the printer, or a pool
	// it is in, that would no longer fit there, by template and destination.
	Stranded []StrandedLabels `json:"stranded"`
	// Categories are those whose label template no printer would suit.
	Categories []SpoolChoice `json:"categories"`
	// Pools are those whose printers would no longer be loaded alike.
	Pools []SpoolChoice `json:"pools"`
}

// StrandedLabels are waiting labels a change would leave where they no
// longer fit.
type StrandedLabels struct {
	TemplateID   string `json:"template_id"`
	TemplateName string `json:"template_name"`
	Destination  string `json:"destination"`
	// Target names the printer or pool.
	Target string `json:"target"`
	Labels int    `json:"labels"`
}

// Empty reports whether the change affects nothing.
func (c PrinterChanges) Empty() bool {
	return len(c.Stranded) == 0 && len(c.Categories) == 0 && len(c.Pools) == 0
}

// printerChanges works out what replacing a printer with next would affect.
func (s *Server) printerChanges(next *models.Printer) (PrinterChanges, error) {
	changes := PrinterChanges{Stranded: []StrandedLabels{}, Categories: []SpoolChoice{}, Pools: []SpoolChoice{}}
	all, err := s.store.ListPrinters()
	if err != nil {
		return changes, err
	}
	templates, err := s.store.ListTemplates()
	if err != nil {
		return changes, err
	}
	pools, err := s.store.ListPools()
	if err != nil {
		return changes, err
	}
	byID := map[string]*models.Printer{}
	for i := range all {
		byID[all[i].ID] = &all[i]
	}
	// Whether a template suits a printer, before the change and after it.
	type key struct{ template, printer string }
	before, after := map[key]bool{}, map[key]bool{}
	templateByID := map[string]*labels.Template{}
	for i := range templates {
		t := &templates[i]
		templateByID[t.ID] = t
		for _, p := range all {
			_, _, err := planFor(&p, t)
			before[key{t.ID, p.ID}] = err == nil
			after[key{t.ID, p.ID}] = err == nil
		}
		_, _, err := planFor(next, t)
		after[key{t.ID, next.ID}] = err == nil
	}
	anyFits := func(fits map[key]bool, templateID string, printerIDs []string) bool {
		for _, id := range printerIDs {
			if fits[key{templateID, id}] {
				return true
			}
		}
		return false
	}
	printerIDs := make([]string, 0, len(all))
	for _, p := range all {
		printerIDs = append(printerIDs, p.ID)
	}
	poolByID := map[string]*models.PrinterPool{}
	for i := range pools {
		poolByID[pools[i].ID] = &pools[i]
	}
	members := func(p *models.PrinterPool) []string {
		ids := make([]string, 0, len(p.Members))
		for _, m := range p.Members {
			ids = append(ids, m.PrinterID)
		}
		return ids
	}

	// Labels waiting for it, or for a pool it is in.
	entries, err := s.store.ListSpool(store.SpoolFilter{})
	if err != nil {
		return changes, err
	}
	stranded := map[string]*StrandedLabels{}
	var order []string
	for _, e := range entries {
		if e.Job != nil || e.TemplateID == nil || templateByID[*e.TemplateID] == nil {
			continue
		}
		t := *e.TemplateID
		var target string
		switch {
		case e.Destination == models.DestinationPrinterPrefix+next.ID:
			if !before[key{t, next.ID}] || after[key{t, next.ID}] {
				continue
			}
			target = next.Name
		case strings.HasPrefix(e.Destination, models.DestinationPoolPrefix):
			pool := poolByID[strings.TrimPrefix(e.Destination, models.DestinationPoolPrefix)]
			if pool == nil || !contains(members(pool), next.ID) || !anyFits(before, t, members(pool)) || anyFits(after, t, members(pool)) {
				continue
			}
			target = pool.Name
		default:
			continue
		}
		k := t + "|" + e.Destination
		if stranded[k] == nil {
			stranded[k] = &StrandedLabels{TemplateID: t, TemplateName: templateByID[t].Name, Destination: e.Destination, Target: target}
			order = append(order, k)
		}
		stranded[k].Labels += e.Copies
	}
	for _, k := range order {
		changes.Stranded = append(changes.Stranded, *stranded[k])
	}

	// Categories whose template only it suits.
	categories, err := s.store.ListCategories()
	if err != nil {
		return changes, err
	}
	for _, c := range categories {
		if c.LabelTemplateID == nil || templateByID[*c.LabelTemplateID] == nil {
			continue
		}
		if anyFits(before, *c.LabelTemplateID, printerIDs) && !anyFits(after, *c.LabelTemplateID, printerIDs) {
			changes.Categories = append(changes.Categories, SpoolChoice{ID: c.ID, Name: c.Name})
		}
	}

	// Pools loaded alike that it would set apart.
	alike := func(ids []string, swap bool) bool {
		seen := map[string]bool{}
		for _, id := range ids {
			p := byID[id]
			if swap && id == next.ID {
				p = next
			}
			if p != nil {
				seen[loadedKey(p)] = true
			}
		}
		return len(seen) <= 1
	}
	for _, p := range pools {
		ids := members(&p)
		if len(ids) > 1 && contains(ids, next.ID) && alike(ids, false) && !alike(ids, true) {
			changes.Pools = append(changes.Pools, SpoolChoice{ID: p.ID, Name: p.Name})
		}
	}
	sort.Slice(changes.Categories, func(i, j int) bool { return changes.Categories[i].Name < changes.Categories[j].Name })
	return changes, nil
}

// loadedKey says what a printer prints: its roll's size and its DPI.
func loadedKey(p *models.Printer) string {
	if p.Loaded == nil {
		return p.Driver + "|" + p.Media
	}
	dpi := 0
	if d, err := printers.Lookup(p.Driver); err == nil {
		dpi = d.Info().ModelDPI[p.Model]
	}
	return fmt.Sprintf("%g×%g@%d", p.Loaded.WidthMM, p.Loaded.LengthMM, dpi)
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
