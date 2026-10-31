[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](conventional-commits.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](conventional-commits.en.md)

# Conventional Commits in main and release branches

The team wants readable history in `main` and `release/`: defined change types, `Signed-off-by`, and explanations for `feat` and `fix`. Release branches allow only `fix`, `docs` and `chore`; this file does not check work branches.

## Rules

Save this as `COMMITPOLICY` in the repository root. Run `gitlab-repopolicy validate -f COMMITPOLICY` before merging it into the default branch. `release-bot` must exist and be active: acceptance checks a new `except.users` entry through the API.

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

`main` matches `conventional`. `release/1` matches both rules: general requirements remain, and `release` narrows the type list. `header-max-length` and issue references produce warnings rather than denials. Exceptions allow ordinary merge commits, standard reverts and pushes by a recognized release-bot; they apply only to their own rule.

## Message and validation

This commit satisfies both rules:

```text
fix(billing): round refunds to cents

Refunds lost a cent.

Refs: #665
Signed-off-by: A <a@example.com>
```

Save the message in `message.txt` and run `gitlab-repopolicy lint-commit -f COMMITPOLICY -m "$(cat message.txt)"`. Exit 0 means no `error` findings. Add `--rule conventional` to check only the general standard: the local linter does not select rules using `match`.

`feat(api): add validation` with a body and trailers can pass the general rule but fails `release` when linting all rules locally or pushing to a release branch. Do not expand the release list if new features are intentionally forbidden there.

Start observation with `mode: warn` in the file. Under global `audit`, even policy `mode: enforce` does not block pushes; testing actual rejection requires server `enforce`.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
