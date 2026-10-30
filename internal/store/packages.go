package store

import (
	"context"
	"database/sql"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// What importing a package (#145) asks of the store: whether the icons
// would compile with what it brings, and its assets' history.

// IconFile is an icon source an import brings: a library file, by its
// path, or a category's icon, by its name.
type IconFile struct {
	ID, Name, Source string
}

// CheckIconImport compiles the lexicon with the library files, categories'
// icons and palette tokens given added to what is published, and keeps
// nothing.
func (s *Store) CheckIconImport(ctx context.Context, c IconCompiler, library, icons []IconFile, palette []models.IconColor) (*IconCheck, error) {
	in, err := s.readIconInputs(s.Query)
	if err != nil {
		return nil, err
	}
	in.palette = append(in.palette, palette...)
	for _, f := range library {
		in.library = append(in.library, iconSource{id: f.ID, name: f.Name, source: f.Source})
	}
	for _, f := range icons {
		in.icons = append(in.icons, iconSource{id: f.ID, name: f.Name, source: f.Source})
	}
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
		for _, f := range icons {
			check.Errors = append(check.Errors, iconDefined(res, f.Name)...)
		}
	}
	return check, nil
}

// RecordImportedTx records, in each asset's history, that an import made
// it, by whoever s acts for.
func (s *Store) RecordImportedTx(tx *sql.Tx, assetIDs []string) error {
	for _, id := range assetIDs {
		if err := s.recordAssetEvent(tx, models.EventAssetCreated, id, models.EventPayload{Trigger: models.TriggerImport}); err != nil {
			return err
		}
	}
	return nil
}
