package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/modules"
)

func runHook(t *testing.T, args []string, env []string, stdin string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Run(context.Background(), args, env, Stdio{In: strings.NewReader(stdin), Out: &out, Err: &errOut})
	return code, out.String(), errOut.String()
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

const validConfig = "gitlab_url: http+unix://%2Frun%2Fsocket\naudit: {file: /tmp/x.log}\n"

func TestHook_RejectsWhenItCannotWork(t *testing.T) {
	env := []string{"GL_ID=user-1", "GL_USERNAME=a", "GL_PROTOCOL=http", "GL_REPOSITORY=project-1", "GL_PROJECT_PATH=g/p", "GIT_DIR=/nonexistent", "PATH=/nonexistent"}
	stdin := strings.Repeat("0", 40) + " " + strings.Repeat("a", 40) + " refs/heads/main\n"
	tests := []struct {
		name string
		args []string
		env  []string
		want string
	}{
		{"missing config", []string{"hook", "pre-receive", "--config", "/nonexistent/config.yaml"}, env, "configuration on the server is invalid"},
		{"invalid config", []string{"hook", "pre-receive", "--config", writeConfig(t, "mode: maybe\n")}, env, "configuration on the server is invalid"},
		{"audit only to stdout", []string{"hook", "pre-receive", "--config", writeConfig(t, "gitlab_url: http+unix://%2Frun%2Fsocket\naudit: {file: \"\", stdout: true}\n")}, env, "configuration on the server is invalid"},
		{"unknown module", []string{"hook", "pre-receive", "--config", writeConfig(t, validConfig+"modules: {tagpolcy: disabled}\n")}, env, "configuration on the server is invalid"},
		{"unknown flag", []string{"hook", "pre-receive", "--frobnicate"}, env, "installed with invalid arguments"},
		{"no Gitaly environment", []string{"hook", "pre-receive", "--config", writeConfig(t, validConfig)}, nil, "outside of Gitaly"},
		{"no git", []string{"hook", "pre-receive", "--config", writeConfig(t, validConfig)}, env, "git is not available"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := runHook(t, tt.args, tt.env, stdin)
			assert.Equal(t, ExitError, code)
			assert.Empty(t, out)
			assert.Contains(t, errOut, tt.want)
			assert.Contains(t, errOut, "GL-HOOK-ERR: gitlab-repopolicy: internal error: ")
		})
	}
}

func TestHook_ModeOffAndNonProject(t *testing.T) {
	off := writeConfig(t, "mode: off\n"+validConfig)
	code, out, errOut := runHook(t, []string{"hook", "pre-receive", "--config", off}, nil, "")
	assert.Equal(t, ExitOK, code)
	assert.Empty(t, out+errOut)

	env := []string{"GL_ID=user-1", "GL_USERNAME=a", "GL_PROTOCOL=http", "GL_REPOSITORY=wiki-1", "GL_PROJECT_PATH=g/p.wiki", "GIT_DIR=/x"}
	code, out, errOut = runHook(t, []string{"hook", "pre-receive", "--config", writeConfig(t, validConfig)}, env, "garbage")
	assert.Equal(t, ExitOK, code, "wiki, design and snippet repositories are allowed without reading stdin")
	assert.Empty(t, out+errOut)
}

func TestHook_Usage(t *testing.T) {
	code, _, errOut := runHook(t, []string{"hook"}, nil, "")
	assert.Equal(t, ExitUsage, code)
	assert.Contains(t, errOut, `expected "pre-receive"`)
}

func TestLoadConfig_ModuleNames(t *testing.T) {
	a := App{Modules: modules.Default}
	_, err := a.loadConfig(writeConfig(t, validConfig+"modules: {tagpolcy: disabled, pathpolicy: enabled}\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "modules.tagpolcy: unknown module, known: pathpolicy, commitpolicy, tagpolicy")
	cfg, err := a.loadConfig(writeConfig(t, validConfig+"modules: {tagpolicy: disabled}\n"))
	require.NoError(t, err)
	assert.False(t, cfg.ModuleEnabled("tagpolicy"))
}
