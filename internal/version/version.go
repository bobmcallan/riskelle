// Package version reports which riskelle build is running. It is the single
// owner of the version string: the CLI prints it and reports record it.
//
// The value is resolved without a clock or network, so the same build always
// reports the same version.
package version

import (
	"fmt"
	"runtime/debug"
)

// version is set by release builds:
//
//	-ldflags "-X github.com/bobmcallan/riskelle/internal/version.version=v1.2.3"
var version string

// commitLen is how many characters of the VCS revision are reported.
const commitLen = 12

// Info identifies a build.
type Info struct {
	Version  string // release tag, module version, or "dev"
	Commit   string // abbreviated VCS revision, empty when unknown
	Modified bool   // the build's working tree had uncommitted changes
}

// Get returns the running binary's build information.
func Get() Info {
	bi, ok := debug.ReadBuildInfo()
	return resolve(version, bi, ok)
}

// resolve picks the version from the ldflags value, then the module version,
// then "dev", and adds the VCS revision when the build recorded one.
func resolve(ld string, bi *debug.BuildInfo, ok bool) Info {
	info := Info{Version: ld}
	if !ok || bi == nil {
		if info.Version == "" {
			info.Version = "dev"
		}
		return info
	}
	if info.Version == "" {
		info.Version = bi.Main.Version
	}
	if info.Version == "" || info.Version == "(devel)" {
		info.Version = "dev"
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Commit = s.Value
			if len(info.Commit) > commitLen {
				info.Commit = info.Commit[:commitLen]
			}
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	return info
}

// String renders the info as "v1.2.3 (commit 0123456789ab, modified)",
// dropping the parts that are unknown.
func (i Info) String() string {
	switch {
	case i.Commit != "" && i.Modified:
		return fmt.Sprintf("%s (commit %s, modified)", i.Version, i.Commit)
	case i.Commit != "":
		return fmt.Sprintf("%s (commit %s)", i.Version, i.Commit)
	default:
		return i.Version
	}
}
