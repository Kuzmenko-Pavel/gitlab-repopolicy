//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// historyExample reads a policy of examples/history-new-content or
// examples/history-incoming.
func historyExample(t *testing.T, history, file string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "history-"+history, file))
	require.NoError(t, err)
	return string(data)
}

// historyCase is one sequence of actions, checked with check_history:
// new-content and incoming; the last push of the sequence is the one that counts.
type historyCase struct {
	name       string
	push       func(s *Stand, base string) Result
	newContent bool
	incoming   bool
}

func runHistoryCases(t *testing.T, file string, files func(policy string) map[string]string, cases []historyCase) {
	t.Helper()
	for _, tc := range cases {
		for _, mode := range []struct {
			history string
			allow   bool
		}{{"new-content", tc.newContent}, {"incoming", tc.incoming}} {
			t.Run(tc.name+"/"+mode.history, func(t *testing.T) {
				s, base := mainWith(t, files(historyExample(t, mode.history, file)))
				s.Team()
				r := tc.push(s, base)
				if mode.allow {
					assertAllowed(t, r)
				} else {
					assertDenied(t, r, "rule")
				}
			})
		}
	}
}

// aliceFeature: alice pushes ci.yml v2 to feature, made from main.
func aliceFeature(t *testing.T, s *Stand) {
	t.Helper()
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	s.Commit("ci: v2", map[string]string{"ci.yml": "v2"})
	assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
}

// mainMovesOn: bob pushes a change of app.go to main, the work clone is on feature.
func mainMovesOn(t *testing.T, s *Stand) {
	t.Helper()
	s.git(s.Work, "checkout", "-q", "main")
	s.Commit("feat: app", map[string]string{"app.go": "b"})
	assertAllowed(t, s.Push(bob, "HEAD:refs/heads/main"))
	s.git(s.Work, "checkout", "-q", "feature")
}

// TestCheckHistory_PathPolicy: the same actions of bob on a change of ci.yml
// that alice pushed, under examples/history-new-content and history-incoming.
func TestCheckHistory_PathPolicy(t *testing.T) {
	files := func(policy string) map[string]string {
		return map[string]string{"PATHPOLICY": policy, "ci.yml": "v1", "app.go": "a"}
	}
	runHistoryCases(t, "PATHPOLICY", files, []historyCase{
		{"bob pushes his own version of ci.yml to feature", func(s *Stand, _ string) Result {
			s.git(s.Work, "checkout", "-q", "-b", "feature")
			s.Commit("ci: mine", map[string]string{"ci.yml": "bob"})
			return s.Push(bob, "HEAD:refs/heads/feature")
		}, false, false},
		{"bob rebases feature onto main", func(s *Stand, _ string) Result {
			aliceFeature(t, s)
			mainMovesOn(t, s)
			s.git(s.Work, "rebase", "-q", "main")
			return s.Push(bob, "+HEAD:refs/heads/feature")
		}, true, false},
		{"bob squashes feature", func(s *Stand, _ string) Result {
			aliceFeature(t, s)
			s.Commit("test: app", map[string]string{"app_test.go": "t"})
			assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
			s.git(s.Work, "reset", "-q", "--soft", "HEAD~2")
			s.git(s.Work, "commit", "-q", "-m", "feat: ci and tests")
			return s.Push(bob, "+HEAD:refs/heads/feature")
		}, true, false},
		{"bob merges feature into main", func(s *Stand, _ string) Result {
			aliceFeature(t, s)
			mainMovesOn(t, s)
			s.git(s.Work, "checkout", "-q", "main")
			s.git(s.Work, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature' into 'main'", "feature")
			return s.Push(bob, "HEAD:refs/heads/main")
		}, true, false},
		{"bob fast-forwards main to feature", func(s *Stand, _ string) Result {
			aliceFeature(t, s)
			return s.Push(bob, "HEAD:refs/heads/main")
		}, true, false},
		{"bob squash-merges feature into main", func(s *Stand, _ string) Result {
			aliceFeature(t, s)
			mainMovesOn(t, s)
			s.git(s.Work, "checkout", "-q", "main")
			s.git(s.Work, "merge", "-q", "--squash", "feature")
			s.git(s.Work, "commit", "-q", "-m", "ci: v2")
			return s.Push(bob, "HEAD:refs/heads/main")
		}, false, false},
		{"bob creates feature-2 from feature", func(s *Stand, _ string) Result {
			aliceFeature(t, s)
			return s.Push(bob, "HEAD:refs/heads/feature-2")
		}, true, false},
		{"bob rolls main back over the change of alice", func(s *Stand, base string) Result {
			s.Commit("ci: v2", map[string]string{"ci.yml": "v2"})
			assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
			return s.Push(bob, "+"+base+":refs/heads/main")
		}, true, false},
		{"bob changes ci.yml and reverts it in one push to main", func(s *Stand, _ string) Result {
			s.Commit("ci: evil", map[string]string{"ci.yml": "evil"})
			s.Commit("ci: revert", map[string]string{"ci.yml": "v1"})
			return s.Push(bob, "HEAD:refs/heads/main")
		}, false, false},
	})
}

// TestCheckHistory_CommitPolicy: the same actions with a header that is not
// Conventional Commits, under a rule of main only.
func TestCheckHistory_CommitPolicy(t *testing.T) {
	files := func(policy string) map[string]string { return map[string]string{"COMMITPOLICY": policy} }
	wipFeature := func(s *Stand) {
		s.git(s.Work, "checkout", "-q", "-b", "feature")
		s.Commit("wip login", map[string]string{"login.go": "x"})
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature")) // the rule is for main only
	}
	runHistoryCases(t, "COMMITPOLICY", files, []historyCase{
		{"bob pushes wip to main", func(s *Stand, _ string) Result {
			s.Commit("wip login", map[string]string{"login.go": "x"})
			return s.Push(bob, "HEAD:refs/heads/main")
		}, false, false},
		{"bob merges feature with wip into main", func(s *Stand, _ string) Result {
			wipFeature(s)
			s.git(s.Work, "checkout", "-q", "main")
			s.Commit("feat: app", map[string]string{"app.go": "b"})
			s.git(s.Work, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature' into 'main'", "feature")
			return s.Push(bob, "HEAD:refs/heads/main")
		}, true, false},
		{"bob fast-forwards main to feature with wip", func(s *Stand, _ string) Result {
			wipFeature(s)
			return s.Push(bob, "HEAD:refs/heads/main")
		}, true, false},
		{"bob squash-merges feature with wip into main", func(s *Stand, _ string) Result {
			wipFeature(s)
			s.git(s.Work, "checkout", "-q", "main")
			s.git(s.Work, "merge", "-q", "--squash", "feature")
			s.git(s.Work, "commit", "-q", "-m", "feat: login")
			return s.Push(bob, "HEAD:refs/heads/main")
		}, true, true},
	})
}
