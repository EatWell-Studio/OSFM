package version

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	vcs := func(rev, modified string) []debug.BuildSetting {
		return []debug.BuildSetting{{Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: modified}}
	}
	for _, tc := range []struct {
		name   string
		linked string
		info   *debug.BuildInfo
		want   string
	}{
		{"linked tag", "v1.2.3", &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}, "1.2.3"},
		{"linked pre-release", " v1.3.0-rc.1\n", nil, "1.3.0-rc.1"},
		{"linked without v", "0.4.0", nil, "0.4.0"},
		{"module version", "", &debug.BuildInfo{Main: debug.Module{Version: "v0.5.1"}}, "0.5.1"},
		{"pseudo-version", "", &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261126101500-0123456789ab+dirty"}},
			"0.0.0-20261126101500-0123456789ab+dirty"},
		{"revision only", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"},
			Settings: vcs("0123456789abcdef0123", "false")}, "dev+0123456789ab"},
		{"modified revision", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"},
			Settings: vcs("0123456789abcdef0123", "true")}, "dev+0123456789ab.dirty"},
		{"nothing", "", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, Dev},
		{"no build info", "", nil, Dev},
		{"not a tag", "", &debug.BuildInfo{Main: debug.Module{Version: "vnext"}}, "vnext"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(tc.linked, tc.info); got != tc.want {
				t.Errorf("resolve = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestString(t *testing.T) {
	if String() == "" {
		t.Error("String() is empty")
	}
}
