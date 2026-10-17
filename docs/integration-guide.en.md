[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](integration-guide.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](integration-guide.en.md)

# Integration guide

How to turn gitlab-repopolicy on in a production instance without breaking anything. The order comes from the first production deployment; each installation decides on its own groups, people, keys and rules.

The main idea is to go from the least risky to the most: first only observe, then commit hygiene, then access to files, then releases.

## 1. Deployment repository

Keep everything that belongs to your installation in a separate repository:
- the pinned version of gitlab-repopolicy
- `config.yaml` and the COMMITPOLICY presets
- CI that verifies the signature and checksums of artifacts ([install.en.md](install.en.md#1-download-and-verify-the-release)) before any installation, stores the verified packages in your registry and delivers them to the nodes
- rollback is the same job with the previous version

Run delivery and rollback by hand (a manual job, the cloud console), not by a push: then a broken hook cannot block its own fix, even if the delivery lives in the same GitLab.

## 2. Service account before the hook is on

1. Create an instance service account and a `read_api` + `self_rotate` token ([install.en.md](install.en.md#3-service-account-and-token)), check with `selftest`
2. Run `coverage` with an administrator token and add the service account as a Maintainer to every top-level group from the output
3. Add the step "add the service account as a Maintainer" to your procedure for creating a top-level group

A project with a policy in a group without the service account is denied with `authorization_backend_failure` (`project_not_visible` in audit). This is the intended behaviour: protection is never skipped silently.

## 3. `mode: audit` everywhere

Install the hook on every Gitaly node with `mode: audit` and leave it for 1-2 weeks. The hook denies nobody and writes decisions to audit with `"enforced": false`.

What to look at in audit:
- `decision` events with `"decision": "deny"` - who and what `enforce` would break
- `authorization_backend_failure` - groups without the service account, token problems
- `duration_ms` and `api_calls` - the cost of the hook under your load
- `invalid_policy` and `decision` with `code: invalid_policy_update` - policies that block a project in `enforce`
- `decision` with `code: default_branch_not_allowed`, if `default_branch` is set ([configuration.en.md](configuration.en.md#default_branch)) - projects whose default branch has another name; in `enforce` pushes to them will be denied

## 4. COMMITPOLICY in a few projects

COMMITPOLICY is the safest start: it cannot lock anyone out, and an API failure only skips the check. The exception is a new COMMITPOLICY version with `except.users`: if the API did not answer, its acceptance is denied with `authorization_backend_failure`.

1. Describe the team standard as a preset in `commitpolicy_presets` ([commitpolicy.en.md](commitpolicy.en.md#presets-extends)). Derive it from the real commits of the team, not from earlier client-side schemas
2. In 2-3 projects add a COMMITPOLICY with `extends:` and `mode: warn`
3. Set the squash commit template of the project so that the squash commit follows the standard. The easiest way to get an issue reference is the `%{issues}` variable: GitLab fills in `Closes #N` for issues from the MR description
4. When there are almost no warnings, switch the file to `mode: enforce`

Turn on a required issue reference gradually: in the preset `reference` with `level: warn`, a project that is ready raises it to `error` in its own file (file rules override the preset), then `error` moves into the preset. Audit shows readiness: the share of `reference` warnings per project.

On a merge through the UI the author does not see warnings: GitLab does not show the successful hook output for web operations. The `warn` stage gives statistics, not training.

## 5. PATHPOLICY in one or two projects

Start with the CI logic of deployment projects ([examples/ci-config/PATHPOLICY](../examples/ci-config/PATHPOLICY)):
- protect not only `.gitlab-ci.yml` but everything that decides what a deploy job runs: includes, `Makefile`, scripts. Protecting only `.gitlab-ci.yml` without `Makefile` is for show if the job calls `make`
- the CI configuration path of the project, if it is not `.gitlab-ci.yml`, must be in the rule too. `settings-check` compares it
- pin components from the CI/CD Catalog to tags: their logic lives in the component project and is protected there
- delegate COMMITPOLICY, TAGPOLICY and BRANCHPOLICY to their owners with path rules ([examples/delegation/PATHPOLICY](../examples/delegation/PATHPOLICY))

Check a policy before the merge: `gitlab-repopolicy validate --online --config /etc/gitlab-repopolicy/config.yaml --project <project>` and `explain` ([pathpolicy.en.md](pathpolicy.en.md#checking-without-a-push)).

## 6. TAGPOLICY for releases

1. Move the keys of the people who sign releases into TAGPOLICY, and the regex of the rules from the CI rules that deploy tags ([tagpolicy.en.md](tagpolicy.en.md)). Extend expired keys or do not move them: acceptance rejects a new entry with an expired key
2. Split signers by environment: a separate rule for staging and for prod
3. While both the old check in CI and TAGPOLICY run, delete nothing. After the first releases through TAGPOLICY the old signature check can go from the deploy jobs
4. Checks that are not about authorization (a release window, a schedule) stay in CI

Before adding TAGPOLICY make sure that an import or `git push --mirror` does not bring old unsigned tags under the rules.

## 7. `mode: enforce` everywhere

Before the switch:
- audit for the `mode: audit` period has no `invalid_policy` and `invalid_policy_update` events: an invalid PATHPOLICY in the default branch blocks the project completely, an invalid TAGPOLICY blocks tags
- alerts are set up ([operations.en.md](operations.en.md#events-and-alerts)): `policy_recovery`, `breakglass`, `internal_error`, `invalid_policy`, `settings_drift` are required, `authorization_backend_failure` by a threshold
- the `token rotate` and `settings-check` timers are on
- the on-call runbook has break-glass and a quick rollback through `mode: audit`

## 8. The other projects

Policy files are added by merge requests as needed, one repository at a time. The default `refs: all` does not let a Developer push even a proposal branch with a protected path; for paths where this gets in the way, use `refs: protected` ([pathpolicy.en.md](pathpolicy.en.md#where-a-rule-applies-refs)).

Only an organizational measure closes the bypasses through project settings fully: the Maintainer role only for the owners of protected paths. Where this is not possible, `settings-check` is required ([threat-model.en.md](threat-model.en.md#bypasses-through-project-settings)).
