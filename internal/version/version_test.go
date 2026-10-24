package version

import (
	"runtime/debug"
	"testing"
)

func buildInfo(mainVersion string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Version: mainVersion}, Settings: settings}
}

func TestResolve(t *testing.T) {
	rev := debug.BuildSetting{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"}
	dirty := debug.BuildSetting{Key: "vcs.modified", Value: "true"}
	clean := debug.BuildSetting{Key: "vcs.modified", Value: "false"}

	tests := []struct {
		name string
		ld   string
		bi   *debug.BuildInfo
		ok   bool
		want Info
	}{
		{"ldflags wins over module version", "v1.2.3", buildInfo("v0.9.0"), true, Info{Version: "v1.2.3"}},
		{"module version when no ldflags", "", buildInfo("v0.0.0-20261024-abc"), true, Info{Version: "v0.0.0-20261024-abc"}},
		{"devel maps to dev", "", buildInfo("(devel)"), true, Info{Version: "dev"}},
		{"empty maps to dev", "", buildInfo(""), true, Info{Version: "dev"}},
		{"no build info gives dev", "", nil, false, Info{Version: "dev"}},
		{"no build info keeps ldflags", "v2.0.0", nil, false, Info{Version: "v2.0.0"}},
		{"revision shortened to 12", "", buildInfo("(devel)", rev), true, Info{Version: "dev", Commit: "0123456789ab"}},
		{"modified tree", "v1.0.0", buildInfo("", rev, dirty), true, Info{Version: "v1.0.0", Commit: "0123456789ab", Modified: true}},
		{"clean tree", "v1.0.0", buildInfo("", rev, clean), true, Info{Version: "v1.0.0", Commit: "0123456789ab"}},
		{"short revision kept whole", "", buildInfo("v1.0.0", debug.BuildSetting{Key: "vcs.revision", Value: "abc123"}), true, Info{Version: "v1.0.0", Commit: "abc123"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolve(tt.ld, tt.bi, tt.ok); got != tt.want {
				t.Errorf("resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestInfoString(t *testing.T) {
	tests := []struct {
		info Info
		want string
	}{
		{Info{Version: "v1.2.3", Commit: "0123456789ab"}, "v1.2.3 (commit 0123456789ab)"},
		{Info{Version: "v1.2.3", Commit: "0123456789ab", Modified: true}, "v1.2.3 (commit 0123456789ab, modified)"},
		{Info{Version: "v1.2.3"}, "v1.2.3"},
		{Info{Version: "dev"}, "dev"},
	}
	for _, tt := range tests {
		if got := tt.info.String(); got != tt.want {
			t.Errorf("%+v.String() = %q, want %q", tt.info, got, tt.want)
		}
	}
}
