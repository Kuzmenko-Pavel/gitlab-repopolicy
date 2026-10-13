// Package gitx runs git for the hook. Every child git gets an explicit
// environment: the repository and quarantine from Gitaly, isolation from
// replace refs and from system and global config (architecture §12.1, D22).
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Env is what a child git may see.
type Env struct {
	// Dir is GIT_DIR, the bare repository.
	Dir string
	// ObjectDirectory and AlternateObjectDirs are the quarantine variables, when set.
	ObjectDirectory     string
	AlternateObjectDirs string
	// Path is searched for git; Gitaly puts its own git first.
	Path string
	// NoObjectMode disables the %(objectmode) fast path even when git supports it.
	NoObjectMode bool
}

// Repo runs git commands against one repository.
type Repo struct {
	git     string
	env     []string
	noMode  bool
	catfile *catFile
}

// maxStderr bounds the stderr kept for error messages.
const maxStderr = 2048

// Open finds git in env.Path and prepares the isolated environment.
func Open(env Env) (*Repo, error) {
	if env.Dir == "" {
		return nil, errors.New("gitx: GIT_DIR is not set")
	}
	git, err := lookGit(env.Path)
	if err != nil {
		return nil, err
	}
	// A plain git hook gets relative paths such as GIT_DIR=.; children run from "/".
	if env.Dir, err = filepath.Abs(env.Dir); err != nil {
		return nil, err
	}
	if env.ObjectDirectory != "" {
		if env.ObjectDirectory, err = filepath.Abs(env.ObjectDirectory); err != nil {
			return nil, err
		}
	}
	if env.AlternateObjectDirs != "" {
		dirs := filepath.SplitList(env.AlternateObjectDirs)
		for i := range dirs {
			if dirs[i], err = filepath.Abs(dirs[i]); err != nil {
				return nil, err
			}
		}
		env.AlternateObjectDirs = strings.Join(dirs, string(filepath.ListSeparator))
	}
	vars := []string{
		"GIT_DIR=" + env.Dir,
		"PATH=" + env.Path,
		"GIT_NO_REPLACE_OBJECTS=1",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"HOME=/nonexistent",
		"XDG_CONFIG_HOME=/nonexistent",
		"LC_ALL=C",
	}
	if env.ObjectDirectory != "" {
		vars = append(vars, "GIT_OBJECT_DIRECTORY="+env.ObjectDirectory)
	}
	if env.AlternateObjectDirs != "" {
		vars = append(vars, "GIT_ALTERNATE_OBJECT_DIRECTORIES="+env.AlternateObjectDirs)
	}
	return &Repo{git: git, env: vars, noMode: env.NoObjectMode}, nil
}

func lookGit(path string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, "git")
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("gitx: git not found in PATH %q", path)
}

// Close stops the long-lived cat-file process.
func (r *Repo) Close() error {
	if r.catfile == nil {
		return nil
	}
	return r.catfile.close()
}

func (r *Repo) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.git, args...) //nolint:gosec // G204: git from Gitaly's PATH, arguments built by gitx
	cmd.Env = r.env
	cmd.Dir = "/"
	cmd.WaitDelay = time.Second
	return cmd
}

// run executes git and returns stdout; a non-zero exit is an error with stderr.
func (r *Repo) run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := r.command(ctx, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout bytes.Buffer
	stderr := &limitedBuffer{max: maxStderr}
	cmd.Stdout, cmd.Stderr = &stdout, stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), &Error{Args: args, Err: err, Stderr: strings.TrimSpace(stderr.String())}
	}
	return stdout.Bytes(), nil
}

// Error is a failed git command.
type Error struct {
	Args   []string
	Err    error
	Stderr string
}

func (e *Error) Error() string {
	sub := ""
	if len(e.Args) > 0 {
		sub = e.Args[0]
	}
	if e.Stderr != "" {
		return fmt.Sprintf("git %s: %v: %s", sub, e.Err, e.Stderr)
	}
	return fmt.Sprintf("git %s: %v", sub, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); room > 0 {
		b.Buffer.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}

// Version returns the version of git, for example "2.55.0".
func (r *Repo) Version(ctx context.Context) (string, error) {
	out, err := r.run(ctx, nil, "version")
	if err != nil {
		return "", err
	}
	v, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "git version ")
	if !ok {
		return "", fmt.Errorf("gitx: unexpected git version output %q", out)
	}
	return v, nil
}

// HeadRef returns the ref HEAD points to, for example "refs/heads/main".
func (r *Repo) HeadRef(ctx context.Context) (string, error) {
	out, err := r.run(ctx, nil, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(string(out))
	if !strings.HasPrefix(ref, "refs/heads/") {
		return "", fmt.Errorf("gitx: HEAD points to %q", ref)
	}
	return ref, nil
}

// Refs lists the branches and tags of the repository.
func (r *Repo) Refs(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, nil, "for-each-ref", "--format=%(refname)", "refs/heads/", "refs/tags/")
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// IsOID reports whether s is a full lowercase SHA-1 or SHA-256 object name.
func IsOID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// checkRev guards arguments built from push input: object names and full ref names only.
func checkRev(rev string) error {
	if IsOID(rev) || strings.HasPrefix(rev, "refs/") && !strings.ContainsAny(rev, " \t\n\x00^~:?*[\\") {
		return nil
	}
	return fmt.Errorf("gitx: invalid revision %q", rev)
}

// DiffNames returns the paths that differ between the trees of commits a and b.
func (r *Repo) DiffNames(ctx context.Context, a, b string) ([]string, error) {
	for _, rev := range []string{a, b} {
		if err := checkRev(rev); err != nil {
			return nil, err
		}
	}
	out, err := r.run(ctx, nil, "diff-tree", "-r", "-z", "--no-renames", "--name-only", a, b)
	if err != nil {
		return nil, err
	}
	return splitZ(out), nil
}

// CommitPaths returns the union of paths changed by each commit: against all
// parents for merges (combined diff), against the empty tree for root commits.
func (r *Repo) CommitPaths(ctx context.Context, commits []string) ([]string, error) {
	if len(commits) == 0 {
		return nil, nil
	}
	var in bytes.Buffer
	for _, c := range commits {
		if !IsOID(c) {
			return nil, fmt.Errorf("gitx: invalid commit %q", c)
		}
		in.WriteString(c + "\n")
	}
	// The header "/<oid>" cannot be a path: tree paths never start with "/".
	out, err := r.run(ctx, in.Bytes(), "diff-tree", "--stdin", "--always", "-r", "-c", "--root",
		"--no-renames", "--name-only", "-z", "--pretty=format:/%H")
	if err != nil {
		return nil, err
	}
	return parseCommitPaths(out), nil
}

func parseCommitPaths(out []byte) []string {
	seen := map[string]bool{}
	var paths []string
	for _, tok := range bytes.Split(out, []byte{0}) {
		if len(tok) == 0 {
			continue
		}
		if tok[0] == '/' {
			_, rest, ok := bytes.Cut(tok, []byte{'\n'})
			if !ok || len(rest) == 0 {
				continue
			}
			tok = rest
		}
		if p := string(tok); !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	return paths
}

// RevListOptions select commits for RevList.
type RevListOptions struct {
	// Include and Exclude are object names or full ref names.
	Include, Exclude []string
	// NotBranchesTags also excludes everything reachable from branches and tags.
	NotBranchesTags bool
	// FirstParent follows only the first parent of merge commits.
	FirstParent bool
	// Max is the largest number of commits returned; more is ErrLimit.
	Max int
}

// RevList lists commits reachable from o.Include and not from o.Exclude.
func (r *Repo) RevList(ctx context.Context, o RevListOptions) ([]string, error) {
	args := []string{"rev-list", fmt.Sprintf("--max-count=%d", o.Max+1)}
	if o.FirstParent {
		args = append(args, "--first-parent")
	}
	for _, rev := range o.Include {
		if err := checkRev(rev); err != nil {
			return nil, err
		}
		args = append(args, rev)
	}
	for _, rev := range o.Exclude {
		if err := checkRev(rev); err != nil {
			return nil, err
		}
		args = append(args, "^"+rev)
	}
	if o.NotBranchesTags {
		args = append(args, "--not", "--branches", "--tags")
	}
	out, err := r.run(ctx, nil, args...)
	if err != nil {
		return nil, err
	}
	commits := strings.Fields(string(out))
	if len(commits) > o.Max {
		return nil, ErrLimit
	}
	return commits, nil
}

// ErrLimit is returned when a list is longer than the requested maximum.
var ErrLimit = errors.New("gitx: limit exceeded")

func splitZ(out []byte) []string {
	var res []string
	for _, tok := range bytes.Split(out, []byte{0}) {
		if len(tok) > 0 {
			res = append(res, string(tok))
		}
	}
	return res
}

// Discover returns the git directory of the repository that contains dir.
func Discover(ctx context.Context, path, dir string) (string, error) {
	git, err := lookGit(path)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, git, "rev-parse", "--absolute-git-dir") //nolint:gosec // G204: git from PATH, fixed arguments
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + path, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "LC_ALL=C"}
	var stderr limitedBuffer
	stderr.max = maxStderr
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", &Error{Args: []string{"rev-parse"}, Err: err, Stderr: strings.TrimSpace(stderr.String())}
	}
	return strings.TrimSpace(string(out)), nil
}

// ResolveCommit returns the commit a revision such as "origin/main" names.
func (r *Repo) ResolveCommit(ctx context.Context, rev string) (string, error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("gitx: invalid revision %q", rev)
	}
	info, err := r.Info(ctx, rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("gitx: %s: %w", rev, err)
	}
	return info.OID, nil
}
