[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](operations.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](operations.en.md)

# Operations

## Audit

The hook writes every decision that touched a rule to `audit.file` (default `/var/log/gitlab-repopolicy/audit.log`): one JSON object per line, up to 4 KiB per event. A push that touched no rule is not logged, otherwise there would be a line for every push of the instance.

```json
{"ts":"2026-10-14T09:14:02.418Z","event":"decision","module":"pathpolicy","decision":"deny","enforced":true,"code":"protected_path_violation","project_id":812,"project_path":"example/payments/deployment","user_id":314,"username":"alice","gl_id":"user-314","protocol":"web","ref":"refs/heads/main","oldrev":"9f1c...","newrev":"b77a...","anchor_rev":"9f1c...","policy_blob":"3e5d...","violations":[{"rule":"ci-config","code":"protected_path_violation","paths":[".gitlab-ci.yml"],"reason":"not in allow: alice"}],"api_calls":2,"duration_ms":143,"version":"1.0.0","git":"2.55.0","node":"gitaly-01"}
```

| Field | Meaning |
| --- | --- |
| `ts` | time in UTC |
| `event`, `module`, `decision`, `code`, `action` | what happened, see the table below |
| `enforced` | `false` in `mode: audit`: the decision is recorded, but the push passed |
| `project_id`, `project_path` | the project |
| `user_id`, `username`, `gl_id`, `protocol` | who made the change and how: `http`, `ssh` or `web` (merge, Web IDE, API, UI operations) |
| `ref`, `oldrev`, `newrev` | the branch or tag and its values before and after |
| `anchor_rev`, `policy_blob` | the anchor commit and the policy file blob: they restore the exact policy the decision was made by |
| `violations` | rule, code, paths or commits, `reason` |
| `rules`, `signer`, `fingerprint` | for TAGPOLICY: the rules, the label and the fingerprint of the signing key |
| `reason` | the reason with API results. It never gets into the message to the user |
| `api_calls`, `duration_ms` | the cost of the decision |
| `version`, `git`, `node` | the gitlab-repopolicy version, the git the hook saw, and the node |

A module can write several events for one push: one per branch or tag with violations.

### Events and alerts

| `event` | When | Alert |
| --- | --- | --- |
| `decision` | allow, deny or warn when a rule is touched | no |
| `policy_change` | a policy file is created, changed or deleted | no |
| `policy_bootstrap` | the first PATHPOLICY in a project | no |
| `policy_recovery` | an Owner changed PATHPOLICY through recovery | **yes, every one** |
| `invalid_policy` | PATHPOLICY or TAGPOLICY in the default branch is invalid | **yes** |
| `invalid_commit_policy` | COMMITPOLICY in the default branch is invalid (the check is skipped) | no |
| `authorization_backend_failure` | the API is unavailable on a protected operation, the token does not work, the service account does not see the project (`reason`: `project_not_visible`, `role_below_maintainer`) | yes, if more than N in 5 minutes |
| `unknown_principal` | a deploy key or an unparsable `GL_ID` on a protected path | no |
| `breakglass` | a break-glass grant is issued (`action: grant`), revoked (`action: revoke`) or used by the hook | **yes** |
| `settings_drift` | `settings-check` found a difference | **yes** |
| `internal_error` | an internal error of the binary, a failure of `token rotate` (`action: token_rotate`) | **yes** |

Also watch `decision` with `code: invalid_policy_update` in `mode: audit`: the hook would not have let such a policy version through in `enforce`, and after the switch it blocks the project.

### Shipping and rotating the log

Where to ship the log (Fluent Bit, Vector, journald, SIEM) is up to the installation. The hook opens the file for every write with `O_APPEND`, so logrotate does not need `copytruncate`. The package does not install a logrotate config, an example:

```text
# /etc/logrotate.d/gitlab-repopolicy
/var/log/gitlab-repopolicy/audit.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
}
```

The hook creates the new file as the git user. Other commands run as root create a missing file with the owner of the log directory, so the hook keeps its write access.

## Recovery

Recovery turns on when PATHPOLICY has no effective admin left: all were removed from the project, blocked or demoted below Maintainer. Then an Owner of the project (including an Owner of a parent group) may change PATHPOLICY. The rules - [pathpolicy.en.md](pathpolicy.en.md#admins-the-first-policy-and-recovery).

```sh
sudo gitlab-repopolicy admins --project group/project
```

shows admins from the file, effective admins and whether recovery is on. An API failure is an error, not "0 admins".

```text
$ sudo gitlab-repopolicy admins --project acme/app
  alice                          missing: access level 30, Maintainer (40) is required
acme/app: configured 1, effective 0, missing 1
recovery active: an Owner of the project may change PATHPOLICY
```

Every recovery is a `policy_recovery` event. Namespace Owners are not always the people responsible for the policy, so this event needs an alert.

## Break-glass

An emergency bypass for one project, when a policy blocked work and cannot be fixed the usual way (for example an invalid PATHPOLICY after an import). It needs root on the Gitaly node and cannot be turned on through GitLab.

```sh
sudo gitlab-repopolicy breakglass grant --project-id 812 --ttl 30m --reason "INC-123 policy lockout"
sudo gitlab-repopolicy breakglass list
sudo gitlab-repopolicy breakglass revoke --project-id 812
```

- `--ttl` is required, at most 24 hours. After it the grant is ignored
- while the grant is valid, every module lets pushes to the project through, the push output carries a warning, and every such push is a `breakglass` event
- a grant is the file `<breakglass_dir>/<project-id>` with the time, the reason and the operator (`SUDO_USER`). The hook trusts only a file and directory owned by root and not writable by group and others
- with several Gitaly nodes, grant it on every node where the hook can run

## Token rotation

The service account token expires. An expired token denies every protected change with `authorization_backend_failure`.

```sh
sudo gitlab-repopolicy token rotate --before 30d
```

- if more than `--before` is left until expiry, the command does nothing
- otherwise it rotates the token through the API with an explicit `expires_at` = today + `token_lifetime` (without it GitLab would issue a token for 7 days)
- the old token stops working at once, so the new one is written atomically: a temporary file in the same directory, fsync, rename. A hook that got 401 during the rotation re-reads the file once and repeats the request
- `400` from GitLab (for example an expiry beyond the instance maximum) leaves the current token working and writes an `internal_error` event with `action: token_rotate`

The `gitlab-repopolicy-token-rotate.timer` timer runs the rotation daily ([install.en.md](install.en.md#7-timers)). With several Gitaly nodes, either each node has its own token, or one node rotates and the others get the file through your delivery system: otherwise a rotation on one node breaks the token on the others.

## `settings-check`

The hook sees only changes of branches and tags. Project settings change in Settings, and a Maintainer can bypass PATHPOLICY through them: move the CI configuration path to an unprotected file, make a branch without the policy the default, turn on push with `CI_JOB_TOKEN`. `settings-check` finds such changes.

For every project with PATHPOLICY that the service account sees:

| Check (`violations[].rule`) | What counts as a difference |
| --- | --- |
| `default_branch` | the default branch is not the one in `settings_check.default_branch` |
| `protected_branches`, `protected_tags` | the expected branches or tag patterns are not protected |
| `job_token_push` | the permission to push with `CI_JOB_TOKEN` is not the expected one |
| `ci_config_path` | the CI configuration is in another project, at a URL or not covered by a PATHPOLICY path rule |
| `principals` | usernames from the policy files are no longer active GitLab users |
| `access` | the role of the service account is below Maintainer: the settings are not visible |
| `policy` | a policy file cannot be parsed |
| `unavailable` | the API did not answer for this project |

A difference is a `settings_drift` event per project and exit code 1. A project with PATHPOLICY is looked up both on the current default branch and on the expected one: switching the default to a branch without PATHPOLICY is a difference, not "a project without a policy".

```text
$ sudo -u git gitlab-repopolicy settings-check
acme/app: protected_tags: not protected: v*
```

This is detection within minutes, not prevention: `settings-check` does not see a change that was reverted between two runs. Only an organizational measure closes this gap fully: the Maintainer role only for the owners of protected paths.

### On the node with a timer

```sh
sudo systemctl enable --now gitlab-repopolicy-settings-check.timer
journalctl -u gitlab-repopolicy-settings-check.service
```

A run every 15 minutes, the same token as the hook, events go to the audit log. The alert is built from audit (`settings_drift`).

### In a scheduled GitLab CI pipeline

If you would rather see the result as a job in GitLab, `settings-check` can run on a schedule in CI. The code stays the same, but you need:
- a **separate** personal access token of the same service account with scope `read_api`, in a masked and protected CI/CD variable. The hook token does not fit: `token rotate` rotates it on the node, and the CI variable goes stale
- a protected project where only administrators see the job and the variable: the token reads the settings of every group
- a config with `audit.stdout: true` and an empty `audit.file`: events go to the job log

```yaml
# .gitlab-ci.yml
settings-check:
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule"
  script:
    - printf '%s' "$REPOPOLICY_TOKEN" > "$CI_PROJECT_DIR/token"
    - |
      cat > config.yaml <<EOF
      gitlab_url: $CI_SERVER_URL
      token_file: $CI_PROJECT_DIR/token
      breakglass_dir: /nonexistent
      audit: { file: "", stdout: true }
      settings_check:
        default_branch: main
        protected_branches: [main]
        protected_tags: ["v*"]
        job_token_push: false
      EOF
    - gitlab-repopolicy settings-check --config config.yaml
```

The binary in the job comes from a verified release ([install.en.md](install.en.md#1-download-and-verify-the-release)), for example in the runner image. The job fails on any difference.

The recommended way is the timer on the node plus an alert from audit: it needs no extra token with access to every group.

## `coverage`

The service account itself does not see private groups it is not a member of, and cannot report them. `coverage` with an administrator token (scope `read_api`) finds them:

```sh
sudo gitlab-repopolicy coverage --token-file /root/admin-token
```

It prints the top-level groups without the service account (Maintainer) and the projects with policy files in them. The exit code is 1 if there are any. The administrator token is only read and not stored anywhere. Run it after new groups are created or on a schedule.

## Upgrade and rollback

- Upgrade with the package manager. The binary is replaced atomically, hooks that are already running finish on the old version
- with several Gitaly nodes the version must be the same; it shows in every audit event (`version`)
- rollback is installing the previous version. The PATHPOLICY and TAGPOLICY `version: 1` schemas are frozen, so a rollback does not make them invalid. A COMMITPOLICY with a rule the old version does not have loses only that rule (a warning)
- if the cause of a failure is unclear, start with `mode: audit` ([configuration.en.md](configuration.en.md#mode)): pushes pass, decisions go to the log

Every release checks that the previous minor version accepts every valid PATHPOLICY and TAGPOLICY from the test corpus and from `examples/`.

## Common problems

| Symptom | Cause | What to do |
| --- | --- | --- |
| every push to group projects: `push could not be verified` | the hook cannot read the config or the token (permissions, a 0750 root directory), git is not available | `sudo -u git gitlab-repopolicy selftest` |
| `the hook configuration on the server is invalid` | an invalid `config.yaml` or an empty `audit.file` | `sudo gitlab-repopolicy selftest` |
| `unable to verify authorization` on protected changes | the API is unavailable, the token expired, the service account is not a group member or its role is below Maintainer | `selftest`; `reason` in audit (`project_not_visible`, `role_below_maintainer`); `coverage` |
| the same in a personal project | the project owner is blocked or banned: the service account token does not see such users, and the project is treated as a group project | expected behaviour, fail closed |
| a merge request or the Web IDE shows only "pre-receive hook declined" without text | another hook in `pre-receive.d/` denied the push without a `GL-HOOK-ERR` line | check the neighbouring files in `pre-receive.d/` |
| the hook is not called | `custom_hooks_dir` is not set or is wrong, the wrapper is not executable, its name ends with `~`, the file sits at `custom_hooks_dir/pre-receive` | `grep -A2 '^\[hooks\]' /var/opt/gitlab/gitaly/config.toml`, `ls -l <custom_hooks_dir>/pre-receive.d/` |
| every change of a project is denied with `policy is invalid` | an invalid PATHPOLICY in the default branch | an Owner fixes PATHPOLICY (recovery) or break-glass |
| tags are denied with `policy is invalid` | an invalid TAGPOLICY in the default branch | fix TAGPOLICY through a merge |
| a merge in the UI is slow or `unable to verify authorization` under peak load | web operations hold a Puma worker while the hook calls back into Rails | `api_calls`, `duration_ms` in audit; `timeouts.http` |

`gitlab-repopolicy test` makes the hook decision on a local repository without a push, which helps to reproduce a denial:

```sh
gitlab-repopolicy test --repo /path/to/clone --old <old> --new <new> --ref refs/heads/main --user alice
```
