package token

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/gitlab"
)

var now = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

type fakeAPI struct {
	expires   string
	rotateErr error
	// revoked makes TokenSelf fail after RotateToken was called.
	revoked bool
	asked   []string
}

func (f *fakeAPI) TokenSelf(context.Context) (gitlab.PersonalToken, error) {
	if f.revoked && len(f.asked) > 0 {
		return gitlab.PersonalToken{}, &gitlab.Error{Kind: gitlab.Unauthorized, Status: 401, Path: "/api/v4/personal_access_tokens/self"}
	}
	return gitlab.PersonalToken{ExpiresAt: f.expires, Active: true}, nil
}

func (f *fakeAPI) RotateToken(_ context.Context, expiresAt string) (string, gitlab.PersonalToken, error) {
	f.asked = append(f.asked, expiresAt)
	if f.rotateErr != nil {
		return "", gitlab.PersonalToken{}, f.rotateErr
	}
	return "glpat-new", gitlab.PersonalToken{ExpiresAt: expiresAt}, nil
}

type chowns struct{ calls []string }

func (c *chowns) chown(path string, uid, gid int) error {
	c.calls = append(c.calls, filepath.Base(path))
	if uid != os.Getuid() || gid != os.Getgid() {
		return errors.New("not the owner of the old file")
	}
	return nil
}

func setup(t *testing.T) (string, *chowns, Options) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(file, []byte("glpat-old\n"), 0o600))
	require.NoError(t, os.Chmod(file, 0o440))
	c := &chowns{}
	return file, c, Options{File: file, Before: 30 * 24 * time.Hour, Lifetime: 364 * 24 * time.Hour, Now: now, Chown: c.chown}
}

func leftovers(t *testing.T, file string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(file))
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestRotate_NotDue(t *testing.T) {
	file, _, o := setup(t)
	api := &fakeAPI{expires: "2026-11-14"}
	res, err := Rotate(context.Background(), api, o)
	require.NoError(t, err)
	assert.False(t, res.Rotated)
	assert.Empty(t, api.asked)
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "glpat-old\n", string(data))
}

func TestRotate_Due(t *testing.T) {
	for name, expires := range map[string]string{"within 30 days": "2026-11-12", "no expiry": "", "expired": "2026-10-01"} {
		t.Run(name, func(t *testing.T) {
			file, c, o := setup(t)
			api := &fakeAPI{expires: expires}
			res, err := Rotate(context.Background(), api, o)
			require.NoError(t, err)
			assert.True(t, res.Rotated)
			assert.Equal(t, []string{"2027-10-13"}, api.asked, "always an explicit expires_at: today + token_lifetime")
			assert.Equal(t, "2027-10-13", res.NewExpiry)
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			assert.Equal(t, "glpat-new\n", string(data))
			info, err := os.Stat(file)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o440), info.Mode().Perm(), "the mode of the old file")
			require.Len(t, c.calls, 1)
			assert.True(t, strings.HasPrefix(c.calls[0], ".token-"), "the temporary file gets the owner before the token is written")
			assert.Equal(t, []string{"token"}, leftovers(t, file))
		})
	}
}

func TestRotate_Refused(t *testing.T) {
	file, _, o := setup(t)
	api := &fakeAPI{expires: "2026-10-20", rotateErr: &gitlab.Error{Kind: gitlab.Unexpected, Status: http.StatusBadRequest,
		Path: "/api/v4/personal_access_tokens/self/rotate", Err: errors.New("Expiration date must be before 2027-10-01")}}
	res, err := Rotate(context.Background(), api, o)
	require.ErrorIs(t, err, ErrRefused)
	assert.ErrorContains(t, err, "Expiration date must be before")
	assert.False(t, res.Rotated)
	data, rerr := os.ReadFile(file)
	require.NoError(t, rerr)
	assert.Equal(t, "glpat-old\n", string(data), "the current token stays")
	assert.Equal(t, []string{"token"}, leftovers(t, file))

	api.rotateErr = &gitlab.Error{Kind: gitlab.Unavailable, Status: http.StatusBadGateway}
	_, err = Rotate(context.Background(), api, o)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRefused)
}

func TestRotate_KeepsPendingFile(t *testing.T) {
	_, _, o := setup(t)
	// A directory in place of the file: the rename after the rotation fails.
	dir := filepath.Join(t.TempDir(), "tokens")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "token", "x"), 0o700))
	o.File = filepath.Join(dir, "token")
	res, err := Rotate(context.Background(), &fakeAPI{expires: "2026-10-20"}, o)
	require.Error(t, err)
	assert.True(t, res.Rotated)
	require.NotEmpty(t, res.Pending)
	data, rerr := os.ReadFile(res.Pending)
	require.NoError(t, rerr)
	assert.Equal(t, "glpat-new\n", string(data), "the new token is not lost")
	assert.Contains(t, err.Error(), res.Pending)
}

func TestRotate_ChownFailure(t *testing.T) {
	file, _, o := setup(t)
	o.Chown = func(string, int, int) error { return errors.New("chown denied") }
	api := &fakeAPI{expires: "2026-10-20"}
	_, err := Rotate(context.Background(), api, o)
	require.ErrorContains(t, err, "chown denied")
	assert.Empty(t, api.asked, "nothing is rotated when the new file cannot be prepared")
	assert.Equal(t, []string{"token"}, leftovers(t, file))
}

// TestRotate_LostReply: GitLab gave no usable reply to the rotation; the error
// says whether the current token still works.
func TestRotate_LostReply(t *testing.T) {
	lost := &gitlab.Error{Kind: gitlab.Unavailable, Path: "/api/v4/personal_access_tokens/self/rotate", Err: errors.New("read: connection reset")}
	for name, tc := range map[string]struct {
		err     error
		revoked bool
		want    string
	}{
		"no reply, old token works":   {lost, false, "the current token still works"},
		"no reply, old token revoked": {lost, true, "issue a new token"},
		"5xx, old token revoked":      {&gitlab.Error{Kind: gitlab.Unavailable, Status: 502}, true, "issue a new token"},
	} {
		t.Run(name, func(t *testing.T) {
			file, _, o := setup(t)
			api := &fakeAPI{expires: "2026-10-20", rotateErr: tc.err, revoked: tc.revoked}
			res, err := Rotate(context.Background(), api, o)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.False(t, res.Rotated)
			assert.False(t, errors.Is(err, ErrRefused))
			assert.Equal(t, []string{"token"}, leftovers(t, file))
		})
	}
}
