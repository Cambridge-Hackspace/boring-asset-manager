package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Cambridge-Hackspace/boring-asset-manager/internal/models"
)

// A drill, drawn down, lent and running low through the state axis; it has
// no variant for maintenance.
const (
	drillStates = `axis "state" {
  variant "down" { part "flag" { tile 0 0 1 0.3 0.3 } }
  variant "lent" { part "flag" { tile 0.5 0 1 0.3 0.3 } }
  variant "low" { part "flag" { tile 0 0.5 1 0.3 0.3 } }
}`
	drillSrc = `icon "drill" { category "tools"; footprint 1 1 1; axes "state"; part "box" { box 0 0 0 1 1 1 } }`
)

// An asset's icon shows the worst of its states that its icon has a
// variant for, and its plain icon when it has none.
func TestAnAssetsIconShowsItsState(t *testing.T) {
	st := setupTestStore(t)
	c := compilerFor(t)
	if _, save, err := st.CreateIconLibraryFile(context.Background(), c, "state.ilx", drillStates); err != nil || !save.Published {
		t.Fatalf("%+v %v", save, err)
	}
	tools, drills, lender, bo := lending(t, st)
	if s := saveIcon(t, st, c, tools.ID, "drill", drillSrc); !s.Published {
		t.Fatalf("%+v", s.Errors)
	}
	drill := asset(t, st, "Drill", drills.ID)
	shown := func(want, state string) {
		t.Helper()
		ic := resolved(t, st, drill)
		if ic == nil || ic.Name != want || ic.State != state || ic.Plain() != "drill" {
			t.Fatalf("got %+v, want %s for %q", ic, want, state)
		}
		if state != "" && (ic.Base != "drill" || ic.Variant != want[len("drill-"):]) {
			t.Errorf("base and variant: %+v", ic)
		}
	}
	shown("drill", "")

	// Needing maintenance has no variant here, so it is drawn plain;
	// lent, it shows that.
	if _, _, err := st.SetAssetCondition(drill, models.ConditionNeedsMaintenance, "blunt"); err != nil {
		t.Fatal(err)
	}
	shown("drill", "")
	if _, err := st.As(lender.ID).CheckOut(drill, bo.ID, nil); err != nil {
		t.Fatal(err)
	}
	shown("drill-lent", models.IconStateOnLoan)

	// Out of service is worse than lent.
	if _, _, err := st.SetAssetCondition(drill, models.ConditionOutOfService, "smoking"); err != nil {
		t.Fatal(err)
	}
	shown("drill-down", models.IconStateOutOfService)

	// Variants are chosen: maintenance drawn as down, out of service as
	// none, so the loan shows again.
	variants, err := st.SetIconStateVariants(map[string]string{models.IconStateNeedsMaintenance: "down", models.IconStateOnLoan: "lent"})
	if err != nil {
		t.Fatal(err)
	}
	if variants[models.IconStateOutOfService] != "" || variants[models.IconStateLowStock] != "" || len(variants) != len(models.IconStates) {
		t.Errorf("states left out draw none: %v", variants)
	}
	shown("drill-lent", models.IconStateOnLoan)
	if _, _, err := st.SetAssetCondition(drill, models.ConditionNeedsMaintenance, "blunt"); err != nil {
		t.Fatal(err)
	}
	shown("drill-down", models.IconStateNeedsMaintenance)
	if got, err := st.IconStateVariants(); err != nil || !reflect.DeepEqual(got, variants) {
		t.Errorf("read back %v, %v; want %v", got, err, variants)
	}

	// An icon of its own shows its state too; one with no such variant,
	// or not drawn at all, does not.
	own, err := st.CreateAssetWithIcon("Spare", []string{drills.ID}, nil, nil, nil, "drill")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SetAssetCondition(own.Asset.ID, models.ConditionNeedsMaintenance, ""); err != nil {
		t.Fatal(err)
	}
	if ic := resolved(t, st, own.Asset.ID); ic.Name != "drill-down" || ic.From != models.IconFromAsset {
		t.Errorf("its own icon: %+v", ic)
	}

	// Choosing bad variants is refused.
	for _, bad := range []map[string]string{{"broken": "down"}, {models.IconStateOnLoan: "-x"}, {models.IconStateOnLoan: "a b"}} {
		if _, err := st.SetIconStateVariants(bad); !errors.Is(err, ErrInvalidIconStates) {
			t.Errorf("%v: %v", bad, err)
		}
	}
}

// The states an asset is in, worst first, and the variant shown for them.
func TestAssetStates(t *testing.T) {
	low, empty := &models.AssetStock{Quantity: 1, IsRunningLow: true}, &models.AssetStock{Quantity: 0, IsRunningLow: true}
	for _, c := range []struct {
		d    models.AssetWithDetails
		want []string
	}{
		{models.AssetWithDetails{Asset: models.Asset{Condition: models.ConditionOK}}, nil},
		{models.AssetWithDetails{Asset: models.Asset{Condition: models.ConditionOutOfService}, Loan: &models.Loan{Overdue: true}},
			[]string{models.IconStateOutOfService, models.IconStateOverdue, models.IconStateOnLoan}},
		{models.AssetWithDetails{Asset: models.Asset{Condition: models.ConditionNeedsMaintenance, Stock: low}, Loan: &models.Loan{}},
			[]string{models.IconStateNeedsMaintenance, models.IconStateOnLoan, models.IconStateLowStock}},
		{models.AssetWithDetails{Asset: models.Asset{Stock: empty}}, []string{models.IconStateOutOfStock, models.IconStateLowStock}},
	} {
		if got := assetStates(&c.d); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: got %v, want %v", c.d.Asset, got, c.want)
		}
	}

	drawn := map[string]bool{"spool": true, "spool-empty": true, "spool-low": true, "spool-late": true}
	variants := models.DefaultIconVariants
	for _, c := range []struct {
		states      []string
		name, state string
	}{
		{nil, "spool", ""},
		{[]string{models.IconStateOutOfStock, models.IconStateLowStock}, "spool-empty", models.IconStateOutOfStock},
		// overdue's default variant is not drawn; the next one is
		{[]string{models.IconStateOverdue, models.IconStateOnLoan, models.IconStateLowStock}, "spool-low", models.IconStateLowStock},
	} {
		icon := &models.ResolvedIcon{Name: "spool"}
		showState(icon, c.states, variants, drawn)
		if icon.Name != c.name || icon.State != c.state {
			t.Errorf("%v: got %+v", c.states, icon)
		}
	}
	icon := &models.ResolvedIcon{Name: "spool"}
	showState(icon, []string{models.IconStateOverdue}, map[string]string{models.IconStateOverdue: "late"}, drawn)
	if icon.Name != "spool-late" || icon.Base != "spool" || icon.Variant != "late" {
		t.Errorf("a chosen variant: %+v", icon)
	}
}

// The state starter compiles under the settings it was made for, and an
// icon that opts into its axis is drawn in every state that has a variant.
func TestTheStateStarterCompiles(t *testing.T) {
	for name, c := range map[string]struct {
		settings models.IconSettings
		variants map[string]string
		want     []string
	}{
		"the defaults": {models.IconSettings{}, nil, []string{"drill-down", "drill-maintenance", "drill-overdue", "drill-lent", "drill-empty", "drill-low"}},
		"reserved tokens and widths": {models.IconSettings{StrokeWidths: []float64{1.2, 1.5}, Reserved: []string{"accent", "accent-dim"}}, nil,
			[]string{"drill-down", "drill-low"}},
		"chosen variants": {models.IconSettings{}, map[string]string{models.IconStateOutOfService: "broken", models.IconStateNeedsMaintenance: "broken", models.IconStateLowStock: "low"},
			[]string{"drill-broken", "drill-low"}},
	} {
		t.Run(name, func(t *testing.T) {
			st := setupTestStore(t)
			comp := compilerFor(t)
			ctx := context.Background()
			if _, err := st.SetIconSettings(ctx, comp, c.settings); err != nil {
				t.Fatal(err)
			}
			if c.variants != nil {
				if _, err := st.SetIconStateVariants(c.variants); err != nil {
					t.Fatal(err)
				}
			}
			starter, err := st.IconStateStarter()
			if err != nil {
				t.Fatal(err)
			}
			if _, save, err := st.CreateIconLibraryFile(ctx, comp, "states.ilx", starter); err != nil || !save.Published || len(save.Warnings) != 0 {
				t.Fatalf("%v %+v\n%s", err, save, starter)
			}
			tools, err := st.CreateCategoryFrom(CategoryInput{Name: "Tools"})
			if err != nil {
				t.Fatal(err)
			}
			if s := saveIcon(t, st, comp, tools.ID, "drill", drillSrc); !s.Published {
				t.Fatalf("%+v", s.Errors)
			}
			names, err := st.BuiltIconNames()
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range c.want {
				if !names[n] {
					t.Errorf("no %s in %v", n, names)
				}
			}
			if len(c.settings.Reserved) > 0 && !strings.Contains(starter, `grant "accent"`) {
				t.Errorf("no grant:\n%s", starter)
			}
			if c.settings.StrokeWidths != nil && strings.Contains(starter, "width=2") {
				t.Errorf("a width not allowed:\n%s", starter)
			}
		})
	}
}
