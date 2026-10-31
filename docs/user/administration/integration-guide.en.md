[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](integration-guide.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](integration-guide.en.md)

# Integration guide

Start a production rollout by observing in `audit`. Before enforcing denials, check access, policies and expected violations under your workload.

The main idea is to go from the least risky to the most. First only observe, then commit and branch hygiene, then access to files, then releases.

## In short

| Step | What we do | When to move on | How to check | How to roll back |
| --- | --- | --- | --- | --- |
| [1. Deployment repository](#1-deployment-repository) | Prepare a separate repository for installation, configuration and upgrades | Version, artifact verification, delivery and rollback defined | Check the CI pipeline, manual deployment and restoring the previous version | No rollback needed; the hook is not connected yet |
| [2. Service account before the hook is on](#2-service-account-before-the-hook-is-on) | The service account in all top-level groups | `coverage` finds no groups without the service account | `sudo gitlab-repopolicy selftest`, `coverage --token-file <admin-token>` | Nothing to turn back, the hook is not installed yet |
| [3. `mode: audit` everywhere](#3-mode-audit-everywhere) | The hook on all nodes with `mode: audit` | 1-2 weeks in `audit`, every `deny` is understood, no `invalid_policy` and no `decision` with `code: invalid_policy_update` | `jq` over the audit log ([operations.en.md](operations.en.md#audit)) | `mode: off` or `uninstall-hook` |
| [4. COMMITPOLICY in a few projects](#4-commitpolicy-in-a-few-projects) | COMMITPOLICY with `mode: warn` in 2-3 projects, then `mode: enforce` | Almost no warnings | `gitlab-repopolicy lint-commit --range`, `decision` events of the module `commitpolicy` | `mode: warn` in the project file |
| [5. BRANCHPOLICY](#5-branchpolicy) | BRANCHPOLICY with `mode: warn`, then `mode: enforce` | Almost no warnings | `gitlab-repopolicy validate`, `decision` events of the module `branchpolicy` | `mode: warn` in the project file |
| [6. PATHPOLICY in one or two projects](#6-pathpolicy-in-one-or-two-projects) | PATHPOLICY in one or two projects | Audit has no unexpected `deny` for these projects | `validate --online`, `explain` | A merge request with the previous version by an admin, or break-glass ([operations.en.md](operations.en.md#break-glass)) |
| [7. TAGPOLICY for releases](#7-tagpolicy-for-releases) | TAGPOLICY for releases | The first releases went through TAGPOLICY | `gitlab-repopolicy test` on a tag | A merge request with the previous version of TAGPOLICY |
| [8. `mode: enforce` everywhere](#8-mode-enforce-everywhere) | `mode: enforce` everywhere | All denials understood, access checked, operator ready to roll back | The alerts from [operations.en.md](operations.en.md#events-and-alerts) | `mode: audit`, applies from the next push |
| [9. The other projects](#9-the-other-projects) | Add agreed presets to remaining projects in small batches | Each batch passes `validate`, control pushes and audit review | Policy CI checks, node `selftest`, `settings-check` | Owner restores project policy; temporarily use `audit` for a widespread incident |

During steps 4-7 global mode remains `audit`: policy `mode: enforce` does not yet block pushes. Readiness is checked through logs and local tools. Step 8 enables blocking for the instance. After that, new projects need their own preparation and control checks before policy merge.

In this process `settings-check` only detects changes of project Settings on a schedule (once a day by a timer), it does not forbid them. A change made and reverted between two runs it does not see ([operations.en.md](operations.en.md#settings-check)).

## 1. Deployment repository

Keep everything that belongs to your installation in a separate repository:
- the pinned version of gitlab-repopolicy
- `config.yaml` with the presets of all modules
- CI that verifies the signature and checksums of artifacts ([install.en.md](install.en.md#1-prepare-and-verify-artifacts)) before any installation, stores the verified packages in your registry and delivers them to the nodes
- rollback is the same job with the previous version

Run delivery and rollback by hand (a manual job, the cloud console), not by a push: then a broken hook cannot block its own fix, even if the delivery lives in the same GitLab.

## 2. Service account before the hook is on

Before `selftest`, install the package and prepare the audit configuration and token file following [installation](install.en.md). Do not connect the wrapper yet. Running as git also requires readable files; `install-hook` sets final ownership in the next step.

1. Create an instance service account and a `read_api` + `self_rotate` token ([install.en.md](install.en.md#3-service-account-and-token)), check with `selftest`
2. Run `coverage` with an administrator token and add the service account as a Maintainer to every top-level group from the output
3. Add the step "add the service account as a Maintainer" to your procedure for creating a top-level group

A project with a policy in a group without the service account is denied with `authorization_backend_failure` (`project_not_visible` in audit). This is the intended behaviour: protection is never skipped silently.

## 3. `mode: audit` everywhere

Install the hook on every Gitaly node with `mode: audit` and leave it for 1-2 weeks. Core decisions do not block pushes and are recorded with `"enforced": false`. Startup failures before the core, including unreadable configuration, can still reject pushes; run `selftest` as root and git.

What to look at in audit:
- all events with `"decision": "deny"` - who and what `enforce` would break
- `authorization_backend_failure` - groups without the service account, token problems
- `duration_ms` and `api_calls` - the cost of the hook under your load
- `invalid_policy` and `decision` with `code: invalid_policy_update` - policies that block a project in `enforce`
- `decision` with `code: default_branch_not_allowed`, if `default_branch` is not empty, and by default it is `[main, master]` ([configuration.en.md](configuration.en.md#default_branch)) - projects whose default branch has another name; in `enforce` pushes to them will be denied
- `decision` with `code: tag_not_on_branch` - who puts tags on commits outside of branches. This check applies to every group project, even without policy files ([tagpolicy.en.md](../examples/tagpolicy/tag-on-branch.en.md)). Before `enforce` agree with the teams that tags are pushed together with the branch (`git push --follow-tags`)

## 4. COMMITPOLICY in a few projects

COMMITPOLICY is the safest start: it cannot lock anyone out, and an API failure only skips the check. The exception is a new COMMITPOLICY version with `except.users`: if the API did not answer, its acceptance is denied with `authorization_backend_failure`.

1. Describe the team standard as a preset in `presets.commitpolicy` ([configuration.en.md](configuration.en.md#presets)). Agree on requirements with the team and check them against real messages
2. In 2-3 projects add a COMMITPOLICY with `extends:` and `mode: warn`
3. Set the squash commit template of the project so that the squash commit follows the standard. The easiest way to get an issue reference is the `%{issues}` variable: GitLab fills in `Closes #N` for issues from the MR description
4. When there are almost no warnings, switch the file to `mode: enforce`

Turn on a required issue reference gradually: in the preset a `pattern` check with `level: warn`, a project that is ready raises it to `error` in its own file (`checks.pattern: { level: error }`, the file merges over the preset), then `error` moves into the preset. Audit shows readiness: the share of `pattern` warnings per project.

On a merge through the UI the author does not see warnings: GitLab does not show the successful hook output for web operations. The `warn` stage gives statistics, not training.

## 5. BRANCHPOLICY

BRANCHPOLICY checks only branch creation, so it can be introduced without renaming existing branches. An API failure skips the check with a warning. Create a preset in `presets.branchpolicy`, add project BRANCHPOLICY files with `extends` and `mode: warn`, review audit warnings for a week or two, then set `mode: enforce`. The [BRANCHPOLICY quick start](../policies/branchpolicy.en.md#quick-start) provides the configuration for these steps. Before merging, check the intended names with `validate --config presets.yaml --branch <name>`.

`git push --mirror` of an old repository creates all branches at once and is rejected as a whole when one name does not follow the standard. Add BRANCHPOLICY after the import.

## 6. PATHPOLICY in one or two projects

Start with the CI logic of deployment projects ([examples/ci-config/PATHPOLICY](../../../examples/ci-config/PATHPOLICY)):
- protect not only `.gitlab-ci.yml` but everything that decides what a deploy job runs: includes, `Makefile`, scripts. Protecting only `.gitlab-ci.yml` without `Makefile` is for show if the job calls `make`
- the CI configuration path of the project, if it is not `.gitlab-ci.yml`, must be in the rule too. `settings-check` compares it
- pin components from the CI/CD Catalog to tags: their logic lives in the component project and is protected there
- delegate COMMITPOLICY, TAGPOLICY and BRANCHPOLICY to their owners with path rules ([examples/delegation/PATHPOLICY](../../../examples/delegation/PATHPOLICY))

Check a policy before the merge: `gitlab-repopolicy validate --online --config /etc/gitlab-repopolicy/config.yaml --project <project>` and `explain` ([pathpolicy.en.md](../policies/pathpolicy.en.md#checking-without-a-push)).

## 7. TAGPOLICY for releases

1. Move the keys of the people who sign releases into TAGPOLICY, and the regex of the rules from the CI rules that deploy tags ([tagpolicy.en.md](../policies/tagpolicy.en.md)). Extend expired keys or do not move them: acceptance rejects a new entry with an expired key
2. Split signers by environment: a separate rule for staging and for prod
3. While both the old check in CI and TAGPOLICY run, delete nothing. After the first releases through TAGPOLICY the old signature check can go from the deploy jobs
4. Checks that are not about authorization (a release window, a schedule) stay in CI

Before adding TAGPOLICY make sure that an import or `git push --mirror` does not bring old unsigned tags under the rules.

## 8. `mode: enforce` everywhere

Before the switch:
- audit for the `mode: audit` period has no `invalid_policy` events and no `decision` with `code: invalid_policy_update`. An invalid PATHPOLICY in the default branch blocks the project completely, an invalid TAGPOLICY blocks tags
- alerts are set up ([operations.en.md](operations.en.md#events-and-alerts)): `policy_recovery`, `breakglass`, `internal_error`, `invalid_policy`, `settings_drift` are required, `authorization_backend_failure` by a threshold
- the `token rotate` and `settings-check` timers are on
- the on-call runbook has break-glass and a quick rollback through `mode: audit`

## 9. The other projects

With hundreds of projects the rules are not copied into every repository. The order:
1. The standards of the organization are presets in the server config ([configuration.en.md](configuration.en.md#presets)): `org-ci` for PATHPOLICY, `org-commits`, `org-branches`, `org-tags`. The presets live in the deployment repository and reach every node with the config
2. The file of a project is one line `extends` plus what is its own: `admins` of PATHPOLICY, `keys` of TAGPOLICY, deviations from the standard ([an example](../examples/pathpolicy/organization-preset.en.md))
3. Before the merge `validate --config presets.yaml` checks every file in the CI of the merge request ([validate.en.md](../reference/validate.en.md#validate-in-the-ci-of-a-merge-request)). The copy of the presets for CI is the same file of the deployment repository
4. The first denials after the merge. `validate` does not know everything the hook knows ([validate.en.md](../reference/validate.en.md#what-validate-does-not-know)): most often it is a new username that GitLab does not have, or an admin without the role Maintainer. The text of the denial names the rule and the field, the fix is a new merge request with the file. Denials to developers for protected files are the expected behavior: the text of the denial says whom to ask
5. A change of a preset applies to every project from the first push after the config is delivered. Check it the same way: `validate --config` on a few files of projects before the delivery, `selftest` after

A rule with `match: all` does not let a Developer push even a proposal branch with a changed protected file. For files GitLab reads only from the default branch `match: default` is enough ([pathpolicy.en.md](../policies/pathpolicy.en.md#where-a-file-takes-effect)).

After rollout: old branches created before the policies stay as they are. Their merge into `main` is checked by the rules, and whoever performs the merge needs permission to introduce protected changes. An old commit's authorship alone does not determine denial. Delete the branches nobody plans to merge, and rebase the needed ones onto a fresh `main`: then they hold only the own changes of their author.

Reduce the risk of bypasses through project settings: grant Maintainer access to people you trust to manage protected paths, and run `settings-check` to detect drift. The hook does not authorize Settings changes; the [threat model](threat-model.en.md#bypasses-through-project-settings) explains their consequences.
