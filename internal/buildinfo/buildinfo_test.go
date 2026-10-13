package buildinfo

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolve(t *testing.T) {
	vcs := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "0123456789abcdef0123456789abcdef01234567"},
		{Key: "vcs.time", Value: "2026-10-13T12:00:00Z"},
	}
	tests := []struct {
		name   string
		linked Info
		bi     *debug.BuildInfo
		want   Info
	}{
		{
			name:   "ldflags win over build info",
			linked: Info{Version: "1.0.0", Commit: "aaaaaaaaaaaa", Date: "2026-10-01T00:00:00Z"},
			bi:     &debug.BuildInfo{Main: debug.Module{Version: "v2.0.0"}, Settings: vcs},
			want:   Info{Version: "1.0.0", Commit: "aaaaaaaaaaaa", Date: "2026-10-01T00:00:00Z"},
		},
		{
			name: "go install takes the module version without v",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "v1.0.0-rc.1"}},
			want: Info{Version: "1.0.0-rc.1"},
		},
		{
			name: "build from a clone takes VCS stamps",
			bi:   &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: vcs},
			want: Info{Version: Devel, Commit: "0123456789ab", Date: "2026-10-13T12:00:00Z"},
		},
		{
			name: "no build info",
			want: Info{Version: Devel},
		},
		{
			name: "short revision is kept as is",
			bi:   &debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}}},
			want: Info{Version: Devel, Commit: "abc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolve(tt.linked, tt.bi))
		})
	}
}
