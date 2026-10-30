package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/isolex"
	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Icons as an isolex library folder (#146): what bam writes as the
// folder's lexicon.ilx, and a set of icons from such a folder applied as
// one change.

// lexiconText is the lexicon block bam writes for in, with the palette
// after it as a `palette` block, so the folder describes itself.
func lexiconText(in iconInputs) (string, error) {
	in.wrappers, in.library, in.icons = nil, nil, nil
	files, err := in.files()
	if err != nil {
		return "", err
	}
	text := files[0].Text
	palette := make([]isolex.PaletteColor, len(in.palette))
	for i, c := range in.palette {
		palette[i] = isolex.PaletteColor{Token: c.Token, Colors: isolex.Colors{Light: c.Light, Dark: c.Dark}}
	}
	if block := isolex.PaletteBlock(palette); block != "" {
		if text != "" {
			block = "\n" + block
		}
		text += block
	}
	return text, nil
}

// IconLexiconTx is the lexicon.ilx of the icons as tx reads them: the
// lexicon block and the palette.
func (s *Store) IconLexiconTx(tx *sql.Tx) (string, error) {
	in, err := s.readIconInputs(s.txQuerier(tx))
	if err != nil {
		return "", err
	}
	return lexiconText(in)
}

// IconSet is icons from a library folder, to apply: library files by
// path, categories' icons' sources by their names, and palette
// colors, each added or replacing what is here. What it does not name is
// left as it is.
type IconSet struct {
	Library []IconSourceFile
	// Icons are categories' icons' new sources, by icon name, whatever
	// their renditions.
	Icons   map[string]string
	Palette []models.IconColor
}

// applyTo changes inputs as the set would.
func (set IconSet) applyTo(in *iconInputs, libraryIDs map[string]string) {
	for _, f := range set.Library {
		replaced := false
		for k := range in.library {
			if in.library[k].name == f.Path {
				in.library[k].source, replaced = f.Source, true
			}
		}
		if !replaced {
			in.library = append(in.library, iconSource{id: libraryIDs[f.Path], name: f.Path, source: f.Source})
		}
	}
	for k := range in.icons {
		if src, ok := set.Icons[in.icons[k].name]; ok {
			in.icons[k].source = src
		}
	}
	for _, c := range set.Palette {
		replaced := false
		for k := range in.palette {
			if in.palette[k].Token == c.Token {
				in.palette[k], replaced = c, true
			}
		}
		if !replaced {
			in.palette = append(in.palette, c)
		}
	}
}

// checkSet refuses what a set cannot be: paths, tokens and colors as bam
// takes them, and icons of categories that have none.
func (s *Store) checkSet(set IconSet) error {
	seen := map[string]bool{}
	for _, f := range set.Library {
		if err := ValidIconLibraryPath(f.Path); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		if err := checkIconSource(f.Source); err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		if seen[f.Path] {
			return fmt.Errorf("%s is given twice", f.Path)
		}
		seen[f.Path] = true
	}
	for name, src := range set.Icons {
		if err := checkIconSource(src); err != nil {
			return fmt.Errorf("the icon %s: %w", name, err)
		}
	}
	for _, c := range set.Palette {
		if !iconTokenPattern.MatchString(c.Token) || c.Token == "none" || c.Token == CategoryToken {
			return fmt.Errorf("the palette's %q is not a token bam keeps", c.Token)
		}
		if !iconColorPattern.MatchString(c.Light) || !iconColorPattern.MatchString(c.Dark) {
			return fmt.Errorf("the palette's %q: colors are written #rgb, #rrggbb or #rrggbbaa", c.Token)
		}
	}
	return nil
}

// CheckIconSet compiles the icons as the set would leave them, keeping
// nothing.
func (s *Store) CheckIconSet(ctx context.Context, c IconCompiler, set IconSet) (*IconCheck, error) {
	if err := s.checkSet(set); err != nil {
		return nil, err
	}
	in, err := s.readIconInputs(s.Query)
	if err != nil {
		return nil, err
	}
	set.applyTo(&in, map[string]string{})
	in.wrappers = nil
	in.sort()
	files, err := in.files()
	if err != nil {
		return nil, err
	}
	res, err := compile(ctx, c, files)
	if err != nil {
		return nil, err
	}
	check := &IconCheck{Errors: iconDiags(res.Errors), Warnings: iconDiags(res.Warnings)}
	if res.OK {
		for _, i := range in.icons {
			if _, ok := set.Icons[i.name]; ok {
				check.Errors = append(check.Errors, iconDefined(res, i.name)...)
			}
		}
	}
	return check, nil
}

// ApplyIconSet applies a set as one change, refused whole if the icons
// would not compile after: library files added or replaced, categories'
// icons replaced, and palette colors added or replaced, each with its
// revision.
func (s *Store) ApplyIconSet(ctx context.Context, c IconCompiler, set IconSet) (*models.IconSave, error) {
	if err := s.checkSet(set); err != nil {
		return nil, err
	}
	existing, err := s.ListIconLibrary()
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	created := map[string]bool{}
	for _, f := range existing {
		ids[f.Path] = f.ID
	}
	for _, f := range set.Library {
		if _, ok := ids[f.Path]; !ok {
			ids[f.Path], created[f.Path] = uuid.NewString(), true
		}
	}
	icons, err := s.ListCategoryIcons()
	if err != nil {
		return nil, err
	}
	owners := map[string]models.CategoryIcon{}
	for _, i := range icons {
		owners[i.IconName] = i
	}
	names := make([]string, 0, len(set.Icons))
	for name := range set.Icons {
		if _, ok := owners[name]; !ok {
			return nil, fmt.Errorf("%w: no category has the icon %s to replace", ErrCategoryIconNotFound, name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return s.changeIcons(ctx, c, iconChange{
		refuse: true,
		apply:  func(in *iconInputs) error { set.applyTo(in, ids); return nil },
		check: func(r *isolex.Result) []models.IconDiag {
			var errs []models.IconDiag
			for _, name := range names {
				errs = append(errs, iconDefined(r, name)...)
			}
			return errs
		},
		keep: func(tx *sql.Tx, _ bool, _ []models.IconDiag) error {
			now := dbTime(time.Now())
			for _, f := range set.Library {
				if created[f.Path] {
					_, err := s.TxExec(tx, `INSERT INTO icon_library (id, path, updated_at) VALUES (?, ?, ?)`, ids[f.Path], f.Path, now)
					if IsUniqueViolation(err) {
						return ErrIconPathTaken
					}
					if err != nil {
						return err
					}
				}
				if _, err := s.TxExec(tx, `UPDATE icon_library SET source = ?, draft = NULL, draft_errors = NULL, updated_at = ? WHERE id = ?`, f.Source, now, ids[f.Path]); err != nil {
					return err
				}
				if err := s.addIconRevision(tx, "library_id", ids[f.Path], f.Source, now); err != nil {
					return err
				}
			}
			for _, name := range names {
				o := owners[name]
				if err := s.keepCategoryIcon(tx, o.CategoryID, o.Rendition, name, set.Icons[name], true, nil); err != nil {
					return err
				}
			}
			for _, col := range set.Palette {
				light, dark := strings.ToLower(col.Light), strings.ToLower(col.Dark)
				var here int
				if err := s.TxQueryRow(tx, `SELECT COUNT(*) FROM icon_palette WHERE token = ?`, col.Token).Scan(&here); err != nil {
					return err
				}
				if here > 0 {
					if _, err := s.TxExec(tx, `UPDATE icon_palette SET light = ?, dark = ? WHERE token = ?`, light, dark, col.Token); err != nil {
						return err
					}
					continue
				}
				var last int
				if err := s.TxQueryRow(tx, `SELECT COALESCE(MAX(sort_order), -1) FROM icon_palette`).Scan(&last); err != nil {
					return err
				}
				if _, err := s.TxExec(tx, `INSERT INTO icon_palette (token, light, dark, sort_order) VALUES (?, ?, ?, ?)`, col.Token, light, dark, last+1); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

// IconSettingsBlock is the lexicon block bam writes from its palette's
// tokens and icon settings, without the palette's colors.
func (s *Store) IconSettingsBlock() (string, error) {
	in, err := s.readIconInputs(s.Query)
	if err != nil {
		return "", err
	}
	in.wrappers, in.library, in.icons = nil, nil, nil
	files, err := in.files()
	if err != nil {
		return "", err
	}
	return files[0].Text, nil
}
