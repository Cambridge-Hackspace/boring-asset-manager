// Package release tells releases of BAM! apart from other builds, and orders
// them, as packaging/bam-update does (docs/installing.md).
package release

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// LatestURL is where BAM! asks for its latest release unless told
// otherwise: GitHub's releases API, for the project's repository.
const LatestURL = "https://api.github.com/repos/Cambridge-Hackspace/boring-asset-manager/releases/latest"

var (
	// tag is a release's tag, such as v1.2.3, or v1.2.3-rc.1 for a
	// pre-release.
	tag = regexp.MustCompile(`^v([0-9]+)\.([0-9]+)\.([0-9]+)(-[0-9A-Za-z.-]+)?$`)
	// described is what git describe adds to a tag for a commit after it,
	// such as -4-gabc1234, or for a checkout with changes, -dirty.
	described = regexp.MustCompile(`-[0-9]+-g[0-9a-f]+(-dirty)?$|-dirty$`)
)

// Is reports whether a version is a release's: a tag, such as v1.2.3 or
// the pre-release v1.2.3-rc.1. "dev", and what git describe says of a
// commit that is not tagged, such as v1.2.3-4-gabc1234, are not.
func Is(version string) bool {
	return tag.MatchString(version) && !described.MatchString(version)
}

// IsFull reports whether a version is a full release's, such as v1.2.3:
// not a pre-release.
func IsFull(version string) bool {
	return Is(version) && !strings.Contains(version, "-")
}

// Newer reports whether release a is newer than release b: by major, minor
// and patch, and then a full release is newer than a pre-release of the
// same numbers, and pre-releases are ordered by their suffixes, as text.
// Versions that are not releases are newer than nothing.
func Newer(a, b string) bool {
	ma, mb := tag.FindStringSubmatch(a), tag.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(ma[i])
		y, _ := strconv.Atoi(mb[i])
		if x != y {
			return x > y
		}
	}
	pa, pb := ma[4], mb[4]
	if pa == pb {
		return false
	}
	return pa == "" || (pb != "" && pa > pb)
}

// Pages answers where a release's notes are, and the docs on updating as
// they were for it, for releases asked for at apiURL: pages of the GitHub
// repository it names. Both are empty when apiURL is not GitHub's API, as
// for a mirror.
func Pages(apiURL, version string) (notes, updating string) {
	u, err := url.Parse(apiURL)
	if err != nil || u.Scheme != "https" || u.Host != "api.github.com" {
		return "", ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "repos" || parts[1] == "" || parts[2] == "" {
		return "", ""
	}
	repo := "https://github.com/" + url.PathEscape(parts[1]) + "/" + url.PathEscape(parts[2])
	ref := "main"
	if Is(version) {
		ref = version
		notes = repo + "/releases/tag/" + url.PathEscape(version)
	}
	return notes, repo + "/blob/" + url.PathEscape(ref) + "/docs/installing.md#updating-automatically"
}
