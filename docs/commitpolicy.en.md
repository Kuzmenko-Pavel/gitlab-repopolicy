[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](commitpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](commitpolicy.en.md)

# COMMITPOLICY

## In short

COMMITPOLICY checks how the messages of the commits a push adds to a branch are written. When a message breaks the rules, GitLab rejects the push, and the commit has to be reworded. The file is in the root of the repository under the name `COMMITPOLICY`, the content is YAML. The smallest working file:

```yaml
version: 1
rules:
  conventional:
    match: default
    parser: conventional
    message: Commits in the default branch follow Conventional Commits.
```

Here every message in the default branch follows `type(scope): subject`. There are no other requirements.

## Quick start

1. Start with `mode: warn`: violations are only shown in the output of the push and written to audit, the push goes through. This shows how many commits of the team break the rules before anyone gets a denial
2. Check the file with `gitlab-repopolicy validate` and messages with `gitlab-repopolicy lint-commit` ([below](#checking-without-a-push))
3. Merge the file into the default branch. The hook reads the rules only from there
4. When there are almost no warnings left, remove `mode: warn`, and violations of level `error` start to deny

Who may change COMMITPOLICY itself is decided by a rule of PATHPOLICY ([pathpolicy.en.md](pathpolicy.en.md#6-who-changes-the-other-policy-files)). Without such a rule anyone who can push may change the file.

## What a commit message consists of

```text
fix(billing): round refunds to cents

Refunds were computed in floating point and lost a cent on some amounts.
Amounts are now kept in integer cents.

Refs: #665
Signed-off-by: Olena Ivanenko <olena@example.com>
```

Three parts with an empty line between them:

| Part | In the example | What it is |
| --- | --- | --- |
| header | `fix(billing): round refunds to cents` | the first line. `git log --oneline`, the list of commits in GitLab and the title of a merge request show it |
| body | two lines about refunds | the explanation: what changed and why |
| trailer block | `Refs: #665` and `Signed-off-by: ...` | the last paragraph, where every line looks like `Key: value` or `Key #value`: a link to an issue, the author by DCO |

A header in the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) format:

| Part | In the example | What it is |
| --- | --- | --- |
| type | `fix` | the kind of change: `feat` - a new feature, `fix` - a fix, `docs` - documentation |
| scope | `billing` | optional, in parentheses: the part of the system the change is about |
| `!` | none | optional, before the colon: the change breaks compatibility |
| `: ` | `: ` | a colon and exactly one space |
| subject | `round refunds to cents` | a short description of the change, not empty |

How to write such a message. Every `-m` is a separate paragraph:

```sh
git commit -s \
  -m "fix(billing): round refunds to cents" \
  -m "Refunds were computed in floating point and lost a cent on some amounts." \
  -m "Refs: #665"
```

`-s` (`--signoff`) adds `Signed-off-by: Name <email>` from `user.name` and `user.email`. `-S` (capital) is something else, a cryptographic signature of the commit. COMMITPOLICY checks only the text of the message, it does not check the signature of a commit. Signatures of tags are checked by TAGPOLICY ([tagpolicy.en.md](tagpolicy.en.md)).

How to fix a commit after a denial. The hook rejects the whole push, so the rejected commits exist only on your machine:
- the last commit: `git commit --amend` (with `-s` when `Signed-off-by` is missing)
- several commits: `git rebase -i origin/main`, replace `pick` with `reword` next to the commits

If `rebase -i` rewrote commits that were already on the server, push with `--force-with-lease`.

## Field reference

| Field | Required | Value |
| --- | --- | --- |
| `version` | yes | `1` |
| `mode` | no | `enforce` (the default) or `warn`. `warn` turns every error into a warning |
| `extends` | no | the name of a preset of the server config or a list of names ([configuration.en.md](configuration.en.md#presets)) |
| `rules` | no | rules, up to 50: rule name → rule. The name is `[a-z0-9-]{1,63}` |
| `rules.<name>.match` | yes | the branches the rule applies to: `all`, `default` or a list of branches ([match.en.md](match.en.md)) |
| `rules.<name>.parser` | yes | `conventional` or `free` ([below](#parser)) |
| `rules.<name>.except` | no | which commits to skip ([below](#exceptions-except)) |
| `rules.<name>.checks` | no | checks of the catalogue ([below](#catalogue-of-checks-checks)) |
| `rules.<name>.message` | yes | text for the author of a commit with a violation, up to 1000 characters |
| `rules.<name>.help` | no | `contact` and `docs_url`, as in [PATHPOLICY](pathpolicy.en.md#field-reference) |

"Required" means required after the merge with the presets. Any field of a rule in the file may be `null`: this removes a rule, a check or a field of a preset.

Every rule whose `match` matches the branch applies. An error of any of them is a denial. A branch that no rule matches is not checked.

Unknown keys, YAML anchors and aliases are an error. The file is up to 64 KiB, with up to 50 usernames in `except.users` of one rule.

### `parser`

`parser` says how the hook takes the header apart.

- `conventional` - a Conventional Commits header, `type(scope)!: subject`. Scope and `!` are optional. Type is Latin letters, digits, `_` and `-`
- `free` - a free header, the whole first line is the subject. Checks of type and scope are not available for such a rule

These requirements always apply, there is no separate check for them:
- `conventional`: the header follows the scheme above, and an empty line separates the header and the body
- `free`: the header is not empty, and an empty line comes before the body
- a message with a NUL byte or a commit larger than 1 MiB is a violation

How the hook reads a message:
- `\r\n` becomes `\n` (the GitLab UI writes `\r\n`), trailing empty lines are dropped
- the trailer block is the last paragraph whose lines all look like `Key: value` or `Key #value`, as in `git interpret-trailers`. When one line of the paragraph is not a trailer, the whole paragraph is part of the body

### Catalogue of checks (`checks`)

Every check is written as `{ level, value, types }`:
- `level` - `error` (a denial) or `warn` (only a warning). A check of a preset is turned off only with `null`
- `value` - the value of the check. `body-required` has none
- `types` - an optional list of types: the check applies only to commits of these types. Only with `parser: conventional`

| Check | `value` | `parser` | What it checks | Fails |
| --- | --- | --- | --- | --- |
| `type-enum` | a list | conventional | the type is in the list | `bugfix: round refunds` with `[feat, fix]` |
| `scope` | `optional`, `required`, `forbidden` | conventional | whether there is a scope | `fix: round refunds` with `required` |
| `scope-enum` | a list | conventional | the scope is in the list. A scope `a, b` or `a/b` is split into parts | `fix(ui): round refunds` with `[api, billing]` |
| `breaking-bang` | `allow`, `forbid` | conventional | whether `!` is allowed | `feat!: drop v1` with `forbid` |
| `header-max-length` | a number | any | the length of the header in characters | a header of 101 characters with `100` |
| `subject-case` | `lower-first`, `sentence`, `any` | any | the first letter of the subject | `fix: Round refunds` with `lower-first` |
| `subject-full-stop` | `forbid`, `require` | any | a full stop at the end of the subject | `fix: round refunds.` with `forbid` |
| `body-required` | none | any | there is a body | a message of one header |
| `body-max-line-length` | a number | any | the length of every line of the body. Lines with URLs and code do not count | a body line of 101 characters with `100` |
| `trailer-required` | a list of keys | any | the trailer block has these keys | a commit without `-s` with `[Signed-off-by]` |
| `pattern` | `location`, `value`, `keys` | any | a regular expression in a part of the message | [below](#pattern) |

Numbers in `value` are from 1 to 10000. New checks appear in new versions without a change of `version`. An unknown check in a new version of the file is an error, and in the default branch (for example after a rollback of the binary) it is skipped with a warning.

### `pattern`

`pattern` looks for a regular expression in the given part of the message:

| Field | Value |
| --- | --- |
| `location` | `header`, `body`, `trailer` or `message` (the whole message) |
| `value` | `/regex/` or `/regex/i` - must be found, `!/regex/` or `!/regex/i` - must not be found |
| `keys` | only with `location: trailer`: the trailer keys whose values are searched, case-insensitively. Without `keys` - all trailers |

The regex is in RE2 syntax, as in GitLab CI and in [match](match.en.md), and is searched anywhere in the part, like `=~`. To anchor to the start or the end, write `^` and `$`. For `body` and `message` `^` and `$` mean the start and the end of the whole part, for lines write `(?m)`. With `location: trailer` the regex is checked against the value of every trailer separately, and one match is enough.

One rule has one `pattern`. Several requirements are several rules with the same `match` ([example 6](#6-several-requirements-for-a-message)).

### Exceptions (`except`)

| Key | What is skipped |
| --- | --- |
| `merge_commit: true` | the merge commit itself |
| `merged_commits: true` | the commits a merge commit brings through its second parent. Only the first-parent chain is checked. Fast-forward, rebase and squash are checked as usual |
| `git_reverts: true` | messages of `git revert` and of the revert button of GitLab: the header `Revert "..."` and the line `This reverts commit <sha>` |
| `users: [...]` | pushes of these users, for example of a release bot |

`except.users` applies only to a person or a bot the hook recognized. A push with a deploy key does not fall under the exception.

### Which commits are checked

| `match` of the rule | Branch | Which commits |
| --- | --- | --- |
| `default`, `all` | the default branch | every commit the push adds to the branch |
| a list | a branch of the list | commits that the default branch and the other branches of the list do not have yet |
| `all` | other branches | only new commits that no branch or tag has yet |

- a merge of `main` into a feature branch or a rebase onto a fresh `main` does not check the commits that already exist, only the merge commit and the own new commits
- a merge request without squash into the default branch brings every commit of the branch and the merge commit. A squash merge brings one commit, its message comes from the squash commit template of the project
- messages of tags are not checked
- history before COMMITPOLICY appeared is not checked. The exception is a merge into the default branch of a branch with old commits: they enter the default branch with the merge
- more than 10 000 commits entering a branch in one push are not checked: `limit_exceeded`, a denial in `enforce`, a warning in `warn`

## Examples

### 1. Conventional Commits in main and release branches

The task: in `main` and `release/*` messages follow Conventional Commits, with `Signed-off-by` and a body for `feat` and `fix`. Release branches take only `fix`, `docs` and `chore`. Other branches are not checked, there people write as they like.

```yaml
# examples/conventional-commits/COMMITPOLICY
# Conventional Commits in main and release branches; a stricter type list in
# release branches. Other branches are not checked.
version: 1
mode: enforce

rules:
  conventional:
    match: [main, '/^release\//']
    parser: conventional
    except:
      merge_commit: true
      git_reverts: true
      users: [release-bot]
    checks:
      type-enum:         { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
      scope:             { level: error, value: optional }
      breaking-bang:     { level: error, value: allow }
      header-max-length: { level: warn,  value: 100 }
      subject-case:      { level: error, value: lower-first }
      subject-full-stop: { level: error, value: forbid }
      body-required:     { level: error, types: [feat, fix] }
      pattern:           { level: warn,  location: message, value: '/[A-Za-z0-9_.\/-]*#[0-9]+/', types: [feat, fix] }
      trailer-required:  { level: error, value: [Signed-off-by] }
    message: >-
      Commits in main and release branches follow Conventional Commits.
      Reword them with git rebase -i or squash the merge request.

  release:
    match: ['/^release\//']
    parser: conventional
    checks:
      type-enum: { level: error, value: [fix, docs, chore] }
    message: Release branches take only fix, docs and chore commits.
```

In `release/1` both rules apply: a message must pass `conventional` and `release`.

### 2. Which messages pass

For the rule `conventional` of example 1 in `main`. `\n` in the table is a line break.

<!-- check: lint-commit examples/conventional-commits/COMMITPOLICY conventional -->
| Message | Result | Why |
| --- | --- | --- |
| `fix(billing): round refunds to cents\n\nRefunds lost a cent.\n\nRefs: #665\nSigned-off-by: A <a@example.com>` | passes | everything is in place |
| `docs: fix a typo\n\nSigned-off-by: A <a@example.com>` | passes | `body-required` and `pattern` apply only to `feat` and `fix` |
| `feat(api)!: drop v1\n\nV1 is gone.\n\nRefs: #2\nSigned-off-by: A <a@example.com>` | passes | `!` is allowed (`breaking-bang: allow`) |
| `bugfix: round refunds\n\nSigned-off-by: A <a@example.com>` | denied | `type-enum`: `bugfix` is not in the list |
| `fix: Round refunds\n\nBody.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | denied | `subject-case`: the subject starts with a capital letter |
| `fix: round refunds.\n\nBody.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | denied | `subject-full-stop`: a full stop at the end |
| `fix: round refunds\n\nSigned-off-by: A <a@example.com>` | denied | `body-required`: `fix` has no body |
| `fix: round refunds\n\nBody without a reference.\n\nSigned-off-by: A <a@example.com>` | warning | `pattern` of level `warn`: no reference to an issue |
| `docs: fix a typo` | denied | `trailer-required`: no `Signed-off-by` |
| `Revert "feat: x"\n\nThis reverts commit 0123456789abcdef0123456789abcdef01234567.` | passes | `except.git_reverts` |

The denial in the output of a push:

```text
remote: COMMITPOLICY rule "conventional", branch main: 1 commit message does not follow the format
remote:   required: type(scope): subject, for example feat(api): add refunds
remote:   types: feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test
remote:   commits:
remote:     60a6b73c3 "Fix deploy."
remote:       - header: expected "type(scope): subject"
remote:       - trailer-required: missing Signed-off-by in the trailer block
remote:   Commits in main and release branches follow Conventional Commits. Reword them with git rebase -i or squash the merge request.
remote:   how to fix: reword the commit (git commit --amend for the last one, git rebase -i for an older one), then push again
```

### 3. A header that `parser: conventional` cannot read

These errors always appear, with any `checks` or none: a rule with `parser: conventional` requires a header in this format.

<!-- check: lint-commit parser=conventional -->
| Header | Error of `lint-commit` |
| --- | --- |
| `fix round refunds` | `header: expected "type(scope): subject"` |
| `fix:round refunds` | `header: expected "type(scope): subject", a space must follow ":"` |
| `fix:  round refunds` | `header: expected "type(scope): subject", exactly one space must follow ":"` |
| `fix(billing: round refunds` | `header: expected "type(scope): subject", the scope parenthesis is not closed` |
| `fix(): round refunds` | `header: the scope in parentheses is empty` |
| `fix:` | `header: expected "type(scope): subject", the subject is empty` |
| `fix (billing): round refunds` | `header: expected "type(scope): subject"` |
| `[fix] round refunds` | `header: expected "type(scope): subject"` |
| `fix: round\nbody right after` | `the body must be separated from the header by an empty line` |

The hint after the comma appears when the hook can tell what exactly is wrong. Without a hint the header does not look like `type(scope): subject` at all: a space before the parenthesis, square brackets, an issue key instead of a type.

### 4. A free header with an issue key

The task: the team writes headers as `PAY-123 Round refunds to cents`. The Conventional Commits format is not needed, but the issue key at the start is.

```yaml
# examples/free-format/COMMITPOLICY
# A free-form header that starts with an issue key, on the default branch only.
version: 1

rules:
  issue-key:
    match: default
    parser: free
    except:
      merge_commit: true
    checks:
      header-max-length: { level: error, value: 72 }
      subject-full-stop: { level: error, value: forbid }
      pattern:           { level: error, location: header, value: '/^[A-Z][A-Z0-9]+-[0-9]+ \S/' }
    message: Start the header with the issue key, for example PAY-123.
```

<!-- check: lint-commit examples/free-format/COMMITPOLICY -->
| Message | Result | Why |
| --- | --- | --- |
| `PAY-123 Round refunds to cents` | passes | the issue key is at the start |
| `Round refunds to cents` | denied | `pattern`: no key at the start of the header |
| `pay-123 round refunds` | denied | `pattern`: the issue key is in capital letters |
| `PAY-123 Round refunds to cents.` | denied | `subject-full-stop`: with `parser: free` the subject is the whole header |

### 5. `pattern` in every part of a message

Every row is a separate rule with `parser: free` and one `pattern`.

<!-- check: pattern -->
| `location` | `value` | `keys` | Message | Result | Why |
| --- | --- | --- | --- | --- | --- |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | | `PAY-12 Round refunds` | passes | the issue key is at the start of the header |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | | `Round refunds PAY-12` | denied | the key is there, but not at the start, and the regex has `^` |
| `header` | `!/^(WIP\|fixup!)/i` | | `wip: try` | denied | `!` requires that the regex is not found, and `i` ignores case |
| `header` | `!/^(WIP\|fixup!)/i` | | `Fix login` | passes | the regex is not found |
| `body` | `/(?m)^- /` | | `Fix login\n\n- check the token` | passes | `(?m)` makes `^` the start of every line of the body |
| `body` | `/(?m)^- /` | | `Fix login\n\nChecks the token.` | denied | the body has no list line |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nRefs: payments/api#12` | passes | the value of the trailer `Refs` matches |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nCloses: #12` | denied | there is a trailer, but with another key |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nSee #12 in the tracker` | denied | the last paragraph is plain text, not a trailer |
| `message` | `/#[0-9]+/` | | `Fix login (#12)` | passes | `message` is the whole message, the header too |
| `message` | `!/password/i` | | `Fix login\n\nThe Password is hunter2.` | denied | the regex is found in the body |

### 6. Several requirements for a message

The task: the default branch has no WIP commits, and every commit has a reference to an issue in a trailer. These are two `pattern` checks, so two rules with the same `match`. Both apply.

```yaml
# examples/commit-patterns/COMMITPOLICY
# Two requirements for the commits of the default branch, one rule each: no
# work-in-progress headers, and a reference to an issue in a trailer.
version: 1

rules:
  no-wip:
    match: default
    parser: free
    checks:
      pattern: { level: error, location: header, value: '!/^(WIP|fixup!|squash!)/i' }
    message: Squash or reword work-in-progress commits before the merge.
  issue-ref:
    match: default
    parser: free
    except:
      merge_commit: true
    checks:
      pattern: { level: error, location: trailer, keys: [Refs, Closes], value: '/^[A-Za-z0-9_.\/-]*#[0-9]+$/' }
    message: 'End the message with a trailer such as "Refs: payments/api#123".'
```

<!-- check: lint-commit examples/commit-patterns/COMMITPOLICY -->
| Message | Result | Why |
| --- | --- | --- |
| `Fix login\n\nRefs: payments/api#12` | passes | both rules are met |
| `Fix login\n\nCloses: #12` | passes | `Closes` is in `keys` too |
| `Fix login\n\nRefs: 12` | denied | `issue-ref`: the value has no `#` |
| `fixup! Fix login\n\nRefs: #12` | denied | `no-wip`: the header starts with `fixup!` |

### 7. The commitizen schema

The task: the project checked messages with a commitizen schema: a type from a list, a header of up to 100 characters, a trailer `ref` with a GitLab issue. In COMMITPOLICY the schema falls into three parts: `parser: conventional` takes the header apart, `checks` check the list and the length, `pattern` checks the trailer.

```yaml
# examples/commitizen/COMMITPOLICY
# The commitizen schema of a project: the type from a list, a header of up to
# 100 characters and a trailer ref, refs or issue with a GitLab issue. The
# parser takes the header apart, the checks cover the list and the length,
# the pattern covers the trailer.
version: 1

rules:
  commitizen:
    match: all
    parser: conventional
    except:
      merge_commit: true
    checks:
      type-enum:         { level: error, value: [build, ci, docs, feat, fix, perf, refactor, style, test, chore, revert, bump] }
      header-max-length: { level: error, value: 100 }
      pattern:           { level: error, location: trailer, keys: [ref, refs, issue], value: '/^[a-zA-Z0-9_.\/-]*#\d+$/' }
    message: 'Commits follow the commitizen schema: type: subject, then a trailer such as "ref: payments/api#123".'
```

<!-- check: lint-commit examples/commitizen/COMMITPOLICY -->
| Message | Result | Why |
| --- | --- | --- |
| `feat: add x\n\nref: group/project#1` | passes | a type from the list, a trailer `ref` with an issue |
| `fix: x\n\nref: group/my.project#1\nSigned-off-by: A <a@example.com>` | passes | other trailers do not get in the way |
| `feat(: add x\n\nref: group/project#1` | denied | the header cannot be read |
| `feat: add x\nbody right after the header\nref: group/project#1` | denied | no empty line after the header |
| `feat: add x` | denied | no trailer `ref` |

### 8. A preset of the organization

The task: the commit rules are shared by all projects and live in the server config. The project only warns about long headers and allows a full stop at the end of the subject.

The preset (a fragment of `examples/presets.yaml`, the key `presets.commitpolicy`):

```yaml
presets:
  commitpolicy:
    org-commits:
      rules:
        conventional:
          match: default
          parser: conventional
          except:
            merge_commit: true
            git_reverts: true
          checks:
            type-enum: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, test] }
            header-max-length: { level: error, value: 100 }
            subject-full-stop: { level: error, value: forbid }
          message: Commits in the default branch follow Conventional Commits.
          help:
            docs_url: https://www.conventionalcommits.org/en/v1.0.0/
```

The file of the project:

```yaml
# examples/org-commits/COMMITPOLICY
# The commit rules of the organization come from the preset org-commits.
# This project only warns about long headers and allows a full stop at the
# end of the subject.
version: 1
extends: org-commits

rules:
  conventional:
    checks:
      header-max-length: { level: warn }
      subject-full-stop: null
```

- `header-max-length: { level: warn }` changes only the level, `value: 100` stays from the preset: a mapping merges field by field
- `subject-full-stop: null` removes the check of the preset

```text
$ gitlab-repopolicy validate --config presets.yaml
COMMITPOLICY: valid
  extends: org-commits
  rule conventional: preset:org-commits, file
    checks: preset:org-commits, file
    checks.header-max-length: preset:org-commits, file
    checks.type-enum: preset:org-commits
    except: preset:org-commits
    except.git_reverts: preset:org-commits
    except.merge_commit: preset:org-commits
    help: preset:org-commits
    help.docs_url: preset:org-commits
    match: preset:org-commits
    message: preset:org-commits
    parser: preset:org-commits
```

When the preset of `extends` is missing from the server config or broken, COMMITPOLICY is skipped with a warning: a check of commit messages is not worth blocking the work of every project.

## Denials

| Situation | Result |
| --- | --- |
| a violation of a check of level `error` | denied with `commit_format_violation` |
| a violation of a check of level `warn` or any violation with `mode: warn` | the push goes through, the violation is in the output of the push and in audit |
| more than 10 000 commits enter the branch | `limit_exceeded`: a denial in `enforce`, a warning in `warn` |
| COMMITPOLICY in the default branch is invalid, a preset is missing or broken | the check is skipped, a warning |
| an unknown check in the default branch | only this check is skipped, a warning |
| the API did not answer when the hook needs to know who pushes (for `except.users`) | the rule is skipped, a warning |
| a push changes COMMITPOLICY to an invalid version | denied with `invalid_policy_update` |

COMMITPOLICY keeps the history in order, it does not guard access. So when the check cannot be made, the push goes through with a warning instead of being blocked.

A warning in the output of a push:

```text
remote: gitlab-repopolicy: push accepted with 1 warning
remote:
remote: COMMITPOLICY rule "conventional", branch main: 1 commit message does not follow the format; this is a warning, it does not block the push
remote:   required: type(scope): subject, for example feat(api): add refunds
remote:   types: feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test
remote:   commits:
remote:     7680d8aca "fix: round refunds"
remote:       - pattern: no match of /[A-Za-z0-9_.\/-]*#[0-9]+/ in the message
remote:   Commits in main and release branches follow Conventional Commits. Reword them with git rebase -i or squash the merge request.
remote:   next time: follow the format; commits that are pushed stay as they are
```

The warnings of a successful push show in the terminal as `remote:` lines. For a merge by the button, the Web IDE and other actions in the GitLab UI GitLab does not show the successful output of the hook: there warnings are visible only in audit.

## Common mistakes

| Symptom | Cause | What to do |
| --- | --- | --- |
| `parser: required` from `validate` | `parser` is required | write `parser: conventional` or `parser: free` |
| `checks.type-enum: needs parser: conventional` | a check of the type with `parser: free` | change `parser` or remove the check |
| `pattern` with `location: trailer` does not see the reference | the reference is in the body, not in the last paragraph, or a plain line sits between the trailers | move the reference into a separate last paragraph `Refs: #12` |
| `pattern` for `body` does not find a line that starts with `- ` | `^` means the start of the whole body | write `/(?m)^- /` |
| a denial for `feat:` in a release branch, although it passes in `main` | both rules of example 1 apply in a release branch | this is intended. For local checks use `lint-commit --rule` |
| a merge commit of GitLab is rejected | the header `Merge branch ...` is not in the Conventional Commits format | `except.merge_commit: true` |
| `checks.body-required.level: "off" is not one of error, warn` | there is no value `off` | to turn a check of a preset off, write `checks.body-required: null` |

## Checking without a push

`gitlab-repopolicy lint-commit` checks messages with the same code as the hook.

| Command | What it checks |
| --- | --- |
| `gitlab-repopolicy lint-commit -m "feat: add x"` | one message against `COMMITPOLICY` of the current directory |
| `gitlab-repopolicy lint-commit --range origin/main..HEAD` | the commits of a range. One revision is its whole history. At most 100 000 commits |
| `-f FILE` | another COMMITPOLICY file |
| `--rule NAME` | only this rule, can be repeated. An unknown name is an error with the list of rules of the file |
| `--skip-author EMAIL` | with `--range`: skip the commits of this author, can be repeated |
| `--config FILE` | the server config with presets, when the file uses `extends`. Other keys are not read |

Without `--rule` `lint-commit` applies every rule of the file and ignores `match`: locally it is unknown which branch the commit goes to. `except.users` and `except.merge_commit` do not apply to `-m`.

```text
$ gitlab-repopolicy lint-commit -f examples/conventional-commits/COMMITPOLICY -m "Fix deploy."
message: [conventional] error: header: expected "type(scope): subject"
message: [conventional] error: trailer-required: missing Signed-off-by in the trailer block
message: [release] error: header: expected "type(scope): subject"
```

As a `commit-msg` hook of git:

```sh
#!/bin/sh
# .git/hooks/commit-msg
exec gitlab-repopolicy lint-commit --rule conventional -m "$(git stripspace --strip-comments < "$1")"
```

`git stripspace` removes the comment lines of the editor. `--rule conventional` is needed when the rules of different branches contradict each other: the rule `release` of example 1 allows only `fix`, `docs` and `chore`, and without `--rule` the hook would reject `feat:`.

In the CI of a merge request the same checks run before the merge:

```yaml
# .gitlab-ci.yml
repopolicy:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - gitlab-repopolicy validate
    - gitlab-repopolicy lint-commit --rule conventional --range "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME..HEAD"
```

For `origin/<target>` the job needs the history of the target branch: `GIT_DEPTH: 0` or `git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"`. The exit code of `lint-commit` is 0 when there are no violations of level `error`, 1 when there are, and 2 for wrong arguments. More about `validate` - [validate.en.md](validate.en.md), all flags - [cli.en.md](cli.en.md).
