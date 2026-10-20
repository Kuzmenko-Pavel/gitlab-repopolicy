//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// containerFile writes a file readable by everybody in the GitLab container
// and returns its path.
func containerFile(t *testing.T, name, content string) string {
	t.Helper()
	path := "/tmp/" + uniq("validate") + "-" + name
	out, code, err := gl.containerExec(t.Context(), "", strings.NewReader(content), "sh", "-c", "cat > "+path+" && chmod 0644 "+path)
	require.NoError(t, err)
	require.Zero(t, code, out)
	return path
}

// TestValidate_Online: validate --online asks GitLab whether the usernames
// of a file are active users and, with --project, whether the admins have
// Maintainer in the project. Without an answer of GitLab the checks are
// reported as not checked, or as an error when the project itself cannot be
// read.
func TestValidate_Online(t *testing.T) {
	t.Parallel()
	p := newProject(t, "validate")
	valid := containerFile(t, "PATHPOLICY", ciPolicy)
	nobody := uniq("nobody")
	ghost := containerFile(t, "PATHPOLICY", strings.Replace(ciPolicy, "allow: [alice]", "allow: [alice, "+nobody+"]", 1))
	bobAdmin := containerFile(t, "PATHPOLICY", policyWithAdmins("alice", "bob"))
	down := containerFile(t, "config.yaml", strings.Replace(inContainer(t, "cat "+hookConfig),
		"gitlab_url: ", "gitlab_url: https://127.0.0.1:9 #", 1))
	validate := func(args ...string) (string, int) {
		out, code := rp(t, "git", append([]string{"validate", "--module", "pathpolicy", "--online"}, args...)...)
		t.Logf("validate %s -> %d\n%s", strings.Join(args, " "), code, out)
		return out, code
	}

	out, code := validate("-f", valid, "--config", hookConfig)
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, ": valid\n  not checked: admins cannot be checked: --project is not given\n")

	out, code = validate("-f", ghost, "--config", hookConfig)
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, `error: "`+nobody+`" is not an active GitLab user`)

	out, code = validate("-f", bobAdmin, "--config", hookConfig, "--project", p.Path)
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, `warning: admin "bob" is not an active user with Maintainer or higher in this project`)
	assert.Contains(t, out, "warning: 1 of 2 admins work")

	out, code = validate("-f", valid, "--config", down)
	assert.Equal(t, 0, code, out)
	assert.Contains(t, out, "not checked:")

	out, code = validate("-f", valid, "--config", down, "--project", p.Path)
	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, "--project "+p.Path)
}
