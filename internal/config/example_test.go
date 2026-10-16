package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExampleConfig: config/config.yaml, the conffile of the package, passes
// the strict decoder and documents the defaults.
func TestExampleConfig(t *testing.T) {
	data, err := os.ReadFile("../../config/config.yaml")
	require.NoError(t, err)
	cfg, err := Parse(data)
	require.NoError(t, err)

	want := Defaults()
	want.GitLabURL = socketURL
	want.Modules = map[string]string{"pathpolicy": ModuleEnabled, "commitpolicy": ModuleEnabled, "tagpolicy": ModuleEnabled}
	no := false
	want.SettingsCheck = SettingsCheck{DefaultBranch: "main", ProtectedBranches: []string{"main"}, ProtectedTags: []string{"v*"}, JobTokenPush: &no}
	assert.Empty(t, cfg.CommitPolicyPresets, "presets are values of an installation, the example has none")
	want.CommitPolicyPresets = cfg.CommitPolicyPresets
	assert.Equal(t, &want, cfg, "the example shows the defaults")
}

// TestReleaseVerifyConfig: config/release-verify.yaml of release-verify-tag
// enables only TAGPOLICY and writes audit to stdout.
func TestReleaseVerifyConfig(t *testing.T) {
	data, err := os.ReadFile("../../config/release-verify.yaml")
	require.NoError(t, err)
	cfg, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, ModeEnforce, cfg.Mode)
	assert.True(t, cfg.Audit.Stdout)
	assert.Empty(t, cfg.Audit.File)
	assert.True(t, cfg.ModuleEnabled("tagpolicy"))
	assert.False(t, cfg.ModuleEnabled("pathpolicy"))
	assert.False(t, cfg.ModuleEnabled("commitpolicy"))
}

func TestDuration_Text(t *testing.T) {
	for d, want := range map[Duration]string{
		Duration(364 * 24 * time.Hour): "364d",
		Duration(90 * time.Minute):     "1h30m0s",
		Duration(0):                    "0s",
	} {
		text, err := d.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, want, string(text))
		assert.Equal(t, want, d.String())
		var back Duration
		require.NoError(t, back.UnmarshalText(text))
		assert.Equal(t, d, back)
	}
}
