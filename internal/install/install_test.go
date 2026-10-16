package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type owner struct {
	paths []string
	err   error
}

func (o *owner) Chown(path string) error {
	o.paths = append(o.paths, filepath.Base(path))
	return o.err
}

func options(t *testing.T, o *owner) Options {
	t.Helper()
	return Options{HooksDir: t.TempDir(), Binary: "/usr/bin/gitlab-repopolicy",
		Config: "/etc/gitlab-repopolicy/config.yaml", Chown: o.Chown}
}

func TestInstall(t *testing.T) {
	o := &owner{}
	opts := options(t, o)
	path, res, err := Install(opts)
	require.NoError(t, err)
	assert.Equal(t, Created, res)
	assert.Equal(t, filepath.Join(opts.HooksDir, "pre-receive.d", WrapperName), path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\nexec /usr/bin/gitlab-repopolicy hook pre-receive --config /etc/gitlab-repopolicy/config.yaml\n", string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())
	dir, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), dir.Mode().Perm())
	require.Len(t, o.paths, 2)
	assert.Equal(t, "pre-receive.d", o.paths[0])
	assert.Contains(t, o.paths[1], "."+WrapperName+"-", "the temporary file is chowned before the rename")

	_, res, err = Install(opts)
	require.NoError(t, err)
	assert.Equal(t, Unchanged, res)

	opts.Config = "/etc/other dir/config.yaml"
	_, res, err = Install(opts)
	require.NoError(t, err)
	assert.Equal(t, Updated, res)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "--config '/etc/other dir/config.yaml'\n")

	require.NoError(t, os.Chmod(path, 0o700))
	_, res, err = Install(opts)
	require.NoError(t, err)
	assert.Equal(t, Updated, res, "a wrong mode is repaired")

	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary files are left")
}

func TestInstall_Errors(t *testing.T) {
	o := &owner{}
	opts := options(t, o)
	opts.HooksDir = "relative"
	_, _, err := Install(opts)
	assert.ErrorContains(t, err, "--hooks-dir")

	opts = options(t, o)
	opts.HooksDir = filepath.Join(opts.HooksDir, "missing", "custom_hooks")
	_, _, err = Install(opts)
	assert.ErrorContains(t, err, "custom_hooks_dir: the parent")

	opts = options(t, o)
	require.NoError(t, os.WriteFile(filepath.Join(opts.HooksDir, "file"), nil, 0o600))
	opts.HooksDir = filepath.Join(opts.HooksDir, "file")
	_, _, err = Install(opts)
	assert.ErrorContains(t, err, "is not a directory")

	opts = options(t, o)
	require.NoError(t, os.WriteFile(filepath.Join(opts.HooksDir, "pre-receive.d"), nil, 0o600))
	_, _, err = Install(opts)
	assert.ErrorContains(t, err, "not a directory")

	opts = options(t, o)
	require.NoError(t, os.MkdirAll(filepath.Join(opts.HooksDir, "pre-receive.d", WrapperName), 0o750))
	_, _, err = Install(opts)
	assert.ErrorContains(t, err, "not a regular file")

	failing := &owner{err: errors.New("chown denied")}
	opts = options(t, failing)
	require.NoError(t, os.Mkdir(filepath.Join(opts.HooksDir, "pre-receive.d"), 0o750))
	_, _, err = Install(opts)
	require.ErrorContains(t, err, "chown denied")
	entries, err := os.ReadDir(filepath.Join(opts.HooksDir, "pre-receive.d"))
	require.NoError(t, err)
	assert.Empty(t, entries, "a failed install leaves nothing behind")
}

// TestInstall_CreatesHooksDir: gitlab-ctl reconfigure does not create custom_hooks_dir.
func TestInstall_CreatesHooksDir(t *testing.T) {
	o := &owner{}
	opts := options(t, o)
	opts.HooksDir = filepath.Join(opts.HooksDir, "custom_hooks")
	path, res, err := Install(opts)
	require.NoError(t, err)
	assert.Equal(t, Created, res)
	assert.FileExists(t, path)
	info, err := os.Stat(opts.HooksDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o750), info.Mode().Perm())
	assert.Equal(t, []string{"custom_hooks", "pre-receive.d"}, o.paths[:2])
}

func TestUninstall(t *testing.T) {
	opts := options(t, &owner{})
	path, _, err := Install(opts)
	require.NoError(t, err)
	other := filepath.Join(filepath.Dir(path), "10-other")
	require.NoError(t, os.WriteFile(other, []byte("#!/bin/sh\n"), 0o600))

	got, res, err := Uninstall(opts.HooksDir)
	require.NoError(t, err)
	assert.Equal(t, Removed, res)
	assert.Equal(t, path, got)
	assert.NoFileExists(t, path)
	assert.FileExists(t, other, "other hooks stay")

	_, res, err = Uninstall(opts.HooksDir)
	require.NoError(t, err)
	assert.Equal(t, Unchanged, res)
	_, _, err = Uninstall("relative")
	assert.Error(t, err)
}
