package hookenv

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

var baseEnv = []string{
	"GL_ID=user-2", "GL_USERNAME=alice", "GL_PROTOCOL=http", "GL_REPOSITORY=project-1",
	"GL_PROJECT_PATH=acme/platform/app", "GIT_DIR=/repos/1.git",
	"GIT_OBJECT_DIRECTORY=/repos/1.git/objects/tmp_objdir-incoming-x",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES=/repos/1.git/objects", "PATH=/gitaly/git:/usr/bin",
}

func TestParseEnv(t *testing.T) {
	e, err := ParseEnv(baseEnv)
	require.NoError(t, err)
	assert.Equal(t, module.Principal{GLID: "user-2", Username: "alice"}, e.Principal)
	assert.Equal(t, int64(1), e.ProjectID)
	assert.True(t, e.IsProject())
	assert.Equal(t, "acme/platform/app", e.ProjectPath)
	assert.Equal(t, GitEnv{Dir: "/repos/1.git", ObjectDirectory: "/repos/1.git/objects/tmp_objdir-incoming-x",
		AlternateObjectDirs: "/repos/1.git/objects", Path: "/gitaly/git:/usr/bin"}, e.Git)
}

func TestParseEnv_NonProject(t *testing.T) {
	for _, repo := range []string{"wiki-1", "design-1", "snippet-1"} {
		env := append(append([]string{}, baseEnv...), "GL_REPOSITORY="+repo)
		e, err := ParseEnv(env)
		require.NoError(t, err)
		assert.False(t, e.IsProject(), repo)
	}
}

func TestParseEnv_Invalid(t *testing.T) {
	_, err := ParseEnv([]string{"GL_ID=user-2"})
	assert.ErrorContains(t, err, "GL_USERNAME, GL_PROTOCOL, GL_REPOSITORY, GL_PROJECT_PATH, GIT_DIR not set")
	for _, repo := range []string{"project-", "project-0", "project-01", "project-x"} {
		_, err = ParseEnv(append(append([]string{}, baseEnv...), "GL_REPOSITORY="+repo))
		assert.ErrorContains(t, err, "is not project-<id>", repo)
	}
}

func TestParse_IgnoresPushOptions(t *testing.T) {
	env := append(append([]string{}, baseEnv...), "GIT_PUSH_OPTION_COUNT=2",
		"GIT_PUSH_OPTION_0=skip-policy", "GIT_PUSH_OPTION_1=GL_USERNAME=root", "GL_USERNAME=alice")
	e, err := ParseEnv(env)
	require.NoError(t, err)
	plain, err := ParseEnv(baseEnv)
	require.NoError(t, err)
	assert.Equal(t, plain, e)
}

func TestParseUpdates(t *testing.T) {
	z, a, b := strings.Repeat("0", 40), strings.Repeat("a", 40), strings.Repeat("b", 40)
	in := z + " " + a + " refs/heads/new\n" + a + " " + b + " refs/heads/main\n" + b + " " + z + " refs/tags/v1\n"
	got, err := ParseUpdates(strings.NewReader(in))
	require.NoError(t, err)
	assert.Equal(t, []module.RefUpdate{
		{Old: z, New: a, Ref: "refs/heads/new"},
		{Old: a, New: b, Ref: "refs/heads/main"},
		{Old: b, New: z, Ref: "refs/tags/v1"},
	}, got)

	z64, a64 := strings.Repeat("0", 64), strings.Repeat("a", 64)
	got, err = ParseUpdates(strings.NewReader(z64 + " " + a64 + " refs/heads/main"))
	require.NoError(t, err)
	assert.Equal(t, module.Create, got[0].Kind())
}

func TestParseUpdates_Invalid(t *testing.T) {
	z, a := strings.Repeat("0", 40), strings.Repeat("a", 40)
	tests := map[string]string{
		"":                                       "no ref updates",
		a + " " + a:                              "expected",
		a + " " + a + " refs/heads/x extra":      "expected",
		a + "  " + a + " refs/heads/x":           "expected",
		strings.ToUpper(a) + " " + a + " refs/x": "lowercase hex",
		a + " " + strings.Repeat("a", 64) + " r": "same length",
		z + " " + z + " refs/heads/x":            "both revisions are null",
		a + " " + a + " heads/x":                 "invalid ref",
		a + " " + a + " refs/x\n" + a + " " + a + " refs/x": "repeated",
	}
	for in, want := range tests {
		_, err := ParseUpdates(strings.NewReader(in))
		assert.ErrorContains(t, err, want, in)
	}
	big := bytes.Repeat([]byte(a+" "+a+" refs/heads/"+strings.Repeat("x", 100)+"\n"), MaxStdin/150)
	_, err := ParseUpdates(bytes.NewReader(big))
	assert.Error(t, err)
}

func FuzzParse(f *testing.F) {
	z, a := strings.Repeat("0", 40), strings.Repeat("a", 40)
	f.Add([]byte(z+" "+a+" refs/heads/main\n"), "user-2", "project-1")
	f.Add([]byte(a+" "+z+" refs/tags/v1\r\n"), "key-1", "wiki-1")
	f.Fuzz(func(t *testing.T, stdin []byte, glid, repo string) {
		updates, err := ParseUpdates(bytes.NewReader(stdin))
		if err == nil {
			for _, u := range updates {
				if !strings.HasPrefix(u.Ref, "refs/") || len(u.Old) != len(u.New) {
					t.Fatalf("accepted invalid update %+v", u)
				}
			}
		}
		env := append(append([]string{}, baseEnv...), "GL_ID="+glid, "GL_REPOSITORY="+repo)
		if e, err := ParseEnv(env); err == nil && strings.HasPrefix(repo, "project-") && !e.IsProject() {
			t.Fatalf("project repository %q not recognised", repo)
		}
	})
}
