//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The installation scenarios change pre-receive.d of the whole stand, so they
// do not call t.Parallel: Go runs them before the parallel tests start.

const (
	hookDir = hooksDir + "/pre-receive.d"
	wrapper = hookDir + "/50-gitlab-repopolicy"
)

// rp runs gitlab-repopolicy in the GitLab container as login, root when empty.
func rp(t *testing.T, login string, args ...string) (string, int) {
	t.Helper()
	out, code, err := gl.containerExec(t.Context(), login, nil, append([]string{"gitlab-repopolicy"}, args...)...)
	require.NoError(t, err)
	return out, code
}

// reinstall restores the hook as the bootstrap left it.
func reinstall(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, code, err := gl.containerExec(ctx, "", nil, "gitlab-repopolicy", "install-hook", "--hooks-dir", hooksDir, "--user", "git")
	if err != nil || code != 0 {
		t.Errorf("reinstall the hook: %v, exit %d: %s", err, code, out)
	}
}

// TestInstall_InContainer covers OP-17: install-hook and selftest run in the
// GitLab container the way the documentation installs the hook.
func TestInstall_InContainer(t *testing.T) {
	out, code := rp(t, "", "install-hook", "--hooks-dir", hooksDir, "--user", "git")
	require.Zero(t, code, out)
	assert.Contains(t, out, wrapper+": unchanged")
	assert.Contains(t, out, hookConfig+": owner set")
	assert.Contains(t, out, hookToken+": owner set")
	assert.Contains(t, out, "/var/log/gitlab-repopolicy: owner set")
	assert.Contains(t, out, "Next: gitlab-repopolicy selftest")

	out, code = rp(t, "git", "install-hook", "--hooks-dir", hooksDir, "--user", "git")
	assert.NotZero(t, code, out)
	assert.Contains(t, out, "this command must run as root")

	for _, login := range []string{"", "git"} {
		out, code = rp(t, login, "selftest")
		require.Zero(t, code, "selftest as %q:\n%s", login, out)
		assert.NotContains(t, out, "FAIL")
	}
	stat := inContainer(t, "stat -c '%U:%G %a %n' "+hookConfig+" "+hookToken+" /var/log/gitlab-repopolicy")
	assert.Contains(t, stat, "root:git 640 "+hookConfig)
	assert.Contains(t, stat, "root:git 640 "+hookToken)
	assert.Contains(t, stat, "git:git 750 /var/log/gitlab-repopolicy")
}

// TestInstall_WrapperProperties covers OP-19 (architecture §18.3).
func TestInstall_WrapperProperties(t *testing.T) {
	t.Cleanup(func() {
		reinstall(t)
		_, _, _ = gl.containerExec(context.Background(), "", nil, "rm", "-f", hookDir+"/10-e2e-neighbour", hookDir+"/60-e2e-neighbour~")
	})
	stat := inContainer(t, "stat -c '%U:%G %a' "+wrapper)
	assert.Equal(t, "git:git 755", strings.TrimSpace(stat))
	assert.False(t, strings.HasSuffix(wrapper, "~"), "Gitaly skips names ending in ~")
	assert.Equal(t, "#!/bin/sh\nexec /usr/bin/gitlab-repopolicy hook pre-receive --config "+hookConfig+"\n",
		inContainer(t, "cat "+wrapper))

	neighbours := "printf '#!/bin/sh\\nexit 0\\n' > " + hookDir + "/10-e2e-neighbour && chmod 750 " + hookDir + "/10-e2e-neighbour && " +
		"printf 'backup\\n' > " + hookDir + "/60-e2e-neighbour~"
	inContainer(t, neighbours)
	snapshot := func() string {
		return inContainer(t, "cd "+hookDir+" && stat -c '%U:%G %a %s %n' 10-e2e-neighbour 60-e2e-neighbour~ && sha256sum 10-e2e-neighbour 60-e2e-neighbour~")
	}
	before := snapshot()

	inContainer(t, "rm "+wrapper)
	out, code := rp(t, "", "install-hook", "--hooks-dir", hooksDir, "--user", "git")
	require.Zero(t, code, out)
	assert.Contains(t, out, wrapper+": created")
	assert.Equal(t, before, snapshot())

	out, code = rp(t, "", "uninstall-hook", "--hooks-dir", hooksDir)
	require.Zero(t, code, out)
	assert.Contains(t, out, wrapper+": removed")
	assert.Equal(t, before, snapshot())
	ls := inContainer(t, "ls -A "+hookDir)
	assert.Equal(t, []string{"10-e2e-neighbour", "60-e2e-neighbour~"}, strings.Fields(ls))

	out, code = rp(t, "", "uninstall-hook", "--hooks-dir", hooksDir)
	require.Zero(t, code, out)
	assert.Contains(t, out, wrapper+": not installed")
}

// TestInstall_ChainStopsOnEarlierFailure covers OP-20: Gitaly runs
// pre-receive.d in name order and stops at the first failure.
func TestInstall_ChainStopsOnEarlierFailure(t *testing.T) {
	neighbour := hookDir + "/10-e2e-fail"
	t.Cleanup(func() { _, _, _ = gl.containerExec(context.Background(), "", nil, "rm", "-f", neighbour) })
	p := withCIPolicy(t, "chain")
	inContainer(t, "printf '#!/bin/sh\\necho \"GL-HOOK-ERR: e2e neighbour declined\" >&2\\nexit 1\\n' > "+neighbour+" && chmod 755 "+neighbour)

	alice := user("alice")
	g := clone(t, alice.Token, p.Path)
	g.commitFile("ci/build.yml", "build", "ci: change the build")
	out, err := g.push(httpsURL(alice.Token, p.Path), "", "HEAD:refs/heads/chain")
	requirePushDenied(t, out, err, "e2e neighbour declined")
	assert.NotContains(t, out, "gitlab-repopolicy")
	for _, e := range auditEvents(t, p.Path) {
		assert.NotEqual(t, "refs/heads/chain", e.Ref, "gitlab-repopolicy ran after a failed neighbour")
	}

	inContainer(t, "rm "+neighbour)
	out, err = g.push(httpsURL(alice.Token, p.Path), "", "HEAD:refs/heads/chain")
	require.NoError(t, err, out)
	requireEvent(t, p, "allow without the neighbour", decisionOn("refs/heads/chain"))
}

// TestInstall_SurvivesReconfigure covers OP-18: gitlab-ctl reconfigure keeps
// custom_hooks_dir in the Gitaly config and the wrapper in it.
func TestInstall_SurvivesReconfigure(t *testing.T) {
	before := inContainer(t, "sha256sum "+wrapper)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	out, code, err := gl.containerExec(ctx, "", nil, "gitlab-ctl", "reconfigure")
	require.NoError(t, err)
	require.Zero(t, code, tail(out, 40))
	assert.Equal(t, before, inContainer(t, "sha256sum "+wrapper))
	cfg := inContainer(t, "grep -A2 '^\\[hooks\\]' /var/opt/gitlab/gitaly/config.toml")
	assert.Contains(t, cfg, `custom_hooks_dir = "`+hooksDir+`"`)
	waitUntil(t, 5*time.Minute, "GitLab ready after reconfigure", func() bool {
		_, code, err := gl.containerExec(t.Context(), "", nil, "sh", "-c",
			`curl -fsSk --max-time 10 "`+gl.baseURL+`/-/readiness?all=1" | grep -q '"status":"ok"'`)
		return err == nil && code == 0
	})

	p := withCIPolicy(t, "reconfigure")
	alice := user("alice")
	g := clone(t, alice.Token, p.Path)
	g.commitFile("ci/build.yml", "build", "ci: change the build")
	out, err = g.push(httpsURL(alice.Token, p.Path), "", "HEAD:refs/heads/after-reconfigure")
	require.NoError(t, err, out)
	requireEvent(t, p, "allow after reconfigure", decisionOn("refs/heads/after-reconfigure"))
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
