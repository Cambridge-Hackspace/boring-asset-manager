package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/textcheck"
	"github.com/google/uuid"
)

// Icons (README §3.7). Every category's published source, every library
// file's, the palette and the settings make one lexicon, compiled as a
// whole. A change is compiled with everything else before it is kept:
// a source that compiles clean is published, and its build becomes the
// one icons are drawn from; one that does not is kept as a draft, and the
// last good build stays. A change with no draft to fall back on, to the
// palette, the settings or what exists, is refused if it would break the
// build.

var (
	ErrCategoryIconNotFound    = errors.New("this category has no icon")
	ErrIconLibraryFileNotFound = errors.New("icon library file not found")
	ErrIconNameTaken           = errors.New("another category's icon already has this name")
	ErrIconPathTaken           = errors.New("another library file already has this path")
	ErrIconRename              = errors.New("an icon's name changes by renaming it, not by saving its source")
	// ErrIconsChanging means other changes kept landing while a change
	// compiled, so it could not be checked against what is there now.
	ErrIconsChanging = errors.New("the icons kept changing while this change was compiled; try again")
)

// IconBuildError refuses a change the lexicon would not compile with.
type IconBuildError struct {
	Errors []models.IconDiag
}

func (e *IconBuildError) Error() string {
	if len(e.Errors) == 0 {
		return "the icons would not compile with this change"
	}
	d := e.Errors[0]
	return fmt.Sprintf("the icons would not compile with this change: %s:%d:%d: %s", d.File, d.Line, d.Col, d.Message)
}

// IconCompiler compiles a lexicon; *isolex.Compiler is one.
type IconCompiler interface {
	Compile(ctx context.Context, files []isolex.File) (*isolex.Result, error)
	Version() (string, error)
}

const (
	// MaxIconSource bounds one source, in characters.
	MaxIconSource = 256 << 10
	// CategoryToken is the token every lexicon declares beside the
	// palette's: what it paints is drawn in the category's own color.
	CategoryToken = "category"

	iconsLock   = "icons"
	iconBuildID = "current"
)

var (
	iconNamePattern  = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]{0,63}$`)
	libraryPathPart  = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9_.-]*$`)
	iconTokenPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
	iconColorPattern = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
)

// ValidIconName checks a category icon's name: letters, digits, '-', '_'
// and '.', not starting with '.', at most 64; names starting "bam-" are
// bam's own.
func ValidIconName(name string) error {
	if !iconNamePattern.MatchString(name) {
		return errors.New("an icon's name is 1 to 64 letters, digits, '-', '_' and '.', not starting with '.'")
	}
	if strings.HasPrefix(name, "bam-") {
		return errors.New(`icon names starting "bam-" are kept for bam's own`)
	}
	return nil
}

// ValidIconLibraryPath checks a library file's path: segments of letters,
// digits, '-', '_' and '.' separated by '/', none starting with '.',
// ending ".ilx".
func ValidIconLibraryPath(path string) error {
	bad := errors.New(`a library file's path is names of letters, digits, '-', '_' and '.' separated by '/', ending ".ilx"`)
	if len(path) > 255 || !strings.HasSuffix(path, ".ilx") || len(path) == len(".ilx") {
		return bad
	}
	for _, part := range strings.Split(path, "/") {
		if !libraryPathPart.MatchString(part) {
			return bad
		}
	}
	return nil
}

func checkIconSource(src string) error {
	return textcheck.Check("icon source", src, MaxIconSource)
}

// categoryIconFile and libraryFile name sources in a build, and so in
// its diagnostics.
func categoryIconFile(name string) string { return "categories/" + name + ".ilx" }
func libraryFile(path string) string      { return "library/" + path }

type iconSource struct {
	id, name, source string
}

// iconInputs is everything a build compiles.
type iconInputs struct {
	palette  []models.IconColor
	settings models.IconSettings
	library  []iconSource // by path
	icons    []iconSource // by icon name
}

func (in iconInputs) clone() iconInputs {
	out := in
	out.palette = append([]models.IconColor(nil), in.palette...)
	out.library = append([]iconSource(nil), in.library...)
	out.icons = append([]iconSource(nil), in.icons...)
	return out
}

func (in *iconInputs) sort() {
	sort.Slice(in.library, func(i, j int) bool { return in.library[i].name < in.library[j].name })
	sort.Slice(in.icons, func(i, j int) bool { return in.icons[i].name < in.icons[j].name })
}

// files are the build's sources: the lexicon block, then the library by
// path, then the categories' icons by name.
func (in iconInputs) files() ([]isolex.File, error) {
	tokens := make([]string, 0, len(in.palette)+1)
	for _, c := range in.palette {
		tokens = append(tokens, c.Token)
	}
	tokens = append(tokens, CategoryToken)
	block, err := isolex.LexiconBlock(isolex.Vocabulary{
		Tokens:       tokens,
		StrokeWidths: in.settings.StrokeWidths,
		Reserved:     in.settings.Reserved,
		Limits:       in.settings.Limits,
	})
	if err != nil {
		return nil, err
	}
	files := []isolex.File{{Name: isolex.LexiconFileName, Text: block}}
	for _, l := range in.library {
		files = append(files, isolex.File{Name: libraryFile(l.name), Text: l.source})
	}
	for _, i := range in.icons {
		files = append(files, isolex.File{Name: categoryIconFile(i.name), Text: i.source})
	}
	return files, nil
}

// hash identifies a build's inputs and the compiler that builds them.
func (in iconInputs) hash(version string) (string, error) {
	files, err := in.files()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(struct {
		Isolex string        `json:"isolex"`
		Files  []isolex.File `json:"files"`
	}{version, files})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type querier func(query string, args ...any) (*sql.Rows, error)

func (s *Store) txQuerier(tx *sql.Tx) querier {
	return func(query string, args ...any) (*sql.Rows, error) { return s.TxQuery(tx, query, args...) }
}

// readIconInputs reads what a build compiles: published sources only.
func (s *Store) readIconInputs(q querier) (iconInputs, error) {
	var in iconInputs
	var err error
	if in.palette, err = readIconPalette(q); err != nil {
		return in, err
	}
	if in.settings, err = readIconSettings(q); err != nil {
		return in, err
	}
	read := func(query string) ([]iconSource, error) {
		rows, err := q(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []iconSource
		for rows.Next() {
			var src iconSource
			if err := rows.Scan(&src.id, &src.name, &src.source); err != nil {
				return nil, err
			}
			out = append(out, src)
		}
		return out, rows.Err()
	}
	if in.library, err = read(`SELECT id, path, source FROM icon_library WHERE source IS NOT NULL`); err != nil {
		return in, fmt.Errorf("reading the icon library: %w", err)
	}
	if in.icons, err = read(`SELECT category_id, icon_name, source FROM category_icons WHERE source IS NOT NULL`); err != nil {
		return in, fmt.Errorf("reading category icons: %w", err)
	}
	in.sort()
	return in, nil
}

func readIconPalette(q querier) ([]models.IconColor, error) {
	rows, err := q(`SELECT token, light, dark FROM icon_palette ORDER BY sort_order, token`)
	if err != nil {
		return nil, fmt.Errorf("reading the icon palette: %w", err)
	}
	defer rows.Close()
	palette := []models.IconColor{}
	for rows.Next() {
		var c models.IconColor
		if err := rows.Scan(&c.Token, &c.Light, &c.Dark); err != nil {
			return nil, err
		}
		palette = append(palette, c)
	}
	return palette, rows.Err()
}

func readIconSettings(q querier) (models.IconSettings, error) {
	settings := models.IconSettings{StrokeWidths: []float64{}, Reserved: []string{}, Limits: map[string]int64{}}
	rows, err := q(`SELECT stroke_widths, reserved, limits FROM icon_settings WHERE id = 'icons'`)
	if err != nil {
		return settings, fmt.Errorf("reading icon settings: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return settings, rows.Err()
	}
	var widths, reserved, limits string
	if err := rows.Scan(&widths, &reserved, &limits); err != nil {
		return settings, err
	}
	for _, f := range []struct {
		text string
		into any
	}{{widths, &settings.StrokeWidths}, {reserved, &settings.Reserved}, {limits, &settings.Limits}} {
		if err := json.Unmarshal([]byte(f.text), f.into); err != nil {
			return settings, fmt.Errorf("reading icon settings: %w", err)
		}
	}
	return settings, rows.Err()
}

func iconDiags(ds []isolex.Diag) []models.IconDiag {
	out := make([]models.IconDiag, len(ds))
	for i, d := range ds {
		out[i] = models.IconDiag(d)
	}
	return out
}

// iconChange is one change to the icons: how it edits the inputs, what
// else a clean build must hold, and how it is kept.
type iconChange struct {
	apply func(in *iconInputs) error
	// check, given a clean build, returns any further errors.
	check func(r *isolex.Result) []models.IconDiag
	// keep writes the change in tx, published or not.
	keep func(tx *sql.Tx, published bool, errs []models.IconDiag) error
	// refuse: a change that would break the build is refused, not kept.
	refuse bool
}

// changeIcons compiles the lexicon as ch would leave it and keeps ch.
// The compile runs outside any transaction, so as not to hold the
// database for it; the change is then kept under the icons lock only if
// nothing it was compiled against has changed meanwhile, and compiled
// again otherwise.
func (s *Store) changeIcons(ctx context.Context, c IconCompiler, ch iconChange) (*models.IconSave, error) {
	version, err := c.Version()
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 3; attempt++ {
		before, err := s.readIconInputs(s.Query)
		if err != nil {
			return nil, err
		}
		was, err := before.hash(version)
		if err != nil {
			return nil, err
		}
		after := before.clone()
		if ch.apply != nil {
			if err := ch.apply(&after); err != nil {
				return nil, err
			}
			after.sort()
		}
		files, err := after.files()
		if err != nil {
			return nil, err
		}
		res, err := c.Compile(ctx, files)
		if err != nil {
			return nil, err
		}
		errs := iconDiags(res.Errors)
		if res.OK && ch.check != nil {
			errs = append(errs, ch.check(res)...)
		}
		published := len(errs) == 0
		if !published && ch.refuse {
			return nil, &IconBuildError{Errors: errs}
		}

		save, retry, err := s.keepIconChange(ch, was, version, after, res, published, errs)
		if err != nil {
			return nil, err
		}
		if !retry {
			return save, nil
		}
	}
	return nil, ErrIconsChanging
}

func (s *Store) keepIconChange(ch iconChange, was, version string, after iconInputs, res *isolex.Result, published bool, errs []models.IconDiag) (*models.IconSave, bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, false, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()
	if err := s.takeLock(tx, iconsLock); err != nil {
		return nil, false, err
	}
	now, err := s.readIconInputs(s.txQuerier(tx))
	if err != nil {
		return nil, false, err
	}
	if h, err := now.hash(version); err != nil || h != was {
		return nil, err == nil, err
	}
	if err := ch.keep(tx, published, errs); err != nil {
		return nil, false, err
	}
	save := &models.IconSave{Published: published, Errors: errs, Warnings: iconDiags(res.Warnings)}
	if published {
		hash, err := after.hash(version)
		if err != nil {
			return nil, false, err
		}
		if save.Build, err = s.writeIconBuild(tx, hash, version, res); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, fmt.Errorf("committing icons: %w", err)
	}
	return save, false, nil
}

func (s *Store) writeIconBuild(tx *sql.Tx, hash, version string, res *isolex.Result) (*models.IconBuild, error) {
	warnings, err := json.Marshal(iconDiags(res.Warnings))
	if err != nil {
		return nil, err
	}
	b := &models.IconBuild{Hash: hash, IsolexVersion: version, IR: res.IR, Warnings: iconDiags(res.Warnings), BuiltAt: dbTime(time.Now())}
	if _, err := s.TxExec(tx, `DELETE FROM icon_build WHERE id = ?`, iconBuildID); err != nil {
		return nil, err
	}
	_, err = s.TxExec(tx, `INSERT INTO icon_build (id, source_hash, isolex_version, ir, warnings, built_at) VALUES (?, ?, ?, ?, ?, ?)`,
		iconBuildID, b.Hash, b.IsolexVersion, string(b.IR), string(warnings), b.BuiltAt)
	if err != nil {
		return nil, fmt.Errorf("saving the icon build: %w", err)
	}
	return b, nil
}

// CurrentIconBuild returns the last build that compiled clean, or nil if
// there has been none.
func (s *Store) CurrentIconBuild() (*models.IconBuild, error) {
	var b models.IconBuild
	var ir, warnings string
	err := s.QueryRow(`SELECT source_hash, isolex_version, ir, warnings, built_at FROM icon_build WHERE id = ?`, iconBuildID).
		Scan(&b.Hash, &b.IsolexVersion, &ir, &warnings, &b.BuiltAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the icon build: %w", err)
	}
	b.IR = json.RawMessage(ir)
	if err := json.Unmarshal([]byte(warnings), &b.Warnings); err != nil {
		return nil, err
	}
	return &b, nil
}

// RebuildIcons compiles what is published if the current build is not
// of it: none yet, or one by another isolex. A lexicon that no longer
// compiles keeps its last good build, and the errors are returned.
func (s *Store) RebuildIcons(ctx context.Context, c IconCompiler) (*models.IconSave, error) {
	version, err := c.Version()
	if err != nil {
		return nil, err
	}
	in, err := s.readIconInputs(s.Query)
	if err != nil {
		return nil, err
	}
	hash, err := in.hash(version)
	if err != nil {
		return nil, err
	}
	if b, err := s.CurrentIconBuild(); err != nil {
		return nil, err
	} else if b != nil && b.Hash == hash {
		return &models.IconSave{Published: true, Errors: []models.IconDiag{}, Warnings: b.Warnings, Build: b}, nil
	}
	return s.changeIcons(ctx, c, iconChange{keep: func(*sql.Tx, bool, []models.IconDiag) error { return nil }})
}

// IconCheck is a compile that keeps nothing: its errors and warnings, and
// the IR when it is clean.
type IconCheck struct {
	Errors   []models.IconDiag `json:"errors"`
	Warnings []models.IconDiag `json:"warnings"`
	IR       json.RawMessage   `json:"ir,omitempty"`
}

// CheckCategoryIcon compiles the lexicon with source as categoryID's
// icon, named name, and keeps nothing: what an editor checks as it goes.
func (s *Store) CheckCategoryIcon(ctx context.Context, c IconCompiler, categoryID, name, source string) (*IconCheck, error) {
	if err := ValidIconName(name); err != nil {
		return nil, err
	}
	if err := checkIconSource(source); err != nil {
		return nil, err
	}
	in, err := s.readIconInputs(s.Query)
	if err != nil {
		return nil, err
	}
	setCategoryIcon(&in, categoryID, name, source)
	in.sort()
	files, err := in.files()
	if err != nil {
		return nil, err
	}
	res, err := c.Compile(ctx, files)
	if err != nil {
		return nil, err
	}
	check := &IconCheck{Errors: iconDiags(res.Errors), Warnings: iconDiags(res.Warnings)}
	if res.OK {
		check.Errors = append(check.Errors, iconDefined(res, name)...)
		if len(check.Errors) == 0 {
			check.IR = res.IR
		}
	}
	return check, nil
}

func setCategoryIcon(in *iconInputs, categoryID, name, source string) {
	for i := range in.icons {
		if in.icons[i].id == categoryID {
			in.icons[i].name, in.icons[i].source = name, source
			return
		}
	}
	in.icons = append(in.icons, iconSource{id: categoryID, name: name, source: source})
}

// iconDefined requires the build to emit the category's icon by its name.
func iconDefined(res *isolex.Result, name string) []models.IconDiag {
	if res.Doc != nil && res.Doc.Icon(name) != nil {
		return nil
	}
	return []models.IconDiag{{File: categoryIconFile(name), Line: 1, Col: 1,
		Message: fmt.Sprintf("this category's icon is named %q, but nothing here draws an icon by that name; add `icon %q { … }`", name, name)}}
}

// GetCategoryIcon returns a category's icon source.
func (s *Store) GetCategoryIcon(categoryID string) (*models.CategoryIcon, error) {
	rows, err := s.Query(`SELECT category_id, icon_name, source, draft, draft_errors, updated_at FROM category_icons WHERE category_id = ?`, categoryID)
	if err != nil {
		return nil, err
	}
	list, err := scanCategoryIcons(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrCategoryIconNotFound
	}
	return &list[0], nil
}

// ListCategoryIcons returns every category's icon source, by icon name.
func (s *Store) ListCategoryIcons() ([]models.CategoryIcon, error) {
	rows, err := s.Query(`SELECT category_id, icon_name, source, draft, draft_errors, updated_at FROM category_icons ORDER BY icon_name`)
	if err != nil {
		return nil, err
	}
	return scanCategoryIcons(rows)
}

func scanCategoryIcons(rows *sql.Rows) ([]models.CategoryIcon, error) {
	defer rows.Close()
	list := []models.CategoryIcon{}
	for rows.Next() {
		var ic models.CategoryIcon
		var source, draft, errs sql.NullString
		if err := rows.Scan(&ic.CategoryID, &ic.IconName, &source, &draft, &errs, &ic.UpdatedAt); err != nil {
			return nil, err
		}
		if err := fillSources(&ic.Source, &ic.Draft, &ic.DraftErrors, source, draft, errs); err != nil {
			return nil, err
		}
		list = append(list, ic)
	}
	return list, rows.Err()
}

func fillSources(source, draft **string, errs *[]models.IconDiag, s, d, e sql.NullString) error {
	if s.Valid {
		*source = &s.String
	}
	if d.Valid {
		*draft = &d.String
	}
	*errs = []models.IconDiag{}
	if e.Valid && e.String != "" {
		return json.Unmarshal([]byte(e.String), errs)
	}
	return nil
}

// SaveCategoryIcon saves source as categoryID's icon, named name: the
// first save names it, and later ones must give the same name (see
// RenameCategoryIcon). Published if the lexicon compiles clean with it
// and draws an icon by that name; kept as a draft otherwise.
func (s *Store) SaveCategoryIcon(ctx context.Context, c IconCompiler, categoryID, name, source string) (*models.IconSave, error) {
	if err := ValidIconName(name); err != nil {
		return nil, err
	}
	if err := checkIconSource(source); err != nil {
		return nil, err
	}
	if _, err := s.GetCategoryByID(categoryID); err != nil {
		return nil, err
	}
	existing, err := s.GetCategoryIcon(categoryID)
	switch {
	case errors.Is(err, ErrCategoryIconNotFound):
		existing = nil
	case err != nil:
		return nil, err
	case existing.IconName != name:
		return nil, ErrIconRename
	}
	if existing == nil {
		if taken, err := s.iconNameTaken(name); err != nil {
			return nil, err
		} else if taken {
			return nil, ErrIconNameTaken
		}
	}
	return s.changeIcons(ctx, c, iconChange{
		apply: func(in *iconInputs) error { setCategoryIcon(in, categoryID, name, source); return nil },
		check: func(r *isolex.Result) []models.IconDiag { return iconDefined(r, name) },
		keep: func(tx *sql.Tx, published bool, errs []models.IconDiag) error {
			return s.keepCategoryIcon(tx, categoryID, name, source, published, errs)
		},
	})
}

func (s *Store) iconNameTaken(name string) (bool, error) {
	var n int
	err := s.QueryRow(`SELECT COUNT(*) FROM category_icons WHERE icon_name = ?`, name).Scan(&n)
	return n > 0, err
}

func (s *Store) keepCategoryIcon(tx *sql.Tx, categoryID, name, source string, published bool, errs []models.IconDiag) error {
	now := dbTime(time.Now())
	var exists int
	if err := s.TxQueryRow(tx, `SELECT COUNT(*) FROM category_icons WHERE category_id = ?`, categoryID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		_, err := s.TxExec(tx, `INSERT INTO category_icons (category_id, icon_name, updated_at) VALUES (?, ?, ?)`, categoryID, name, now)
		if IsUniqueViolation(err) {
			return ErrIconNameTaken
		}
		if err != nil {
			return err
		}
	}
	if !published {
		data, err := json.Marshal(errs)
		if err != nil {
			return err
		}
		_, err = s.TxExec(tx, `UPDATE category_icons SET draft = ?, draft_errors = ?, updated_at = ? WHERE category_id = ?`, source, string(data), now, categoryID)
		return err
	}
	if _, err := s.TxExec(tx, `UPDATE category_icons SET source = ?, draft = NULL, draft_errors = NULL, updated_at = ? WHERE category_id = ?`, source, now, categoryID); err != nil {
		return err
	}
	if err := s.addIconRevision(tx, "category_id", categoryID, source, now); err != nil {
		return err
	}
	return s.recordEvent(tx, models.EventCategoryIconChanged, categoryID, models.EventPayload{
		Changes: []models.EventChange{{What: "icon", To: name}},
	})
}

func (s *Store) addIconRevision(tx *sql.Tx, column, id, source string, at time.Time) error {
	var actor *string
	if s.actor != "" {
		actor = &s.actor
	}
	_, err := s.TxExec(tx, `INSERT INTO icon_revisions (id, `+column+`, source, actor_user_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		uuid.NewString(), id, source, actor, at)
	return err
}

// RenameCategoryIcon renames a category's published icon, which is
// refused if anything else names it by its old name; the source must
// already draw it under the new one.
func (s *Store) RenameCategoryIcon(ctx context.Context, c IconCompiler, categoryID, name string) (*models.IconSave, error) {
	if err := ValidIconName(name); err != nil {
		return nil, err
	}
	existing, err := s.GetCategoryIcon(categoryID)
	if err != nil {
		return nil, err
	}
	if existing.IconName == name {
		return s.RebuildIcons(ctx, c)
	}
	if taken, err := s.iconNameTaken(name); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrIconNameTaken
	}
	return s.changeIcons(ctx, c, iconChange{
		refuse: true,
		apply: func(in *iconInputs) error {
			for i := range in.icons {
				if in.icons[i].id == categoryID {
					in.icons[i].name = name
				}
			}
			return nil
		},
		check: func(r *isolex.Result) []models.IconDiag {
			if existing.Source == nil {
				return nil
			}
			return iconDefined(r, name)
		},
		keep: func(tx *sql.Tx, _ bool, _ []models.IconDiag) error {
			_, err := s.TxExec(tx, `UPDATE category_icons SET icon_name = ?, updated_at = ? WHERE category_id = ?`, name, dbTime(time.Now()), categoryID)
			if IsUniqueViolation(err) {
				return ErrIconNameTaken
			}
			return err
		},
	})
}

// DeleteCategoryIcon removes a category's icon and its history, which is
// refused if anything else draws with it.
func (s *Store) DeleteCategoryIcon(ctx context.Context, c IconCompiler, categoryID string) (*models.IconSave, error) {
	if _, err := s.GetCategoryIcon(categoryID); err != nil {
		return nil, err
	}
	return s.changeIcons(ctx, c, iconChange{
		refuse: true,
		apply: func(in *iconInputs) error {
			kept := in.icons[:0]
			for _, i := range in.icons {
				if i.id != categoryID {
					kept = append(kept, i)
				}
			}
			in.icons = kept
			return nil
		},
		keep: func(tx *sql.Tx, _ bool, _ []models.IconDiag) error {
			_, err := s.TxExec(tx, `DELETE FROM category_icons WHERE category_id = ?`, categoryID)
			return err
		},
	})
}

// DiscardCategoryIconDraft drops a category's draft; an icon never
// published goes with it.
func (s *Store) DiscardCategoryIconDraft(categoryID string) error {
	ic, err := s.GetCategoryIcon(categoryID)
	if err != nil {
		return err
	}
	if ic.Source == nil {
		_, err = s.Exec(`DELETE FROM category_icons WHERE category_id = ?`, categoryID)
	} else {
		_, err = s.Exec(`UPDATE category_icons SET draft = NULL, draft_errors = NULL, updated_at = ? WHERE category_id = ?`, dbTime(time.Now()), categoryID)
	}
	return err
}

// ListCategoryIconRevisions returns a category icon's published versions,
// newest first.
func (s *Store) ListCategoryIconRevisions(categoryID string) ([]models.IconRevision, error) {
	if _, err := s.GetCategoryIcon(categoryID); err != nil {
		return nil, err
	}
	return s.iconRevisions("category_id", categoryID)
}

func (s *Store) iconRevisions(column, id string) ([]models.IconRevision, error) {
	rows, err := s.Query(`SELECT id, source, actor_user_id, created_at FROM icon_revisions WHERE `+column+` = ? ORDER BY created_at DESC, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []models.IconRevision{}
	for rows.Next() {
		var r models.IconRevision
		var actor sql.NullString
		if err := rows.Scan(&r.ID, &r.Source, &actor, &r.CreatedAt); err != nil {
			return nil, err
		}
		if actor.Valid {
			r.ActorID = &actor.String
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// ListIconLibrary returns the library's files, by path.
func (s *Store) ListIconLibrary() ([]models.IconLibraryFile, error) {
	rows, err := s.Query(`SELECT id, path, source, draft, draft_errors, updated_at FROM icon_library ORDER BY path`)
	if err != nil {
		return nil, err
	}
	return scanLibraryFiles(rows)
}

// GetIconLibraryFile returns one library file.
func (s *Store) GetIconLibraryFile(id string) (*models.IconLibraryFile, error) {
	rows, err := s.Query(`SELECT id, path, source, draft, draft_errors, updated_at FROM icon_library WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	list, err := scanLibraryFiles(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrIconLibraryFileNotFound
	}
	return &list[0], nil
}

func scanLibraryFiles(rows *sql.Rows) ([]models.IconLibraryFile, error) {
	defer rows.Close()
	list := []models.IconLibraryFile{}
	for rows.Next() {
		var f models.IconLibraryFile
		var source, draft, errs sql.NullString
		if err := rows.Scan(&f.ID, &f.Path, &source, &draft, &errs, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if err := fillSources(&f.Source, &f.Draft, &f.DraftErrors, source, draft, errs); err != nil {
			return nil, err
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

// CreateIconLibraryFile adds a library file at path, published or kept
// as a draft as SaveCategoryIcon does.
func (s *Store) CreateIconLibraryFile(ctx context.Context, c IconCompiler, path, source string) (*models.IconLibraryFile, *models.IconSave, error) {
	if err := ValidIconLibraryPath(path); err != nil {
		return nil, nil, err
	}
	if err := checkIconSource(source); err != nil {
		return nil, nil, err
	}
	var n int
	if err := s.QueryRow(`SELECT COUNT(*) FROM icon_library WHERE path = ?`, path).Scan(&n); err != nil {
		return nil, nil, err
	} else if n > 0 {
		return nil, nil, ErrIconPathTaken
	}
	id := uuid.NewString()
	save, err := s.saveLibraryFile(ctx, c, id, path, source, true)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.GetIconLibraryFile(id)
	return f, save, err
}

// SaveIconLibraryFile saves a library file's source, published or kept
// as a draft.
func (s *Store) SaveIconLibraryFile(ctx context.Context, c IconCompiler, id, source string) (*models.IconSave, error) {
	if err := checkIconSource(source); err != nil {
		return nil, err
	}
	f, err := s.GetIconLibraryFile(id)
	if err != nil {
		return nil, err
	}
	return s.saveLibraryFile(ctx, c, id, f.Path, source, false)
}

func (s *Store) saveLibraryFile(ctx context.Context, c IconCompiler, id, path, source string, create bool) (*models.IconSave, error) {
	return s.changeIcons(ctx, c, iconChange{
		apply: func(in *iconInputs) error {
			for i := range in.library {
				if in.library[i].id == id {
					in.library[i].source = source
					return nil
				}
			}
			in.library = append(in.library, iconSource{id: id, name: path, source: source})
			return nil
		},
		keep: func(tx *sql.Tx, published bool, errs []models.IconDiag) error {
			now := dbTime(time.Now())
			if create {
				_, err := s.TxExec(tx, `INSERT INTO icon_library (id, path, updated_at) VALUES (?, ?, ?)`, id, path, now)
				if IsUniqueViolation(err) {
					return ErrIconPathTaken
				}
				if err != nil {
					return err
				}
			}
			if !published {
				data, err := json.Marshal(errs)
				if err != nil {
					return err
				}
				_, err = s.TxExec(tx, `UPDATE icon_library SET draft = ?, draft_errors = ?, updated_at = ? WHERE id = ?`, source, string(data), now, id)
				return err
			}
			if _, err := s.TxExec(tx, `UPDATE icon_library SET source = ?, draft = NULL, draft_errors = NULL, updated_at = ? WHERE id = ?`, source, now, id); err != nil {
				return err
			}
			return s.addIconRevision(tx, "library_id", id, source, now)
		},
	})
}

// RenameIconLibraryFile moves a library file to another path, which only
// changes how diagnostics name it.
func (s *Store) RenameIconLibraryFile(ctx context.Context, c IconCompiler, id, path string) (*models.IconSave, error) {
	if err := ValidIconLibraryPath(path); err != nil {
		return nil, err
	}
	if _, err := s.GetIconLibraryFile(id); err != nil {
		return nil, err
	}
	return s.changeIcons(ctx, c, iconChange{
		refuse: true,
		apply: func(in *iconInputs) error {
			for i := range in.library {
				if in.library[i].id == id {
					in.library[i].name = path
				}
			}
			return nil
		},
		keep: func(tx *sql.Tx, _ bool, _ []models.IconDiag) error {
			_, err := s.TxExec(tx, `UPDATE icon_library SET path = ?, updated_at = ? WHERE id = ?`, path, dbTime(time.Now()), id)
			if IsUniqueViolation(err) {
				return ErrIconPathTaken
			}
			return err
		},
	})
}

// DeleteIconLibraryFile removes a library file and its history, which is
// refused if any icon still uses what it defines.
func (s *Store) DeleteIconLibraryFile(ctx context.Context, c IconCompiler, id string) (*models.IconSave, error) {
	if _, err := s.GetIconLibraryFile(id); err != nil {
		return nil, err
	}
	return s.changeIcons(ctx, c, iconChange{
		refuse: true,
		apply: func(in *iconInputs) error {
			kept := in.library[:0]
			for _, l := range in.library {
				if l.id != id {
					kept = append(kept, l)
				}
			}
			in.library = kept
			return nil
		},
		keep: func(tx *sql.Tx, _ bool, _ []models.IconDiag) error {
			_, err := s.TxExec(tx, `DELETE FROM icon_library WHERE id = ?`, id)
			return err
		},
	})
}

// DiscardIconLibraryDraft drops a library file's draft; a file never
// published goes with it.
func (s *Store) DiscardIconLibraryDraft(id string) error {
	f, err := s.GetIconLibraryFile(id)
	if err != nil {
		return err
	}
	if f.Source == nil {
		_, err = s.Exec(`DELETE FROM icon_library WHERE id = ?`, id)
	} else {
		_, err = s.Exec(`UPDATE icon_library SET draft = NULL, draft_errors = NULL, updated_at = ? WHERE id = ?`, dbTime(time.Now()), id)
	}
	return err
}

// ListIconLibraryRevisions returns a library file's published versions,
// newest first.
func (s *Store) ListIconLibraryRevisions(id string) ([]models.IconRevision, error) {
	if _, err := s.GetIconLibraryFile(id); err != nil {
		return nil, err
	}
	return s.iconRevisions("library_id", id)
}

// GetIconPalette returns the palette, in its order.
func (s *Store) GetIconPalette() ([]models.IconColor, error) {
	return readIconPalette(s.Query)
}

// SetIconPalette replaces the palette, which is refused if an icon
// paints with a token it would no longer have.
func (s *Store) SetIconPalette(ctx context.Context, c IconCompiler, palette []models.IconColor) (*models.IconSave, error) {
	seen := map[string]bool{}
	for i, col := range palette {
		switch {
		case !iconTokenPattern.MatchString(col.Token):
			return nil, fmt.Errorf("palette[%d]: a token is 1 to 64 lowercase letters, digits and '-', starting with a letter", i)
		case col.Token == "none" || col.Token == CategoryToken:
			return nil, fmt.Errorf("palette[%d]: %q is not a color but means something of its own", i, col.Token)
		case seen[col.Token]:
			return nil, fmt.Errorf("palette[%d]: %q is in the palette twice", i, col.Token)
		case !iconColorPattern.MatchString(col.Light) || !iconColorPattern.MatchString(col.Dark):
			return nil, fmt.Errorf("palette[%d]: colors are written #rgb, #rrggbb or #rrggbbaa", i)
		}
		seen[col.Token] = true
	}
	return s.changeIcons(ctx, c, iconChange{
		refuse: true,
		apply:  func(in *iconInputs) error { in.palette = palette; return nil },
		keep: func(tx *sql.Tx, _ bool, _ []models.IconDiag) error {
			if _, err := s.TxExec(tx, `DELETE FROM icon_palette`); err != nil {
				return err
			}
			for i, col := range palette {
				if _, err := s.TxExec(tx, `INSERT INTO icon_palette (token, light, dark, sort_order) VALUES (?, ?, ?, ?)`,
					col.Token, strings.ToLower(col.Light), strings.ToLower(col.Dark), i); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// GetIconSettings returns the lexicon settings.
func (s *Store) GetIconSettings() (models.IconSettings, error) {
	return readIconSettings(s.Query)
}

// SetIconSettings replaces the lexicon settings, which is refused if the
// icons would not compile under them.
func (s *Store) SetIconSettings(ctx context.Context, c IconCompiler, settings models.IconSettings) (*models.IconSave, error) {
	if settings.StrokeWidths == nil {
		settings.StrokeWidths = []float64{}
	}
	if settings.Reserved == nil {
		settings.Reserved = []string{}
	}
	if settings.Limits == nil {
		settings.Limits = map[string]int64{}
	}
	if _, err := isolex.LexiconBlock(isolex.Vocabulary{StrokeWidths: settings.StrokeWidths, Reserved: settings.Reserved, Limits: settings.Limits}); err != nil {
		return nil, err
	}
	widths, _ := json.Marshal(settings.StrokeWidths)
	reserved, _ := json.Marshal(settings.Reserved)
	limits, _ := json.Marshal(settings.Limits)
	return s.changeIcons(ctx, c, iconChange{
		refuse: true,
		apply:  func(in *iconInputs) error { in.settings = settings; return nil },
		keep: func(tx *sql.Tx, _ bool, _ []models.IconDiag) error {
			_, err := s.TxExec(tx, `UPDATE icon_settings SET stroke_widths = ?, reserved = ?, limits = ? WHERE id = 'icons'`,
				string(widths), string(reserved), string(limits))
			return err
		},
	})
}
