package breakglass

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var t0 = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func newStore(t *testing.T) Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "breakglass")
	require.NoError(t, os.Mkdir(dir, 0o755))
	return Store{Dir: dir, TrustedUID: os.Getuid()}
}

func grant(id int64, ttl time.Duration) Grant {
	return Grant{ProjectID: id, GrantedAt: t0, ExpiresAt: t0.Add(ttl), Reason: "INC-123 policy lockout", Operator: "alice"}
}

func TestGrantActiveRevoke(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.Grant(grant(812, 30*time.Minute)))

	reason, ok := s.Active(812, t0.Add(time.Minute))
	assert.True(t, ok)
	assert.Equal(t, "INC-123 policy lockout; operator alice; expires 2026-10-14T09:30:00Z", reason)
	_, ok = s.Active(813, t0.Add(time.Minute))
	assert.False(t, ok, "another project")
	_, ok = s.Active(812, t0.Add(30*time.Minute))
	assert.False(t, ok, "expired")
	_, ok = s.Active(812, t0.Add(-10*time.Minute))
	assert.False(t, ok, "granted in the future beyond the clock skew")

	info, err := os.Stat(s.path(812))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	entries, err := os.ReadDir(s.Dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary files are left")

	had, err := s.Revoke(812)
	require.NoError(t, err)
	assert.True(t, had)
	_, ok = s.Active(812, t0.Add(time.Minute))
	assert.False(t, ok)
	had, err = s.Revoke(812)
	require.NoError(t, err)
	assert.False(t, had)
}

func TestGrant_Rejects(t *testing.T) {
	s := newStore(t)
	for name, g := range map[string]Grant{
		"no project":   grant(0, time.Hour),
		"zero ttl":     grant(1, 0),
		"ttl over 24h": grant(1, MaxTTL+time.Second),
		"no reason":    {ProjectID: 1, GrantedAt: t0, ExpiresAt: t0.Add(time.Hour), Reason: " ", Operator: "alice"},
		"no operator":  {ProjectID: 1, GrantedAt: t0, ExpiresAt: t0.Add(time.Hour), Reason: "x"},
	} {
		assert.Error(t, s.Grant(g), name)
	}
	require.NoError(t, s.Grant(grant(1, MaxTTL)), "exactly 24h is allowed")
	require.NoError(t, os.Chmod(s.Dir, 0o775))
	assert.ErrorContains(t, s.Grant(grant(2, time.Hour)), "writable by group or others")
}

// write puts a grant file in place by hand, as an attacker or a broken tool would.
func write(t *testing.T, s Store, name string, g Grant, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(s.Dir, name)
	data, err := json.Marshal(g)
	require.NoError(t, err)
	require.NoError(t, writeFile(s.Dir, p, data))
	require.NoError(t, os.Chmod(p, mode))
}

func TestActive_Ignores(t *testing.T) {
	now := t0.Add(time.Minute)
	t.Run("ttl over 24h written by hand", func(t *testing.T) {
		s := newStore(t)
		write(t, s, "1", grant(1, 48*time.Hour), 0o644)
		_, ok := s.Active(1, now)
		assert.False(t, ok)
	})
	t.Run("group writable file", func(t *testing.T) {
		s := newStore(t)
		write(t, s, "1", grant(1, time.Hour), 0o664)
		_, ok := s.Active(1, now)
		assert.False(t, ok)
	})
	t.Run("group writable directory", func(t *testing.T) {
		s := newStore(t)
		write(t, s, "1", grant(1, time.Hour), 0o644)
		require.NoError(t, os.Chmod(s.Dir, 0o777))
		_, ok := s.Active(1, now)
		assert.False(t, ok)
	})
	t.Run("not owned by the trusted user", func(t *testing.T) {
		s := newStore(t)
		write(t, s, "1", grant(1, time.Hour), 0o644)
		s.TrustedUID = os.Getuid() + 1
		_, ok := s.Active(1, now)
		assert.False(t, ok)
	})
	t.Run("symlink", func(t *testing.T) {
		s := newStore(t)
		write(t, s, "2", grant(1, time.Hour), 0o644)
		require.NoError(t, os.Symlink(filepath.Join(s.Dir, "2"), filepath.Join(s.Dir, "1")))
		_, ok := s.Active(1, now)
		assert.False(t, ok)
	})
	t.Run("file of another project", func(t *testing.T) {
		s := newStore(t)
		write(t, s, "1", grant(2, time.Hour), 0o644)
		_, ok := s.Active(1, now)
		assert.False(t, ok)
	})
	t.Run("not JSON", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, os.WriteFile(filepath.Join(s.Dir, "1"), []byte("yes"), 0o644))
		_, ok := s.Active(1, now)
		assert.False(t, ok)
	})
	t.Run("no directory", func(t *testing.T) {
		s := Store{Dir: filepath.Join(t.TempDir(), "missing"), TrustedUID: os.Getuid()}
		_, ok := s.Active(1, now)
		assert.False(t, ok)
	})
}

func TestList(t *testing.T) {
	s := newStore(t)
	require.NoError(t, s.Grant(grant(2, time.Hour)))
	require.NoError(t, s.Grant(grant(1, time.Hour)))
	write(t, s, "3", grant(4, time.Hour), 0o644)
	write(t, s, "5", grant(5, time.Hour), 0o666)

	entries, err := s.List()
	require.NoError(t, err)
	require.Len(t, entries, 4)
	assert.Equal(t, "1", entries[0].Name)
	require.NoError(t, entries[0].Err)
	assert.Equal(t, int64(1), entries[0].Grant.ProjectID)
	assert.NoError(t, entries[1].Err)
	assert.ErrorContains(t, entries[2].Err, "not the project ID")
	assert.ErrorContains(t, entries[3].Err, "writable by group or others")

	require.NoError(t, os.Chmod(s.Dir, 0o777))
	_, err = s.List()
	assert.Error(t, err)
}
