[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](message-results.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](message-results.en.md)

# Why a message passes or fails

Distinguish format errors from warnings and see how an empty body or misplaced trailer changes the result.

## Configuration for comparison

Use this complete `COMMITPOLICY` in the directory root. The table below checks only `conventional`; `release` is not selected for this local check.

```yaml
# examples/conventional-commits/COMMITPOLICY
# Conventional Commits in main and release branches; a stricter type list in
# release branches. Every commit that enters these branches is checked, also
# the commits of a merged branch. Other branches are not checked.
version: 1
mode: enforce

rules:
  conventional:
    match: [main, '/^release\//']
    check_history: incoming
    parser: conventional
    except:
      merge_commit: true
      git_reverts: true
      users: [release-bot]
    checks:
      header-format:
        conventional: { level: error }
      header-max-length:
        limit: { level: warn, value: 100 }
      type-enum:
        allowed: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
      scope:
        optional: { level: error, value: optional }
      breaking-bang:
        optional: { level: error, value: optional }
      subject-starts-lowercase:
        lowercase: { level: error, value: required }
      subject-trailing-dot:
        no-dot: { level: error, value: forbidden }
      body-leading-blank-lines:
        blank-line: { level: error, value: required }
      body-min-line-length:
        feat-fix: { level: error, value: 1, types: [feat, fix] }
      trailer-required:
        dco: { level: error, value: [Signed-off-by] }
      pattern:
        issue-ref: { level: warn, location: message, value: '/[A-Za-z0-9_.\/-]*#[0-9]+/', types: [feat, fix] }
    message: >-
      Commits in main and release branches follow Conventional Commits.
      Reword them with git rebase -i or squash the merge request.

  release:
    match: ['/^release\//']
    check_history: incoming
    parser: conventional
    checks:
      type-enum:
        release: { level: error, value: [fix, docs, chore] }
    message: Release branches take only fix, docs and chore commits.
```

For each row, save the message with actual line breaks in `message.txt`, then run `gitlab-repopolicy lint-commit -f COMMITPOLICY --rule conventional -m "$(cat message.txt)"`. In the table, a backslash followed by n represents a line break.

<!-- check: lint-commit examples/conventional-commits/COMMITPOLICY conventional -->
| Message | Result | Why |
| --- | --- | --- |
| `fix(billing): round refunds to cents\n\nRefunds lost a cent.\n\nRefs: #665\nSigned-off-by: A <a@example.com>` | passes | everything is in place |
| `docs: fix a typo\n\nSigned-off-by: A <a@example.com>` | passes | `body-min-line-length` and `pattern` apply only to `feat` and `fix` |
| `feat(api)!: drop v1\n\nV1 is gone.\n\nRefs: #2\nSigned-off-by: A <a@example.com>` | passes | `!` is allowed (`breaking-bang: optional`) |
| `bugfix: round refunds\n\nSigned-off-by: A <a@example.com>` | denied | `type-enum`: `bugfix` is not in the list |
| `fix: Round refunds\n\nBody.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | denied | `subject-starts-lowercase`: the subject starts with a capital letter |
| `fix: round refunds.\n\nBody.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | denied | `subject-trailing-dot`: a full stop at the end |
| `fix: round refunds\nRefunds lost a cent.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | denied | `body-leading-blank-lines`: the body starts right on the second line |
| `fix: round refunds\n\nSigned-off-by: A <a@example.com>` | denied | `body-min-line-length`: `fix` has no body text |
| `fix: round refunds\n\nBody without a reference.\n\nSigned-off-by: A <a@example.com>` | warning | `pattern` of level `warn`: no reference to an issue |
| `docs: fix a typo` | denied | `body-leading-blank-lines` and `trailer-required`: a message of one line has neither an empty line after the header nor `Signed-off-by` |
| `Revert "feat: x"\n\nThis reverts commit 0123456789abcdef0123456789abcdef01234567.` | passes | `except.git_reverts` |

The denial in the output of a push:

```text
remote: COMMITPOLICY rule "conventional", branch main: 1 commit message does not follow the format
remote:   required: type(scope): subject, for example feat(api): add refunds
remote:   types: feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test
remote:   commits:
remote:     60a6b73c3 "Fix deploy."
remote:       - header-format conventional: expected "type(scope): subject"
remote:       - body-leading-blank-lines blank-line: an empty line must follow the header
remote:       - trailer-required dco: missing Signed-off-by in the trailer block
remote:   Commits in main and release branches follow Conventional Commits. Reword them with git rebase -i or squash the merge request.
remote:   how to fix: reword the commit (git commit --amend for the last one, git rebase -i for an older one), then push again
```

Every reason starts with the type and the name of the check, for example `trailer-required dco`: the name tells which check of the file fired. `Signed-off-by` is a trailer, so it does not replace a body. `docs` does not require a body; `fix` does. A missing issue reference is only a `warning` because `pattern` uses `level: warn`. Locally a denial exits 1 and a warning exits 0; the hook also considers `match`, exceptions and global mode.

Do not copy literal backslash-n characters into a commit message. After fixing the last commit with `git commit --amend`, run the linter again. Success validates the message under this rule, not permission to change protected files.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
