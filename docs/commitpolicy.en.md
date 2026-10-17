[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](commitpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](commitpolicy.en.md)

# COMMITPOLICY

COMMITPOLICY checks how the messages of the commits that a push adds to a branch are written. The file lives in the repository root as `COMMITPOLICY`, its content is YAML. If a message does not follow the rules, GitLab rejects the push, and the commit has to be reworded.

First, what a commit message consists of and how to write one. Then, how COMMITPOLICY checks it.

## What a commit message consists of

Here is a full message in the [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/) style:

```text
fix(billing): round refunds to cents

Refunds were computed in floating point and lost a cent on some amounts.
Amounts are now kept in integer cents.

Refs: #665
Signed-off-by: Olena Ivanenko <olena@example.com>
```

It has three parts, separated by an empty line.

| Part | In the example | What it is |
| --- | --- | --- |
| header | `fix(billing): round refunds to cents` | the first line. `git log --oneline`, the commit list in GitLab and the merge request title show it |
| body | two lines about refunds | the explanation: what changed and why. Optional unless the project rules require it |
| trailer block | `Refs: #665` and `Signed-off-by: ...` | the last paragraph, where every line looks like `Key: value` or `Key #value`. It holds service data: an issue reference, the author under the DCO |

A Conventional Commits header has parts too.

| Part | In the example | What it is |
| --- | --- | --- |
| type | `fix` | the kind of change: `feat` - a new feature, `fix` - a fix, `docs` - documentation and so on. The project sets the list of allowed types |
| scope | `billing` | optional, in parentheses right after the type. The part of the system the change is about |
| `!` | none | optional, before the colon: `feat(api)!: drop the v1 endpoints`. Marks a change that breaks compatibility |
| `: ` | `: ` | a colon and exactly one space |
| subject | `round refunds to cents` | a short description of the change, not empty |

This is how the header looks in different variants. The errors are the output of `gitlab-repopolicy lint-commit`, the hook prints the same text on push.

| Header | Result |
| --- | --- |
| `fix: round refunds to cents` | correct, without a scope |
| `fix(billing): round refunds to cents` | correct, with a scope |
| `feat(api)!: drop the v1 endpoints` | correct, the change breaks compatibility |
| `fix round refunds to cents` | `header: expected "type(scope): subject"` - no colon |
| `fix:round refunds to cents` | `header: expected "type(scope): subject", a space must follow ":"` |
| `fix:  round refunds to cents` | `header: expected "type(scope): subject", exactly one space must follow ":"` |
| `fix(billing: round refunds to cents` | `header: expected "type(scope): subject", the scope parenthesis is not closed` |
| `fix(): round refunds to cents` | `header: the scope in parentheses is empty` |
| `fix:` | `header: expected "type(scope): subject", the subject is empty` |

If text follows the header right away, without an empty line, it is the error `the body must be separated from the header by an empty line`.

### How to write such a message

Every `-m` is a separate paragraph, git puts an empty line between them itself.

```sh
git commit -s \
  -m "fix(billing): round refunds to cents" \
  -m "Refunds were computed in floating point and lost a cent on some amounts. Amounts are now kept in integer cents." \
  -m "Refs: #665"
```

Or `git commit -s` without `-m`. Then git opens an editor, the first line is the header, then an empty line and the body. To see the result, run `git log -1 --format=%B`.

`-s` (`--signoff`) adds the line `Signed-off-by: Name <email>` from `user.name` and `user.email` of your git to the trailer block. It is a text mark that the author agrees to the contribution terms of the project (DCO). If the project requires `trailer-required: [Signed-off-by]`, a push without `-s` does not pass.

`-S` (`--gpg-sign`, a capital letter) is a different thing. It is a cryptographic signature of the commit with a GPG or SSH key. COMMITPOLICY checks only the text of the message, it neither checks nor requires a commit signature. TAGPOLICY checks tag signatures ([tagpolicy.en.md](tagpolicy.en.md)).

### How to fix a commit after a denial

The hook rejects the whole push, so the rejected commits exist only on your machine. Fix the message and push again:
- the last commit: `git commit --amend` (with `-s` if `Signed-off-by` is missing)
- several commits: `git rebase -i origin/main`, in the list replace `pick` with `reword` next to the commits and rewrite their messages

If `rebase -i` also rewrote commits that were already on the server before, push with `--force-with-lease`.

You can check a message locally before the push, see [Checking locally](#checking-locally).

## How COMMITPOLICY checks it

The file consists of rulesets. A ruleset is a set of rules and the branches it applies to. For example, strict rules for `main` and release branches, and feature branches without checks. A push passes only when every ruleset that applies to the branch found no errors.

Teams have different standards: Conventional Commits with or without a scope, a required issue or not, prose or lists in the body, a free format. So the rules are a catalogue of independent checks, not one schema. Every rule has a level, a value and an optional filter by type. A rule that is not in the file is not checked, there are no built-in default rules.

Who may change COMMITPOLICY itself is set by a PATHPOLICY rule ([pathpolicy.en.md](pathpolicy.en.md)). Without such a rule anyone with push access changes the file.

## Examples

Conventional Commits in protected branches:

```yaml
# examples/conventional-commits/COMMITPOLICY
# Conventional Commits in protected branches; a stricter type list in
# release branches. Feature branches are not checked.
version: 1
mode: enforce

rulesets:
  - name: protected
    refs: protected
    format: conventional
    except:
      merge_commits: true
      git_reverts: true
      users: [release-bot]
    rules:
      type-enum:         { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
      scope:             { level: error, value: optional }
      breaking-bang:     { level: error, value: allow }
      header-max-length: { level: warn,  value: 100 }
      subject-case:      { level: error, value: lower-first }
      subject-full-stop: { level: error, value: forbid }
      body-required:     { level: error, types: [feat, fix] }
      reference:         { level: warn,  location: any, pattern: "[A-Za-z0-9_./-]*#[0-9]+", types: [feat, fix] }
      trailer-required:  { level: error, value: [Signed-off-by] }
    message: >-
      Commits in protected branches follow Conventional Commits.
      Reword them with git rebase -i or squash the merge request.

  - name: release
    refs: ["refs/heads/release/*"]
    format: conventional
    rules:
      type-enum: { level: error, value: [fix, docs, chore] }
```

A free format with an issue key at the start of the header:

```yaml
# examples/free-format/COMMITPOLICY
# A free-form header that starts with an issue key, on the default branch only.
version: 1

rulesets:
  - name: issue-key
    refs: default
    format: free
    except:
      merge_commits: true
    rules:
      header-max-length: { level: error, value: 72 }
      subject-full-stop: { level: error, value: forbid }
      pattern:
        level: error
        value: '[A-Z][A-Z0-9]+-[0-9]+ \S'
        examples:
          valid: ["PAY-123 Round refunds to cents"]
          invalid: ["Round refunds to cents", "pay-123 Round refunds"]
    message: Start the header with the issue key, for example PAY-123.
```

## File structure

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | `1` |
| `mode` | no | `enforce` (default) or `warn`. `warn` turns every `error` into a warning: the whole rollout is switched by one line |
| `extends` | no | the name of a preset from `commitpolicy_presets` of the server config ([configuration.en.md](configuration.en.md)) |
| `rulesets` | yes | rule sets |
| `rulesets[].name` | yes | `[a-z0-9-]{1,63}`, unique in the file |
| `rulesets[].refs` | no | `all` (default), `default`, `protected` or a list of branches and tags |
| `rulesets[].format` | no | `conventional` (default) or `free` |
| `rulesets[].except` | no | exceptions, see below |
| `rulesets[].rules` | no | rules from the catalogue |
| `rulesets[].message` | no | text the author of a failing commit sees |

Every ruleset whose `refs` cover the branch applies to it. A violation of any of them is a denial.

### Which branches a ruleset applies to (`refs`)

- `all` - all branches
- `default` - only the default branch of the project, usually `main`
- `protected` - the default branch and the branches the project protects in GitLab (Settings > Repository > Protected branches)
- a list - branches and tags by their full git name

A full name is how git names a branch or a tag inside the repository: the branch `main` is `refs/heads/main`, the branch `release/1.2` is `refs/heads/release/1.2`, the tag `v1.0.0` is `refs/tags/v1.0.0`. The list needs exactly the full name. For a short `main` the `validate` command answers with the error `refs: main is not a ref pattern such as refs/heads/release/*`.

`*` in the list stands for any characters except `/`. `refs/heads/release/*` covers `refs/heads/release/1.2` but not `refs/heads/release/1.2/hotfix`. `refs/heads/*` covers `refs/heads/main` but not `refs/heads/feature/x`.

Unknown keys, YAML anchors and aliases are an error. The file size is up to 64 KiB, up to 50 rulesets and up to 50 usernames in `except.users` of one ruleset.

## Formats

The format sets how the hook parses the header.

**`conventional`** (default) - a Conventional Commits header, as in [What a commit message consists of](#what-a-commit-message-consists-of). The scheme is `type(scope)!: subject`, scope and `!` are optional. A type is Latin letters, digits, `_` and `-`.

**`free`** - a free header, the whole first line is the subject. Rules about type and scope are not available for such a ruleset. The format is for teams that write, for example, `PAY-123 Round refunds to cents`.

These checks always apply, there is no separate rule for them:
- `conventional`: the header as above and an empty line between the header and the body
- `free`: a non-empty header and an empty line before the body
- a message with a NUL byte or a commit object larger than 1 MiB is a violation

How the hook reads a message:
- `\r\n` becomes `\n` (the GitLab UI writes `\r\n`), trailing empty lines are dropped (git always adds `\n`)
- the trailer block is the last paragraph whose every line looks like `Key: value` or `Key #value`, as in `git interpret-trailers`. `BREAKING CHANGE:` is a trailer too. The order of trailers does not matter. If even one line of the paragraph is not a trailer, the whole paragraph is part of the body
- the body is not limited in form: lists, paragraphs, code, long descriptions are fine as long as `body-max-line-length` is not set

## Rule catalogue

Every rule is written as `{ level, value, types }`:
- `level` - `error` (a denial), `warn` (only a warning) or `off` (disabled)
- `value` - the value of the rule. `body-required` has none
- `types` - an optional list of types. The rule applies only to commits of these types. For example, `body-required` with `types: [feat, fix]` requires a body only in `feat` and `fix`

The examples in the last column fail the check with the given value.

| Rule | `value` | Format | What it checks | Fails |
| --- | --- | --- | --- | --- |
| `type-enum` | list | conventional | the type is in the list | `bugfix: round refunds` with `[feat, fix]` |
| `scope` | `optional`, `required`, `forbidden` | conventional | whether there is a scope | `fix: round refunds` with `required` |
| `scope-enum` | list | conventional | the scope is in the list. A scope `a, b` or `a/b` is split into parts, each must be in the list | `fix(ui): round refunds` with `[api, billing]` |
| `breaking-bang` | `allow`, `forbid` | conventional | whether `!` is allowed | `feat!: drop v1` with `forbid` |
| `header-max-length` | number | any | length of the header in Unicode characters | a 101-character header with `100` |
| `subject-case` | `lower-first`, `sentence`, `any` | any | the first letter of the subject: lowercase, uppercase or any | `fix: Round refunds` with `lower-first` |
| `subject-full-stop` | `forbid`, `require` | any | a full stop at the end of the subject | `fix: round refunds.` with `forbid` |
| `body-required` | none | any | there is a body | a message of only a header |
| `body-max-line-length` | number | any | length of every body line. Lines with a URL and code (a fenced block or indented lines) are not counted | a 101-character body line with `100` |
| `reference` | `location`, `pattern`, `keys` | any | an issue reference in the given place | a message without `#665` |
| `trailer-required` | list of keys | any | the trailer block has lines with these keys | a commit without `-s` with `[Signed-off-by]` |
| `pattern` | regex | any | the message matches the regex from its start | see [`pattern`](#pattern) |

`reference`:
- `location: any` - a reference anywhere in the message, for example `(group/project#665)` inside a body paragraph
- `location: body` - only in the body
- `location: trailer` - only in the trailer block; `keys` limits the keys, for example `[Refs, Issue]`. Keys are compared case-insensitively
- `pattern` - an RE2 regex of the reference

Numbers in `value` are from 1 to 10000. The rule catalogue grows in minor versions without a schema change.

## `pattern`

For formats that structural rules do not express. The regex is compiled by the `regexp2` library with the `RE2` option: lookahead and lookbehind are available, and so are `(?P<name>…)` groups. The match starts at the beginning of the normalized message, like `re.match` in Python.

A difference from Python `re`: in Python `$` also matches before a final `\n`. The message is normalized (the final `\n` is dropped), so a pattern tuned in Python works the same here.

`regexp2` is a backtracking engine without a linear time guarantee. So one message gets 100 ms and the whole push 2 s (`timeouts.pattern`, `timeouts.pattern_budget` in the server config). Running out is a `commit_check_timeout` violation at the level of the `pattern` rule for every unchecked commit: the author controls the input and must not bypass the check with slow messages.

`examples` with `valid` and `invalid` are checked on acceptance of a new version and by `validate`. A failing example makes the file invalid.

## Exceptions (`except`)

| Key | What is skipped |
| --- | --- |
| `merge_commits: true` | the merge commit itself |
| `merged_commits: true` | commits a merge commit brings in through its second and further parents. Only the first-parent chain is checked. Fast-forward, rebase and squash are checked as usual |
| `git_reverts: true` | messages of `git revert` and of the GitLab revert button: the header `Revert "..."` and the line `This reverts commit <sha>` |
| `users: [...]` | pushes of these users, for example a release bot. Usernames are compared case-insensitively |

`except.users` applies only to a person or bot the hook recognized. A push with a deploy key does not get the exception. For SSH the hook checks through the API that the key is personal; if the API does not answer, the ruleset is skipped with a warning.

## Which commits are checked

- Only in branches from the `refs` of the ruleset. With `refs: protected` feature branches are free, WIP commits are not blocked
- In the default branch, and for a ruleset with `refs: protected` or a list also in those branches, every commit that this push adds to the branch is checked. When a protected branch is created - the commits that are not in the default branch and other protected branches
- In other branches only the new commits are checked, the ones that no branch or tag of the repository has yet. Merging `main` into a feature branch or rebasing onto a fresh `main` does not check the commits that already exist, only the merge commit itself (unless `except.merge_commits` is on) and the own new commits. A ruleset with `refs: all` checks every commit when it first appears in any branch
- A merge request without squash into the default branch brings every commit of the branch and the merge commit. A squash merge brings one commit, its message comes from the squash commit template of the project
- Tag messages are not checked. TAGPOLICY checks tag signatures ([tagpolicy.en.md](tagpolicy.en.md))
- History from before COMMITPOLICY appeared is not checked. The exception is a merge into the default branch of a branch with old commits: they enter the default branch with the merge and are checked

## Presets (`extends`)

A preset is a named set of rules in the server config, so that the same rules are not copied into every repository:

```yaml
# /etc/gitlab-repopolicy/config.yaml
commitpolicy_presets:
  org-conventional:
    rules:
      type-enum: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
```

Preset rules apply to every ruleset of the file, ruleset rules override them by name. Rules for `conventional` do not apply to a ruleset with `format: free`. An unknown or invalid preset is an acceptance and `validate` error. If a preset disappears from the config after the file was accepted, the hook skips it with a warning.

## Modes and denials

COMMITPOLICY keeps the history in order, it does not guard access. So when the check cannot be done (the file in the default branch is broken, the GitLab API did not answer), the push passes with a warning instead of being blocked. This is called fail open. What the author of a push controls, for example a message that is slow for the regex, is a violation, not a skip.

| Situation | Result |
| --- | --- |
| `mode: warn`, violations | the push passes, violations are in the push output and in audit |
| `mode: enforce`, `error` level violations | denied, `commit_format_violation` |
| a rule with `level: warn` | only a warning, in `enforce` too |
| `pattern` timeout or budget | a `commit_check_timeout` violation at the level of the rule |
| more than 10 000 incoming commits | `limit_exceeded`: denied in `enforce`, a warning in `warn` |
| COMMITPOLICY in the default branch is invalid | the check is skipped, a warning, audit `invalid_commit_policy` |
| an unknown rule in the default branch (for example after a binary rollback) | only this rule is skipped, a warning |
| the API did not answer when it is needed to know whether the branch is protected | a ruleset with `refs: protected` is skipped, a warning |
| a push changes COMMITPOLICY to an invalid version | denied, `invalid_policy_update` |

An unknown rule in a new version of the file is an acceptance error: a typo does not pass silently.

If the project has no PATHPOLICY on the default branch, anyone who can push may change COMMITPOLICY, including weakening its rules. The hook accepts such a version with the warning `the project has no PATHPOLICY on the default branch: anyone who can push may change COMMITPOLICY`. A PATHPOLICY rule protects the file ([pathpolicy.en.md](pathpolicy.en.md#example)). If the PATHPOLICY module is disabled in the config, there is no warning.

Warnings of a successful push show in the terminal as `remote:` lines. For a merge button, the Web IDE and other UI operations GitLab does not show the successful output of the hook: there the warnings are visible only in audit.

## Checking locally

`gitlab-repopolicy lint-commit` uses the same implementation as the hook, so a local check and the server agree. Python or Node is not needed. A developer can install the binary with `go install` ([install.en.md](install.en.md)).

| Command | What it checks |
| --- | --- |
| `gitlab-repopolicy lint-commit -m "feat: add x"` | one message against `COMMITPOLICY` of the current directory |
| `gitlab-repopolicy lint-commit --range origin/main..HEAD` | the commits of a range; one revision means its whole history. At most 100 000 commits |
| `-f FILE` | another COMMITPOLICY file |
| `--skip-author EMAIL` | with `--range`: skip the commits of this author; repeat it for several. A skipped commit prints the line `skipped, author EMAIL` |
| `--ruleset NAME` | only this ruleset; repeat it for several. An unknown name is an error that lists the rulesets of the file |
| `--config FILE` | a file with `commitpolicy_presets`, if COMMITPOLICY uses `extends`. Other config keys are not needed ([configuration.en.md](configuration.en.md#commitpolicy_presets)) |

Without `--ruleset`, `lint-commit` applies every ruleset of the file and ignores `refs`: locally it is not known which branch the commit will go to. `except.users` does not apply locally.

```text
$ gitlab-repopolicy lint-commit -f examples/conventional-commits/COMMITPOLICY -m "Fix deploy."
message: [protected] error: header: expected "type(scope): subject"
message: [protected] error: trailer-required: missing Signed-off-by in the trailer block
message: [release] error: header: expected "type(scope): subject"
```

As a git `commit-msg` hook:

```sh
#!/bin/sh
# .git/hooks/commit-msg
exec gitlab-repopolicy lint-commit --ruleset protected -m "$(git stripspace --strip-comments < "$1")"
```

`git stripspace` removes the comment lines left by the editor: git passes the message file to the hook with them still in place. `--ruleset protected` is needed when rulesets for different branches contradict each other. In the example above the `release` ruleset allows only `fix`, `docs` and `chore`, and without `--ruleset` the hook would reject `feat:`, though main allows it. Take the ruleset of the branches where work usually goes.

In the CI of a merge request the same checks run before the merge, so the author sees the problem in the pipeline and not at merge time:

```yaml
# .gitlab-ci.yml
repopolicy:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - gitlab-repopolicy validate
    - gitlab-repopolicy lint-commit --ruleset protected --range "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME..HEAD"
```

The binary in the job comes from a verified release, for example in the runner image. For `origin/<target>` the job needs the history of the target branch: `GIT_DEPTH: 0` or `git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"`. The exit code of `lint-commit` is 0 when there are no `error` level violations (warnings do not count), 1 when there are, and 2 for wrong command arguments.
