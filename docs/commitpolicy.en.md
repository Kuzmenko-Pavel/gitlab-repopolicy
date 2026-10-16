[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](commitpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](commitpolicy.en.md)

# COMMITPOLICY

COMMITPOLICY checks the format of every commit message that this push brings into a branch. The file lives in the repository root as `COMMITPOLICY`, its content is YAML.

Teams have different standards: Conventional Commits with or without a scope, a required issue or not, prose or lists in the body, a free format. So the module is not one schema but a catalogue of independent rules. Every rule has a level, a value and an optional filter by type. A rule that is not listed is not checked: there are no built-in default rules.

Who changes COMMITPOLICY itself is set by a PATHPOLICY path rule ([pathpolicy.en.md](pathpolicy.en.md)). Without such a rule anyone with push access changes the file.

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

Every ruleset that matches the branch applies (AND). In a `refs` list, `*` matches within one segment: `refs/heads/*` matches `main` but not `feature/x`.

Unknown keys, YAML anchors and aliases are an error. The file size is up to 64 KiB.

## Formats

**`conventional`** - the header is `type(scope)!: subject`: a type, an optional scope in parentheses, an optional `!`, a colon, one space and a non-empty subject.

**`free`** - the whole header is the subject. Rules about type and scope are not available for such a ruleset.

The grammar of the format is always checked, there is no separate rule for it:
- for `conventional` - the header as above, an empty line between the header and the body, a correct trailer block
- for `free` - a non-empty header and an empty line before the body
- a message with a NUL byte or a commit object larger than 1 MiB is a violation

How a message is read:
- `\r\n` becomes `\n` (the GitLab UI writes `\r\n`), trailing empty lines are dropped (git always adds `\n`)
- the trailer block is the last paragraph whose every line looks like `Key: value` or `Key #value`, as in `git interpret-trailers`. `BREAKING CHANGE:` is a trailer too. The order of trailers does not matter
- the body is not limited in form: lists, paragraphs, code, long descriptions are fine as long as `body-max-line-length` is not set

## Rule catalogue

Every rule is `{ level, value, types }`:
- `level` - `error`, `warn` or `off`
- `value` - the value of the rule (`body-required` has none)
- `types` - an optional list of types: the rule applies only to commits of these types

| Rule | `value` | Format | What it checks |
| --- | --- | --- | --- |
| `type-enum` | list | conventional | the type is in the list |
| `scope` | `optional`, `required`, `forbidden` | conventional | presence of a scope |
| `scope-enum` | list | conventional | the scope is in the list. A scope `a, b` or `a/b` is split into parts, each must be in the list |
| `breaking-bang` | `allow`, `forbid` | conventional | `type!:` |
| `header-max-length` | number | any | length of the first line in Unicode characters |
| `subject-case` | `lower-first`, `sentence`, `any` | any | case of the first letter of the subject |
| `subject-full-stop` | `forbid`, `require` | any | a full stop at the end of the subject |
| `body-required` | none | any | there is a body |
| `body-max-line-length` | number | any | length of every body line. Lines with a URL and code (a fenced block or indented lines) are not counted |
| `reference` | `location`, `pattern`, `keys` | any | an issue reference is found in the given place |
| `trailer-required` | list of keys | any | trailers with these keys are present |
| `pattern` | regex | any | the whole message matches the regex |

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
- Every commit that this push adds to the branch is checked. When a protected branch is created - the commits that are not in the default branch and other protected branches. When another branch is created - the new commits
- A merge request without squash brings every commit of the branch and the merge commit. A squash merge brings one commit, its message comes from the squash commit template of the project
- Tag messages are not checked. TAGPOLICY checks tag signatures ([tagpolicy.en.md](tagpolicy.en.md))
- History from before COMMITPOLICY appeared is not checked

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

COMMITPOLICY is about hygiene, not access, so a failure does not block work (fail open). What the author of a push controls is a violation, not a skip.

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
| `gitlab-repopolicy lint-commit --range origin/main..HEAD` | the commits of a range; one revision means its whole history |
| `-f FILE` | another COMMITPOLICY file |
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

The binary in the job comes from a verified release, for example in the runner image. For `origin/<target>` the job needs the history of the target branch: `GIT_DEPTH: 0` or `git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"`. The exit code of `lint-commit` is 0 when there are no `error` level violations (warnings do not count) and 1 when there are.
