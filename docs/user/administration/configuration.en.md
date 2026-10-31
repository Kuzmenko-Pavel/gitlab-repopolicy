[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](configuration.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](configuration.en.md)

# Server configuration

The server configuration is `/etc/gitlab-repopolicy/config.yaml`, YAML. The package installs it as a conffile: an upgrade does not overwrite your changes. The hook wrapper passes the path (`--config`). Server commands generally use it by default; local `validate` and `lint-commit` read configuration only with explicit `--config`.

The configuration describes the installation: the API address, the token, the log, limits, presets. Project rules are not set here: they live in the policy files of the repositories.

Unknown keys, including an unknown module name under `modules`, YAML anchors and aliases are an error. With an invalid configuration the hook denies every push to projects with "the hook configuration on the server is invalid", so run `selftest` after an edit.

## Minimal configuration

For initial observation on a Linux package server with local Workhorse, this `/etc/gitlab-repopolicy/config.yaml` is enough:

```yaml
mode: audit
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket
```

The configuration loader supplies the remaining values. It expects the token in `/etc/gitlab-repopolicy/token` and the log in `/var/log/gitlab-repopolicy/audit.log`; all four modules are enabled. Prepare files and permissions following the [installation guide](install.en.md). Minimal YAML does not create the service account, token or directories.

`audit` is explicit because the program default for `mode` is `enforce`. An omitted `default_branch` equals `[main, master]`: the default branch of a group project must be called `main` or `master`, otherwise the push is rejected. On a separate Gitaly node, replace `gitlab_url` with your GitLab HTTPS address and add `ca_file` if needed.

### Before copying the packaged configuration

The supplied file below is a full example, rather than a minimal first-run configuration. Its `default_branch: [main, master]` is the same as the program default: in `enforce` it rejects pushes to group projects with another default branch name, even without policies. `settings_check` also contains example expectations that the command immediately applies.

Before connecting the hook, set `mode: audit`, check that `[main, master]` fits the instance, or set your own names or `default_branch: []`, and agree on `settings_check` expectations with project owners. Do not narrow an existing production branch list without checking audit results.

## Full configuration

The following is the exact [config/config.yaml](../../../config/config.yaml) shipped in the package. Values in the next table's "Default" column are loader values when a key is absent. They do not always match the package: `gitlab_url` has no program default, `default_branch` is empty and both `settings_check` expectations are unset.

```yaml
# config/config.yaml
# Server config of gitlab-repopolicy: /etc/gitlab-repopolicy/config.yaml.
# The package installs this file as a conffile; values below are the defaults
# unless marked as an example. Unknown keys are an error.

# off | audit | enforce: the global switch. audit writes every decision to the
# audit log with "enforced": false and lets the push through.
mode: enforce

# GitLab API: the Workhorse socket of the same node, or https:// for Gitaly
# on a separate node. Required.
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket

# Extra CA bundle for https://; an error with http+unix://.
ca_file: ""

# Token of the instance service account: Maintainer in every top-level group,
# scopes exactly read_api and self_rotate. root:<git-user> 0440, written only
# by "gitlab-repopolicy token rotate".
token_file: /etc/gitlab-repopolicy/token

# Expiry of a rotated token: token rotate always sends expires_at = today + this.
token_lifetime: 364d

audit:
  # The hook writes only to this file: its stdout and stderr reach the pusher.
  file: /var/log/gitlab-repopolicy/audit.log
  # Other commands (settings-check, token rotate, breakglass, test) also print
  # events to stdout, for journald or a container runtime.
  stdout: false

# Break-glass grants, written by "gitlab-repopolicy breakglass grant" as root.
breakglass_dir: /etc/gitlab-repopolicy/breakglass

timeouts:
  total: 10s            # one hook invocation
  http: 3s              # one API request

# API calls per push; more is limit_exceeded.
max_api_calls: 30

# Modules are enabled unless disabled here.
modules:
  pathpolicy: enabled
  commitpolicy: enabled
  tagpolicy: enabled
  branchpolicy: enabled

# Allowed names of the default branch, used by the hook and by settings-check.
# The hook rejects every push to a project whose default branch has another
# name; it applies to every project of the instance, so check it with
# mode: audit first. [] turns the check off. [main, master] is the default.
default_branch: [main, master]

# What settings-check expects of the projects with policy files. Empty values
# are not checked and are the default; ["v*"] and false below are examples.
# Every policy file of a project is also checked to parse with its presets;
# projects with PATHPOLICY always get ci_config_path, principals and the
# protection of the default branch checked; projects with BRANCHPOLICY the
# existing branches that its rules require to be protected; protected_tags
# are checked only in projects with PATHPOLICY and TAGPOLICY.
settings_check:
  protected_tags: ["v*"]
  job_token_push: false

# Where a pusher gets help, printed in denials; both are optional. contact is
# named when the server or a policy in the default branch is at fault, docs_url
# next to the rules. Up to 200 characters each.
help:
  contact: ""
  docs_url: ""

# Presets for "extends:" in the policy files, by module. A preset holds only
# rules; examples are in docs/user/administration/configuration.md.
presets:
  pathpolicy: {}
  commitpolicy: {}
  branchpolicy: {}
  tagpolicy: {}
```

## Configuration parameters

| Key | Default | Meaning |
| --- | --- | --- |
| `mode` | `enforce` | `off`, `audit` or `enforce`. The global switch, see below |
| `gitlab_url` | required | the GitLab API: `http+unix://` (the Workhorse socket of the same node) or `https://host` |
| `ca_file` | `""` (empty string) | an extra CA bundle for `https://`. A nonempty value together with `http+unix://` is a configuration error |
| `token_file` | `/etc/gitlab-repopolicy/token` | the service account token ([install.en.md](install.en.md#3-service-account-and-token)) |
| `token_lifetime` | `364d` | the new expiry on `token rotate`: today plus this value. At least `1d` |
| `audit.file` | `/var/log/gitlab-repopolicy/audit.log` | the audit log. The hook writes only here, so the hook requires it |
| `audit.stdout` | `false` | `true` enables audit events on stdout for `settings-check`, `token rotate`, `breakglass` and `test`; `false` leaves this output disabled. The hook always writes events only to the file |
| `breakglass_dir` | `/etc/gitlab-repopolicy/breakglass` | break-glass grants ([operations.en.md](operations.en.md#break-glass)) |
| `timeouts.total` | `10s` | one hook run. Exceeding it is `limit_exceeded` |
| `timeouts.http` | `3s` | one API request. Not more than `timeouts.total` |
| `max_api_calls` | `30` | API calls per push. Exceeding it is `limit_exceeded` |
| `modules.<name>` | `enabled` | `enabled` or `disabled` for `pathpolicy`, `commitpolicy`, `tagpolicy`, `branchpolicy` |
| `default_branch` | `[main, master]` | allowed default branch names for the hook and `settings-check`; an empty list disables the name restriction |
| `settings_check.protected_tags` | Empty list (`[]`) | tag protection patterns expected by `settings-check` in projects with PATHPOLICY and TAGPOLICY; an empty list skips this comparison |
| `settings_check.job_token_push` | Not set | expected state of push through `CI_JOB_TOKEN` in projects with PATHPOLICY: `true` or `false`. Omitting the key skips this comparison |
| `help.contact` | `""` (empty string) | who to ask, for example `#gitlab-support`; an empty string adds no server contact |
| `help.docs_url` | `""` (empty string) | a link to rule documentation, `http://` or `https://`; an empty string adds no server documentation link |
| `presets.<module>.<name>` | No presets defined | presets for `extends` in policy files, separately for `pathpolicy`, `commitpolicy`, `branchpolicy`, `tagpolicy`. See below |

All parameters except `gitlab_url` may be omitted to use the loader values in the table. An empty `token_file` does not mean "API without a token": it must be a nonempty absolute path. `audit.file: ""` is valid only for separate commands with `audit.stdout: true`; the hook itself requires a nonempty log path.

| Key | Packaged value | First-run decision |
| --- | --- | --- |
| `mode` | `enforce` | explicitly set `audit` before connecting the hook |
| `gitlab_url` | Linux package Workhorse socket | check the path or use HTTPS for a separate node |
| `token_file` | `/etc/gitlab-repopolicy/token` | create the token file and check readability as git |
| `modules` | all `enabled` | retain unless deliberately disabling a policy across the instance |
| `default_branch` | `[main, master]` | keep it if the default branches are called `main` or `master`; otherwise set your own names or `[]` for no restriction |
| `settings_check.protected_tags` | `["v*"]` | define only expectations shared by relevant projects |
| `settings_check.job_token_push` | `false` | an explicit expectation rather than an omitted check |
| `presets` | empty mappings for four modules | add only your organization's rule sets |

A duration is written as in Go (`10s`, `100ms`, `1h30m`) or in whole days with the suffix `d` (`364d`). Paths are absolute.

`gitlab_url` for a socket is the socket path encoded as the host:

```yaml
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket
```

## `mode`

| `mode` | What happens |
| --- | --- |
| `enforce` | decisions apply: a violation denies the push |
| `audit` | policy violations do not block a push; decisions are logged with `"enforced": false`. Use for the first run or to temporarily disable policy enforcement |
| `off` | the hook checks nothing and writes nothing |

Modes apply after configuration loads successfully. Invalid configuration, an unreadable file or failure to start git can deny before the core even in `audit`. `off` also requires valid configuration and a nonempty `audit.file` for the hook wrapper.

A change applies from the next push. The hook reads the configuration on every run, nothing needs a restart.

The first run on a production instance is with `audit`:

```yaml
mode: audit
```

The audit log shows whom the hook would have denied: a `decision` event with `"decision": "deny"` and `"enforced": false`. When there are no such events or all of them are expected, move to `enforce` using the [integration guide](integration-guide.en.md).

Before switching from `audit` to `enforce`, make sure audit has no `invalid_policy` events. After the switch an invalid PATHPOLICY in the default branch blocks the project completely, an invalid TAGPOLICY blocks tags.

## `modules`

The hook does not load a disabled module: its policy file becomes an ordinary file of the repository. New versions of the file do not go through acceptance, and it can be changed by whoever the PATHPOLICY path rules allow (if there is a rule for this file). Disable a module only on the whole instance and for a long time: after it is enabled again, projects may hold invalid files, and an invalid TAGPOLICY blocks tags.

## `default_branch`

The list of allowed names of the default branch, `[main, master]` by default: both common names are allowed without configuration. An empty list turns the check off. Names are short, as GitLab shows them, without `refs/heads/` and without `*`.

```yaml
default_branch: [main, master]    # the default
default_branch: [main]            # every project has the default branch main
default_branch: []                # no restriction of the name
```

The hook reads every policy file from the default branch. In Settings > Repository > Branch defaults a Maintainer can make a branch without policies the default one, and the next push would pass without rules. So the hook denies every push to a project whose default branch has a name outside the list, with the code `default_branch_not_allowed`. The modules do not run then.

In an empty repository GitLab makes the first pushed branch the default one, whatever its name. So the first push into a repository without branches and tags must bring the branch HEAD names (usually `main`). A push of only another branch is denied with a hint to push `main` first; a tag-only push does not violate this particular check, but must still pass the global tag commit reachability check.

The check applies to every project of the instance, including those without policy files, otherwise the bypass is not closed. Personal namespaces are not checked. Turn it on with the global `mode: audit`. Find `decision` events with `"code": "default_branch_not_allowed"` in audit, fix the default branch in those projects, and only then switch to `enforce`.

`settings-check` uses the same list ([operations.en.md](operations.en.md#settings-check)).

## `settings_check`

What `settings-check` expects of the projects with policy files. An empty `protected_tags` list is not checked; omitting `job_token_push` means no expectation, while explicit `false` requires job-token push to be disabled.

```yaml
settings_check:
  protected_tags: ["v*"]   # tags v* are protected in Settings > Repository > Protected tags
  job_token_push: false    # push with CI_JOB_TOKEN is off
```

| Key | Accepted value | What is compared |
| --- | --- | --- |
| `protected_tags` | A pattern list, such as `["v*"]` | these patterns are protected in Settings. Checked only in projects with both PATHPOLICY and TAGPOLICY on the default branch. An empty list `[]` skips this comparison |
| `job_token_push` | `true` or `false`; the key may be omitted | in projects with PATHPOLICY, `true` requires push through `CI_JOB_TOKEN` to be enabled; `false` requires it to be disabled. Omitting the key leaves this permission unchecked |

Regardless of these keys, `settings-check` checks:
- in every project with policy files - that every file of the default branch parses, and that the presets of its `extends` are in the server config and not broken
- in projects with PATHPOLICY - the name of the default branch against `default_branch`, that the default branch is protected, the CI configuration path and that every principal from the policy files exists
- in projects with BRANCHPOLICY - that GitLab protects the existing branches whose rule has `protected: true`
- in projects without PATHPOLICY - only the name of the default branch, the policy files and the protection of the branches of BRANCHPOLICY

`settings-check` skips projects without policy files. The [settings-check guide](operations.en.md#settings-check) explains how to run it, interpret results and configure the timer.

The expectations are the same for the whole instance; there are no settings per group or project. So put into the lists only what holds for every project with policy files. `protected_tags` compares configured protection patterns; having no tags does not skip this comparison in a project with PATHPOLICY and TAGPOLICY. Keep the CI configuration check also for projects without `.gitlab-ci.yml`: a Maintainer can turn CI on later or point to a configuration in another project, and a path rule on `.gitlab-ci.yml` closes this bypass in advance.

## `help`

What a person whom the hook denied sees, and where to turn. Both keys are optional, up to 200 characters, without control characters.

- `help.contact` is printed in the `who can help:` line of denials the user cannot fix alone: an API failure, server limits, an internal error, a broken policy in the default branch. Without this key the line says `your GitLab administrators`
- `help.docs_url` is printed in the `rules:` line at the end of every denial and warning

```yaml
help:
  contact: "#gitlab-support"
  docs_url: https://wiki.example.com/gitlab/repopolicy
```

```text
remote: rules: PATHPOLICY in main (0316d44); docs: https://wiki.example.com/gitlab/repopolicy
remote: checked as: alice (HTTPS)
```

## `presets`

A preset is a named set of rules of one module in the server config. A standard shared by many projects lives in one place, and the file of a project takes it with the line `extends`. Presets are grouped by module, like `modules`:

```yaml
presets:
  pathpolicy:
    org-ci:
      rules:
        ci-config:
          match: all
          paths: [".gitlab-ci.yml"]
          allow: [platform-lead]
          message: The platform team changes the CI configuration.
          help:
            contact: "#platform"
  commitpolicy: {}
  branchpolicy: {}
  tagpolicy: {}
```

- the name of a preset is the key, `[a-z0-9-]{1,63}`. Presets of different modules may have the same names
- a preset has only `rules`, in the same format as in the file of the module. `admins` of PATHPOLICY and `keys` of TAGPOLICY are always in the file of the project ([why for keys](../examples/tagpolicy/organization-preset.en.md))
- every rule of a preset is complete: all required fields are there
- the `help` of a rule (`contact`, `docs_url`) is printed in a denial by this rule, next to the global `help` above

The file of a project:

```yaml
version: 1
extends: org-ci             # or a list: [org-ci, team-ci]
admins: [platform-lead]
```

### How presets and the file merge

Like the values files of Helm: the layers merge one after another, the presets in the order of `extends`, the file last.
- a mapping (rules, the fields of a rule, `checks`, `except`, `help`) merges key by key
- a list and a value are replaced as a whole: `allow: [kpn]` in the file does not add kpn to the list of the preset, it replaces the list
- `null` removes a key: a rule, a field, a check
- the later layer wins

Required fields are checked after the merge, so a rule in the file may change only some fields of a rule of a preset. A new rule of the file that is in no preset must be complete.

### A step-by-step example

Two presets of COMMITPOLICY: a general one of the organization and a stricter one of the payments team.

```sh
cat > presets.yaml <<'EOF'
presets:
  commitpolicy:
    org-commits:
      rules:
        conventional:
          match: default
          parser: conventional
          checks:
            header-format:
              conventional: { level: error }
            header-max-length:
              limit: { level: error, value: 100 }
            type-enum:
              allowed: { level: error, value: [feat, fix, docs, chore] }
            subject-trailing-dot:
              no-dot: { level: error, value: forbidden }
          message: Commits in the default branch follow Conventional Commits.
    team-strict:
      rules:
        conventional:
          match: default
          parser: conventional
          checks:
            header-max-length:
              limit: { level: error, value: 72 }
            body-min-line-length:
              feat-fix: { level: error, value: 1, types: [feat, fix] }
          message: Commits of the payments team follow Conventional Commits with a body.
          help:
            contact: "#payments-team"
EOF
```

The project takes both presets, only warns about long headers and allows a full stop at the end of the subject:

```sh
cat > COMMITPOLICY <<'EOF'
version: 1
extends: [org-commits, team-strict]
rules:
  conventional:
    checks:
      header-max-length:
        limit: { level: warn }
      subject-trailing-dot:
        no-dot: { value: optional }
EOF
```

```console
$ gitlab-repopolicy validate --config presets.yaml
COMMITPOLICY: valid
  extends: org-commits, team-strict
  rule conventional: preset:org-commits, preset:team-strict, file
    checks: preset:org-commits, preset:team-strict, file
    checks.header-format: preset:org-commits
    checks.header-format.conventional: preset:org-commits
    checks.header-max-length: preset:org-commits, preset:team-strict, file
    checks.header-max-length.limit: preset:org-commits, preset:team-strict, file
    checks.subject-trailing-dot: preset:org-commits, file
    checks.subject-trailing-dot.no-dot: preset:org-commits, file
    checks.type-enum: preset:org-commits
    checks.type-enum.allowed: preset:org-commits
    checks.body-min-line-length: preset:team-strict
    checks.body-min-line-length.feat-fix: preset:team-strict
    match: preset:team-strict
    message: preset:team-strict
    parser: preset:team-strict
    help: preset:team-strict
    help.contact: preset:team-strict
```

How to read it:
- checks merge by type and name. `checks.header-max-length.limit` went through all three layers. `value: 72` of `team-strict` won over `100` of `org-commits`, because `team-strict` comes later in `extends`. `level: warn` of the file won over `error`
- `checks.subject-trailing-dot.no-dot` stayed in the rule, but the file gave it `value: optional`: a full stop at the end of the subject may be there or not. To remove the check completely, the file would write `no-dot: null`, and the line `checks.subject-trailing-dot.no-dot` would not be in the output
- `match`, `message` and `parser` come from `team-strict`: the later layer replaced the values of `org-commits`. For `match` and `parser` they are the same, and `message` became the text of the team
- `checks.type-enum` is only in `org-commits`, `checks.body-min-line-length` only in `team-strict`, and both apply

A check of a message against the merged rules:

```console
$ gitlab-repopolicy lint-commit --config presets.yaml -m "fix: round refunds to cents in the payment service and in the monthly reports"
message: [conventional] warning: header-max-length limit: the header is 77 characters, the limit is 72
message: [conventional] error: body-min-line-length feat-fix: the message needs a body
```

### A broken or missing preset

A config with an invalid preset loads, and the preset is marked broken. `selftest`, `validate --config` and `settings-check` report it. A file that takes such a preset, or a preset that does not exist:

| Module | What happens on a push |
| --- | --- |
| PATHPOLICY, TAGPOLICY | the file counts as broken, like an invalid file in the default branch (fail closed) |
| COMMITPOLICY, BRANCHPOLICY | the module is skipped with a warning |

A new version of a file with such `extends` is not accepted (`invalid_policy_update`).

### Presets for developers

For `validate` and `lint-commit` on a laptop to see the same presets as the server, hand out a file with only the key `presets`, for example in the deployment repository. A full example is [examples/presets.yaml](../../../examples/presets.yaml).

```sh
gitlab-repopolicy validate --config presets.yaml
gitlab-repopolicy lint-commit --config presets.yaml -m "$(cat .git/COMMIT_EDITMSG)"
```

`validate` and `lint-commit` with `--config` read only `presets`; other keys are not needed and not checked. `validate --online` calls the API and reads the whole config. The [validate guide](../reference/validate.en.md#presets) shows how to check a file with a preset locally.

## Other configurations

[config/release-verify.yaml](../../../config/release-verify.yaml) is the configuration the project uses to check the signature of its own release tags: only TAGPOLICY is enabled, audit goes to stdout, the API is not used ([release.en.md](../../development/release.en.md)).

## Settings that need particular care

Configuration changes affect subsequent invocations on that node. They are not authorized by project policy files.

| Change | Effect | Verification and rollback |
| --- | --- | --- |
| The first connection of the hook or narrowing `default_branch` | can reject every push to group projects whose default branch is not called `main` or `master` or not as in the new list | start in `audit`, inspect `default_branch_not_allowed`; restore the previous list |
| Switch `audit` → `enforce` | starts applying recorded denials, including broken policies | review all `deny` and `invalid_policy` events; return to `audit` |
| `modules.*: disabled` | removes that module's checks and file acceptance; disabling PATHPOLICY removes its ownership controls | inspect projects before re-enabling; restore the previous module set |
| Change or remove a preset | affects all projects using its `extends`; broken PATHPOLICY and TAGPOLICY presets can block changes | run `validate --config` on consumers before delivery; restore a consistent preset on every node |
| Reduce time or API budgets | can cause denials or skipped checks under ordinary load | inspect `duration_ms`, `api_calls`, `limit_exceeded`; restore previous limits |
| Change `token_file` or `gitlab_url` | required API checks may lose access | selftest as root and git; restore a readable path or working endpoint |

## Verifying configuration

After an edit and before deployment to all nodes, run on a prepared node:

```sh
sudo gitlab-repopolicy selftest --config /etc/gitlab-repopolicy/config.yaml
sudo -u git gitlab-repopolicy selftest --config /etc/gitlab-repopolicy/config.yaml
```

`selftest` loads the full configuration and checks presets, local files and API access. Offline `validate --config` reads only `presets` and cannot replace server configuration checking. Also validate new presets against actual consumer policy files.

After delivery, make a control push and inspect the log. Do not filter solely for `event == "decision"`: a denial can be an `authorization_backend_failure` or `internal_error` event. The [integration guide](integration-guide.en.md) covers the transition to blocking; [operations](operations.en.md#audit) explains events.
