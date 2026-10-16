package gitx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// Info is the header of an object as cat-file reports it.
type Info struct {
	OID  string
	Type string
	Size int64
	// Mode is the tree entry mode, for example "100644"; set only by Entries.
	Mode string
}

// Entry modes of git trees.
const (
	ModeFile       = "100644"
	ModeExecutable = "100755"
	ModeSymlink    = "120000"
	ModeGitlink    = "160000"
	ModeTree       = "040000"
)

// Errors of object lookups.
var (
	ErrMissing  = errors.New("gitx: object missing")
	ErrTooLarge = errors.New("gitx: object too large")
)

// format is the header of cat-file. It has no %(objectmode): older git, such
// as 2.43 of Ubuntu 24.04 that lint-commit runs on a developer machine, does
// not know it. Entries reads the modes with ls-tree instead.
const format = "%(objecttype) %(objectname) %(objectsize)"

// catFile is the one long-lived "git cat-file --batch-command" of a receive.
// It is not safe for concurrent use: a receive is sequential.
type catFile struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *limitedBuffer
}

func (r *Repo) catFile(ctx context.Context) (*catFile, error) {
	if r.catfile != nil {
		return r.catfile, nil
	}
	cmd := r.command(ctx, "cat-file", "--batch-command="+format)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cf := &catFile{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 64<<10), stderr: &limitedBuffer{max: maxStderr}}
	cmd.Stderr = cf.stderr
	if err := cmd.Start(); err != nil {
		return nil, &Error{Args: []string{"cat-file"}, Err: err}
	}
	r.catfile = cf
	return cf, nil
}

func (c *catFile) close() error {
	closeErr := c.stdin.Close()
	if err := c.cmd.Wait(); err != nil {
		if c.stderr.Len() > 0 {
			return &Error{Args: []string{"cat-file"}, Err: err, Stderr: strings.TrimSpace(c.stderr.String())}
		}
		// Wait after a failed request reports the earlier wait; the failure was returned then.
		return nil
	}
	if closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
		return &Error{Args: []string{"cat-file"}, Err: closeErr}
	}
	return nil
}

// request sends one command and reads the header line.
func (c *catFile) request(cmd, arg string) (string, error) {
	if strings.ContainsAny(arg, "\n\x00") {
		return "", fmt.Errorf("gitx: invalid object name %q", arg)
	}
	if _, err := io.WriteString(c.stdin, cmd+" "+arg+"\n"); err != nil {
		return "", c.failure(err)
	}
	line, err := c.stdout.ReadString('\n')
	if err != nil {
		return "", c.failure(err)
	}
	return strings.TrimSuffix(line, "\n"), nil
}

func (c *catFile) failure(err error) error {
	closeErr := c.stdin.Close()
	werr := c.cmd.Wait()
	if werr == nil {
		werr = errors.Join(err, closeErr)
	}
	return &Error{Args: []string{"cat-file"}, Err: werr, Stderr: strings.TrimSpace(c.stderr.String())}
}

// parseHeader parses "<type> <oid> <size>" and "<spec> missing".
func parseHeader(spec, line string) (Info, error) {
	if strings.HasSuffix(line, " missing") || strings.HasSuffix(line, " ambiguous") {
		return Info{}, ErrMissing
	}
	f := strings.Split(line, " ")
	if len(f) != 3 || !IsOID(f[1]) {
		return Info{}, fmt.Errorf("gitx: unexpected cat-file header %q for %q", line, spec)
	}
	size, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil || size < 0 {
		return Info{}, fmt.Errorf("gitx: unexpected cat-file header %q for %q", line, spec)
	}
	return Info{Type: f[0], OID: f[1], Size: size}, nil
}

// Info returns the header of an object, for example "<commit>:PATHPOLICY" or "<oid>^{}".
func (r *Repo) Info(ctx context.Context, spec string) (Info, error) {
	cf, err := r.catFile(ctx)
	if err != nil {
		return Info{}, err
	}
	line, err := cf.request("info", spec)
	if err != nil {
		return Info{}, err
	}
	return parseHeader(spec, line)
}

// Read returns the type and contents of the object oid, up to max bytes.
func (r *Repo) Read(ctx context.Context, oid string, max int64) (Info, []byte, error) {
	if !IsOID(oid) {
		return Info{}, nil, fmt.Errorf("gitx: invalid object name %q", oid)
	}
	info, err := r.Info(ctx, oid)
	if err != nil {
		return Info{}, nil, err
	}
	if info.Size > max {
		return info, nil, ErrTooLarge
	}
	cf := r.catfile
	line, err := cf.request("contents", oid)
	if err != nil {
		return Info{}, nil, err
	}
	got, err := parseHeader(oid, line)
	if err != nil {
		return Info{}, nil, err
	}
	if got.Size != info.Size {
		return Info{}, nil, fmt.Errorf("gitx: object %s changed size", oid)
	}
	data := make([]byte, got.Size+1)
	if _, err := io.ReadFull(cf.stdout, data); err != nil {
		return Info{}, nil, cf.failure(err)
	}
	if data[got.Size] != '\n' {
		return Info{}, nil, fmt.Errorf("gitx: cat-file output for %s not terminated", oid)
	}
	return got, data[:got.Size], nil
}

// ErrNotCommit is returned when a revision does not peel to a commit.
var ErrNotCommit = errors.New("gitx: object is not a commit")

// Peel returns the commit that rev points to through any chain of tags.
func (r *Repo) Peel(ctx context.Context, rev string) (string, error) {
	if err := checkRev(rev); err != nil {
		return "", err
	}
	info, err := r.Info(ctx, rev+"^{}")
	if err != nil {
		return "", err
	}
	if info.Type != "commit" {
		return "", ErrNotCommit
	}
	return info.OID, nil
}

// Entries returns the tree entries of paths in commit with one ls-tree; a
// path without an entry is not in the map. Mode tells files from
// directories, symlinks and gitlinks.
func (r *Repo) Entries(ctx context.Context, commit string, paths []string) (map[string]Info, error) {
	if !IsOID(commit) {
		return nil, fmt.Errorf("gitx: invalid commit %q", commit)
	}
	for _, p := range paths {
		if p == "" || strings.ContainsAny(p, "\n\x00") || strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("gitx: invalid path %q", p)
		}
	}
	entries := map[string]Info{}
	if len(paths) == 0 {
		return entries, nil
	}
	out, err := r.run(ctx, nil, append([]string{"ls-tree", "-z", "-l", "--full-tree", commit, "--"}, paths...)...)
	if err != nil {
		return nil, err
	}
	for _, rec := range splitZ(out) {
		info, name, err := parseLsTree(rec)
		if err != nil {
			return nil, err
		}
		if slices.Contains(paths, name) {
			entries[name] = info
		}
	}
	return entries, nil
}

// parseLsTree parses "<mode> <type> <oid> <size>\t<path>" of ls-tree -l.
func parseLsTree(rec string) (Info, string, error) {
	meta, name, ok := strings.Cut(rec, "\t")
	f := strings.Fields(meta)
	if !ok || len(f) != 4 || !IsOID(f[2]) {
		return Info{}, "", fmt.Errorf("gitx: unexpected ls-tree record %q", rec)
	}
	info := Info{Mode: f[0], Type: f[1], OID: f[2]}
	if f[3] != "-" {
		size, err := strconv.ParseInt(f[3], 10, 64)
		if err != nil || size < 0 {
			return Info{}, "", fmt.Errorf("gitx: unexpected ls-tree record %q", rec)
		}
		info.Size = size
	}
	return info, name, nil
}
