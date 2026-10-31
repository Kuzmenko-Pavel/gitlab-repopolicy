[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](pathpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](pathpolicy.en.md)

# PATHPOLICY

## In short

PATHPOLICY decides who may change certain files of a repository: the CI configuration, deploy scripts, production values. When a person who is not in the rule changes a protected file, GitLab rejects the push. The permissions of GitLab stay as they are: PATHPOLICY only narrows them and never grants new ones.

The file is in the root of the repository under the name `PATHPOLICY` (no extension, case matters), the content is YAML. The smallest working file:

```yaml
version: 1
admins: [alice]
rules:
  ci:
    match: all
    paths: [".gitlab-ci.yml"]
    allow: [alice]
    message: CI belongs to alice.
```

## Quick start

1. Create `PATHPOLICY` in a branch, check it with `gitlab-repopolicy validate` ([validate.en.md](../reference/validate.en.md)) and merge it into the default branch. The first PATHPOLICY is added by a Maintainer or an Owner of the project
2. From then on the hook reads the rules from the default branch. Only alice may change `.gitlab-ci.yml`, in any branch. GitLab authorizes the other files as usual
3. `PATHPOLICY` itself is now changed only by `admins`. Protect the other policy files (`COMMITPOLICY`, `TAGPOLICY`, `BRANCHPOLICY`) with a rule of PATHPOLICY, otherwise anyone who can push may change them ([example 6](../examples/pathpolicy/delegation.en.md))

What bob, who is not in `allow`, sees when they push a branch with a changed `.gitlab-ci.yml` is in [Denials](#denials).

## Field reference

| Field | Required | Value |
| --- | --- | --- |
| `version` | yes | only `1` |
| `extends` | no | the name of a preset of the server config or a list of names ([configuration.en.md](../administration/configuration.en.md#presets)) |
| `admins` | yes | 1-5 usernames. Only they change PATHPOLICY itself. Presets have no `admins`, it is always in the file of the project |
| `rules` | no | rules, up to 100: rule name → rule. The name is `[a-z0-9-]{1,63}`, it is shown in denials and in audit |
| `rules.<name>.match` | yes | the branches the rule applies to: `all`, `default` or a list of branches ([match.en.md](match.en.md)) |
| `rules.<name>.check_history` | no | how to check changes in these branches: `new-content` (the default) or `incoming` ([below](#what-counts-as-a-change)) |
| `rules.<name>.paths` | yes | 1-50 path patterns ([below](#path-patterns)) |
| `rules.<name>.allow` | yes | 1-50 usernames of those who may change these paths |
| `rules.<name>.message` | yes | text for the person who is denied, up to 1000 characters |
| `rules.<name>.help` | no | `contact` (up to 200 characters) and `docs_url` (an `http(s)://` address). A denial by the rule prints `who can help with this rule:` and `docs of this rule:` |

"Required" means required after the merge with the presets: in the file a rule may change only some fields of a rule of a preset ([example 7](../examples/pathpolicy/organization-preset.en.md)). Any field of a rule in the file may be `null`, then it is removed from the result of the merge.

Unknown keys, YAML anchors and aliases are an error. The file is up to 64 KiB.

### Path patterns

A path in `paths` is a pattern relative to the root of the repository:
- `*` - any characters within one part of the path, it does not cross `/`
- `**` - any number of directories, including none
- `?` - one character, `[abc]` - one character from the list

| Pattern | Covers | Does not cover |
| --- | --- | --- |
| `Makefile` | `Makefile` | `src/Makefile`, `makefile` |
| `*.tf` | `main.tf` | `infra/main.tf` |
| `ci/**` | `ci/a.yml`, `ci/x/y.yml` | `x/ci/a.yml` |
| `helm/**/values-prod.yaml` | `helm/values-prod.yaml`, `helm/app/values-prod.yaml` | `helm/app/values-stage.yaml` |

- a pattern always starts at the root: `terraform/prod/**` does not cover `x/terraform/prod/a.tf`
- there is no negation (`!`) and no braces `{a,b}`
- matching is case-sensitive, as git sees the path
- no leading `/`, no `\`, `.`, `..` or empty parts, up to 256 bytes and at most four `**`
- a rename is a deletion of the old path and an addition of the new one, both are checked. A change of the file mode, a symlink and a submodule are changes of the path too

**Every rule that matches applies.** When a path is in two rules, the change is allowed only to someone who is in the `allow` of both. A new rule can only narrow the circle of people. GitLab authorizes a path that no rule matches as usual: PATHPOLICY is not an allowlist of the repository.

### Who makes the change

`admins` and `allow` hold only usernames of people or bots. There are no groups or roles on purpose: membership in a group changes outside the review of the policy file, and a person added to a group for another task would get the right silently. Usernames are compared case-insensitively.

| How the push is made | Who makes the change |
| --- | --- |
| HTTPS, Web IDE, merge and other actions in the UI and the API | the user the action runs as |
| SSH with a personal key | the owner of the key |
| SSH deploy key | nobody: a deploy key is not a person, on a protected path the denial is `unknown_principal` |
| project or group access token, service account | the bot, when its username is in the rule |
| the merge button, auto-merge | who pressed merge or turned auto-merge on. The author of the commits does not count |
| a push from CI with `CI_JOB_TOKEN` | the user who started the pipeline |

Author and committer are commit metadata and do not establish the pusher's identity. Commit signatures are also not used for this authorization: the decision concerns whoever introduces the change into the branch. Automation that needs a protected path gets a project access token whose bot username is in the rule.

### What counts as a change

`match` only selects branches. What counts as a change of a protected file in these branches is set by `check_history` of the rule:

| `check_history` | What counts as a change of a protected file |
| --- | --- |
| `new-content` (the default) | a new state of the file in new commits that no branch or tag has yet. A commit changes the file only when the state of the file after it differs from every version in this branch before the push and from the default branch. The state is the content and the mode of the file in git: a regular file, an executable one (`chmod +x`), a symlink or a submodule. So `chmod +x ci.yml` without a change of the text is a change too |
| `incoming` | every commit that this push brings into the branch, and the difference of the content of the branch before and after the push. It does not matter who made the commit and whether another branch already holds it: the change is made by whoever pushes or merges it into the branch. For a new branch, the commits that the default branch does not have |

The value works the same in the default branch, in branches protected in GitLab and in ordinary branches: the status of a branch in GitLab does not change the check. Tags are not checked: a tag can point only to a commit that a branch already holds ([tagpolicy.en.md](../examples/tagpolicy/tag-on-branch.en.md)), and that commit was checked with the branch.

A [separate example](../examples/pathpolicy/check-history.en.md) compares the same actions with both values. The consequences, shown in the examples:
- with `new-content` a rebase, a squash and a cherry-pick of commits of alice are not a change while the content of the file is the same ([example 4](../examples/pathpolicy/shared-branch.en.md))
- with `new-content` a merge into `main` of a branch that was already pushed does not check its commits again: they are not new to the repository. If the rule must check merge requests into `main` or release branches, set `check_history: incoming` ([example 3](../examples/pathpolicy/production-values.en.md))
- with `incoming` a change reverted in the same push is still a change, and so is a rollback of the branch by a force push ([example 5](../examples/pathpolicy/change-and-revert.en.md))

`PATHPOLICY` itself does not fall under the `new-content` exception: only `admins` change it, in any branch, and in the default branch, where it takes effect, every commit that enters the branch is checked.

When more than 50 000 commits enter a branch in one push, the hook does not check them and rejects the push with `limit_exceeded`. Check large imports or history rewrites separately and split them when needed.

### Where a file takes effect

The right `match` depends on where GitLab reads the file from:

| Files | Where GitLab reads them | `match` |
| --- | --- | --- |
| `.gitlab-ci.yml`, `.gitlab/ci/**`, scripts that CI calls | the pipeline of any branch runs the CI configuration of that branch, with the runners and variables of the project | `all` |
| the agent configuration `.gitlab/agents/**`, `CODEOWNERS`, policy files | only from the default branch | `default` |
| deploy values (`values-prod.yaml`) | from the branches deployments run from | a list of branches, for example `[main, '/^release\//']` |

The agent configuration and the deploy values reach the default branch and release branches through merge requests from work branches, where their rule does not apply. For such a merge to be checked, give the rules with `default` and with a list of branches `check_history: incoming` ([what counts as a change](#what-counts-as-a-change)).

## Examples

| Example | What it explains |
| --- | --- |
| [CI/CD protection](../examples/pathpolicy/ci-configuration.en.md) | alice and sre-lead own CI and deploy scripts in every branch |
| [GitLab Agent configuration](../examples/pathpolicy/agent-configuration.en.md) | Anyone proposes changes; only alice accepts them into the default branch |
| [Production configuration](../examples/pathpolicy/production-values.en.md) | Rules for `main` and release branches |
| [`new-content` and `incoming`](../examples/pathpolicy/check-history.en.md) | The same rebase, squash, merge and rollback with both values of `check_history` |
| [Shared branches](../examples/pathpolicy/shared-branch.en.md) | Rebase, squash, cherry-pick and the limits of the known-content exception |
| [Change and revert](../examples/pathpolicy/change-and-revert.en.md) | Why an empty endpoint diff does not cancel commit checks |
| [Policy delegation](../examples/pathpolicy/delegation.en.md) | Separate COMMITPOLICY and TAGPOLICY owners |
| [Organization preset](../examples/pathpolicy/organization-preset.en.md) | Rule merging, list replacement and removal with `null` |
| [Access recovery](../examples/pathpolicy/recovery.en.md) | An Owner appoints admins when the API confirms no working admins remain |

## The first PATHPOLICY and new versions

**The first PATHPOLICY** in the default branch is added by a user with the role Maintainer or higher. A malicious first file is an inconvenience, not an escalation: PATHPOLICY only narrows permissions, and an Owner can remove the membership of whoever took the file, and recovery turns on.

**A new version of any policy file** is checked by the hook at a push to any branch, not only to `main`. The author sees the error at once, not at the merge. An error is the denial `invalid_policy_update`.

| Check | PATHPOLICY | COMMITPOLICY | TAGPOLICY | BRANCHPOLICY |
| --- | --- | --- | --- | --- |
| a regular file (not a directory, not a symlink, not a submodule), within the size limit | yes | yes | yes | yes |
| `version`, schema, unknown keys, limits, `match`, the merge with presets | yes | yes | yes | yes |
| usernames added in this version exist and are active | yes | `except.users` | no | no |
| signing keys | no | no | yes | no |
| at least one working admin, otherwise `lockout_prevented` | yes | no | no | no |
| the file is not protected by a rule of PATHPOLICY | no | warning | warning | warning |

Usernames that the version of the default branch already had are not checked again. Those that disappear later are found by `settings-check` ([operations.en.md](../administration/operations.en.md#settings-check)).

**An invalid PATHPOLICY in the default branch** (it happens only around the hook, through break-glass or `mode: audit`) blocks every change of the content of branches and tags of the project, because the rules are unknown. Deletions of branches and tags, break-glass and an Owner who fixes or deletes only PATHPOLICY go through.

**Deleting a policy file** returns to the state without it. PATHPOLICY is deleted by an admin (by an Owner during recovery), the other files by whoever a rule of PATHPOLICY allows. When PATHPOLICY is deleted and the other files stay, anyone who can push may change them.

## Denials

This is the denial when bob changed `.gitlab-ci.yml` in the branch `feature` ([example 1](../examples/pathpolicy/ci-configuration.en.md)):

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: PATHPOLICY rule "ci-config", branch feature: you may not change these files
remote:   may change them: alice, sre-lead
remote:   files:
remote:     .gitlab-ci.yml
remote:   The platform team changes the CI/CD configuration and the deploy scripts. Ask alice or sre-lead to push this change.
remote:   the content of .gitlab-ci.yml in commit 11e97b832 differs both from this branch and from main; new content is pushed by one of them
remote:   who can help with this rule: #platform
remote:
remote: rules: PATHPOLICY in main (f50c5a0)
remote: checked as: bob (SSH)
remote: GL-HOOK-ERR: gitlab-repopolicy: you may not change .gitlab-ci.yml (PATHPOLICY rule ci-config; may change: alice, sre-lead)
```

git adds `remote:` to every line from the server.

| Line | What it means |
| --- | --- |
| `push rejected by the rules of this repository (1 problem)` | the push is rejected as a whole, no branch and no tag of it changed |
| `PATHPOLICY rule "ci-config", branch feature: ...` | the policy file, the rule, the branch and what is wrong |
| `may change them:` | who may make this change |
| `files:` | which files of the push this rule protects |
| the English text | the `message` of the rule |
| `the content of ... differs ...` | with `check_history: new-content`: which commit brought new content. With `incoming` there is advice whom to ask instead |
| `who can help with this rule:`, `docs of this rule:` | the `help` of the rule |
| `rules: PATHPOLICY in main (f50c5a0)` | the rules come from `main`, the commit `f50c5a0`. This version decides, not the one in your branch |
| `checked as: bob (SSH)` | whom the hook checked and how the push came. If it is not you, the push was made under another name, for example from a CI job started by another user |
| `who can help:` | only in denials you cannot fix yourself: a failure of the GitLab API, a setting of the server, a broken file in `main` |
| `GL-HOOK-ERR: ...` | the summary in one line. A merge request, the Web IDE and the GitLab API show only this line |

What to do next:
- the change is really needed - ask one of `may change them` to make it
- the rule seems wrong - contact the `admins` of PATHPOLICY (`gitlab-repopolicy admins` or the file in `main`)
- a protected file was touched by accident - restore it from `main` (`git restore --source=origin/main -- .gitlab-ci.yml`, then `git commit --amend`) and push again

A change of PATHPOLICY itself by somebody who is not an admin:

```text
remote: PATHPOLICY, branch feature: only PATHPOLICY admins may change PATHPOLICY
remote:   admins: alice, sre-lead
remote:   files:
remote:     PATHPOLICY
remote:   ask one of them to make this change; this applies to every branch, including yours
```

| Code | Title in the output | When |
| --- | --- | --- |
| `protected_path_violation` | you may not change these files | a path of a rule is changed by someone not in `allow` |
| `policy_admin_violation` | only PATHPOLICY admins may change PATHPOLICY | PATHPOLICY is changed by someone who is not an admin |
| `lockout_prevented` | the changed PATHPOLICY would have no working admin | the new version has no working admin |
| `invalid_policy` | PATHPOLICY in main is broken, so no changes are accepted | PATHPOLICY in the default branch is invalid |
| `invalid_policy_update` | the changed PATHPOLICY is invalid | a new version of a policy file failed the checks |
| `authorization_backend_failure` | could not check permissions: the GitLab API did not answer | the GitLab API did not answer, the token expired or the service account does not see the project |
| `unknown_principal` | only a person may change these files | a deploy key on a protected path or in PATHPOLICY |
| `limit_exceeded` | the check did not finish within the server limits | the limit of time, of API calls or more than 50 000 commits |
| `internal_error` | internal error: the push was not checked and is rejected | an internal error, the output has an id to find in audit |

PATHPOLICY guards access. When the hook cannot get the answer it needs (the API did not answer, the file in `main` is broken, an internal error), a push that touches protected files is rejected, not let through. A push that touches no rule does not depend on the API and goes through.

## Common mistakes

| Symptom | Cause | What to do |
| --- | --- | --- |
| bob cannot push even a branch with `.gitlab-ci.yml` | a rule with `match: all` applies to every branch | this is intended for CI. The owner of the file pushes the branch with the change |
| a denial after a rebase of a branch, although `ci.yml` was not touched | the rule has `check_history: incoming`, and every commit that enters the branch counts | for CI files of work branches remove `check_history` or set `new-content` |
| bob changed the file in a branch, and the hook let it through | the rule with `match: default` or a list does not apply to this branch | this is intended. At the merge into `main` a rule with `check_history: incoming` checks the change; with `new-content` the commit of the branch is no longer new then and is not checked |
| a denial for a change that was reverted right away | the intermediate commit brought new content, and every new commit counts, not only the difference at the ends of the push | remove both commits from the history (`git rebase -i`) |
| a new PATHPOLICY is rejected with `lockout_prevented` | a typo in the name of an admin, or the admin is not a Maintainer | fix the name, check it with `validate --online --project` |
| `checked as:` shows another username | a push from a CI job runs as the user who started the pipeline | give automation a project access token and put the bot in `allow` |
| `allow: [kpn]` in a file with `extends` removed the other people of the preset | a list in the file replaces the list of the preset | list everyone who has the right in the file |

## Checking without a push

| Command | What it does |
| --- | --- |
| `gitlab-repopolicy validate -f PATHPOLICY` | the same checks as at a push, without GitLab. With `--config` also the merge with presets and the origin of the rules |
| `gitlab-repopolicy validate -f PATHPOLICY --online --config FILE --project P` | also that usernames exist and the role of the admins in the project, through the API |
| `gitlab-repopolicy explain --project P --user U --path X [--ref R]` | which rules match the path, where they come from and whether they allow the change |
| `gitlab-repopolicy admins --project P` | admins of the file, working admins and whether recovery is on |
| `gitlab-repopolicy test` | the decision of the hook for a given push without the push ([operations.en.md](../administration/operations.en.md#check-a-push-without-pushing)) |

The [validate guide](../reference/validate.en.md) shows how to check a file and interpret the result. Use the [CLI reference](../reference/cli.en.md) for other tasks and command parameters.

An error of `validate`, with the line of the file:

```text
$ gitlab-repopolicy validate -f PATHPOLICY
PATHPOLICY: invalid
  line 6: rule ci: paths: "/ci/**": patterns are relative to the repository root, remove the leading /
```

## What PATHPOLICY does not protect

The hook sees only changes of branches and tags. The settings of a project (the path of the CI configuration, protected branches, the default branch, job-token push) change in Settings, and the hook does not see them. `settings-check` compares them ([operations.en.md](../administration/operations.en.md#settings-check)), the full list of limits is in [threat-model.en.md](../administration/threat-model.en.md).

Projects in personal namespaces are outside the hook's scope. Merging their changes into a group project is checked in that group project. If a personal project deploys code, this hook does not protect that deployment.
