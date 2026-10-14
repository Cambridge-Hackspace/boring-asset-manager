// Package assetcsv reads and writes assets as CSV (README §6.1).
package assetcsv

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// Separators in the categories column: between references, and between the
// names of a path.
const (
	RefSeparator  = ";"
	PathSeparator = " > "
)

// Errors resolving a category reference.
var (
	ErrUnknownCategory   = errors.New("unknown category")
	ErrAmbiguousCategory = errors.New("ambiguous category")
)

// uncategorized is the system category's ID; it is implied by having no
// categories, so it is never written or referenced.
const uncategorized = "uncategorized"

// Categories resolves category references: paths of names from the root,
// or IDs.
type Categories struct {
	byID   map[string]models.Category
	paths  map[string]string   // id -> path
	byPath map[string][]string // path -> ids
}

// NewCategories indexes every category.
func NewCategories(all []models.Category) *Categories {
	c := &Categories{byID: map[string]models.Category{}, paths: map[string]string{}, byPath: map[string][]string{}}
	for _, cat := range all {
		if cat.ID != uncategorized {
			c.byID[cat.ID] = cat
		}
	}
	for id := range c.byID {
		var names []string
		seen := map[string]bool{}
		for cur := id; cur != "" && !seen[cur]; {
			seen[cur] = true
			cat, ok := c.byID[cur]
			if !ok {
				break
			}
			names = append([]string{cat.Name}, names...)
			cur = ""
			if cat.ParentID != nil {
				cur = *cat.ParentID
			}
		}
		path := strings.Join(names, PathSeparator)
		c.paths[id] = path
		c.byPath[path] = append(c.byPath[path], id)
	}
	return c
}

// Path is a category's path of names from the root.
func (c *Categories) Path(id string) string { return c.paths[id] }

// Ref is how a category is written: its path, unless the path would not
// read back as this category (it is ambiguous, or a name contains a
// separator or surrounding spaces), and then its ID.
func (c *Categories) Ref(id string) string {
	path, ok := c.paths[id]
	if !ok {
		return id
	}
	if len(c.byPath[path]) != 1 || strings.Contains(path, RefSeparator) {
		return id
	}
	for _, name := range strings.Split(path, PathSeparator) {
		if name == "" || name != strings.TrimSpace(name) || strings.Contains(name, strings.TrimSpace(PathSeparator)) {
			return id
		}
	}
	// A path spelled like another category's ID would read back as that one.
	if _, clash := c.byID[path]; clash {
		return id
	}
	return path
}

// Refs writes an asset's categories, sorted by path, leaving out the
// system Uncategorized category.
func (c *Categories) Refs(cats []models.Category) string {
	var refs []string
	ids := make([]string, 0, len(cats))
	for _, cat := range cats {
		if cat.ID != uncategorized {
			ids = append(ids, cat.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return c.paths[ids[i]] < c.paths[ids[j]] })
	for _, id := range ids {
		refs = append(refs, c.Ref(id))
	}
	return strings.Join(refs, RefSeparator+" ")
}

// Resolve finds the category a reference names: an ID, or else a path.
func (c *Categories) Resolve(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if _, ok := c.byID[ref]; ok {
		return ref, nil
	}
	parts := strings.Split(ref, strings.TrimSpace(PathSeparator))
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	path := strings.Join(parts, PathSeparator)
	switch ids := c.byPath[path]; len(ids) {
	case 1:
		return ids[0], nil
	case 0:
		return "", fmt.Errorf("%w %q", ErrUnknownCategory, ref)
	default:
		return "", fmt.Errorf("%w %q: %d categories have that path; use an ID", ErrAmbiguousCategory, ref, len(ids))
	}
}

// ResolveAll splits a categories cell and resolves each reference.
func (c *Categories) ResolveAll(cell string) ([]string, []error) {
	var (
		ids  []string
		errs []error
	)
	seen := map[string]bool{}
	for _, ref := range strings.Split(cell, RefSeparator) {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		id, err := c.Resolve(ref)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, errs
}
