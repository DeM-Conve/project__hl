package version

import (
	"runtime/debug"
	"testing"
)

func TestResolve(t *testing.T) {
	vcs := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123"},
		{Key: "vcs.time", Value: "2026-01-02T03:04:05Z"},
	}
	for _, tc := range []struct {
		name string
		in   Info
		bi   *debug.BuildInfo
		want Info
	}{
		{"no build info", Info{"dev", "unknown", "unknown"}, nil, Info{"dev", "unknown", "unknown"}},
		{"go install @version", Info{"dev", "unknown", "unknown"},
			&debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}},
			Info{"v0.1.0", "unknown", "unknown"}},
		{"git checkout", Info{"dev", "unknown", "unknown"},
			&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: vcs},
			Info{"dev", "0123456789ab", "2026-01-02T03:04:05Z"}},
		{"ldflags win", Info{"v1.2.3", "abc1234", "2026-05-06T00:00:00Z"},
			&debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}, Settings: vcs},
			Info{"v1.2.3", "abc1234", "2026-05-06T00:00:00Z"}},
	} {
		if got := resolve(tc.in, tc.bi); got != tc.want {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

func TestString(t *testing.T) {
	got := Info{"v1.2.3", "abc1234", "2026-05-06T00:00:00Z"}.String()
	want := "hl v1.2.3 (commit abc1234, built 2026-05-06T00:00:00Z)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
