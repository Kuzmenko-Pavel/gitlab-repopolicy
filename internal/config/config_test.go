package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const socketURL = "http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket"

func TestParse_Defaults(t *testing.T) {
	cfg, err := Parse([]byte("gitlab_url: " + socketURL + "\n"))
	require.NoError(t, err)
	want := Defaults()
	want.GitLabURL = socketURL
	assert.Equal(t, &want, cfg)
	assert.True(t, cfg.ModuleEnabled("pathpolicy"))
	ep, err := cfg.Endpoint()
	require.NoError(t, err)
	assert.Equal(t, Endpoint{Socket: "/var/opt/gitlab/gitlab-workhorse/sockets/socket", BaseURL: "http://gitlab"}, ep)
}

func TestParse_Full(t *testing.T) {
	data, err := os.ReadFile("testdata/full.yaml")
	require.NoError(t, err)
	cfg, err := Parse(data)
	require.NoError(t, err)
	assert.Equal(t, ModeAudit, cfg.Mode)
	assert.Equal(t, 30*24*time.Hour, cfg.TokenLifetime.Std())
	assert.Equal(t, 8*time.Second, cfg.Timeouts.Total.Std())
	assert.Equal(t, 2*time.Second, cfg.Timeouts.HTTP.Std())
	assert.Equal(t, 20, cfg.MaxAPICalls)
	assert.True(t, cfg.Audit.Stdout)
	assert.True(t, cfg.ModuleEnabled("pathpolicy"))
	assert.False(t, cfg.ModuleEnabled("commitpolicy"))
	assert.True(t, cfg.ModuleEnabled("tagpolicy"))
	assert.Equal(t, []string{"main", "master"}, cfg.DefaultBranch)
	require.NotNil(t, cfg.SettingsCheck.JobTokenPush)
	assert.False(t, *cfg.SettingsCheck.JobTokenPush)
	assert.Contains(t, cfg.Presets["commitpolicy"], "org-conventional")
	ep, err := cfg.Endpoint()
	require.NoError(t, err)
	assert.Equal(t, Endpoint{BaseURL: "https://gitlab.example.com"}, ep)
}

func TestParse_Invalid(t *testing.T) {
	tests := []struct {
		name, in, wantErr string
	}{
		{"no gitlab_url", "mode: enforce\n", "gitlab_url: required"},
		{"bad mode", "gitlab_url: " + socketURL + "\nmode: strict\n", `mode: "strict" is not one of off, audit, enforce`},
		{"http scheme", "gitlab_url: http://gitlab.local\n", "is not an http+unix:// or https://host URL"},
		{"credentials in URL", "gitlab_url: https://user:pw@gitlab.local\n", "is not an http+unix:// or https://host URL"},
		{"relative socket", "gitlab_url: http+unix://socket\n", "needs an absolute socket path"},
		{"ca_file with socket", "gitlab_url: " + socketURL + "\nca_file: /ca.pem\n", "ca_file: only used with https://"},
		{"relative ca_file", "gitlab_url: https://gitlab.local\nca_file: ca.pem\n", `ca_file: "ca.pem" is not an absolute path`},
		{"relative token_file", "gitlab_url: " + socketURL + "\ntoken_file: token\n", "token_file"},
		{"no audit sink", "gitlab_url: " + socketURL + "\naudit: {file: \"\"}\n", "audit: set file, stdout or both"},
		{"bad duration", "gitlab_url: " + socketURL + "\ntimeouts: {total: 10x}\n", `invalid duration "10x"`},
		{"negative days", "gitlab_url: " + socketURL + "\ntoken_lifetime: -1d\n", "invalid duration"},
		{"short token lifetime", "gitlab_url: " + socketURL + "\ntoken_lifetime: 1h\n", "token_lifetime: must be at least 1d"},
		{"http over total", "gitlab_url: " + socketURL + "\ntimeouts: {total: 1s, http: 3s}\n", "timeouts.http: must not exceed timeouts.total"},
		{"zero timeout", "gitlab_url: " + socketURL + "\ntimeouts: {http: 0s}\n", "timeouts.http: must be positive"},
		{"no api calls", "gitlab_url: " + socketURL + "\nmax_api_calls: 0\n", "max_api_calls: must be at least 1"},
		{"module state", "gitlab_url: " + socketURL + "\nmodules: {pathpolicy: on}\n", `modules.pathpolicy: "on" is not one of enabled, disabled`},
		{"empty default branch", "gitlab_url: " + socketURL + "\ndefault_branch: [\"\"]\n", "default_branch: an empty name"},
		{"default branch pattern", "gitlab_url: " + socketURL + "\ndefault_branch: [\"release/*\"]\n", `default_branch: "release/*": a name, not a pattern`},
		{"default branch ref", "gitlab_url: " + socketURL + "\ndefault_branch: [refs/heads/main]\n", "a short name without refs/heads/"},
		{"default branch twice", "gitlab_url: " + socketURL + "\ndefault_branch: [main, main]\n", `default_branch: "main" is listed twice`},
		{"default branch under settings_check", "gitlab_url: " + socketURL + "\nsettings_check: {default_branch: [main]}\n", "field default_branch not found"},
		{"unknown key", "gitlab_url: " + socketURL + "\ngit_bin: /usr/bin/git\n", "field git_bin not found"},
		{"anchor", "gitlab_url: &u " + socketURL + "\n", "anchors are not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.in))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func FuzzParse(f *testing.F) {
	data, err := os.ReadFile("testdata/full.yaml")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(data)
	f.Add([]byte("gitlab_url: " + socketURL + "\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		cfg, err := Parse(data)
		if err != nil {
			return
		}
		if _, err := cfg.Endpoint(); err != nil {
			t.Fatalf("valid config with invalid endpoint: %v", err)
		}
	})
}

func TestPresets(t *testing.T) {
	presets, err := Presets([]byte("mode: strict\npresets:\n  commitpolicy:\n    org:\n      rules: {}\n  branchpolicy:\n    std:\n      rules: []\n"))
	require.NoError(t, err, "other keys are not checked")
	assert.Equal(t, map[string]any{"org": map[string]any{"rules": map[string]any{}}}, presets["commitpolicy"])
	assert.Equal(t, map[string]any{"std": map[string]any{"rules": []any{}}}, presets["branchpolicy"])
	presets, err = Presets([]byte("gitlab_url: x\n"))
	require.NoError(t, err)
	assert.Nil(t, presets)
	_, err = Presets([]byte("presets: [a]\n"))
	assert.ErrorContains(t, err, "presets: expected a mapping")
	_, err = Presets([]byte("presets:\n  branchpolicy: [a]\n"))
	assert.ErrorContains(t, err, "presets.branchpolicy: expected a mapping")
	_, err = Presets([]byte("- a\n"))
	assert.ErrorContains(t, err, "expected a mapping")
	_, err = Presets([]byte("presets: &a {}\n"))
	assert.ErrorContains(t, err, "anchors are not allowed")
}

func TestHelp(t *testing.T) {
	base := "gitlab_url: " + socketURL + "\n"
	cfg, err := Parse([]byte(base + "help:\n  contact: \"#gitlab-support\"\n  docs_url: https://wiki.example/repopolicy\n"))
	require.NoError(t, err)
	assert.Equal(t, Help{Contact: "#gitlab-support", DocsURL: "https://wiki.example/repopolicy"}, cfg.Help)
	for in, want := range map[string]string{
		"help:\n  contact: " + strings.Repeat("x", 201) + "\n": "help.contact: 201 characters, the limit is 200",
		"help:\n  contact: \"a\\tb\"\n":                        "help.contact: control characters are not allowed",
		"help:\n  docs_url: wiki/repopolicy\n":                 `help.docs_url: "wiki/repopolicy" is not an http(s) URL`,
	} {
		_, err := Parse([]byte(base + in))
		assert.ErrorContains(t, err, want, in)
	}
}
