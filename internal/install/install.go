// Package install connects the hook to Gitaly: it writes and removes the
// wrapper in pre-receive.d of custom_hooks_dir (architecture §18.3).
package install

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WrapperName is the file the hook owns in pre-receive.d; nothing else is touched.
const WrapperName = "50-gitlab-repopolicy"

// Options of install-hook.
type Options struct {
	// HooksDir is custom_hooks_dir of Gitaly; it must exist.
	HooksDir string
	// Binary and Config are absolute paths written into the wrapper.
	Binary string
	Config string
	// Chown sets the owner of the files install creates; the CLI chowns to the git user.
	Chown func(path string) error
}

// Result of install-hook and uninstall-hook.
type Result int

// Results.
const (
	Unchanged Result = iota
	Created
	Updated
	Removed
)

func (r Result) String() string {
	return [...]string{"unchanged", "created", "updated", "removed"}[r]
}

// Wrapper returns the content of the wrapper.
func Wrapper(binary, config string) []byte {
	return fmt.Appendf(nil, "#!/bin/sh\nexec %s hook pre-receive --config %s\n", shellQuote(binary), shellQuote(config))
}

// shellQuote leaves plain paths readable and quotes anything else.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._-") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Install writes the wrapper; it is idempotent.
func Install(o Options) (string, Result, error) {
	for name, p := range map[string]string{"--hooks-dir": o.HooksDir, "--binary": o.Binary, "--config": o.Config} {
		if !filepath.IsAbs(p) {
			return "", Unchanged, fmt.Errorf("%s: %q is not an absolute path", name, p)
		}
	}
	// gitlab-ctl reconfigure sets custom_hooks_dir but does not create it; its
	// parent must exist, so that a mistyped path fails instead of being created.
	if parent, err := os.Stat(filepath.Dir(o.HooksDir)); err != nil || !parent.IsDir() {
		return "", Unchanged, fmt.Errorf("custom_hooks_dir: the parent of %s is not a directory", o.HooksDir)
	}
	dir := filepath.Join(o.HooksDir, "pre-receive.d")
	for _, d := range []string{o.HooksDir, dir} {
		if err := ensureDir(d, o.Chown); err != nil {
			return "", Unchanged, err
		}
	}
	target := filepath.Join(dir, WrapperName)
	want := Wrapper(o.Binary, o.Config)
	result := Created
	switch info, err := os.Lstat(target); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", Unchanged, err
	case !info.Mode().IsRegular():
		return "", Unchanged, fmt.Errorf("%s exists and is not a regular file", target)
	default:
		have, err := os.ReadFile(target) //nolint:gosec // G304: the path is the wrapper of this tool
		if err != nil {
			return "", Unchanged, err
		}
		if bytes.Equal(have, want) && info.Mode().Perm() == 0o755 {
			return target, Unchanged, nil
		}
		result = Updated
	}
	if err := writeExecutable(dir, target, want, o.Chown); err != nil {
		return "", Unchanged, err
	}
	return target, result, nil
}

// ensureDir creates a missing directory for the git user that Gitaly runs hooks as.
func ensureDir(dir string, chown func(string) error) error {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.Mkdir(dir, 0o750); err != nil {
			return err
		}
		return chown(dir)
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	}
	return nil
}

// writeExecutable replaces target atomically: Gitaly may start the hook at any moment.
func writeExecutable(dir, target string, data []byte, chown func(string) error) error {
	tmp, err := os.CreateTemp(dir, "."+WrapperName+"-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(0o755)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = chown(tmp.Name())
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	return nil
}

// Uninstall removes the wrapper and nothing else.
func Uninstall(hooksDir string) (string, Result, error) {
	if !filepath.IsAbs(hooksDir) {
		return "", Unchanged, fmt.Errorf("--hooks-dir: %q is not an absolute path", hooksDir)
	}
	target := filepath.Join(hooksDir, "pre-receive.d", WrapperName)
	switch err := os.Remove(target); {
	case errors.Is(err, fs.ErrNotExist):
		return target, Unchanged, nil
	case err != nil:
		return "", Unchanged, err
	}
	return target, Removed, nil
}
