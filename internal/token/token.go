// Package token rotates the token of the service account. GitLab revokes the old
// token as soon as it issues the new one, so the new token is written to a
// temporary file in the same directory, synced and renamed over the old file; if
// that fails, the temporary file is kept.
package token

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
)

// API is the part of the GitLab client that rotation uses.
type API interface {
	TokenSelf(ctx context.Context) (gitlab.PersonalToken, error)
	RotateToken(ctx context.Context, expiresAt string) (string, gitlab.PersonalToken, error)
}

// Options of a rotation.
type Options struct {
	// File is token_file of the server config.
	File string
	// Before: rotate when the token expires within this time.
	Before time.Duration
	// Lifetime is token_lifetime: the new token expires this long after now.
	Lifetime time.Duration
	Now      time.Time
	// Chown gives the new file the owner of the old one.
	Chown func(path string, uid, gid int) error
}

// Result of a rotation.
type Result struct {
	Rotated bool
	// OldExpiry and NewExpiry are dates, YYYY-MM-DD; OldExpiry is empty for a token without expiry.
	OldExpiry string
	NewExpiry string
	// Pending is a temporary file that holds the new token when it could not
	// replace File; the old token no longer works at that point.
	Pending string
}

// ErrRefused means GitLab refused to rotate, for example because the expiry
// is beyond the maximum of the instance; the current token stays valid.
var ErrRefused = errors.New("GitLab refused to rotate the token; the current token stays valid")

// Rotate rotates the token when it is due.
func Rotate(ctx context.Context, api API, o Options) (Result, error) {
	info, err := os.Stat(o.File)
	if err != nil {
		return Result{}, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return Result{}, errors.New("cannot read the owner of the token file")
	}
	self, err := api.TokenSelf(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("the current token: %w", err)
	}
	res := Result{OldExpiry: self.ExpiresAt}
	if exp, err := time.Parse(time.DateOnly, self.ExpiresAt); err == nil && exp.Sub(o.Now) > o.Before {
		return res, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(o.File), ".token-*")
	if err != nil {
		return res, err
	}
	if err := prepare(tmp, o.Chown, int(st.Uid), int(st.Gid)); err != nil {
		return res, errors.Join(err, tmp.Close(), os.Remove(tmp.Name()))
	}
	res.NewExpiry = o.Now.UTC().Add(o.Lifetime).Format(time.DateOnly)
	secret, _, err := api.RotateToken(ctx, res.NewExpiry)
	if err != nil {
		cerr := errors.Join(tmp.Close(), os.Remove(tmp.Name()))
		var apiErr *gitlab.Error
		switch {
		case errors.As(err, &apiErr) && apiErr.Status == 400:
			err = fmt.Errorf("%w: %w", ErrRefused, err)
		case !errors.As(err, &apiErr) || apiErr.Status == 0 || apiErr.Status >= 500:
			err = checkAfterLostReply(ctx, api, err)
		}
		return res, errors.Join(err, cerr)
	}
	res.Rotated = true
	if err := commit(tmp, o.File, secret, info.Mode().Perm()); err != nil {
		res.Pending = tmp.Name()
		return res, fmt.Errorf("the new token is in %s, move it to %s: %w", tmp.Name(), o.File, err)
	}
	if err := syncDir(filepath.Dir(o.File)); err != nil {
		return res, fmt.Errorf("the new token is in place, but its directory was not synced: %w", err)
	}
	return res, nil
}

// checkAfterLostReply handles a rotation without a usable reply: GitLab may
// have issued a new token, which is lost, and revoked the current one. The
// current token is checked so that the error says which case it is.
func checkAfterLostReply(ctx context.Context, api API, err error) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if _, serr := api.TokenSelf(ctx); serr != nil {
		return fmt.Errorf("%w; the current token does not work either: GitLab may have rotated it, issue a new token: %w", err, serr)
	}
	return fmt.Errorf("%w; the current token still works", err)
}

// prepare makes the temporary file private and gives it the owner of the token file.
func prepare(tmp *os.File, chown func(string, int, int) error, uid, gid int) error {
	if err := tmp.Chmod(0o400); err != nil {
		return err
	}
	return chown(tmp.Name(), uid, gid)
}

// commit writes the token, syncs it, sets the mode of the old file and
// renames the temporary file over it.
func commit(tmp *os.File, target, secret string, mode os.FileMode) error {
	_, err := tmp.WriteString(secret + "\n")
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	return err
}

// syncDir makes the rename durable.
func syncDir(path string) error {
	dir, err := os.Open(path) //nolint:gosec // G304: the directory of token_file
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
