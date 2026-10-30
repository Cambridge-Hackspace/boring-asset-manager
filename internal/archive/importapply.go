package archive

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// checkIcons compiles the icons as an import would leave them, and notes
// what would not compile as the plan's problems.
func checkIcons(ctx context.Context, st *store.Store, c store.IconCompiler, b *built, plan *ImportPlan) error {
	if len(b.library) == 0 && len(b.icons) == 0 && len(b.palette) == 0 {
		return nil
	}
	check, err := st.CheckIconImport(ctx, c, b.library, b.icons, b.palette)
	if errors.Is(err, isolex.ErrUnavailable) {
		plan.Warnings = append(plan.Warnings, "The icons could not be checked, as isolex is not available here.")
		return nil
	}
	if err != nil {
		return err
	}
	for _, d := range check.Errors {
		plan.Problems = append(plan.Problems, fmt.Sprintf("the icons would not compile: %s:%d:%d: %s", d.File, d.Line, d.Col, d.Message))
	}
	if len(check.Errors) > 0 {
		plan.Fingerprint = fingerprint(plan)
	}
	return nil
}

// applier applies a plan: the rows worked out, then the assets' and the
// files as the archive is read again.
type applier struct {
	ctx   context.Context
	st    *store.Store
	tx    *sql.Tx
	blobs filestore.Blobs
	a     *archived
	b     *built
	plan  *ImportPlan
	// stored are the files stored, to remove if the import fails.
	stored []string
	// links are those dropped, so are their placements.
	dropped map[string]bool
	assets  []string
	batch   *batcher
	// codesTaken counts the codes left out, as an asset here has them.
	codesTaken int
}

func (ap *applier) removeFiles() {
	for _, key := range ap.stored {
		_ = ap.blobs.Delete(context.Background(), key)
	}
}

func (ap *applier) apply(r io.Reader) error {
	in := &inserter{ctx: ap.ctx, st: ap.st, tx: ap.tx}
	for _, tr := range ap.b.rows {
		bt := &batcher{in: in, t: tr.t}
		for _, row := range tr.rows {
			if err := bt.add(row); err != nil {
				return fmt.Errorf("importing %s: %w", tr.t.Name, err)
			}
		}
		if err := bt.flush(); err != nil {
			return fmt.Errorf("importing %s: %w", tr.t.Name, err)
		}
	}
	ap.dropped = map[string]bool{}
	ids := ap.b.ids
	err := eachMember(r, ap.b.kinds, func(t *Table, row map[string]any) error {
		if !streamed(t) {
			return nil
		}
		if ap.batch == nil || ap.batch.t != t {
			if err := ap.flush(); err != nil {
				return err
			}
			ap.batch = &batcher{in: in, t: t}
			if t.Name == "asset_generator_keys" {
				ap.batch.filter = ap.freeCodes
			}
		}
		out, ok := ap.row(t, row, ids)
		if !ok {
			return nil
		}
		if t.Name == "assets" {
			ap.assets = append(ap.assets, str(out, "id"))
		}
		return ap.batch.add(out)
	}, func(key string, r io.Reader, size int64) error {
		to, ok := ap.fileKey(key)
		if !ok {
			return nil
		}
		if err := ap.blobs.Put(ap.ctx, to, r, size, "application/octet-stream"); err != nil {
			return fmt.Errorf("storing %s: %w", key, err)
		}
		ap.stored = append(ap.stored, to)
		return nil
	}, nil)
	if err != nil {
		return err
	}
	if err := ap.flush(); err != nil {
		return err
	}
	if ap.plan.Assets != nil {
		ap.plan.Assets.CodesTaken = ap.codesTaken
	}
	return ap.st.RecordImportedTx(ap.tx, ap.assets)
}

func (ap *applier) flush() error {
	if ap.batch == nil {
		return nil
	}
	if err := ap.batch.flush(); err != nil {
		return fmt.Errorf("importing %s: %w", ap.batch.t.Name, err)
	}
	return nil
}

// row is a streamed row as imported, or false to leave it out.
func (ap *applier) row(t *Table, r map[string]any, ids newIDs) (map[string]any, bool) {
	b := ap.b
	switch t.Name {
	case "assets":
		// its own icons, as renamed; an archive made before renditions
		// has no label icon
		var icon, labelIcon any
		if n := str(r, "icon_name"); n != "" {
			icon = iconRenamed(b, n)
		}
		if n := str(r, "label_icon_name"); n != "" {
			labelIcon = iconRenamed(b, n)
		}
		return with(r, "id", ids.of(str(r, "id")), "icon_name", icon, "label_icon_name", labelIcon), true
	case "asset_categories":
		c, ok := b.cats[str(r, "category_id")]
		return with(r, "asset_id", ids.of(str(r, "asset_id")), "category_id", c), ok
	case "asset_field_values":
		f, ok := b.fields[str(r, "field_id")]
		return with(r, "asset_id", ids.of(str(r, "asset_id")), "field_id", f), ok
	case "asset_generator_keys":
		return with(r, "asset_id", ids.of(str(r, "asset_id"))), true
	case "asset_links":
		rt, ok := b.relTypes[str(r, "relation_type_id")]
		if !ok {
			ap.dropped[str(r, "id")] = true
			return nil, false
		}
		return with(r, "id", ids.of(str(r, "id")), "relation_type_id", rt, "from_asset_id", ids.of(str(r, "from_asset_id")), "to_asset_id", ids.of(str(r, "to_asset_id"))), true
	case "asset_placements":
		if ap.dropped[str(r, "link_id")] {
			return nil, false
		}
		return with(r, "link_id", ids.of(str(r, "link_id"))), true
	case "asset_photos":
		return with(r, "id", ids.of(str(r, "id")), "thumb_id", ids.of(str(r, "thumb_id")), "asset_id", ids.of(str(r, "asset_id"))), true
	case "events":
		var cat any
		if c, ok := b.cats[str(r, "category_id")]; ok {
			cat = c
		}
		payload, err := withoutPeople(r["payload"])
		if err != nil {
			payload = "{}"
		}
		var asset any
		if a := str(r, "asset_id"); a != "" {
			asset = ids.of(a)
		}
		return with(r, "id", ids.of(str(r, "id")), "asset_id", asset, "category_id", cat, "actor_user_id", nil, "payload", payload), true
	}
	return nil, false
}

// fileKey is where a file of the archive is stored, or false when it is
// not imported.
func (ap *applier) fileKey(key string) (string, bool) {
	if !fileWanted(key, ap.b.kinds) {
		return "", false
	}
	if id, ok := strings.CutPrefix(key, "photos/"); ok {
		return "photos/" + ap.b.ids.of(id), true
	}
	if id, ok := strings.CutPrefix(key, "images/"); ok && ap.b.imagesStored[id] {
		return "images/" + ap.b.images[id], true
	}
	return "", false
}

// freeCodes leaves out the codes some asset here has already.
func (ap *applier) freeCodes(t *Table, cols []Column, rows [][]any) ([][]any, error) {
	ti, ci := colIndexIn(cols, "generator_type"), colIndexIn(cols, "assigned_code")
	codes := make([][2]string, len(rows))
	for i, r := range rows {
		codes[i] = [2]string{fmt.Sprint(r[ti]), fmt.Sprint(r[ci])}
	}
	taken, err := codesTaken(ap.ctx, ap.st, ap.tx, codes)
	if err != nil {
		return nil, err
	}
	var out [][]any
	for i, r := range rows {
		if taken[codes[i]] {
			ap.codesTaken++
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// batcher inserts a table's rows in batches.
type batcher struct {
	in     *inserter
	t      *Table
	cols   []Column
	rows   [][]any
	filter func(t *Table, cols []Column, rows [][]any) ([][]any, error)
}

func (bt *batcher) add(row map[string]any) error {
	if bt.cols == nil {
		for _, c := range bt.t.Columns {
			if _, ok := row[c.Name]; ok {
				bt.cols = append(bt.cols, c)
			}
		}
	}
	vals := make([]any, len(bt.cols))
	for i, c := range bt.cols {
		vals[i] = row[c.Name]
	}
	bt.rows = append(bt.rows, vals)
	if (len(bt.rows)+1)*len(bt.cols) > batchArgs {
		return bt.flush()
	}
	return nil
}

func (bt *batcher) flush() error {
	rows := bt.rows
	bt.rows = nil
	if bt.filter != nil && len(rows) > 0 {
		var err error
		if rows, err = bt.filter(bt.t, bt.cols, rows); err != nil {
			return err
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return bt.in.insert(bt.t, bt.cols, rows)
}
