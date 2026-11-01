package assetcsv

import (
	"sort"
	"strings"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// AssetRefs names assets in a links cell (docs/csv.md): by ID, by name when
// no other active asset has it, or by one of its codes.
type AssetRefs struct {
	names  map[string]string   // ID → name
	byName map[string][]string // name → IDs
	byCode map[string][]string // code → IDs, of any generator type
}

// NewAssetRefs indexes active assets.
func NewAssetRefs(active []models.AssetWithDetails) *AssetRefs {
	r := &AssetRefs{names: map[string]string{}, byName: map[string][]string{}, byCode: map[string][]string{}}
	for _, a := range active {
		codes := make([]string, 0, len(a.GeneratorKeys))
		for _, gk := range a.GeneratorKeys {
			codes = append(codes, gk.AssignedCode)
		}
		r.add(a.Asset.ID, a.Asset.Name, codes)
	}
	return r
}

func (r *AssetRefs) add(id, name string, codes []string) {
	r.names[id] = name
	r.byName[name] = append(r.byName[name], id)
	for _, c := range codes {
		r.byCode[c] = append(r.byCode[c], id)
	}
}

// Resolve gives the distinct assets a reference could mean.
func (r *AssetRefs) Resolve(ref string) []string {
	seen := map[string]bool{}
	var ids []string
	note := func(id string) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if _, ok := r.names[ref]; ok {
		note(ref)
	}
	for _, id := range r.byName[ref] {
		note(id)
	}
	for _, id := range r.byCode[ref] {
		note(id)
	}
	sort.Strings(ids)
	return ids
}

// Ref is how an export names an asset: by name when that names it alone and
// can be written in a cell, otherwise by ID.
func (r *AssetRefs) Ref(id, name string) string {
	if name != "" && name == strings.TrimSpace(name) && !strings.Contains(name, LinkSeparator) {
		if ids := r.Resolve(name); len(ids) == 1 && ids[0] == id {
			return name
		}
	}
	return id
}
