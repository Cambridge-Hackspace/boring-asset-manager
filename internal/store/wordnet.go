package store

import "fmt"

// Seeding related terms from WordNet (docs/data-model.md#seeding-from-wordnet).

// WordNetSourceSetting is where WordNet is downloaded from, when an admin
// has pointed it somewhere other than Princeton, such as a mirror on the
// instance's own network. Empty is Princeton's.
const WordNetSourceSetting = "terms.wordnet_source"

// WordNetSource is the instance's WordNet download address, or def.
func (s *Store) WordNetSource(def string) (string, error) {
	v, err := s.Setting(WordNetSourceSetting, "")
	if err != nil || v == "" {
		return def, err
	}
	return v, nil
}

// SetWordNetSource sets it; empty goes back to Princeton's.
func (s *Store) SetWordNetSource(url string) error {
	return s.SetSetting(WordNetSourceSetting, url)
}

// CatalogNames is the names of the active assets and of every category
// but Uncategorized: the words WordNet is looked up for.
func (s *Store) CatalogNames() ([]string, error) {
	rows, err := s.Query("SELECT name FROM assets WHERE archived_at IS NULL UNION ALL SELECT name FROM categories WHERE id <> ?", UncategorizedCategoryID)
	if err != nil {
		return nil, fmt.Errorf("reading names: %w", err)
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("reading names: %w", err)
		}
		names = append(names, n)
	}
	return names, rows.Err()
}
