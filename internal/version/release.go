package version

import (
	"regexp"
	"strconv"
	"strings"
)

// Kinds of version, for telling whether a build can be compared with a release.
const (
	KindRelease = "release" // v1.2.3 or v1.2.3-rc.1, from a tag
	KindPseudo  = "pseudo"  // go install of a commit: v0.0.0-20261016123456-abcdef123456
	KindDev     = "dev"     // a local build: "dev", or anything else
)

var (
	semverRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)
	// Go's pseudo-versions end in a 14-digit UTC time and a 12-character commit.
	pseudoRE = regexp.MustCompile(`[-.]\d{14}-[0-9a-f]{12}(?:\+[0-9A-Za-z.-]+)?$`)
)

// Kind says whether v is a release, a Go pseudo-version or a development build.
func Kind(v string) string {
	switch {
	case !semverRE.MatchString(v):
		return KindDev
	case pseudoRE.MatchString(v):
		return KindPseudo
	}
	return KindRelease
}

// Newer reports whether release version a is newer than b. Versions that aren't releases are
// never newer.
func Newer(a, b string) bool {
	pa, pb := semverRE.FindStringSubmatch(a), semverRE.FindStringSubmatch(b)
	if pa == nil || pb == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x > y
		}
	}
	// Same x.y.z: a release is newer than its pre-releases.
	switch {
	case pa[4] == pb[4]:
		return false
	case pa[4] == "":
		return true
	case pb[4] == "":
		return false
	}
	return comparePre(pa[4], pb[4]) > 0
}

// comparePre compares pre-release strings by semver's rules: dot-separated parts, numbers
// numerically and below words.
func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		x, xerr := strconv.Atoi(as[i])
		y, yerr := strconv.Atoi(bs[i])
		switch {
		case xerr == nil && yerr == nil:
			if x != y {
				return x - y
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return len(as) - len(bs)
}
