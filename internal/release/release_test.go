package release

import "testing"

func TestWhichVersionsAreReleases(t *testing.T) {
	for v, want := range map[string][2]bool{
		"v1.2.3":                  {true, true},
		"v10.0.12":                {true, true},
		"v1.2.3-rc.1":             {true, false},
		"v1.2.3-beta":             {true, false},
		"dev":                     {false, false},
		"":                        {false, false},
		"1.2.3":                   {false, false},
		"v1.2":                    {false, false},
		"v1.2.3-4-gabc1234":       {false, false},
		"v1.2.3-dirty":            {false, false},
		"v1.2.3-4-gabc1234-dirty": {false, false},
		"abc1234":                 {false, false},
		"v1.2.3 ":                 {false, false},
	} {
		if Is(v) != want[0] || IsFull(v) != want[1] {
			t.Errorf("%q: release %v, full %v; want %v", v, Is(v), IsFull(v), want)
		}
	}
}

func TestPages(t *testing.T) {
	notes, updating := Pages(LatestURL, "v1.2.3")
	if notes != "https://github.com/Cambridge-Hackspace/boring-asset-manager/releases/tag/v1.2.3" ||
		updating != "https://github.com/Cambridge-Hackspace/boring-asset-manager/blob/v1.2.3/docs/installing.md#updating-automatically" {
		t.Errorf("GitHub: %q, %q", notes, updating)
	}
	notes, updating = Pages("https://api.github.com/repos/someone/fork/releases", "dev")
	if notes != "" || updating != "https://github.com/someone/fork/blob/main/docs/installing.md#updating-automatically" {
		t.Errorf("a fork, from a build that is not a release: %q, %q", notes, updating)
	}
	for _, mirror := range []string{"https://mirror.example/bam/latest.json", "http://api.github.com/repos/a/b/releases/latest", "https://api.github.com/users/a"} {
		if notes, updating := Pages(mirror, "v1.2.3"); notes != "" || updating != "" {
			t.Errorf("%s: %q, %q", mirror, notes, updating)
		}
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v1.2.4", "v1.2.3", true},
		{"v1.3.0", "v1.2.9", true},
		{"v2.0.0", "v1.99.99", true},
		{"v1.10.0", "v1.9.0", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.3", "v1.2.4", false},
		{"v1.2.3", "v1.2.3-rc.1", true},
		{"v1.2.3-rc.1", "v1.2.3", false},
		{"v1.2.3-rc.2", "v1.2.3-rc.1", true},
		{"v1.2.3", "dev", false},
		{"dev", "v1.2.3", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
