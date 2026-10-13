package gitx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Info is the header of an object as cat-file reports it.
type Info struct {
	OID  string
	Type string
	Size int64
	// Mode is the tree entry mode, for example "100644"; set only for <rev>:<path> lookups.
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

const (
	formatMode  = "%(objecttype) %(objectname) %(objectsize) %(objectmode)"
	formatPlain = "%(objecttype) %(objectname) %(objectsize)"
)

// catFile is the one long-lived "git cat-file --batch-command" of a receive.
type catFile struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *limitedBuffer
	mode   bool
}

func (r *Repo) catFile(ctx context.Context) (*catFile, error) {
	if r.catfile != nil {
		return r.catfile, nil
	}
	cf, err := r.startCatFile(ctx, !r.noMode)
	if err != nil {
		return nil, err
	}
	r.catfile = cf
	return cf, nil
}

func (r *Repo) startCatFile(ctx context.Context, mode bool) (*catFile, error) {
	format := formatPlain
	if mode {
		format = formatMode
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
	cf := &catFile{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 64<<10), stderr: &limitedBuffer{max: maxStderr}, mode: mode}
	cmd.Stderr = cf.stderr
	if err := cmd.Start(); err != nil {
		return nil, &Error{Args: []string{"cat-file"}, Err: err}
	}
	return cf, nil
}

func (c *catFile) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
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

// parseHeader parses "<type> <oid> <size>[ <mode>]", "<spec> missing" and,
// with %(objectmode), "<oid> submodule" for gitlinks.
func (c *catFile) parseHeader(spec, line string) (Info, error) {
	if strings.HasSuffix(line, " missing") || strings.HasSuffix(line, " ambiguous") {
		return Info{}, ErrMissing
	}
	f := strings.Split(line, " ")
	if len(f) == 2 && f[1] == "submodule" && IsOID(f[0]) {
		return Info{OID: f[0], Type: "commit", Mode: ModeGitlink}, nil
	}
	want := 3
	if c.mode {
		want = 4
	}
	if len(f) != want || !IsOID(f[1]) {
		return Info{}, fmt.Errorf("gitx: unexpected cat-file header %q for %q", line, spec)
	}
	size, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil || size < 0 {
		return Info{}, fmt.Errorf("gitx: unexpected cat-file header %q for %q", line, spec)
	}
	info := Info{Type: f[0], OID: f[1], Size: size}
	if c.mode {
		info.Mode = f[3]
	}
	return info, nil
}

// Info returns the header of an object, for example "<commit>:PATHPOLICY" or "<oid>^{}".
func (r *Repo) Info(ctx context.Context, spec string) (Info, error) {
	cf, err := r.catFile(ctx)
	if err != nil {
		return Info{}, err
	}
	cf.mu.Lock()
	line, err := cf.request("info", spec)
	cf.mu.Unlock()
	if err != nil && cf.mode && strings.Contains(err.Error(), "objectmode") {
		// git without %(objectmode): restart once without it.
		if cf, err = r.startCatFile(ctx, false); err != nil {
			return Info{}, err
		}
		r.catfile = cf
		cf.mu.Lock()
		line, err = cf.request("info", spec)
		cf.mu.Unlock()
	}
	if err != nil {
		return Info{}, err
	}
	return cf.parseHeader(spec, line)
}

// ObjectModeSupported reports whether entry modes come from cat-file; it starts cat-file when needed.
func (r *Repo) ObjectModeSupported(ctx context.Context) (bool, error) {
	if _, err := r.Info(ctx, "HEAD"); err != nil && !errors.Is(err, ErrMissing) {
		return false, err
	}
	return r.catfile.mode, nil
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
	cf.mu.Lock()
	defer cf.mu.Unlock()
	line, err := cf.request("contents", oid)
	if err != nil {
		return Info{}, nil, err
	}
	got, err := cf.parseHeader(oid, line)
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

// Entry returns the tree entry of path in commit; ok is false when there is none.
// Mode tells files from directories, symlinks and gitlinks.
func (r *Repo) Entry(ctx context.Context, commit, path string) (Info, bool, error) {
	if !IsOID(commit) {
		return Info{}, false, fmt.Errorf("gitx: invalid commit %q", commit)
	}
	if path == "" || strings.ContainsAny(path, "\n\x00") || strings.HasPrefix(path, "/") {
		return Info{}, false, fmt.Errorf("gitx: invalid path %q", path)
	}
	if mode, err := r.ObjectModeSupported(ctx); err != nil {
		return Info{}, false, err
	} else if mode {
		info, err := r.Info(ctx, commit+":"+path)
		if errors.Is(err, ErrMissing) {
			return Info{}, false, nil
		}
		return info, err == nil, err
	}
	out, err := r.run(ctx, nil, "ls-tree", "-z", "-l", "--full-tree", commit, "--", path)
	if err != nil {
		return Info{}, false, err
	}
	for _, rec := range splitZ(out) {
		info, name, err := parseLsTree(rec)
		if err != nil {
			return Info{}, false, err
		}
		if name == path {
			return info, true, nil
		}
	}
	return Info{}, false, nil
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
