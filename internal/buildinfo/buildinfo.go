// Package buildinfo holds build metadata injected at link time with -ldflags.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version is the product version without the "v" prefix, for example "1.0.0".
var Version string

// Commit is the abbreviated commit hash the binary was built from.
var Commit string

// Date is the commit date in RFC 3339 format; it keeps builds reproducible.
var Date string

// Devel is the version of a build that carries no version information.
const Devel = "devel"

// Info is the resolved build metadata.
type Info struct {
	Version string
	Commit  string
	Date    string
}

// Resolve returns the metadata from -ldflags. A build without them, such as
// go install, falls back to the module version and VCS stamps recorded by Go.
func Resolve() Info {
	bi, _ := debug.ReadBuildInfo()
	return resolve(Info{Version: Version, Commit: Commit, Date: Date}, bi)
}

func resolve(linked Info, bi *debug.BuildInfo) Info {
	info := linked
	if bi == nil {
		if info.Version == "" {
			info.Version = Devel
		}
		return info
	}
	if info.Version == "" {
		info.Version = Devel
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			// The module version is the tag vX.Y.Z; the product version has no "v".
			info.Version = strings.TrimPrefix(v, "v")
		}
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = s.Value[:min(12, len(s.Value))]
			}
		case "vcs.time":
			if info.Date == "" {
				info.Date = s.Value
			}
		}
	}
	return info
}
