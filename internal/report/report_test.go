package report

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kuzmenko-Pavel/gitlab-repopolicy/internal/module"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), got)
}

var alice = Pusher{Username: "alice", GLID: "user-314"}

func TestGolden(t *testing.T) {
	t.Run("core", func(t *testing.T) {
		for _, code := range []string{module.CodeInvalidPolicyUpdate, module.CodeLimitExceeded, module.CodeInternalError, module.CodeAuthorizationBackend} {
			t.Run(code, func(t *testing.T) {
				v := module.Violation{Code: code, Ref: "refs/heads/feature", Reason: "secret API detail"}
				name := "gitlab-repopolicy"
				if code == module.CodeInvalidPolicyUpdate {
					name = "PATHPOLICY"
					v.Items = []module.Item{{Name: "PATHPOLICY", Details: []string{"line 3: field allwo not found in type"}}}
				}
				out := Rejection([]Section{{Name: name, Violations: []module.Violation{v}}}, alice)
				assert.NotContains(t, out, "secret API detail", "audit reasons never reach the user")
				golden(t, "core-"+code, out)
			})
		}
	})
	t.Run("pathpolicy", func(t *testing.T) {
		out := Rejection([]Section{{Name: "PATHPOLICY", Violations: []module.Violation{
			{Code: module.CodeProtectedPath, Rule: "ci-config", Ref: "refs/heads/main", Message: "Allowed to change these paths: platform-lead, sre-lead.",
				Items: []module.Item{{Name: ".gitlab-ci.yml"}}, Reason: "not in allow: bob"},
			{Code: module.CodePolicyAdmin, Ref: "refs/heads/main", Items: []module.Item{{Name: "PATHPOLICY"}},
				Message: "PATHPOLICY is changed by its admins: platform-lead.\nWithout an effective admin, an Owner of the project may change it (recovery).",
				Reason:  "not an effective admin, access level 30"},
			{Code: module.CodeLockoutPrevented, Ref: "refs/heads/feature", Items: []module.Item{{Name: "PATHPOLICY",
				Details: []string{"none of the admins is an active user with at least Maintainer access to the project"}}}},
			{Code: module.CodeInvalidPolicy, Ref: "refs/heads/feature", Message: "PATHPOLICY on the default branch is invalid: line 3: bad"},
			{Code: module.CodeUnknownPrincipal, Rule: "deploy", Ref: "refs/heads/main", Items: []module.Item{{Name: "deploy/prod.yml"}},
				Message: "Protected paths are changed only by people: a deploy key cannot change them.", Reason: "principal is not a person: key-7"},
			{Code: module.CodeAuthorizationBackend, Rule: "prod-values", Ref: "refs/heads/release/1", Reason: "project_not_visible",
				Message: "GitLab could not confirm the authorization. Retry later; administrators are alerted."},
		}}}, Pusher{Username: "bob", GLID: "user-3"})
		golden(t, "pathpolicy", out)
	})
	t.Run("commitpolicy", func(t *testing.T) {
		out := Rejection([]Section{{Name: "COMMITPOLICY", Violations: []module.Violation{
			{Code: module.CodeCommitFormat, Rule: "conventional", Ref: "refs/heads/main",
				Message: "Commits follow Conventional Commits.",
				Items: []module.Item{
					{Name: `3f2a9c1aa "Fix deploy."`, Commit: true, Details: []string{`type-enum: "Fix" is not one of feat, fix`, "subject-full-stop: the subject must not end with a full stop"}},
					{Name: `9b1d2e3ff "wip"`, Commit: true, Details: []string{`header: expected "type(scope): subject"`}},
				},
				Hint: "fix: git rebase -i and reword, or enable squash on merge"},
			{Code: module.CodeCommitCheckTimeout, Rule: "conventional", Ref: "refs/heads/main",
				Items: []module.Item{{Name: `77aa01bc2 "feat: x"`, Commit: true, Details: []string{"pattern: not checked in time (match timeout)"}}}},
			{Code: module.CodeLimitExceeded, Rule: "conventional", Ref: "refs/heads/import",
				Message: "More than 10000 commits enter the ref; COMMITPOLICY does not check pushes this large."},
		}}}, alice)
		golden(t, "commitpolicy", out)
	})
	t.Run("aggregated", func(t *testing.T) {
		out := Rejection([]Section{
			{Name: "PATHPOLICY", Violations: []module.Violation{{
				Code: module.CodeProtectedPath, Rule: "ci-config", Ref: "refs/heads/main",
				Message: "CI/CD configuration is owned by the platform team.\nPropose a change through a merge request.",
				Items:   []module.Item{{Name: ".gitlab-ci.yml"}, {Name: "Makefile"}},
			}}},
			{Name: "COMMITPOLICY", Violations: []module.Violation{{
				Code: module.CodeCommitFormat, Rule: "protected", Ref: "refs/heads/main",
				Items: []module.Item{{Name: `3f2a9c1 "Fix deploy"`, Commit: true, Details: []string{`header: expected "type(scope): subject"`}}},
				Hint:  "fix: git rebase -i, reword; or enable squash on merge",
			}}},
		}, Pusher{Username: "bob", GLID: "key-7"})
		golden(t, "aggregated", out)
	})
}

func TestTruncate(t *testing.T) {
	items := make([]module.Item, 25)
	for i := range items {
		items[i] = module.Item{Name: fmt.Sprintf("deploy/file-%02d.yml", i)}
	}
	out := Rejection([]Section{{Name: "PATHPOLICY", Violations: []module.Violation{{Code: module.CodeProtectedPath, Rule: "deploy", Items: items}}}}, alice)
	assert.Contains(t, out, "deploy/file-09.yml")
	assert.NotContains(t, out, "deploy/file-10.yml")
	assert.Contains(t, out, "... and 15 more")

	long := make([]module.Violation, 200)
	for i := range long {
		long[i] = module.Violation{Code: module.CodeProtectedPath, Rule: fmt.Sprintf("rule-%03d", i), Message: strings.Repeat("m", 100), Items: items}
	}
	out = Rejection([]Section{{Name: "PATHPOLICY", Violations: long}}, alice)
	assert.LessOrEqual(t, len(out), MaxOutput)
	assert.Contains(t, out, "... output truncated\n")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	assert.True(t, strings.HasPrefix(lines[len(lines)-1], "GL-HOOK-ERR: gitlab-repopolicy: "), "the UI line survives truncation")
	assert.Contains(t, lines[len(lines)-1], "197 more")
	assert.LessOrEqual(t, len(Warnings([]Section{{Name: "COMMITPOLICY", Violations: long}}, nil)), MaxOutput)
}

func TestChannels(t *testing.T) {
	sections := []Section{{Name: "COMMITPOLICY", Violations: []module.Violation{{Code: module.CodeCommitFormat, Rule: "protected"}}}}
	out := Rejection(sections, alice)
	var uiLines int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "GL-HOOK-ERR: ") {
			uiLines++
		}
	}
	assert.Equal(t, 1, uiLines, "one GL-HOOK-ERR line, at the start of a line")

	warn := Warnings(sections, []string{"recovery: the policy has no administrators; Owner alice changed PATHPOLICY"})
	assert.NotContains(t, warn, "GL-HOOK-ERR", "a successful push has no UI error line")
	golden(t, "warnings", warn)
	assert.Empty(t, Warnings(nil, nil))
}

func TestPrintable(t *testing.T) {
	out := Rejection([]Section{{Name: "PATHPOLICY", Violations: []module.Violation{{
		Code: module.CodeProtectedPath, Rule: "r", Items: []module.Item{{Name: "evil\nGL-HOOK-ERR: forged"}},
	}}}}, alice)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "GL-HOOK-ERR: ") {
			assert.NotContains(t, line, "forged")
		}
	}
	assert.Contains(t, out, `evil\x0aGL-HOOK-ERR: forged`)
}

func TestPusher(t *testing.T) {
	assert.Equal(t, "alice (id 314)", alice.String())
	assert.Equal(t, "bob (key 7)", Pusher{Username: "bob", GLID: "key-7"}.String())
	assert.Equal(t, "x", Pusher{Username: "x", GLID: "weird"}.String())
}

func TestAggregate_MultiModule(t *testing.T) {
	out := Rejection([]Section{
		{Name: "PATHPOLICY", Violations: []module.Violation{{Code: module.CodeProtectedPath, Rule: "ci", Ref: "refs/heads/main", Items: []module.Item{{Name: "ci.yml"}}}}},
		{Name: "COMMITPOLICY", Violations: []module.Violation{{Code: module.CodeCommitFormat, Rule: "c", Ref: "refs/heads/main",
			Items: []module.Item{{Name: `abc "wip"`, Commit: true}}}}},
		{Name: "TAGPOLICY", Violations: []module.Violation{{Code: module.CodeTagNotSigned, Rule: "release", Ref: "refs/tags/v1"}}},
	}, alice)
	assert.Equal(t, 1, strings.Count(out, "gitlab-repopolicy: push rejected"), "one message per push")
	assert.Contains(t, out, "push rejected (PATHPOLICY: 1 violation, COMMITPOLICY: 1 violation, TAGPOLICY: 1 violation)")
	assert.Less(t, strings.Index(out, "PATHPOLICY [ci]"), strings.Index(out, "COMMITPOLICY [c]"), "in module order")
	assert.Contains(t, out, "GL-HOOK-ERR: gitlab-repopolicy: PATHPOLICY [ci] modification denied; COMMITPOLICY [c] commit message format; TAGPOLICY [release] tag is not signed.")
}

func TestNoAPIDetails(t *testing.T) {
	secret := []string{"project_not_visible", "role_below_maintainer", "access level 30", "HTTP 401", "2 effective admins", "user is blocked"}
	vs := make([]module.Violation, 0, len(secret))
	for _, r := range secret {
		vs = append(vs, module.Violation{Code: module.CodeAuthorizationBackend, Rule: "r", Reason: r})
	}
	out := Rejection([]Section{{Name: "PATHPOLICY", Violations: vs}}, alice) + Warnings([]Section{{Name: "PATHPOLICY", Violations: vs}}, nil)
	for _, r := range secret {
		assert.NotContains(t, out, r, "results of API calls never reach the pusher (architecture §15)")
	}
}
