[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](validate.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](validate.en.md)

# Checking policy files: `validate`

`gitlab-repopolicy validate` checks policy files the way the hook checks a new version of a file at a push: the schema, `match`, regular expressions, the order of the rules of BRANCHPOLICY, the keys of TAGPOLICY, the merge with presets. An error shows before the push and before the merge request, not when GitLab has rejected the push.

This guide goes through all modes of the command on a demo directory, with real output. All flags in short are in [cli.en.md](cli.en.md). Commit messages have a command of their own, `lint-commit` ([commitpolicy.en.md](commitpolicy.en.md#checking-without-a-push)).

## When to run it

| Where | Flags | What is needed |
| --- | --- | --- |
| locally before a push | `validate`, `-f FILE`, `--config presets.yaml` for files with `extends` | the binary and the files. GitLab, a token and git are not needed |
| in the CI of a merge request | the same | a binary of a release in the job ([below](#validate-in-the-ci-of-a-merge-request)) |
| on a Gitaly node | `--online --config /etc/gitlab-repopolicy/config.yaml [--project P]` | the server config and the token of the hook, the GitLab API |

| Mode | Flags | GitLab needed | Where to run |
| --- | --- | --- | --- |
| all files of the directory | no flags | no | anywhere |
| one file | `-f FILE`, `--module NAME` for another name | no | anywhere |
| with presets | `--config FILE` | no | anywhere, with a copy of the presets |
| the rule for the name of a branch or a tag | `--branch NAME`, `--tag NAME` | no | anywhere |
| users exist | `--online --config FILE` | yes | a Gitaly node |
| the role of admins in the project | `--online --config FILE --project P` | yes | a Gitaly node |

## Preparation: a demo directory

`validate` reads files from the directory, it does not need git. The repository is here only to be realistic: later these files can be committed and pushed.

```sh
mkdir demo && cd demo && git init
```

The four policy files are the ones of the examples of the documentation:

```sh
cat > PATHPOLICY <<'EOF'
# The platform team, alice and sre-lead, owns the CI configuration and the
# deploy scripts in every branch and the production values in main and
# release branches. Everything else is authorized by GitLab as usual.
version: 1

admins: [alice, sre-lead]

rules:
  ci-config:
    match: all
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "scripts/deploy/**"
    allow: [alice, sre-lead]
    message: >-
      The platform team changes the CI/CD configuration and the deploy scripts.
      Ask alice or sre-lead to push this change.
    help:
      contact: "#platform"

  prod-values:
    match: [main, '/^release\//']
    paths: ["helm/**/values-prod.yaml"]
    allow: [alice, release-manager]
    message: >-
      Production values are changed in main and release branches by alice or
      release-manager. Propose the change in your branch and ask them to merge it.
EOF
```

```sh
cat > COMMITPOLICY <<'EOF'
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
EOF
```

```sh
cat > TAGPOLICY <<'EOF'
# Release tags: staging is signed by release-lead or dev-lead, prod only by
# release-lead, and a prod tag must be protected in GitLab. Tags that match no
# rule, such as build-42, are not checked.
version: 1

rules:
  staging:
    match: ['/^v\d+\.\d+\.\d+-staging$/']
    signers: [release-lead, dev-lead]
    message: Staging releases are signed by release-lead or dev-lead.
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [release-lead]
    protected: true
    message: >-
      Prod releases are signed by release-lead.
      Protect v*-prod in Settings > Repository > Protected tags.

keys:
  release-lead: |
    -----BEGIN PGP PUBLIC KEY BLOCK-----
    
    mDMEas8yyBYJKwYBBAHaRw8BAQdARgQtR6FZCckYy8pf9+9snMoIb3gcbH9QVotM
    2CEt2za0H1JlbGVhc2UgTGVhZCA8bGVhZEBleGFtcGxlLmNvbT6IkwQTFgoAOxYh
    BGx5V+qHOlRnp48dB1l3WaOCcpNZBQJqzzLIAhsDBQsJCAcCAiICBhUKCQgLAgQW
    AgMBAh4HAheAAAoJEFl3WaOCcpNZf7IBAO02jJtCe9KhJlLAOTz/XX+kpgyY8mth
    6Cku2bukuF6bAP40EeqZQ1ISoo/Agcdncm5Z+KysHKFd7y49xzE0q6ObDA==
    =3XJY
    -----END PGP PUBLIC KEY BLOCK-----
  dev-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE ed25519
EOF
```

```sh
cat > BRANCHPOLICY <<'EOF'
# Strict mode: only release and work branches may be created. The last rule
# has match: all and deny, so a name of no other class is refused instead of
# passing unchecked.
version: 1

rules:
  release:
    match: ['/^release\/v\d+\.\d+\.\d+$/']
    priority: 10
    protected: true
    message: Release branches look like release/v1.4.0.
  work:
    match: ['/^[a-z0-9.]+\/[a-z0-9._-]+$/']
    priority: 20
    message: Work branches are <prefix>/<name>, for example kpn/disable_auth.
  other:
    match: all
    priority: 1000
    deny: true
    message: Use release/vX.Y.Z or <prefix>/<name>.
EOF
```

`presets.yaml` is a server config with only the key `presets`, for the scenarios with `extends`. Without `--online` `validate` does not read the other keys of the config:

```sh
cat > presets.yaml <<'EOF'
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
EOF
```

The drafts of the scenarios with errors go to `drafts/`:

```sh
mkdir drafts
```

## Scenarios

Every scenario: what we check, the command, the output, the exit code (`echo $?`) and what to do. Exit codes: `0` - the files are valid (warnings and `not checked` do not get in the way), `1` - an invalid file or an error reading a file or the config, `2` - wrong arguments of the command.

### All files of the directory

Without flags `validate` checks every policy file of the current directory:

```console
$ gitlab-repopolicy validate
PATHPOLICY: valid
COMMITPOLICY: valid
TAGPOLICY: valid
BRANCHPOLICY: valid
$ echo $?
0
```

### One file

```console
$ gitlab-repopolicy validate -f COMMITPOLICY
COMMITPOLICY: valid
$ echo $?
0
```

### A draft with another name

`validate` tells which policy file it is by the name of the file. Another name needs `--module`:

```console
$ cp PATHPOLICY PATHPOLICY.new
$ gitlab-repopolicy validate -f PATHPOLICY.new
gitlab-repopolicy validate: PATHPOLICY.new is not a policy file; use --module with one of branchpolicy, commitpolicy, pathpolicy, tagpolicy
$ echo $?
2
$ gitlab-repopolicy validate -f PATHPOLICY.new --module pathpolicy
PATHPOLICY.new: valid
$ echo $?
0
```

### An empty directory

```console
$ mkdir empty
$ cd empty
$ gitlab-repopolicy validate
no policy files in the current directory
$ echo $?
0
$ cd ..
```

Exit code 0: there are no policy files and nothing to check. In CI this means the job breaks nothing in a project without policies.

### Wrong arguments

Exit code 2 and the error text in stderr:

```console
$ gitlab-repopolicy validate PATHPOLICY
gitlab-repopolicy validate: unexpected arguments ["PATHPOLICY"]
$ echo $?
2
$ gitlab-repopolicy validate --online
gitlab-repopolicy validate: --online needs --config with gitlab_url and token_file
$ echo $?
2
```

A file is given with `-f`, not as an argument.

### Invalid files

A YAML error, with the line number:

```sh
cat > drafts/yaml-error <<'EOF'
version: 1
admins: [alice]
rules:
  ci:
    match: all
     paths: [.gitlab-ci.yml]
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/yaml-error --module pathpolicy
drafts/yaml-error: invalid
  yaml: line 6: mapping values are not allowed in this context
$ echo $?
1
```

Line 6 has an extra space before `paths`.

An unknown key:

```sh
cat > drafts/unknown-key <<'EOF'
version: 1
admins: [alice]
rules:
  ci:
    match: all
    path: [.gitlab-ci.yml]
    allow: [alice]
    message: CI belongs to alice.
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/unknown-key --module pathpolicy
drafts/unknown-key: invalid
  line 6: rule ci: unknown field "path" (did you mean "paths"?)
$ echo $?
1
```

A required field is missing. `match`, `message`, and in COMMITPOLICY also `parser`, are required in every rule:

```sh
cat > drafts/no-match <<'EOF'
version: 1
rules:
  conventional:
    parser: conventional
    message: Commits follow Conventional Commits.
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/no-match --module commitpolicy
drafts/no-match: invalid
  line 4: rule conventional: match: required
$ echo $?
1
```

A glob in `match`:

```sh
cat > drafts/glob <<'EOF'
version: 1
rules:
  release:
    match: ['release/*']
    priority: 10
    protected: true
    message: Release branches are protected.
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/glob --module branchpolicy
drafts/glob: invalid
  line 4: rule release: match: "release/*": a glob is not supported; use a regex such as /^release\//
$ echo $?
1
```

An invalid regular expression:

```sh
cat > drafts/bad-regex <<'EOF'
version: 1
rules:
  prod:
    match: ['/^v\d+(\.\d+$/']
    signers: [release-lead]
    message: Prod releases are signed by release-lead.
keys:
  release-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl release-lead
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/bad-regex --module tagpolicy
drafts/bad-regex: invalid
  line 4: rule prod: match: "/^v\\d+(\\.\\d+$/": not a valid RE2 regex: error parsing regexp: missing closing ): `^v\d+(\.\d+$`
$ echo $?
1
```

The order of the rules of BRANCHPOLICY: `match: all` is not the last, and a repeated `priority`:

```sh
cat > drafts/order <<'EOF'
version: 1
rules:
  other:
    match: all
    priority: 10
    deny: true
    message: Use release/vX.Y.Z or <prefix>/<name>.
  release:
    match: ['/^release\/v\d+\.\d+\.\d+$/']
    priority: 20
    message: Release branches look like release/v1.4.0.
  hotfix:
    match: ['/^hotfix\//']
    priority: 20
    message: Hotfix branches look like hotfix/v1.4.1.
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/order --module branchpolicy
drafts/order: invalid
  error: rule release: comes after rule other with match: all and is never used; give match: all the highest priority
  error: rule hotfix: priority 20 is also the priority of rule release
  error: rule hotfix: comes after rule other with match: all and is never used; give match: all the highest priority
$ echo $?
1
```

The errors of the order of the rules have the prefix `error:`, because the check after the merge with presets finds them. Errors of parsing the file are printed without a prefix.

`signers` refers to a key that does not exist:

```sh
cat > drafts/signers <<'EOF'
version: 1
rules:
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [release-lead, sec-lead]
    message: Prod releases are signed by release-lead or sec-lead.
keys:
  release-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl release-lead
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/signers --module tagpolicy
drafts/signers: invalid
  rule prod: signers: "sec-lead" is not a label in keys of the file
$ echo $?
1
```

### Warnings

A key that no rule uses is only a warning, exit code 0:

```sh
cat > drafts/unused-key <<'EOF'
version: 1
rules:
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [release-lead]
    message: Prod releases are signed by release-lead.
keys:
  release-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl release-lead
  old-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE old-lead
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/unused-key --module tagpolicy
drafts/unused-key: valid
  warning: keys.old-lead is not used by any rule
$ echo $?
0
```

### An expired key

An expired OpenPGP key is an error, exit code 1:

```sh
cat > drafts/expired-key <<'EOF'
version: 1
rules:
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [old-lead]
    message: Prod releases are signed by old-lead.
keys:
  old-lead: |
    -----BEGIN PGP PUBLIC KEY BLOCK-----
    
    xjMEZZIAgBYJKwYBBAHaRw8BAQdAiav9rGtFxuzsm74siFSXUl2FL4BUmXHXmfZv
    oOEhbDbNJ09sZCBSZWxlYXNlIExlYWQgPG9sZC1sZWFkQGV4YW1wbGUuY29tPsLA
    AwQTFggAdQWCZZIAgAWJACeNAAILBwkQX6GOCC8qk+s1FAAAAAAAHAAQc2FsdEBu
    b3RhdGlvbnMub3BlbnBncGpzLm9yZ+8iD3ZF0ZPTRpvuVlRt4vICFQgCFgACGQEC
    mwMCHgEWIQS7QNFPPR1WN7ZS6TtfoY4ILyqT6wAA3J8A/Am90Vr3su2c84Aau/+h
    FrtSuh62JOxfKrusWpSllM/vAP9nYQT0nUmZxbMUu9t5bWaIr8l6Bnhuwf1h9+Na
    gHLNBc44BGWSAIASCisGAQQBl1UBBQEBB0BaRZLBtMaeNDOxD8z9VMwDtZAgf/nv
    JUHIXAhFxOYZEQMBCgnCrgQYFggAYAWCZZIAgAkQX6GOCC8qk+s1FAAAAAAAHAAQ
    c2FsdEBub3RhdGlvbnMub3BlbnBncGpzLm9yZ21byLI0mcu5rLYID0ILD8gCmwwW
    IQS7QNFPPR1WN7ZS6TtfoY4ILyqT6wAADwgA+wcFBElmZt/jvswEQqfBFSpDUP2A
    V64xDDUPafdFGrwYAP9pnTRsdqfsBgzMA4+hP+8xuDN5Tbw38UuUeCmd17SwCg==
    =G3gt
    -----END PGP PUBLIC KEY BLOCK-----
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/expired-key --module tagpolicy
drafts/expired-key: invalid
  error: keys.old-lead: the key BB40D14F3D1D5637B652E93B5FA18E082F2A93EB has no signing key valid on 2026-10-20: it expired; extend it or remove the entry
$ echo $?
1
```

`validate` checks the file by itself. At a push the hook compares the new version with the version in the default branch, and an expired key in an entry that was not changed is only a warning: somebody else's expired key does not block an edit of the file. More - [below](#what-validate-does-not-know).

### Presets

A file with `extends` cannot be checked fully without the server config: the preset is missing.

```sh
cat > drafts/org-commits <<'EOF'
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
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/org-commits --module commitpolicy
drafts/org-commits: invalid
  error: extends: preset "org-commits" is not defined in the server config
$ echo $?
1
```

With `--config` `validate` merges the preset and the file as the hook does and shows where every rule and field comes from:

```console
$ gitlab-repopolicy validate -f drafts/org-commits --module commitpolicy --config presets.yaml
drafts/org-commits: valid
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
$ echo $?
0
```

- `checks.header-max-length: preset:org-commits, file` - the file changed only `level` in the check of the preset, `value` stayed from the preset
- `checks.subject-full-stop` is not in the output: the file removed it with `null`
- a rule without any field from the file has only `preset:org-commits`

A preset that is not in the config:

```sh
cat > drafts/missing-preset <<'EOF'
version: 1
extends: org-missing
EOF
```

```console
$ gitlab-repopolicy validate -f drafts/missing-preset --module commitpolicy --config presets.yaml
drafts/missing-preset: invalid
  error: extends: preset "org-missing" is not defined in the server config
$ echo $?
1
```

`validate` reports a broken preset of `--config` separately, even when no file uses it: `presets.yaml: invalid presets` and `error:` lines.

### Which rule applies: `--branch` and `--tag`

`--branch NAME` shows the rule of BRANCHPOLICY for a new branch with this name and the result, `--tag NAME` - the rules of TAGPOLICY and who may sign the tag. The file is checked as usual first. The exit code is 0 for any result: it is a query, not a check.

```console
$ gitlab-repopolicy validate -f BRANCHPOLICY --branch release/v1.4.0
BRANCHPOLICY: valid
branch release/v1.4.0: rule release (match: [/^release\/v\d+\.\d+\.\d+$/]; from file)
  requires: the project protects the branch in Settings > Repository > Protected branches
  result: allowed when the requirements hold
$ echo $?
0
$ gitlab-repopolicy validate -f BRANCHPOLICY --branch fix-login
BRANCHPOLICY: valid
branch fix-login: rule other (match: all; from file)
  result: denied, branches of this class may not be created
  message: Use release/vX.Y.Z or <prefix>/<name>.
$ echo $?
0
$ gitlab-repopolicy validate -f TAGPOLICY --tag v1.4.0-staging
TAGPOLICY: valid
tag v1.4.0-staging: rules staging (from file)
  signers: release-lead (OpenPGP), dev-lead (SSH)
  the tag must be signed by one of the signers; validate does not check signatures
$ echo $?
0
```

| Name | Command | Rule | Result |
| --- | --- | --- | --- |
| `release/v1.4.0` | `--branch` | `release` | allowed when the project protects the branch |
| `release/1.4` | `--branch` | `work` | allowed: the name looks like `<prefix>/<name>`. For such a name not to pass, a `deny` rule for the family `release/` is needed ([branchpolicy.en.md](branchpolicy.en.md#2-an-exact-rule-and-deny-for-the-whole-family)) |
| `fix-login` | `--branch` | `other` | denied |
| `v1.4.0-prod` | `--tag` | `prod` | release-lead signs, the tag must be protected |
| `build-42` | `--tag` | none | not checked |

Without a file of the right module next to `--branch` or `--tag` it is a usage error: `--branch needs a BRANCHPOLICY file: -f FILE or BRANCHPOLICY in the current directory`.

### `--online`

`--online` checks through the GitLab API that the usernames of the file exist and are active. With `--project` it also checks that the admins of PATHPOLICY have the role Maintainer or higher in the project. The command runs on a Gitaly node as a user who can read the server config and the token file it names, usually `git`:

```sh
sudo -u git gitlab-repopolicy validate -f PATHPOLICY --online --config /etc/gitlab-repopolicy/config.yaml
```

Without `--project` usernames are checked, but the role of the admins is not:

```text
PATHPOLICY: valid
  not checked: admins cannot be checked: --project is not given
```

Exit code 0. An inactive or missing user is an error, exit code 1:

```text
PATHPOLICY: invalid
  error: "nobody" is not an active GitLab user
  not checked: admins cannot be checked: --project is not given
```

With `--project`, when one of two admins is a Developer:

```text
PATHPOLICY: valid
  warning: admin "bob" is not an active user with Maintainer or higher in this project
  warning: 1 of 2 admins work
```

Exit code 0: while at least one admin works, the file is accepted. When none works, the push is rejected with `lockout_prevented`.

The API is unavailable. Without `--project` exit code 0, the checks that could not be made are marked `not checked`:

```text
PATHPOLICY: valid
  not checked: usernames cannot be checked: gitlab /api/graphql: unavailable: Post "https://gitlab.example.com/api/graphql": dial tcp 192.0.2.10:443: connect: connection refused
```

With `--project`, when the project could not be read, it is an error, exit code 1:

```text
gitlab-repopolicy validate: --project acme/platform/app: gitlab /api/v4/projects/acme%2Fplatform%2Fapp: unavailable: Get "https://gitlab.example.com/api/v4/projects/acme%2Fplatform%2Fapp": dial tcp 192.0.2.10:443: connect: connection refused
```

## `validate` in the CI of a merge request

The job runs only when the merge request changes a policy file. The binary comes from a release and is checked against `checksums.txt`, as at the installation ([install.en.md](install.en.md#1-download-and-verify-the-release)):

```yaml
# .gitlab-ci.yml
validate-policies:
  image: alpine:3.20
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
      changes: [PATHPOLICY, COMMITPOLICY, TAGPOLICY, BRANCHPOLICY]
  variables:
    REPOPOLICY_VERSION: "1.0.0"
  script:
    - apk add --no-cache curl
    - archive="gitlab-repopolicy_${REPOPOLICY_VERSION}_linux_amd64.tar.gz"
    - base="https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases/download/v${REPOPOLICY_VERSION}"
    - curl -fsSLO "$base/checksums.txt"
    - curl -fsSLO "$base/$archive"
    - grep " $archive\$" checksums.txt | sha256sum -c -
    - tar -xzf "$archive" gitlab-repopolicy
    - ./gitlab-repopolicy validate
```

When the files use `extends`, the job needs a copy of the presets: add `--config presets.yaml` and put the file into the deployment repository or into a CI/CD variable of type File. `--online` does not run in CI: it needs the token of the hook.

## What `validate` does not know

`validate` checks a file by itself. At a push the hook knows more:

| What the hook knows | What can pass `validate` but not the push |
| --- | --- |
| the version of the file in the default branch | nothing, the other way round: `validate` treats an expired OpenPGP key in an unchanged entry as an error, the push as a warning |
| that the file is a regular git file | a symlink or a submodule with the name of a policy file: `validate` reads the file the symlink leads to, and the push is rejected with `invalid_policy_update` |
| who exists in GitLab (without `--online`) | a new username in `allow`, `admins` or `except.users` that does not exist or is blocked: the push is rejected with `invalid_policy_update` |
| the role of the admins in the project (without `--project`) | a PATHPOLICY where no admin has the role Maintainer: the push is rejected with `lockout_prevented` |
| the server config of the Gitaly node | a preset that differs in `--config` from the one on the server, or that the server does not have |
| whether a rule of PATHPOLICY protects the file | the push accepts a file without such a rule with a warning, `validate` does not say it |

## Cheat sheet

| Task | Command |
| --- | --- |
| check all files of the directory | `gitlab-repopolicy validate` |
| check one file | `gitlab-repopolicy validate -f COMMITPOLICY` |
| check a draft | `gitlab-repopolicy validate -f PATHPOLICY.new --module pathpolicy` |
| a file with `extends` | `gitlab-repopolicy validate --config presets.yaml` |
| which rule applies to a new branch | `gitlab-repopolicy validate --branch release/v1.4.0` |
| who signs a tag | `gitlab-repopolicy validate --tag v1.4.0-prod` |
| the prefix of a branch is a GitLab user | `gitlab-repopolicy validate --online --config /etc/gitlab-repopolicy/config.yaml --branch kpn/fix` |
| the usernames of the file exist | `gitlab-repopolicy validate --online --config /etc/gitlab-repopolicy/config.yaml` |
| the admins work in the project | `gitlab-repopolicy validate -f PATHPOLICY --online --config /etc/gitlab-repopolicy/config.yaml --project acme/platform/app` |
| commit messages | `gitlab-repopolicy lint-commit` ([commitpolicy.en.md](commitpolicy.en.md#checking-without-a-push)) |
