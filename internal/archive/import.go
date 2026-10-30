package archive

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/filestore"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/store"
)

// Importing (#145, README §2.3): a package, or parts of a backup, merged
// into an instance in use, rather than restored. What the archive holds is
// matched against what the instance has, by the keys that are the same
// everywhere (field and action keys, names, flow addresses, icon names),
// and the plan says what will be created, what used as it is here, what
// copied under another name and what skipped. Import then does exactly
// that, in one transaction, giving everything new IDs, so one package can
// be imported twice under different categories. People, settings and
// authentication are never merged: they are an instance's own.

// Choice is what an import does with one thing an archive holds.
type Choice string

// The choices. Create is for what nothing here clashes with; the others
// are offered for what does.
const (
	ChoiceCreate   Choice = "create"
	ChoiceExisting Choice = "existing"
	ChoiceCopy     Choice = "copy"
	ChoiceSkip     Choice = "skip"
)

// ItemCategoryIcon is a category's own icon, by its category's ID in the
// archive, and for a rendition other than the screen, /<rendition> after
// it (iconItemID).
const ItemCategoryIcon = "category_icon"

// iconItemID is the ID of the item of a category's icon for rendition.
func iconItemID(categoryID, rendition string) string {
	if rendition == "" || rendition == models.RenditionScreen {
		return categoryID
	}
	return categoryID + "/" + rendition
}

// rowRendition is the rendition of a row of a table keyed by category
// and rendition; a row of an archive made before renditions has none, and
// is the screen's.
func rowRendition(r map[string]any) string {
	if rendition := str(r, "rendition"); rendition != "" {
		return rendition
	}
	return models.RenditionScreen
}

// ImportItem is one thing an archive holds, and what importing does with
// it.
type ImportItem struct {
	Ref
	Action Choice `json:"action"`
	// As is what it is called here, created or copied: its key, name,
	// address or path, when that is not its own.
	As string `json:"as,omitempty"`
	// Existing is what it clashes with here, by the same key.
	Existing *Ref `json:"existing,omitempty"`
	// Identical is set when what it clashes with is the same.
	Identical bool `json:"identical,omitempty"`
	// Choices are what may be chosen for it: none when nothing clashes.
	Choices []Choice `json:"choices,omitempty"`
	// Note says what changes as it comes in.
	Note string `json:"note,omitempty"`
}

// Key is how a choice names the item.
func (it ImportItem) Key() string { return it.Kind + ":" + it.ID }

// ImportAssets counts the assets an import brings, and what comes with
// them.
type ImportAssets struct {
	Assets int `json:"assets"`
	Photos int `json:"photos"`
	Links  int `json:"links"`
	Events int `json:"events"`
	// ValuesDropped are values of fields skipped, and LinksDropped links of
	// link types skipped.
	ValuesDropped int `json:"values_dropped"`
	LinksDropped  int `json:"links_dropped"`
	// CodesTaken are codes another asset here has already: those assets
	// come without them.
	CodesTaken int `json:"codes_taken"`
}

// ImportPlan is what importing an archive does.
type ImportPlan struct {
	Header Header `json:"header"`
	// Kinds are the kinds of information merged.
	Kinds []Kind `json:"kinds"`
	// Anchor is the category the archive's top categories go under; nil
	// for the top level.
	Anchor *Ref          `json:"anchor"`
	Items  []ImportItem  `json:"items"`
	Assets *ImportAssets `json:"assets,omitempty"`
	// Files counts the files it stores, and their bytes.
	Files     int   `json:"files"`
	FileBytes int64 `json:"file_bytes"`
	// Permissions are what it needs of the importer.
	Permissions []string `json:"permissions"`
	// Missing are those of them the importer lacks.
	Missing  []string `json:"missing"`
	Warnings []string `json:"warnings"`
	// Problems stop it: what it needs is skipped or not there, or an icon
	// would not compile.
	Problems []string `json:"problems"`
	// Fingerprint is what an import compares, to do what was checked.
	Fingerprint string `json:"fingerprint"`
}

// ImportOptions are what an import is told.
type ImportOptions struct {
	// Anchor is the category the archive's top categories go under; empty
	// for the top level.
	Anchor string
	// Choices are what to do with what clashes, by ImportItem.Key; what is
	// not chosen takes its default: the existing one when identical, a
	// copy otherwise.
	Choices map[string]Choice
	// Kinds are the kinds to merge, of a backup's; nil for every one that
	// can be.
	Kinds []Kind
	// May is what the importer may do (Reader's terms); nil for
	// everything, as the CLI may.
	May Reader
	// Web is set for an import through the web, which never brings
	// authentication.
	Web bool
	// Icons, if set, compiles the icons as they would be, to refuse an
	// import they would not compile after.
	Icons store.IconCompiler
	// Fingerprint, if set, is the check's, which the import must match.
	Fingerprint string
}

var (
	// ErrImport is an archive that cannot be imported as chosen.
	ErrImport = errors.New("archive: cannot be imported")
	// ErrForbidden is an import needing a permission the importer lacks.
	ErrForbidden = errors.New("archive: the import needs a permission you do not have")
	// ErrChanged is an import whose plan is not the one checked, as the
	// instance changed since.
	ErrChanged = errors.New("archive: the instance has changed since the import was checked; check it again")
)

// mergeable are the kinds an import merges.
var mergeable = []Kind{KindCatalog, KindLabels, KindReports, KindIcons, KindFlows, KindAssets}

// mergeTables are the tables an import merges, of the kinds chosen. The
// others name people or are an instance's own.
var mergeTables = map[string]bool{
	"reports": true, "scopes": true, "relation_types": true, "fields": true, "asset_actions": true, "categories": true,
	"category_fields": true, "category_actions": true, "category_relation_types": true, "category_scopes": true,
	"templates": true, "images": true, "category_label_templates": true,
	"icon_palette": true, "icon_settings": true, "icon_library": true, "category_icons": true, "icon_param_bindings": true, "icon_text_bindings": true,
	"assets": true, "asset_categories": true, "asset_field_values": true, "asset_generator_keys": true, "asset_links": true,
	"asset_placements": true, "asset_photos": true, "events": true,
	"flows": true,
}

// streamed are the tables read as they come, however many rows they have:
// the assets and what comes with them. The rest are read whole.
func streamed(t *Table) bool { return t.Kind == KindAssets }

// CheckImport checks an archive, verifying every member first, and works
// out what importing it would do, changing nothing. A plan with problems
// cannot be imported; one needing what the importer may not do is refused
// with ErrForbidden.
func CheckImport(ctx context.Context, st *store.Store, open func() (io.ReadCloser, error), opts ImportOptions) (*ImportPlan, error) {
	if data, err := iconSetUpload(open); err != nil {
		return nil, err
	} else if data != nil {
		plan, _, err := checkIconSetImport(ctx, st, data, opts)
		if err == nil && len(plan.Missing) > 0 {
			err = fmt.Errorf("%w: %s", ErrForbidden, strings.Join(plan.Missing, "; "))
		}
		return plan, err
	}
	plan, _, _, err := checkImport(ctx, st, open, opts)
	return plan, err
}

// iconSetUpload reads an upload that is an icon set, a zip, whole; nil
// for one that is not.
func iconSetUpload(open func() (io.ReadCloser, error)) ([]byte, error) {
	rc, err := open()
	if err != nil {
		return nil, err
	}
	head := make([]byte, 4)
	n, _ := io.ReadFull(rc, head)
	rc.Close()
	if !IsIconSet(head[:n]) {
		return nil, nil
	}
	return readUpload(open, MaxIconSet)
}

// importIconSet imports an icon set as its check says, as one icon change.
func importIconSet(ctx context.Context, st *store.Store, data []byte, opts ImportOptions) (*ImportPlan, error) {
	plan, set, err := checkIconSetImport(ctx, st, data, opts)
	if err != nil {
		return plan, err
	}
	if len(plan.Missing) > 0 {
		return plan, fmt.Errorf("%w: %s", ErrForbidden, strings.Join(plan.Missing, "; "))
	}
	if len(plan.Problems) > 0 {
		return plan, fmt.Errorf("%w: %s", ErrImport, strings.Join(plan.Problems, "; "))
	}
	if opts.Fingerprint != "" && opts.Fingerprint != plan.Fingerprint {
		return plan, ErrChanged
	}
	if len(set.Library) == 0 && len(set.Icons) == 0 && len(set.Palette) == 0 {
		return plan, nil
	}
	if _, err := st.ApplyIconSet(ctx, opts.Icons, set); err != nil {
		return plan, fmt.Errorf("%w: %v", ErrImport, err)
	}
	return plan, nil
}

func checkImport(ctx context.Context, st *store.Store, open func() (io.ReadCloser, error), opts ImportOptions) (*ImportPlan, *archived, *built, error) {
	rc, err := open()
	if err != nil {
		return nil, nil, nil, err
	}
	checked, err := Check(rc)
	rc.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	if checked.Header.Set.Parts > 1 || checked.Header.Set.Part > 1 {
		return nil, nil, nil, fmt.Errorf("%w: it is one of %d parts; an archive in parts is restored, not imported", ErrImport, max(checked.Header.Set.Parts, checked.Header.Set.Part))
	}
	kinds, warnings, err := mergedKinds(checked, opts)
	if err != nil {
		return nil, nil, nil, err
	}
	rc, err = open()
	if err != nil {
		return nil, nil, nil, err
	}
	a, err := readArchived(ctx, st, st.DB, rc, checked, kinds)
	rc.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	plan, b, err := build(ctx, st, st.DB, a, opts)
	if err != nil {
		return nil, nil, nil, err
	}
	plan.Warnings = append(warnings, plan.Warnings...)
	if opts.Icons != nil {
		if err := checkIcons(ctx, st, opts.Icons, b, plan); err != nil {
			return nil, nil, nil, err
		}
	}
	if len(plan.Missing) > 0 {
		return plan, a, b, fmt.Errorf("%w: %s", ErrForbidden, strings.Join(plan.Missing, "; "))
	}
	return plan, a, b, nil
}

// Import imports an archive as CheckImport says it would: checked again
// first, and refused if it has problems or, given the check's fingerprint,
// does not do what was checked. The rows go in one transaction; the files
// are stored before it commits, and removed again if it does not.
func Import(ctx context.Context, st *store.Store, blobs filestore.Blobs, open func() (io.ReadCloser, error), opts ImportOptions) (*ImportPlan, error) {
	if data, err := iconSetUpload(open); err != nil {
		return nil, err
	} else if data != nil {
		return importIconSet(ctx, st, data, opts)
	}
	plan, a, _, err := checkImport(ctx, st, open, opts)
	if err != nil {
		return plan, err
	}
	if len(plan.Problems) > 0 {
		return plan, fmt.Errorf("%w: %s", ErrImport, strings.Join(plan.Problems, "; "))
	}
	if opts.Fingerprint != "" && opts.Fingerprint != plan.Fingerprint {
		return plan, ErrChanged
	}

	tx, err := st.DB.BeginTx(ctx, nil)
	if err != nil {
		return plan, err
	}
	defer tx.Rollback() //nolint:errcheck // after Commit, a no-op
	// worked out again where it is applied, which must come to the same
	again, b, err := build(ctx, st, tx, a, opts)
	if err != nil {
		return plan, err
	}
	if again.Fingerprint != plan.Fingerprint {
		return plan, ErrChanged
	}
	rc, err := open()
	if err != nil {
		return plan, err
	}
	defer rc.Close()
	ap := &applier{ctx: ctx, st: st, tx: tx, blobs: blobs, b: b, a: a, plan: plan}
	if err := ap.apply(rc); err != nil {
		ap.removeFiles()
		return plan, err
	}
	if err := tx.Commit(); err != nil {
		ap.removeFiles()
		return plan, err
	}
	return plan, nil
}

// mergedKinds are the kinds an import merges, and warnings for those it
// does not.
func mergedKinds(c *Checked, opts ImportOptions) ([]Kind, []string, error) {
	has := c.Header.Kinds
	for _, k := range opts.Kinds {
		if !hasKind(has, k) {
			return nil, nil, fmt.Errorf("%w: it holds no %s", ErrImport, k)
		}
		if !hasKind(mergeable, k) {
			return nil, nil, fmt.Errorf("%w: %s is never merged; restore the backup, or replace with it, to bring it", ErrImport, k)
		}
	}
	var kinds []Kind
	var warnings []string
	for _, k := range has {
		switch {
		case k == KindAuth && opts.Web:
			warnings = append(warnings, "Authentication (passwords, sessions and tokens) is never imported through the web, and is left out.")
		case k == KindAuth:
			warnings = append(warnings, "Authentication is never merged into an instance in use, and is left out: bam restore restores it.")
		case k == KindPeople:
			warnings = append(warnings, "People and roles are an instance's own, and are not merged: restore the backup, or replace with it, to bring them.")
		case k == KindSettings:
			warnings = append(warnings, "Settings, printers, label stock and notification channels are an instance's own, and are not merged.")
		case opts.Kinds == nil || hasKind(opts.Kinds, k):
			kinds = append(kinds, k)
		}
	}
	for _, t := range Tables {
		if hasKind(kinds, t.Kind) && !mergeTables[t.Name] && c.Rows[t.Name] > 0 {
			warnings = append(warnings, fmt.Sprintf("%d rows of %s are left out: they name people, or are this instance's own.", c.Rows[t.Name], t.Name))
		}
	}
	if len(kinds) == 0 {
		return nil, warnings, fmt.Errorf("%w: it holds nothing that can be merged", ErrImport)
	}
	return kinds, warnings, nil
}

// archived is what an archive holds, as read: the rows of the tables read
// whole, and counts of those streamed.
type archived struct {
	header Header
	kinds  []Kind
	rows   map[string][]map[string]any
	counts ImportAssets
	// values and links count field values by field, and links by link
	// type.
	values map[string]int
	links  map[string]int
	// catSets are the sets of categories assets are in, and assetIDs the
	// assets, when flows may name them as places.
	catSets  map[string][]string
	assetIDs map[string]bool
	files    int
	bytes    int64
	// images are the label images' files it holds.
	images map[string]bool
}

// readArchived reads an archive's rows: whole, but for the assets', which
// are counted, and their codes checked against q's.
func readArchived(ctx context.Context, st *store.Store, q querier, r io.Reader, c *Checked, kinds []Kind) (*archived, error) {
	a := &archived{header: c.Header, kinds: kinds, rows: map[string][]map[string]any{}, values: map[string]int{}, links: map[string]int{},
		catSets: map[string][]string{}, images: map[string]bool{}}
	if hasKind(kinds, KindFlows) && hasKind(kinds, KindAssets) {
		a.assetIDs = map[string]bool{}
	}
	codes := &codeCheck{ctx: ctx, st: st, q: q}
	sources := map[string]string{}
	var setOf string
	var set []string
	categorized := 0
	endSet := func() {
		if setOf != "" {
			sort.Strings(set)
			a.catSets[strings.Join(set, ",")] = set
			categorized++
		}
		setOf, set = "", nil
	}
	err := eachMember(r, kinds, func(t *Table, row map[string]any) error {
		if !streamed(t) {
			a.rows[t.Name] = append(a.rows[t.Name], row)
			return nil
		}
		switch t.Name {
		case "assets":
			a.counts.Assets++
			if a.assetIDs != nil {
				a.assetIDs[str(row, "id")] = true
			}
		case "asset_categories":
			// in order of the asset, so each asset's are together
			if id := str(row, "asset_id"); id != setOf {
				endSet()
				setOf = id
			}
			set = append(set, str(row, "category_id"))
		case "asset_field_values":
			a.values[str(row, "field_id")]++
		case "asset_generator_keys":
			return codes.add(str(row, "generator_type"), str(row, "assigned_code"))
		case "asset_links":
			a.counts.Links++
			a.links[str(row, "relation_type_id")]++
		case "asset_photos":
			a.counts.Photos++
		case "events":
			a.counts.Events++
		}
		return nil
	}, func(key string, _ io.Reader, size int64) error {
		if !fileWanted(key, kinds) {
			return nil
		}
		a.files++
		a.bytes += size
		if id, ok := strings.CutPrefix(key, "images/"); ok {
			a.images[id] = true
		}
		return nil
	}, func(rest, source string) { sources[rest] = source })
	endSet()
	if err != nil {
		return nil, err
	}
	if c.Header.FormatVersion >= 2 {
		// the icons' sources, from the folder
		for table, col := range iconSourceTables {
			for _, row := range a.rows[table] {
				var src any
				if s, ok := sources[iconSourceKey(table, str(row, col))]; ok {
					src = s
				}
				row["source"] = src
			}
		}
	}
	if categorized < a.counts.Assets {
		// assets in no category, in no scope
		a.catSets[""] = []string{}
	}
	if err := codes.flush(); err != nil {
		return nil, err
	}
	a.counts.CodesTaken = codes.taken
	return a, nil
}

// fileWanted reports whether an import stores a file: photos with assets,
// label images with labels.
func fileWanted(key string, kinds []Kind) bool {
	for prefix, k := range fileKinds {
		if strings.HasPrefix(key, prefix) {
			return hasKind(kinds, k)
		}
	}
	return false
}

// eachMember reads an archive's members, giving each row of the tables
// merged, of the kinds given, to row, each file to file, and each icon
// source in icons/ to icon, if it is not nil.
func eachMember(r io.Reader, kinds []Kind, row func(t *Table, row map[string]any) error, file func(key string, r io.Reader, size int64) error, icon func(rest, source string)) error {
	m, err := openMembers(bufio.NewReader(r))
	if err != nil {
		return err
	}
	for {
		h, err := m.next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		dir, rest, _ := strings.Cut(h.Name, "/")
		switch dir + "/" {
		case DataDir:
			table, _, _ := strings.Cut(rest, "/")
			t := tableByName(table)
			if t == nil || !mergeTables[t.Name] || !hasKind(kinds, t.Kind) {
				continue
			}
			if err := eachRow(t, m.tr, func(r map[string]any) error { return row(t, r) }); err != nil {
				return fmt.Errorf("reading %s: %w", table, err)
			}
		case FilesDir:
			if err := file(rest, m.tr, h.Size); err != nil {
				return err
			}
		case IconsDir:
			if icon == nil || !hasKind(kinds, KindIcons) {
				continue
			}
			src, err := readIconFile(h.Name, m.tr)
			if err != nil {
				return err
			}
			icon(rest, src)
		}
	}
}

// eachRow reads a member's rows, each by column, its values as Write
// wrote them.
func eachRow(t *Table, r io.Reader, fn func(row map[string]any) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), RowChunk+1)
	var cols []Column
	for n := 1; sc.Scan(); n++ {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
			return fmt.Errorf("row %d: %w", n, err)
		}
		if cols == nil {
			var err error
			if cols, err = rowColumns(t, raw, false); err != nil {
				return err
			}
		}
		row := make(map[string]any, len(cols))
		for _, c := range cols {
			v, ok := raw[c.Name]
			if !ok {
				return fmt.Errorf("row %d has no %s", n, c.Name)
			}
			val, err := decodeValue(c, v)
			if err != nil {
				return fmt.Errorf("row %d, %s: %w", n, c.Name, err)
			}
			row[c.Name] = val
		}
		if err := fn(row); err != nil {
			return err
		}
	}
	return sc.Err()
}

// str is a row's column as text, "" when null.
func str(row map[string]any, col string) string {
	s, _ := text(row[col])
	return s
}

// codeCheck counts the assets' codes taken here, a batch at a time.
type codeCheck struct {
	ctx     context.Context
	st      *store.Store
	q       querier
	pending [][2]string
	taken   int
}

func (c *codeCheck) add(kind, code string) error {
	c.pending = append(c.pending, [2]string{kind, code})
	if len(c.pending) >= 500 {
		return c.flush()
	}
	return nil
}

func (c *codeCheck) flush() error {
	if len(c.pending) == 0 {
		return nil
	}
	taken, err := codesTaken(c.ctx, c.st, c.q, c.pending)
	c.pending = c.pending[:0]
	c.taken += len(taken)
	return err
}

// codesTaken are those of the codes some asset here has.
func codesTaken(ctx context.Context, st *store.Store, q querier, codes [][2]string) (map[[2]string]bool, error) {
	args := make([]any, 0, 2*len(codes))
	for _, c := range codes {
		args = append(args, c[0], c[1])
	}
	rows, err := q.QueryContext(ctx, st.Rebind("SELECT generator_type, assigned_code FROM asset_generator_keys WHERE "+keyIn([]string{"generator_type", "assigned_code"}, len(codes))), args...)
	if err != nil {
		return nil, fmt.Errorf("reading codes: %w", err)
	}
	defer rows.Close()
	taken := map[[2]string]bool{}
	for rows.Next() {
		var k [2]string
		if err := rows.Scan(&k[0], &k[1]); err != nil {
			return nil, err
		}
		taken[k] = true
	}
	return taken, rows.Err()
}

// fingerprint identifies what a plan does, IDs apart.
func fingerprint(p *ImportPlan) string {
	type item struct {
		Kind, ID, As, Existing string
		Action                 Choice
	}
	var items []item
	for _, it := range p.Items {
		ex := ""
		if it.Existing != nil {
			ex = it.Existing.ID
		}
		items = append(items, item{it.Kind, it.ID, it.As, ex, it.Action})
	}
	anchor := ""
	if p.Anchor != nil {
		anchor = p.Anchor.ID
	}
	b, _ := json.Marshal(struct {
		Kinds    []Kind
		Anchor   string
		Items    []item
		Problems []string
	}{p.Kinds, anchor, items, p.Problems})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// newIDs gives what an import creates new IDs, the same for the same ID
// in one import, so rows that name one another still do.
type newIDs uuid.UUID

func (n newIDs) of(id string) string {
	if id == "" {
		return ""
	}
	return uuid.NewSHA1(uuid.UUID(n), []byte(id)).String()
}
