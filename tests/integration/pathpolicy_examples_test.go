//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// example reads a file of examples/.
func example(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", filepath.FromSlash(path)))
	require.NoError(t, err)
	return string(data)
}

// TestPathPolicyExamples_CIConfig: the CI configuration of examples/ci-config
// is changed only by the platform team, in every branch.
func TestPathPolicyExamples_CIConfig(t *testing.T) {
	s, _ := pathStand(t, example(t, "ci-config/PATHPOLICY"), map[string]string{".gitlab-ci.yml": "v1", "README.md": "a"})
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	t.Run("bob changes README.md", func(t *testing.T) {
		s.Commit("docs: readme", map[string]string{"README.md": "b"})
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
	})
	ci := s.Commit("ci: add a job", map[string]string{".gitlab-ci.yml": "v2"})
	t.Run("bob pushes a branch that changes .gitlab-ci.yml", func(t *testing.T) {
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/feature"), `PATHPOLICY rule "ci-config", branch feature`,
			"the content of .gitlab-ci.yml in commit "+ci[:9]+" differs both from this branch and from main; new content is pushed by one of them",
			"who can help with this rule: #platform")
	})
	t.Run("alice pushes the same branch", func(t *testing.T) {
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/feature"))
	})
	t.Run("bob tags a commit of the branch", func(t *testing.T) {
		assertAllowed(t, s.Push(bob, ci+":refs/tags/v1.0.0"))
	})
}

// TestPathPolicyExamples_Agents: the agent configuration of
// examples/delegation is read only from the default branch, so match:
// default lets anyone change it in a branch and only alice merge it.
func TestPathPolicyExamples_Agents(t *testing.T) {
	s, base := pathStand(t, example(t, "delegation/PATHPOLICY"), map[string]string{".gitlab/agents/prod/config.yaml": "v1"})
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	s.Commit("feat: agent", map[string]string{".gitlab/agents/prod/config.yaml": "v2"})
	t.Run("bob changes the agent configuration in feature", func(t *testing.T) {
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
	})
	s.git(s.Work, "checkout", "-q", "-B", "main", base)
	s.git(s.Work, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature' into 'main'", "feature")
	t.Run("bob merges feature into main", func(t *testing.T) {
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), `PATHPOLICY rule "kubernetes-agents", branch main`)
	})
	t.Run("alice merges feature into main", func(t *testing.T) {
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	})
}

// TestPathPolicyExamples_ProdValues: anyone proposes a change of the
// production values of examples/ci-config in a branch, only alice or
// release-manager accepts it into main or a release branch.
func TestPathPolicyExamples_ProdValues(t *testing.T) {
	s, base := pathStand(t, example(t, "ci-config/PATHPOLICY"), map[string]string{"helm/app/values-prod.yaml": "replicas: 2"})
	s.Seed(base + ":refs/heads/release/1")
	s.git(s.Work, "checkout", "-q", "-b", "feature")
	s.Commit("feat: more replicas", map[string]string{"helm/app/values-prod.yaml": "replicas: 3"})
	t.Run("bob pushes the change to feature", func(t *testing.T) {
		assertAllowed(t, s.Push(bob, "HEAD:refs/heads/feature"))
	})
	t.Run("bob pushes the change to release/1", func(t *testing.T) {
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/release/1"), `PATHPOLICY rule "prod-values", branch release/1`)
	})
	s.git(s.Work, "checkout", "-q", "-B", "main", base)
	s.git(s.Work, "merge", "-q", "--no-ff", "-m", "Merge branch 'feature' into 'main'", "feature")
	t.Run("bob merges feature into main", func(t *testing.T) {
		assertDenied(t, s.Push(bob, "HEAD:refs/heads/main"), `PATHPOLICY rule "prod-values", branch main`,
			"ask one of them to make the change, or to merge it from your branch")
	})
	t.Run("alice merges feature into main", func(t *testing.T) {
		assertAllowed(t, s.Push(alice, "HEAD:refs/heads/main"))
	})
}
