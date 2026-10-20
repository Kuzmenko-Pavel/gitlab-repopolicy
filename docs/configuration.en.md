[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](configuration.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](configuration.en.md)

# Server configuration

The server configuration is `/etc/gitlab-repopolicy/config.yaml`, YAML. The package installs it as a conffile: an upgrade does not overwrite your changes. The hook wrapper passes the path (`--config`), and the same path is the default of every command.

The configuration describes the installation: the API address, the token, the log, limits, presets. Project rules are not set here: they live in the policy files of the repositories.

Unknown keys, including an unknown module name under `modules`, YAML anchors and aliases are an error. With an invalid configuration the hook denies every push to projects with "the hook configuration on the server is invalid", so run `selftest` after an edit.

## The whole file

This is [config/config.yaml](../config/config.yaml) from the package. Its values are the defaults, except two examples. `default_branch: [main]` and `settings_check` (`protected_tags: ["v*"]`, `job_token_push: false`) are empty by default, that is these checks are off until you set them. The package ships the example to show the format, so before the first run decide whether these values suit your instance.

```yaml
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
# name; it applies to every project of the instance, so turn it on with
# mode: audit first. Empty turns the check off. [main] is an example; the
# default is empty.
default_branch: [main]

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
# rules; examples are in docs/configuration.md.
presets:
  pathpolicy: {}
  commitpolicy: {}
  branchpolicy: {}
  tagpolicy: {}
```

## Settings

| Key | Default | Meaning |
| --- | --- | --- |
| `mode` | `enforce` | `off`, `audit` or `enforce`. The global switch, see below |
| `gitlab_url` | required | the GitLab API: `http+unix://` (the Workhorse socket of the same node) or `https://host` |
| `ca_file` | empty | an extra CA bundle for `https://`. Together with `http+unix://` it is a config error |
| `token_file` | `/etc/gitlab-repopolicy/token` | the service account token ([install.en.md](install.en.md#3-service-account-and-token)) |
| `token_lifetime` | `364d` | the new expiry on `token rotate`: today plus this value. At least `1d` |
| `audit.file` | `/var/log/gitlab-repopolicy/audit.log` | the audit log. The hook writes only here, so the hook requires it |
| `audit.stdout` | `false` | other commands (`settings-check`, `token rotate`, `breakglass`, `test`) also print events to stdout, for journald or a container runtime |
| `breakglass_dir` | `/etc/gitlab-repopolicy/breakglass` | break-glass grants ([operations.en.md](operations.en.md#break-glass)) |
| `timeouts.total` | `10s` | one hook run. Exceeding it is `limit_exceeded` |
| `timeouts.http` | `3s` | one API request. Not more than `timeouts.total` |
| `max_api_calls` | `30` | API calls per push. Exceeding it is `limit_exceeded` |
| `modules.<name>` | `enabled` | `enabled` or `disabled` for `pathpolicy`, `commitpolicy`, `tagpolicy`, `branchpolicy` |
| `default_branch` | empty | allowed names of the default branch for the hook and `settings-check`, see below |
| `settings_check.*` | empty | what `settings-check` expects, see below |
| `help.contact` | empty | who to ask, for example `#gitlab-support`. See below |
| `help.docs_url` | empty | a link to the documentation of the rules for developers, `http://` or `https://` |
| `presets.<module>.<name>` | empty | presets for `extends` in the policy files, separately for `pathpolicy`, `commitpolicy`, `branchpolicy`, `tagpolicy`. See below |

A duration is written as in Go (`10s`, `100ms`, `1h30m`) or in whole days with the suffix `d` (`364d`). Paths are absolute.

`gitlab_url` for a socket is the socket path encoded as the host:

```yaml
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket
```

## `mode`

| `mode` | What happens |
| --- | --- |
| `enforce` | decisions apply: a violation denies the push |
| `audit` | the hook denies nobody and writes decisions to audit with `"enforced": false`. The mode for the first run and for a quick rollback when the cause of a failure is unclear |
| `off` | the hook checks nothing and writes nothing |

A change applies from the next push. The hook reads the configuration on every run, nothing needs a restart.

The first run on a production instance is with `audit`:

```yaml
mode: audit
```

Pushes pass as before, and the audit log shows whom the hook would have denied: a `decision` event with `"decision": "deny"` and `"enforced": false`. When there are no such events or all of them are expected, switch to `enforce`. The order of the steps - [integration-guide.en.md](integration-guide.en.md).

Before switching from `audit` to `enforce`, make sure audit has no `invalid_policy` events. After the switch an invalid PATHPOLICY in the default branch blocks the project completely, an invalid TAGPOLICY blocks tags.

## `modules`

The hook does not load a disabled module: its policy file becomes an ordinary file of the repository. New versions of the file do not go through acceptance, and it can be changed by whoever the PATHPOLICY path rules allow (if there is a rule for this file). Disable a module only on the whole instance and for a long time: after it is enabled again, projects may hold invalid files, and an invalid TAGPOLICY blocks tags.

## `default_branch`

The list of allowed names of the default branch. An empty list (the default) turns the check off. Names are short, as GitLab shows them, without `refs/heads/` and without `*`.

```yaml
default_branch: [main]            # every project has the default branch main
default_branch: [main, master]    # during the move from master to main
```

The hook reads every policy file from the default branch. In Settings > Repository > Branch defaults a Maintainer can make a branch without policies the default one, and the next push would pass without rules. So the hook denies every push to a project whose default branch has a name outside the list, with the code `default_branch_not_allowed`. The modules do not run then.

In an empty repository GitLab makes the first pushed branch the default one, whatever its name. So the first push into a repository without branches and tags must bring the branch HEAD names (usually `main`). A push of only another branch is denied with a hint to push `main` first; a push of only tags passes.

The check applies to every project of the instance, including those without policy files, otherwise the bypass is not closed. Personal namespaces are not checked. Turn it on with the global `mode: audit`. Find `decision` events with `"code": "default_branch_not_allowed"` in audit, fix the default branch in those projects, and only then switch to `enforce`.

`settings-check` uses the same list ([operations.en.md](operations.en.md#settings-check)).

## `settings_check`

What `settings-check` expects of the projects with policy files. An empty value is not checked.

```yaml
settings_check:
  protected_tags: ["v*"]   # tags v* are protected in Settings > Repository > Protected tags
  job_token_push: false    # push with CI_JOB_TOKEN is off
```

| Key | What is compared |
| --- | --- |
| `protected_tags` | these tag patterns are protected. Only in projects with TAGPOLICY on the default branch |
| `job_token_push` | the permission to push with `CI_JOB_TOKEN` (`false` means it must be off) |

Regardless of these keys, `settings-check` checks:
- in every project with policy files - that every file of the default branch parses, and that the presets of its `extends` are in the server config and not broken
- in projects with PATHPOLICY - the name of the default branch against `default_branch`, that the default branch is protected, the CI configuration path and that every principal from the policy files exists
- in projects with BRANCHPOLICY - that GitLab protects the existing branches whose rule has `protected: true`
- in projects without PATHPOLICY - only the name of the default branch, the policy files and the protection of the branches of BRANCHPOLICY

`settings-check` skips projects without policy files. Details - [operations.en.md](operations.en.md#settings-check).

The expectations are the same for the whole instance; there are no settings per group or project. So put into the lists only what holds for every project with policy files. `protected_tags` gives no drift in projects without release tags, because it is checked only where there is TAGPOLICY. Keep the CI configuration check also for projects without `.gitlab-ci.yml`: a Maintainer can turn CI on later or point to a configuration in another project, and a path rule on `.gitlab-ci.yml` closes this bypass in advance.

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
- a preset has only `rules`, in the same format as in the file of the module. `admins` of PATHPOLICY and `keys` of TAGPOLICY are always in the file of the project ([why for keys](tagpolicy.en.md#4-a-preset-with-labels-of-signers))
- every rule of a preset is complete: all required fields are there
- the `help` of a rule (`contact`, `docs_url`) is printed in a denial by this rule, next to the global `help` above

The file of a project:

```yaml
version: 1
extends: org-ci             # або список: [org-ci, team-ci]
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
            type-enum: { level: error, value: [feat, fix, docs, chore] }
            header-max-length: { level: error, value: 100 }
            subject-full-stop: { level: error, value: forbid }
          message: Commits in the default branch follow Conventional Commits.
    team-strict:
      rules:
        conventional:
          match: default
          parser: conventional
          checks:
            header-max-length: { level: error, value: 72 }
            body-required: { level: error, types: [feat, fix] }
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
      header-max-length: { level: warn }
      subject-full-stop: null
EOF
```

```console
$ gitlab-repopolicy validate --config presets.yaml
COMMITPOLICY: valid
  extends: org-commits, team-strict
  rule conventional: preset:org-commits, preset:team-strict, file
    checks: preset:org-commits, preset:team-strict, file
    checks.header-max-length: preset:org-commits, preset:team-strict, file
    checks.type-enum: preset:org-commits
    checks.body-required: preset:team-strict
    match: preset:team-strict
    message: preset:team-strict
    parser: preset:team-strict
    help: preset:team-strict
    help.contact: preset:team-strict
```

How to read it:
- `checks.header-max-length` went through all three layers. `value: 72` of `team-strict` won over `100` of `org-commits`, because `team-strict` comes later in `extends`. `level: warn` of the file won over `error`
- `checks.subject-full-stop` is not in the output: the file removed it with `null`
- `match`, `message` and `parser` come from `team-strict`: the later layer replaced the values of `org-commits`. For `match` and `parser` they are the same, and `message` became the text of the team
- `checks.type-enum` is only in `org-commits`, `checks.body-required` only in `team-strict`, and both apply

A check of a message against the merged rules:

```console
$ gitlab-repopolicy lint-commit --config presets.yaml -m "fix: round refunds to cents in the payment service and in the monthly reports"
message: [conventional] error: body-required: the message needs a body
message: [conventional] warning: header-max-length: the header is 77 characters, the limit is 72
```

### A broken or missing preset

A config with an invalid preset loads, and the preset is marked broken. `selftest`, `validate --config` and `settings-check` report it. A file that takes such a preset, or a preset that does not exist:

| Module | What happens on a push |
| --- | --- |
| PATHPOLICY, TAGPOLICY | the file counts as broken, like an invalid file in the default branch (fail closed) |
| COMMITPOLICY, BRANCHPOLICY | the module is skipped with a warning |

A new version of a file with such `extends` is not accepted (`invalid_policy_update`).

### Presets for developers

For `validate` and `lint-commit` on a laptop to see the same presets as the server, hand out a file with only the key `presets`, for example in the deployment repository. A full example is [examples/presets.yaml](../examples/presets.yaml).

```sh
gitlab-repopolicy validate --config presets.yaml
gitlab-repopolicy lint-commit --config presets.yaml -m "$(cat .git/COMMIT_EDITMSG)"
```

`validate` and `lint-commit` with `--config` read only `presets`; other keys are not needed and not checked. `validate --online` calls the API and reads the whole config. Step by step - [validate.en.md](validate.en.md#presets).

## Other configurations

[config/release-verify.yaml](../config/release-verify.yaml) is the configuration the project uses to check the signature of its own release tags: only TAGPOLICY is enabled, audit goes to stdout, the API is not used ([release.en.md](release.en.md)).
