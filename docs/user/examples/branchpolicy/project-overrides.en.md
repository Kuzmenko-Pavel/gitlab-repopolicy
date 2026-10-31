[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](project-overrides.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](project-overrides.en.md)

# Project overrides on top of a preset

The project includes an organization standard but forbids `master`, removes demo rules and customizes its work-branch guidance. Check not only the YAML edits but also which rule receives a name after its dedicated class is removed.

## Input files

Save this server configuration fragment as `presets.yaml` for local checks. An administrator delivers it to the server.

```yaml
presets:
  branchpolicy:
    org-branches:
      rules:
        release:
          match: ['/^release\/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$/']
          priority: 100
          protected: true
          message: Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
        release-bad:
          match: ['/^release\//']
          priority: 110
          deny: true
          message: Release branches look like release/v1.4.0.
        hotfix:
          match: ['/^hotfix\/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$/']
          priority: 200
          protected: true
          message: Hotfix branches look like hotfix/v1.4.1. Protect hotfix/* with "Allowed to push and merge Maintainers".
        hotfix-bad:
          match: ['/^hotfix\//']
          priority: 210
          deny: true
          message: Hotfix branches look like hotfix/v1.4.1.
        epic:
          match: ['/^epic\/[a-z0-9][a-z0-9._-]*$/']
          priority: 300
          protected: true
          message: Epic branches look like epic/payments. Protect epic/* in Protected branches.
        epic-bad:
          match: ['/^epic\//']
          priority: 310
          deny: true
          message: Epic branches look like epic/payments.
        demo:
          match: ['/^demo\/[a-z0-9][a-z0-9._-]*$/']
          priority: 400
          protected: true
          message: Demo branches look like demo/client-a. Protect demo/* in Protected branches.
        demo-bad:
          match: ['/^demo\//']
          priority: 410
          deny: true
          message: Demo branches look like demo/client-a.
        work:
          match: ['/^[a-z0-9][a-z0-9._-]*\/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)\/)?[a-z0-9][a-z0-9._-]*$/']
          priority: 900
          user_prefix: true
          message: Work branches are <username>/[<issue>/]<name>, for example kpn/disable_auth or kpn/PAY-12/refunds.
```

Create `BRANCHPOLICY` in the project root:

```yaml
# examples/branch-standard/BRANCHPOLICY
# The branch standard of the organization comes from the preset org-branches
# of the server config. This project also forbids master, has no demo
# branches and names its own work branches in the message.
version: 1
extends: org-branches

rules:
  no-master:
    match: [master]
    priority: 10
    deny: true
    message: This project uses main; master must not be created.
  demo: null
  demo-bad: null
  work:
    message: Work branches are <username>/[<issue>/]<name>, for example kpn/PAY-12/refunds; PAY is the Jira project of this team.
```

`no-master` has a lower `priority` than every preset rule. `demo: null` and `demo-bad: null` remove classes without forbidding their names. `work.message` is replaced while `match`, `priority` and `user_prefix` remain inherited.

`validate --config` shows where every rule and field comes from (a fragment):

```text
$ gitlab-repopolicy validate --config presets.yaml
BRANCHPOLICY: valid
  extends: org-branches
  ...
  rule work: preset:org-branches, file
    match: preset:org-branches
    message: file
    priority: preset:org-branches
    user_prefix: preset:org-branches
  rule no-master: file
    match: file
    priority: file
    deny: file
    message: file
```

`validate --branch` answers which rule applies to a name:

```text
$ gitlab-repopolicy validate --config presets.yaml --branch master
...
branch master: rule no-master (match: [master]; from file)
  result: denied, branches of this class may not be created
  message: This project uses main; master must not be created.

$ gitlab-repopolicy validate --config presets.yaml --branch demo/client-a
...
branch demo/client-a: rule work (match: [/^[a-z0-9][a-z0-9._-]*\/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)\/)?[a-z0-9][a-z0-9._-]*$/]; from preset:org-branches, file)
  requires: demo is an active GitLab user (checked with --online)
  result: allowed when the requirements hold
```

After `demo` and `demo-bad` are removed, the name `demo/client-a` falls into the next rule, `work`. Now `demo` has to be a username: worth knowing when you remove a rule of a preset.

<!-- check: branch-push examples/branch-standard/BRANCHPOLICY main,hotfix/* -->
| New branch | Who creates it | Rule | Result | Why |
| --- | --- | --- | --- | --- |
| `master` | alice | `no-master` | denied | `deny` |
| `demo/client-a` | alice | `work` | denied | there is no rule `demo`, and `demo` is not a GitLab user |

The table assumes an active policy on default branch `main`, global `enforce`, an existing user alice and no user demo. GitLab protects `main` and `hotfix/*`. Validate the file with `gitlab-repopolicy validate -f BRANCHPOLICY --config presets.yaml` before merging.

A common mistake is treating rule removal as a class prohibition. `demo/client-a` falls through to `work` and now needs an active user demo. To forbid the prefix regardless of users, add an explicit `deny` rule ordered before `work`.

In `validate --branch`, exit 0 means the query completed. A control push creating `master` under global `enforce` should produce `branch_name_violation`.

[BRANCHPOLICY reference](../../policies/branchpolicy.en.md)
