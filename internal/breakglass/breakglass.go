// Package breakglass grants, revokes and checks break-glass for a project
// (architecture §18.6). A grant is a file that only root may write; while it
// is valid the hook does not enforce policies for the project.
package breakglass

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// MaxTTL is the longest grant.
const MaxTTL = 24 * time.Hour

// maxFile bounds a grant file.
const maxFile = 4096

// clockSkew is how far in the future a grant may start, for clocks of several nodes.
const clockSkew = 5 * time.Minute

// Grant is the content of a grant file.
type Grant struct {
	ProjectID int64     `json:"project_id"`
	GrantedAt time.Time `json:"granted_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Reason    string    `json:"reason"`
	Operator  string    `json:"operator"`
}

// Valid reports whether the grant is in force at now.
func (g Grant) Valid(now time.Time) bool {
	ttl := g.ExpiresAt.Sub(g.GrantedAt)
	return ttl > 0 && ttl <= MaxTTL && !g.GrantedAt.After(now.Add(clockSkew)) && now.Before(g.ExpiresAt)
}

// Text describes the grant for audit and output.
func (g Grant) Text() string {
	return fmt.Sprintf("%s; operator %s; expires %s", g.Reason, g.Operator, g.ExpiresAt.UTC().Format(time.RFC3339))
}

// Store is the break-glass directory.
type Store struct {
	Dir string
	// TrustedUID owns the directory and the grants: root, 0, outside of tests.
	TrustedUID int
}

func (s Store) path(projectID int64) string {
	return filepath.Join(s.Dir, strconv.FormatInt(projectID, 10))
}

// Active implements core.Breakglass: a grant counts only when the file and the
// directory are owned by TrustedUID and writable by nobody else.
func (s Store) Active(projectID int64, now time.Time) (string, bool) {
	if projectID <= 0 || s.trusted(s.Dir, true) != nil {
		return "", false
	}
	g, err := s.read(s.path(projectID))
	if err != nil || g.ProjectID != projectID || !g.Valid(now) {
		return "", false
	}
	return g.Text(), true
}

// trusted checks that p is not a symlink, has the expected type and is owned
// by TrustedUID without write access for group and others.
func (s Store) trusted(p string, dir bool) error {
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	switch {
	case dir && !info.IsDir(), !dir && !info.Mode().IsRegular():
		return fmt.Errorf("%s: unexpected file type %s", p, info.Mode().Type())
	case info.Mode().Perm()&0o022 != 0:
		return fmt.Errorf("%s: writable by group or others (%s)", p, info.Mode().Perm())
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != s.TrustedUID {
		return fmt.Errorf("%s: not owned by uid %d", p, s.TrustedUID)
	}
	return nil
}

func (s Store) read(p string) (Grant, error) {
	if err := s.trusted(p, false); err != nil {
		return Grant{}, err
	}
	f, err := os.Open(p) //nolint:gosec // G304: the name is a project ID inside the configured directory
	if err != nil {
		return Grant{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Grant{}, err
	}
	if len(data) > maxFile {
		return Grant{}, fmt.Errorf("%s: larger than %d bytes", p, maxFile)
	}
	var g Grant
	if err := json.Unmarshal(data, &g); err != nil {
		return Grant{}, fmt.Errorf("%s: %w", p, err)
	}
	return g, nil
}

// Grant writes a grant for the project, replacing an existing one.
func (s Store) Grant(g Grant) error {
	switch ttl := g.ExpiresAt.Sub(g.GrantedAt); {
	case g.ProjectID <= 0:
		return errors.New("the project ID must be positive")
	case ttl <= 0 || ttl > MaxTTL:
		return fmt.Errorf("the TTL must be positive and at most %s", MaxTTL)
	case strings.TrimSpace(g.Reason) == "":
		return errors.New("a reason is required")
	case strings.TrimSpace(g.Operator) == "":
		return errors.New("the operator is unknown")
	}
	if err := s.trusted(s.Dir, true); err != nil {
		return fmt.Errorf("break-glass directory: %w", err)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	if len(data) >= maxFile {
		return errors.New("the reason is too long")
	}
	return writeFile(s.Dir, s.path(g.ProjectID), append(data, '\n'))
}

// writeFile replaces target with data through a temporary file in dir, so
// that the hook never reads a partial grant.
func writeFile(dir, target string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".grant-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(0o644)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	return nil
}

// Revoke removes the grant of the project; it reports whether there was one.
func (s Store) Revoke(projectID int64) (bool, error) {
	err := os.Remove(s.path(projectID))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Entry is a grant file as list shows it.
type Entry struct {
	Name  string
	Grant Grant
	// Err explains why the file is not a usable grant.
	Err error
}

// List returns all grant files in name order, valid or not.
func (s Store) List() ([]Entry, error) {
	if err := s.trusted(s.Dir, true); err != nil {
		return nil, fmt.Errorf("break-glass directory: %w", err)
	}
	names, err := os.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, d := range names {
		if strings.HasPrefix(d.Name(), ".") {
			continue
		}
		e := Entry{Name: d.Name()}
		e.Grant, e.Err = s.read(filepath.Join(s.Dir, d.Name()))
		if id, perr := strconv.ParseInt(d.Name(), 10, 64); e.Err == nil && (perr != nil || id != e.Grant.ProjectID) {
			e.Err = errors.New("the file name is not the project ID of the grant")
		}
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}
