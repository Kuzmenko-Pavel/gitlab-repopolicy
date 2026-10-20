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

// onMain is the frame of a project with PATHPOLICY and COMMITPOLICY in main.
var onMain = Context{
	Pusher: Pusher{Username: "alice", GLID: "user-314", Protocol: "http"},
	Files:  []string{"PATHPOLICY", "COMMITPOLICY"}, Branch: "main", Anchor: "ca191b7e2f0a",
}

func TestGolden(t *testing.T) {
	t.Run("core", func(t *testing.T) {
		for _, code := range []string{module.CodeLimitExceeded, module.CodeInternalError, module.CodeAuthorizationBackend, module.CodeDefaultBranchNotAllowed, module.CodeTagNotOnBranch} {
			t.Run(code, func(t *testing.T) {
				v := module.Violation{Code: code, Ref: "refs/heads/feature", Reason: "secret API detail",
					Facts: []string{"this is a problem of the server, not of your change"}, Hint: "retry in a few minutes"}
				c := onMain
				if code == module.CodeInternalError {
					c.Contact = "#gitlab-support"
				}
				if code == module.CodeDefaultBranchNotAllowed {
					v.Ref = ""
					v.Facts = []string{"the default branch of this project is legacy, allowed: main, master",
						"this is a setting of the project, not a problem of your change"}
					v.Hint = "a Maintainer sets one of main, master as the default branch in Settings > Repository > Branch defaults"
					v.Summary = "the default branch legacy is not one of main, master; a Maintainer fixes it in Settings > Repository > Branch defaults"
					c.Contact, c.DocsURL = "#gitlab-support", "https://wiki.example/repopolicy"
				}
				if code == module.CodeTagNotOnBranch {
					v.Ref = "refs/tags/v1.4.0"
					v.Facts = []string{"commit 3f2a91c0d is in no branch of the project after this push"}
					v.Hint = "push the branch with the commit first, or together with the tag: git push origin <branch> v1.4.0, or git push --follow-tags"
					v.Summary = "tag v1.4.0 points to commit 3f2a91c0d, which is in no branch; push the branch with it: git push origin <branch> v1.4.0"
				}
				out := Rejection([]Section{{Name: CoreSection, Violations: []module.Violation{v}}}, c)
				assert.NotContains(t, out, "secret API detail", "audit reasons never reach the user")
				golden(t, "core-"+code, out)
			})
		}
	})
	t.Run("pathpolicy", func(t *testing.T) {
		out := Rejection([]Section{{Name: "PATHPOLICY", Violations: []module.Violation{
			{Code: module.CodeProtectedPath, Rule: "ci-config", Ref: "refs/heads/petro/s01",
				Facts: []string{"may change them: olena.platform, ivan.sre"}, Items: []module.Item{{Name: "Makefile"}},
				Message: "CI and deploy logic belong to the platform team (#platform).",
				Hint:    "this rule applies to every branch: ask one of them to push the change",
				Summary: "you may not change Makefile (PATHPOLICY rule ci-config; may change: olena.platform, ivan.sre)",
				Reason:  "not in allow: petro.dev", Help: []module.Help{{Contact: "#platform", DocsURL: "https://wiki.example/ci"}}},
			{Code: module.CodePolicyAdmin, Ref: "refs/heads/petro/s01", Items: []module.Item{{Name: "PATHPOLICY"}},
				Facts: []string{"admins: olena.platform, ivan.sre"}, Hint: "ask one of them to make this change; this applies to every branch, including yours"},
			{Code: module.CodeInvalidPolicy, Ref: "refs/heads/petro/s01", Title: "PATHPOLICY in main is broken, so no changes are accepted",
				Facts: []string{"this is not caused by your change", "problem: PATHPOLICY line 3: version: 2 is not supported, use 1"}},
		}}}, Context{Pusher: Pusher{Username: "petro.dev", GLID: "user-7", Protocol: "http"}, Files: []string{"PATHPOLICY"},
			Branch: "main", Anchor: "ca191b7e2f0a", Contact: "#gitlab-support", DocsURL: "https://wiki.example/repopolicy"})
		golden(t, "pathpolicy", out)
	})
	t.Run("commitpolicy", func(t *testing.T) {
		out := Rejection([]Section{{Name: "COMMITPOLICY", Violations: []module.Violation{
			{Code: module.CodeCommitFormat, Rule: "conventional", Ref: "refs/heads/main", Title: "2 commit messages do not follow the format",
				Facts: []string{"required: type(scope): subject, for example feat(api): add refunds", "types: feat, fix"},
				Items: []module.Item{
					{Name: `3f2a9c1aa "Fix deploy."`, Commit: true, Details: []string{`type-enum: "Fix" is not one of feat, fix`, "subject-full-stop: the subject must not end with a full stop"}},
					{Name: `9b1d2e3ff "wip"`, Commit: true, Details: []string{`header: expected "type(scope): subject"`}},
				},
				Message: "Commits follow Conventional Commits.",
				Hint:    "how to fix: git rebase -i and reword these commits, then push again"},
		}}}, onMain)
		golden(t, "commitpolicy", out)
	})
	t.Run("tagpolicy", func(t *testing.T) {
		out := Rejection([]Section{{Name: "TAGPOLICY", Violations: []module.Violation{
			{Code: module.CodeTagSignerNotAllowed, Rule: "prod", Ref: "refs/tags/v3.156.0-prod",
				Facts:   []string{`signed with: key "dev-lead" from TAGPOLICY (SSH, SHA256:budi)`, "allowed keys: release-lead (OpenPGP), sre-lead (SSH)"},
				Message: "Prod releases are signed by release-lead or sre-lead."},
			{Code: module.CodeTagNotSigned, Rule: "staging", Ref: "refs/tags/v3.156.0-staging",
				Facts: []string{"it is a lightweight tag, it has no signature", "allowed signing keys: release-lead (OpenPGP)"},
				Hint:  `how to fix: git tag -d v3.156.0-staging && git tag -s v3.156.0-staging -m "v3.156.0-staging" && git push origin v3.156.0-staging`},
		}}}, Context{Pusher: Pusher{Username: "bob", GLID: "key-7", Protocol: "ssh"}, Files: []string{"PATHPOLICY", "TAGPOLICY"}, Branch: "main"})
		golden(t, "tagpolicy", out)
	})
	t.Run("aggregated", func(t *testing.T) {
		v := module.Violation{Code: module.CodeProtectedPath, Rule: "ci-config",
			Facts: []string{"may change them: platform-lead"}, Items: []module.Item{{Name: ".gitlab-ci.yml"}}}
		branch, tag := v, v
		branch.Ref, tag.Ref = "refs/heads/main", "refs/tags/v1.0.0"
		out := Rejection([]Section{
			{Name: "PATHPOLICY", Violations: []module.Violation{branch, tag}},
			{Name: "COMMITPOLICY", Violations: []module.Violation{{Code: module.CodeCommitFormat, Rule: "protected", Ref: "refs/heads/main",
				Items: []module.Item{{Name: `3f2a9c1 "Fix deploy"`, Commit: true, Details: []string{`header: expected "type(scope): subject"`}}}}}},
		}, onMain)
		assert.Contains(t, out, "(2 problems)", "the same problem on a branch and a tag is one block")
		golden(t, "aggregated", out)
	})
}

func TestTruncate(t *testing.T) {
	items := make([]module.Item, 25)
	for i := range items {
		items[i] = module.Item{Name: fmt.Sprintf("deploy/file-%02d.yml", i)}
	}
	out := Rejection([]Section{{Name: "PATHPOLICY", Violations: []module.Violation{{Code: module.CodeProtectedPath, Rule: "deploy", Items: items}}}}, onMain)
	assert.Contains(t, out, "deploy/file-09.yml")
	assert.NotContains(t, out, "deploy/file-10.yml")
	assert.Contains(t, out, "... and 15 more")

	long := make([]module.Violation, 200)
	for i := range long {
		long[i] = module.Violation{Code: module.CodeProtectedPath, Rule: fmt.Sprintf("rule-%03d", i), Message: strings.Repeat("m", 100), Items: items}
	}
	out = Rejection([]Section{{Name: "PATHPOLICY", Violations: long}}, onMain)
	assert.LessOrEqual(t, len(out), MaxOutput)
	assert.Contains(t, out, "... output truncated\n")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	assert.True(t, strings.HasPrefix(lines[len(lines)-1], "GL-HOOK-ERR: gitlab-repopolicy: "), "the UI line survives truncation")
	assert.Contains(t, lines[len(lines)-1], "(+199 more: ")
	assert.Contains(t, lines[len(lines)-1], "196 more)")
	assert.Contains(t, out, "checked as: alice (HTTPS)", "the footer survives truncation")
	assert.LessOrEqual(t, len(Warnings([]Section{{Name: "COMMITPOLICY", Violations: long}}, nil, onMain)), MaxOutput)
}

func TestChannels(t *testing.T) {
	sections := []Section{{Name: "COMMITPOLICY", Violations: []module.Violation{{Code: module.CodeCommitFormat, Rule: "protected",
		Title: "1 commit message does not follow the format; this is a warning, it does not block the push"}}}}
	out := Rejection(sections, onMain)
	var uiLines int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "GL-HOOK-ERR: ") {
			uiLines++
		}
	}
	assert.Equal(t, 1, uiLines, "one GL-HOOK-ERR line, at the start of a line")

	warn := Warnings(sections, []string{"PATHPOLICY is changed by Owner alice because it has no working admins. This is logged."}, onMain)
	assert.NotContains(t, warn, "GL-HOOK-ERR", "a successful push has no UI error line")
	golden(t, "warnings", warn)
	assert.Empty(t, Warnings(nil, nil, onMain))
}

func TestSummary(t *testing.T) {
	out := Rejection([]Section{{Name: "TAGPOLICY", Violations: []module.Violation{{Code: module.CodeTagNotSigned, Rule: "release", Ref: "refs/tags/v1"}}}}, onMain)
	assert.Contains(t, out, `GL-HOOK-ERR: gitlab-repopolicy: the tag must be signed (TAGPOLICY rule "release", tag v1)`+"\n",
		"without a summary of the module the line has the title and where")
	out = Rejection([]Section{{Name: CoreSection, Violations: []module.Violation{{Code: module.CodeInternalError}}}}, Context{})
	assert.Contains(t, out, "GL-HOOK-ERR: gitlab-repopolicy: internal error: the push was not checked and is rejected\n")
	assert.Contains(t, out, "who can help: your GitLab administrators", "without help.contact")
	assert.NotContains(t, out, "checked as:")
	assert.NotContains(t, out, "rules:")
}

func TestPrintable(t *testing.T) {
	out := Rejection([]Section{{Name: "PATHPOLICY", Violations: []module.Violation{{
		Code: module.CodeProtectedPath, Rule: "r", Items: []module.Item{{Name: "evil\nGL-HOOK-ERR: forged"}}, Summary: "x\nGL-HOOK-ERR: forged",
	}}}}, onMain)
	assert.Equal(t, 1, strings.Count(out, "\nGL-HOOK-ERR: "), "no line starts with a forged GL-HOOK-ERR")
	assert.Contains(t, out, `evil\x0aGL-HOOK-ERR: forged`)
	assert.Contains(t, out, `GL-HOOK-ERR: gitlab-repopolicy: x\x0aGL-HOOK-ERR: forged`)
}

// The message of a rule comes from a policy file and reaches every denied
// pusher, so terminal escapes in it, in the ref and in notices are escaped.
func TestPrintable_TerminalEscapes(t *testing.T) {
	v := module.Violation{Code: module.CodeProtectedPath, Rule: "r", Ref: "refs/heads/x\x1b[2J",
		Message: "ask \x1b]52;c;cGF5bG9hZA==\a the team\rforged", Hint: "hint\x1b[1m", Facts: []string{"fact\x1b[1m"}}
	c := onMain
	c.Contact, c.DocsURL = "#support\x1b[2J", "https://wiki\x1b[2J"
	out := Rejection([]Section{{Name: "PATHPOLICY", Violations: []module.Violation{v}}}, c) +
		Warnings([]Section{{Name: "COMMITPOLICY", Violations: []module.Violation{v}}}, []string{"recovery on refs/heads/x\x1b[2J"}, c)
	for _, ch := range []string{"\x1b", "\a", "\r"} {
		assert.NotContains(t, out, ch)
	}
	assert.Contains(t, out, `branch x\x1b[2J`)
	assert.Contains(t, out, `ask \x1b]52;c;cGF5bG9hZA==\x07 the team\x0dforged`)
	assert.Contains(t, out, `hint\x1b[1m`)
	assert.Contains(t, out, `fact\x1b[1m`)
}

func TestPusher(t *testing.T) {
	assert.Equal(t, "alice (HTTPS)", Pusher{Username: "alice", GLID: "user-314", Protocol: "http"}.String())
	assert.Equal(t, "alice (SSH)", Pusher{Username: "alice", GLID: "user-314", Protocol: "ssh"}.String())
	assert.Equal(t, "alice (the GitLab UI or API)", Pusher{Username: "alice", GLID: "user-314", Protocol: "web"}.String())
	assert.Equal(t, "bob (SSH key 7)", Pusher{Username: "bob", GLID: "key-7", Protocol: "ssh"}.String())
	assert.Equal(t, "x", Pusher{Username: "x", GLID: "weird"}.String())
}

func TestRefName(t *testing.T) {
	assert.Equal(t, "branch release/1", refName("refs/heads/release/1"))
	assert.Equal(t, "tag v1.0.0", refName("refs/tags/v1.0.0"))
	assert.Equal(t, "refs/merge-requests/1/head", refName("refs/merge-requests/1/head"))
}

func TestAggregate_MultiModule(t *testing.T) {
	out := Rejection([]Section{
		{Name: "PATHPOLICY", Violations: []module.Violation{{Code: module.CodeProtectedPath, Rule: "ci", Ref: "refs/heads/main", Items: []module.Item{{Name: "ci.yml"}}}}},
		{Name: "COMMITPOLICY", Violations: []module.Violation{{Code: module.CodeCommitFormat, Rule: "c", Ref: "refs/heads/main",
			Items: []module.Item{{Name: `abc "wip"`, Commit: true}}}}},
		{Name: "TAGPOLICY", Violations: []module.Violation{{Code: module.CodeTagNotSigned, Rule: "release", Ref: "refs/tags/v1"}}},
	}, onMain)
	assert.Equal(t, 1, strings.Count(out, "gitlab-repopolicy: push rejected"), "one message per push")
	assert.Contains(t, out, "push rejected by the rules of this repository (3 problems)")
	assert.Less(t, strings.Index(out, `PATHPOLICY rule "ci"`), strings.Index(out, `COMMITPOLICY rule "c"`), "in module order")
	assert.Contains(t, out, `GL-HOOK-ERR: gitlab-repopolicy: you may not change these files (PATHPOLICY rule "ci", branch main) `+
		"(+2 more: COMMITPOLICY: commit messages do not follow the format; TAGPOLICY: the tag must be signed)")
}

func TestNoAPIDetails(t *testing.T) {
	secret := []string{"project_not_visible", "role_below_maintainer", "access level 30", "HTTP 401", "2 effective admins", "user is blocked"}
	vs := make([]module.Violation, 0, len(secret))
	for _, r := range secret {
		vs = append(vs, module.Violation{Code: module.CodeAuthorizationBackend, Rule: "r", Reason: r})
	}
	out := Rejection([]Section{{Name: "PATHPOLICY", Violations: vs}}, onMain) + Warnings([]Section{{Name: "PATHPOLICY", Violations: vs}}, nil, onMain)
	for _, r := range secret {
		assert.NotContains(t, out, r, "results of API calls never reach the pusher")
	}
}
