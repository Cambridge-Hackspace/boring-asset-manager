package store

import (
	"encoding/json"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

func TestSearchFindsFieldValues(t *testing.T) {
	opts, _ := json.Marshal(models.SelectOptions{Choices: []models.SelectChoice{{Value: "tpu", Label: "Flexible filament"}}})
	a := &models.AssetWithDetails{
		Asset: models.Asset{Name: "Spool"},
		EffectiveFields: []models.EffectiveField{
			{Field: models.Field{Key: "serial", ValidationType: "text"}},
			{Field: models.Field{Key: "material", ValidationType: "select", Options: opts}},
			{Field: models.Field{Key: "keeper", ValidationType: "person"}},
		},
		FieldValues: map[string]string{"serial": "SN-12345", "material": "tpu", "keeper": "u-1,u-2", "retired": "OLD-VALUE"},
		People:      map[string]models.FieldPerson{"u-1": {ID: "u-1", Name: "Ada Lovelace"}},
	}
	for search, want := range map[string]bool{
		"sn-123":    true,  // a text field, ignoring case
		"FLEXIBLE":  true,  // a choice's label
		"tpu":       true,  // and its value
		"lovelace":  true,  // a person the field names and the asset shows
		"u-2":       false, // a person it doesn't show is not searched by ID
		"old-value": false, // a value kept from a field it no longer has
		"spool":     true,
	} {
		if got := MatchesSearch(a, search); got != want {
			t.Errorf("MatchesSearch(%q) = %v, want %v", search, got, want)
		}
	}
}
