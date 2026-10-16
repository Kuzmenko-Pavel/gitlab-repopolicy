[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](configuration.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](configuration.en.md)

# Server configuration

The server configuration is `/etc/gitlab-repopolicy/config.yaml`, YAML. The package installs it as a conffile: an upgrade does not overwrite your changes. The hook wrapper passes the path (`--config`), and the same path is the default of every command.

The configuration describes the installation: the API address, the token, the log, limits, presets. Project rules are not set here: they live in the policy files of the repositories.

Unknown keys, YAML anchors and aliases are an error. With an invalid configuration the hook denies every push to projects with "the hook configuration on the server is invalid", so run `selftest` after an edit.

## The whole file

This is [config/config.yaml](../config/config.yaml) from the package. The values are the defaults, except the example in `settings_check`.

```yaml
# Server config of gitlab-repopolicy: /etc/gitlab-repopolicy/config.yaml.
# The package installs this file as a conffile; values below are the defaults
# unless marked otherwise. Unknown keys are an error.

# off | audit | enforce: the global switch. audit writes every decision to the
# audit log with "enforced": false and lets the push through.
mode: enforce

# GitLab API: the Workhorse socket of the same node, or https:// for Gitaly
# on a separate node. Required.
gitlab_url: http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket

# Extra CA bundle for https://; not used with http+unix://.
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
  pattern: 100ms        # the COMMITPOLICY pattern rule, per message
  pattern_budget: 2s    # the same, per push

# API calls per push; more is limit_exceeded.
max_api_calls: 30

# Modules are enabled unless disabled here.
modules:
  pathpolicy: enabled
  commitpolicy: enabled
  tagpolicy: enabled

# What settings-check expects of every project with PATHPOLICY. An empty
# value is not checked; ci_config_path and principals are always checked.
settings_check:
  default_branch: main
  protected_branches: [main]
  protected_tags: ["v*"]
  job_token_push: false

# Presets for "extends:" in COMMITPOLICY; an example is in docs/configuration.md.
commitpolicy_presets: {}
```

## Settings

| Key | Default | Meaning |
| --- | --- | --- |
| `mode` | `enforce` | `off`, `audit` or `enforce`. The global switch, see below |
| `gitlab_url` | required | the GitLab API: `http+unix://` (the Workhorse socket of the same node) or `https://host` |
| `ca_file` | empty | an extra CA bundle for `https://`. Not used with `http+unix://` |
| `token_file` | `/etc/gitlab-repopolicy/token` | the service account token ([install.en.md](install.en.md#3-service-account-and-token)) |
| `token_lifetime` | `364d` | the new expiry on `token rotate`: today plus this value. At least `1d` |
| `audit.file` | `/var/log/gitlab-repopolicy/audit.log` | the audit log. The hook writes only here, so the hook requires it |
| `audit.stdout` | `false` | other commands (`settings-check`, `token rotate`, `breakglass`, `test`) also print events to stdout, for journald or a container runtime |
| `breakglass_dir` | `/etc/gitlab-repopolicy/breakglass` | break-glass grants ([operations.en.md](operations.en.md#break-glass)) |
| `timeouts.total` | `10s` | one hook run. Exceeding it is `limit_exceeded` |
| `timeouts.http` | `3s` | one API request. Not more than `timeouts.total` |
| `timeouts.pattern` | `100ms` | the COMMITPOLICY `pattern` rule, per message |
| `timeouts.pattern_budget` | `2s` | the same per push. Not less than `timeouts.pattern` |
| `max_api_calls` | `30` | API calls per push. Exceeding it is `limit_exceeded` |
| `modules.<name>` | `enabled` | `enabled` or `disabled` for `pathpolicy`, `commitpolicy`, `tagpolicy` |
| `settings_check.*` | empty | what `settings-check` expects, see below |
| `commitpolicy_presets` | empty | presets for `extends` in COMMITPOLICY ([commitpolicy.en.md](commitpolicy.en.md#presets-extends)) |

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

A change applies from the next push: the hook reads the configuration on every run, nothing needs a restart.

Before switching from `audit` to `enforce`, make sure audit has no `invalid_policy` events: after the switch an invalid PATHPOLICY in the default branch blocks the project completely, an invalid TAGPOLICY blocks tags.

## `modules`

The hook does not load a disabled module: its policy file becomes an ordinary file of the repository. New versions of the file do not go through acceptance, and it can be changed by whoever the PATHPOLICY path rules allow (if there is a rule for this file). Disable a module only on the whole instance and for a long time: after it is enabled again, projects may hold invalid files, and an invalid TAGPOLICY blocks tags.

## `settings_check`

What `settings-check` expects of every project with PATHPOLICY. An empty value is not checked.

| Key | What is compared |
| --- | --- |
| `default_branch` | the default branch of the project |
| `protected_branches` | every branch in the list is protected. Other protected branches of the project do not matter |
| `protected_tags` | these tag patterns are protected |
| `job_token_push` | the permission to push with `CI_JOB_TOKEN` (`false` means it must be off) |

Regardless of these keys, `settings-check` always checks the CI configuration path and that every principal from the policy files exists. Details - [operations.en.md](operations.en.md#settings-check).

## `commitpolicy_presets`

Presets are COMMITPOLICY rules shared by many repositories: a file in a project refers to a preset with `extends` ([commitpolicy.en.md](commitpolicy.en.md#presets-extends)). The section is empty in the package, because every installation has its own commit standard. An example:

```yaml
# /etc/gitlab-repopolicy/config.yaml
commitpolicy_presets:
  org-conventional:
    rules:
      type-enum: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
      subject-case: { level: error, value: lower-first }
      header-max-length: { level: warn, value: 100 }
```

The preset name is the key of the section, `rules` are rules in the same format as in a COMMITPOLICY ruleset. Rules of the project file override the preset rules by name.

## Other configurations

[config/release-verify.yaml](../config/release-verify.yaml) is the configuration the project uses to check the signature of its own release tags: only TAGPOLICY is enabled, audit goes to stdout, the API is not used ([release.en.md](release.en.md)).
