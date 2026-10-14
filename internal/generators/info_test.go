package generators

import (
	"strings"
	"testing"
)

func TestDescribeCoversEveryGenerator(t *testing.T) {
	infos := Describe()
	if len(infos) != len(AllowedGeneratorTypes()) {
		t.Fatalf("%d descriptions for %d generators", len(infos), len(AllowedGeneratorTypes()))
	}
	for _, info := range infos {
		g, err := Lookup(info.Type)
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == "" || info.CodeRules == "" {
			t.Errorf("%s is missing a name or rules: %+v", info.Type, info)
		}
		if strings.Join(info.ErrorCorrectionLevels, ",") != strings.Join(g.ErrorCorrectionLevels(), ",") {
			t.Errorf("%s levels %v, generator has %v", info.Type, info.ErrorCorrectionLevels, g.ErrorCorrectionLevels())
		}
	}
	// The stated limits are the enforced ones.
	if !strings.Contains(Describe()[1].CodeRules, "35 digits") || !strings.Contains(Describe()[1].CodeRules, "21 characters") {
		t.Errorf("micro_qr rules: %s", Describe()[1].CodeRules)
	}
}
