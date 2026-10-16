// Package version is hl's version, commit and build date.
//
// Release builds set them with -ldflags:
//
//	-X github.com/DeM-Conve/project__hl/internal/version.Version=v1.2.3
//	-X github.com/DeM-Conve/project__hl/internal/version.Commit=abc1234
//	-X github.com/DeM-Conve/project__hl/internal/version.Date=2026-01-02T03:04:05Z
//
// Otherwise they come from the Go build info: `go install ...@v1.2.3` gives the
// module version, a build from a git checkout gives the commit and its date.
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Set at build time; "dev" and "unknown" when not.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Info is the version, commit and build date.
type Info struct {
	Version, Commit, Date string
}

// Get fills in what the linker didn't set from the Go build info.
func Get() Info {
	bi, _ := debug.ReadBuildInfo()
	return resolve(Info{Version, Commit, Date}, bi)
}

func resolve(i Info, bi *debug.BuildInfo) Info {
	if bi == nil {
		return i
	}
	if i.Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		i.Version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch {
		case s.Key == "vcs.revision" && i.Commit == "unknown":
			i.Commit = s.Value
			if len(i.Commit) > 12 {
				i.Commit = i.Commit[:12]
			}
		case s.Key == "vcs.time" && i.Date == "unknown":
			i.Date = s.Value
		}
	}
	return i
}

// String is the one line `hl version` prints; commit and date only when known
// (`go install ...@version` builds carry neither).
func (i Info) String() string {
	var extra []string
	if i.Commit != "unknown" {
		extra = append(extra, "commit "+i.Commit)
	}
	if i.Date != "unknown" {
		extra = append(extra, "built "+i.Date)
	}
	if len(extra) == 0 {
		return "hl " + i.Version
	}
	return fmt.Sprintf("hl %s (%s)", i.Version, strings.Join(extra, ", "))
}
