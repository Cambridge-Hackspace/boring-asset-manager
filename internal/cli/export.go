package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/api"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/archive"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/config"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// ExportOptions are what bam export packages (#144, docs/packages.md).
type ExportOptions struct {
	// Select are what to package, each kind:name, as SelectKinds lists.
	Select []string
	// Photos, Links and History come with the assets chosen.
	Photos, Links, History bool
	// Info says what the package is; PreviewFile is its preview image.
	Info        archive.PackageInfo
	PreviewFile string
	// Plan says what the package would hold, writing nothing.
	Plan bool
}

// selectKinds are what --select chooses, and what each is named by.
var selectKinds = []struct{ kind, by, table, column, idKind string }{
	{"category", "name", "categories", "name", archive.ItemCategory},
	{"subtree", "name, with every category under it", "categories", "name", archive.ItemCategory},
	{"field", "key", "fields", "field_key", archive.ItemField},
	{"hook", "key", "asset_actions", "action_key", archive.ItemHook},
	{"link-type", "name", "relation_types", "name", archive.ItemRelationType},
	{"label-template", "name", "templates", "name", archive.ItemLabelTemplate},
	{"scan-template", "name", "reports", "name", archive.ItemScanTemplate},
	{"report", "name", "reports", "name", archive.ItemReport},
	{"flow", "address (slug)", "flows", "slug", archive.ItemFlow},
	{"scope", "name", "scopes", "name", archive.ItemScope},
}

// SelectKinds describes what --select takes.
func SelectKinds() string {
	var b strings.Builder
	for _, k := range selectKinds {
		fmt.Fprintf(&b, "%s:<%s>, ", k.kind, k.by)
	}
	b.WriteString("icons (the library, palette and settings), assets (those in the categories chosen) or assets:all")
	return b.String()
}

// Export writes a package of what opts selects to dest.
func Export(configPath, dest string, opts ExportOptions) error {
	if dest == "" && !opts.Plan {
		return errors.New("--to is required")
	}
	if len(opts.Select) == 0 {
		return errors.New("--select is required: " + SelectKinds())
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}
	st, err := store.Open(&cfg.Database)
	if err != nil {
		return err
	}
	defer st.Close()
	sel, err := ParseSelection(st, opts.Select)
	if err != nil {
		return err
	}
	if sel.Assets != nil {
		sel.Assets.Photos, sel.Assets.Links, sel.Assets.History = opts.Photos, opts.Links, opts.History
	} else if opts.Photos || opts.Links || opts.History {
		return errors.New("--photos, --links and --history come with assets: --select assets or assets:all")
	}
	ctx := context.Background()
	plan, err := archive.Resolve(ctx, st, st.DB, sel, nil)
	if err != nil {
		return err
	}
	if opts.Plan {
		printPlan(os.Stdout, plan)
		return nil
	}
	if opts.Info.Name == "" {
		return errors.New("--name is required: what the package is called")
	}
	var preview []byte
	if opts.PreviewFile != "" {
		if preview, err = os.ReadFile(opts.PreviewFile); err != nil {
			return err
		}
	}
	if sel.IconsOnly() {
		// an icon set: a zipped isolex library folder, as the studio opens
		f, err := os.OpenFile(dest+".partial", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if err := archive.WriteIconSet(ctx, st, f); err != nil {
			f.Close()
			_ = os.Remove(dest + ".partial")
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		if err := os.Rename(dest+".partial", dest); err != nil {
			return err
		}
		fmt.Printf("Wrote the icons as an isolex library folder, zipped, to %s\n", dest)
		return nil
	}
	blobs, err := filestore.Open(cfg.Storage)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dest+".partial", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	sum, err := archive.Write(ctx, st, blobs, func(int) (io.WriteCloser, error) { return f, nil }, archive.Options{
		BamVersion: api.Version, Select: &sel, Info: &opts.Info, Preview: preview,
	})
	if err != nil {
		f.Close()
		_ = os.Remove(dest + ".partial")
		return err
	}
	if err := os.Rename(dest+".partial", dest); err != nil {
		return err
	}
	rows := 0
	for _, n := range sum.Tables {
		rows += n
	}
	printPlan(os.Stdout, plan)
	fmt.Printf("Packaged %d rows and %d files (%d bytes) as %s\n", rows, sum.Files, sum.FileBytes, dest)
	return nil
}

// ParseSelection reads --select's items, each kind:name, into a selection,
// naming each by its ID.
func ParseSelection(st *store.Store, items []string) (archive.Selection, error) {
	var sel archive.Selection
	for _, item := range items {
		for _, part := range strings.Split(item, ",") {
			kind, name, _ := strings.Cut(strings.TrimSpace(part), ":")
			switch {
			case kind == "icons" && name == "":
				sel.Icons = true
				continue
			case kind == "assets" && (name == "" || name == "all"):
				if sel.Assets == nil {
					sel.Assets = &archive.AssetsSelection{}
				}
				sel.Assets.All = sel.Assets.All || name == "all"
				continue
			}
			var k *struct{ kind, by, table, column, idKind string }
			for i := range selectKinds {
				if selectKinds[i].kind == kind {
					k = &selectKinds[i]
				}
			}
			if k == nil || name == "" {
				return sel, fmt.Errorf("--select %q: choose %s", part, SelectKinds())
			}
			id, err := idNamed(st, k.table, k.column, k.idKind, name)
			if err != nil {
				return sel, fmt.Errorf("--select %q: %w", part, err)
			}
			switch k.kind {
			case "category":
				sel.Categories = append(sel.Categories, id)
			case "subtree":
				sel.Subtrees = append(sel.Subtrees, id)
			case "field":
				sel.Fields = append(sel.Fields, id)
			case "hook":
				sel.Hooks = append(sel.Hooks, id)
			case "link-type":
				sel.RelationTypes = append(sel.RelationTypes, id)
			case "label-template":
				sel.LabelTemplates = append(sel.LabelTemplates, id)
			case "scan-template":
				sel.ScanTemplates = append(sel.ScanTemplates, id)
			case "report":
				sel.Reports = append(sel.Reports, id)
			case "flow":
				sel.Flows = append(sel.Flows, id)
			case "scope":
				sel.Scopes = append(sel.Scopes, id)
			}
		}
	}
	return sel, nil
}

// idNamed finds the ID of what a name names: by its ID, or by the column
// named, which must name one alone.
func idNamed(st *store.Store, table, column, kind, name string) (string, error) {
	where := ""
	switch kind {
	case archive.ItemScanTemplate:
		where = " AND kind = 'scan'"
	case archive.ItemReport:
		where = " AND kind <> 'scan'"
	}
	rows, err := st.Query("SELECT id FROM "+table+" WHERE (id = ? OR "+column+" = ?)"+where+" ORDER BY id", name, name)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(ids) {
	case 0:
		return "", fmt.Errorf("there is no %s %q", strings.ReplaceAll(kind, "_", " "), name)
	case 1:
		return ids[0], nil
	}
	return "", fmt.Errorf("%q names %d of them; give one's ID: %s", name, len(ids), strings.Join(ids, ", "))
}

// printPlan says what a package holds, what needs what, and what it goes
// without.
func printPlan(w io.Writer, plan *archive.Plan) {
	fmt.Fprintln(w, "The package holds:")
	for _, it := range plan.Items {
		if it.Kind == archive.ItemAssets {
			continue
		}
		line := "  " + strings.ReplaceAll(it.Kind, "_", " ") + " " + it.Name
		if len(it.NeededBy) > 0 {
			var by []string
			for _, n := range it.NeededBy {
				by = append(by, n.Name)
			}
			sort.Strings(by)
			line += ", needed by " + strings.Join(by, ", ")
		}
		fmt.Fprintln(w, line)
	}
	if a := plan.Assets; a != nil {
		fmt.Fprintf(w, "  %d assets, %d photos, %d links and %d events of their history\n", a.Assets, a.Photos, a.Links, a.Events)
	}
	for _, l := range plan.LeftOut {
		var by []string
		for _, n := range l.NeededBy {
			by = append(by, n.Name)
		}
		fmt.Fprintf(w, "Left out: %s %s, named by %s: %s\n", strings.ReplaceAll(l.Kind, "_", " "), l.Name, strings.Join(by, ", "), l.Reason)
	}
}
