[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](pathpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](pathpolicy.en.md)

# PATHPOLICY

PATHPOLICY sets who may change certain files of the repository: the CI configuration, deploy scripts, production values. The file lives in the repository root as `PATHPOLICY` (no extension, case matters), its content is YAML. If a protected file is changed by a person who is not in the rule, GitLab denies the push.

First we go through PATHPOLICY on an example and see how to read a denial. Then comes the reference of fields and rules.

## Example

The platform team wants only itself to change the CI configuration and the deploy logic, and only itself and release-manager to change the Helm production values:

```yaml
# examples/ci-config/PATHPOLICY
# The platform team owns the CI configuration, the deploy logic and the
# production values; everything else is authorized by GitLab as usual.
version: 1

admins: [platform-lead, sre-lead]

rules:
  - name: ci-config
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "Makefile"
      - "scripts/deploy/**"
    allow: [platform-lead, sre-lead, ci-maintainer]
    message: >-
      The platform team changes the CI/CD configuration and the deploy logic.
      Ask platform-lead, sre-lead or ci-maintainer to make this change.

  - name: prod-values
    paths: ["helm/**/values-prod.yaml"]
    refs: protected
    allow: [platform-lead, release-manager]
```

What it says:
- `admins` - the people who may change the PATHPOLICY file itself. Without this list anyone whom PATHPOLICY gets in the way would simply remove their restriction from it
- `rules` - the rules. Each rule says which files (`paths`) who may change (`allow`) and in which branches (`refs`)
- `message` - the text that the person who is denied sees

How it works for different people. bob is a Developer of the project and is in no `allow`.

| Who | Changes what | Where | Result |
| --- | --- | --- | --- |
| bob | `README.md` | any branch | allowed. No rule concerns this file, GitLab decides as usual |
| bob | `.gitlab-ci.yml` | any branch, even their own feature branch | denied by the rule `ci-config` |
| ci-maintainer | `.gitlab-ci.yml` | any branch | allowed |
| bob | `helm/app/values-prod.yaml` | their own feature branch | allowed. The rule `prod-values` applies only in protected branches |
| bob | a merge of this feature branch into `main` | `main` | denied by the rule `prod-values`, because bob makes the change to `main` |
| release-manager | a merge of the same branch into `main` | `main` | allowed |
| bob | the `PATHPOLICY` file itself | any branch | denied. Only `admins` may change PATHPOLICY |

Why `ci-config` applies to every branch and not only to protected ones. The pipeline of any branch runs the CI configuration of that very branch, with the runners and variables of the project. If the rule applied only to protected branches, a Developer would push a branch with a changed `.gitlab-ci.yml`, and its pipeline would run that change before any review. So the `message` says to ask the owners of the files to make the change rather than to open a merge request.

PATHPOLICY protects the other policy files (`COMMITPOLICY`, `TAGPOLICY`, `BRANCHPOLICY`) the same way, with a rule for their name. This way every file gets its owner:

```yaml
# examples/delegation/PATHPOLICY
# PATHPOLICY stays with the platform team; the other policy files and the
# GitLab agent configuration are delegated to their owners by path rules.
version: 1

admins: [platform-lead]

rules:
  - name: commit-policy
    paths: ["COMMITPOLICY"]
    allow: [dev-lead, platform-lead]

  - name: tag-policy
    paths: ["TAGPOLICY"]
    allow: [release-lead, platform-lead]

  - name: kubernetes-agents
    paths: [".gitlab/agents/**"]
    allow: [cluster-owner]
```

Without such a rule anyone with push access may change a policy file. The hook accepts such a version but warns about it.

## How to read a denial

This is the denial when bob changed `.gitlab-ci.yml` in the branch `feature`:

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: PATHPOLICY rule "ci-config", branch feature: you may not change these files
remote:   may change them: platform-lead, sre-lead, ci-maintainer
remote:   files:
remote:     .gitlab-ci.yml
remote:   The platform team changes the CI/CD configuration and the deploy logic. Ask platform-lead, sre-lead or ci-maintainer to make this change.
remote:   this rule applies to every branch: ask one of them to push the change
remote:
remote: rules: PATHPOLICY in main (9f1c2ab)
remote: checked as: bob (HTTPS)
remote: GL-HOOK-ERR: gitlab-repopolicy: you may not change .gitlab-ci.yml (PATHPOLICY rule ci-config; may change: platform-lead, sre-lead, ci-maintainer)
```

git adds `remote:` to every line the server sent. Line by line:

| Line | What it means |
| --- | --- |
| `push rejected by the rules of this repository (1 problem)` | the push is not accepted as a whole, no branch and no tag from it changed. In parentheses - how many problems were found |
| `PATHPOLICY rule "ci-config", branch feature: ...` | the title of the problem: which rule file, which rule, which branch and what is wrong |
| `may change them:` | who may make this change |
| `files:` | which files of your push this rule protects |
| text in English | the `message` of the rule, written by the owners of the files |
| `this rule applies to every branch: ...` | the rule applies to every branch, so a separate branch does not get around it |
| `rules: PATHPOLICY in main (9f1c2ab)` | the rules are taken from the branch `main`, from the commit `9f1c2ab`. This version decides, not the one in your branch |
| `checked as: bob (HTTPS)` | whom the hook checked and how the push came (HTTPS, SSH, the GitLab UI or API). If it is not you, the push was made on behalf of someone else, for example from a CI job that another user started |
| `who can help: ...` | only in denials you cannot fix yourself: a GitLab API failure, a wrong server setup, a broken rule file in `main` |
| `GL-HOOK-ERR: ...` | a short summary in one line. A merge request, the Web IDE and the GitLab API show only this line |

What to do next:
- the change is really needed - ask someone from `may change them` to make it or to create a branch with it. A merge request from you does not help if the rule applies to every branch, because you cannot even push the branch
- the rule seems wrong - contact the `admins` of PATHPOLICY. `gitlab-repopolicy admins` (below) or the `PATHPOLICY` file in `main` lists them
- you touched a protected file by accident - remove its change from the commits (`git restore --source=origin/main -- .gitlab-ci.yml`, then `git commit --amend` or a new commit) and push again

One problem is one block: if the same violation is on several branches and tags of one push, the head of the block lists all of them. On a merge through the API GitLab answers only "Branch cannot be merged" and writes the denial into the `merge_error` field of the merge request. API results (a role, "blocked") never get into the message, only into audit.

This output is for a branch that is not the default one. To know whether the branch is protected, the hook asks the GitLab API. So `gitlab-repopolicy test` (below) reproduces it only with a working token. For `main` the decision is made without the API.

## How the hook reads the rules

- The hook takes every policy file from the **anchor**. The anchor is the commit the default branch of the project (usually `main`) pointed to before this push. So a push cannot weaken the rules for itself: changes in a push are always evaluated by the rules that were already in `main`
- One anchor for all branches and tags of one push. A branch with its own, weaker PATHPOLICY does not allow changes to itself
- A new version of a policy file is checked when it is pushed to **any** branch, including a feature branch. The author sees the error at once, not at merge time. Details - [acceptance of a new version](#acceptance-of-a-new-version)
- GitLab permissions stay as they are. The hook runs after the regular GitLab permission check and can only deny: PATHPOLICY narrows permissions and never grants new ones

## Fields

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | only `1`. The `version: 1` schema is frozen: new fields come only in `version: 2` |
| `admins` | yes | 1-5 usernames. Only they change PATHPOLICY itself |
| `rules` | no | up to 100 rules |
| `rules[].name` | yes | `[a-z0-9-]{1,63}`, unique in the file. Shown in messages and audit |
| `rules[].paths` | yes | 1-50 doublestar patterns |
| `rules[].refs` | no | `all` (default), `protected`, `default` or a list of branches and tags |
| `rules[].allow` | yes | 1-50 usernames. An empty list is not allowed, so that a path cannot be frozen by accident |
| `rules[].message` | no | text for the person who is denied, up to 1000 characters. Without it the message lists `allow` |

Unknown keys, YAML anchors and aliases are an error. The file size is up to 64 KiB. A push that brings more than 50 000 new commits is not checked by the hook and is denied with `limit_exceeded`.

## Who makes the change

The person who makes the change is called a principal below: a person or a bot on whose behalf GitLab accepted the push. `admins` and `allow` hold only usernames of specific people or bots. There are no groups or roles on purpose: group membership changes outside the review of the policy file, and a person added to a group for another task would silently get the right.

Usernames are compared case-insensitively, as in GitLab. The format is that of a GitLab username.

Whom the hook takes as the author of a change:

| How the push is made | Principal |
| --- | --- |
| HTTPS, Web IDE, merge and other UI and API operations | the user the operation runs as (`GL_USERNAME`). No API requests |
| SSH with a personal key | the key owner. The hook checks once through the API that the key is personal |
| SSH deploy key | nobody. A deploy key is not a person, even if an admin created it: on a protected path the push is denied with `unknown_principal` |
| project or group access token, service account | the bot, if its username is listed explicitly |
| merge button, auto-merge | whoever pressed merge or turned on auto-merge. The author of the commits does not count |
| push from CI with `CI_JOB_TOKEN` | the user who started the pipeline |

The hook sees a push from a CI job as a push of the user who started the pipeline: the hook environment has no other sign of a job. So the denial of such a push shows this user in `checked as:`, and the job is denied if this user is not in `allow`. Automation that needs a protected path can use a project access token whose bot is listed in the rule.

Commit author, committer and commit signatures are not used for authorization: they are easy to forge.

## Path matching

A path in `paths` is a pattern (glob) relative to the repository root. Special characters:
- `*` - any characters within one part of the path, that is it does not cross `/`
- `**` - any number of directories, including none
- `?` - one character, `[abc]` - one character from the list

| Pattern | Matches | Does not match |
| --- | --- | --- |
| `Makefile` | `Makefile` | `src/Makefile`, `makefile` |
| `*.tf` | `main.tf` | `infra/main.tf` |
| `ci/**` | `ci/a.yml`, `ci/x/y.yml` | `x/ci/a.yml` |
| `helm/**/values-prod.yaml` | `helm/values-prod.yaml`, `helm/app/values-prod.yaml`, `helm/a/b/values-prod.yaml` | `helm/app/values-stage.yaml` |

Limitations:
- a pattern always starts at the root, `terraform/prod/**` does not match `x/terraform/prod/a.tf`. There is no gitignore rule "a name without `/` matches in any directory"
- no negation (`!`) and no braces `{a,b}`
- matching is byte-wise and case-sensitive, the way git sees the path
- no leading `/`, no `\`, no empty parts, `.` or `..`, UTF-8, up to 256 bytes and at most four `**`
- rename and move are a deletion of the old path and an addition of the new one: both are checked. A change of file mode, a symlink and a gitlink (submodule) are path changes too

**Every matching rule must be satisfied (AND).** If a path falls under two rules, the author of the change must be in `allow` of both. A new rule can only narrow who may change a path. A narrow rule does not widen a broad one: if `**` is allowed only to platform-lead and `docs/**` to a technical writer, `docs/a.md` may be changed only by someone who is in both lists. The `explain` command (below) shows which rules match.

A path that no rule matches is authorized by GitLab as usual. PATHPOLICY is not an allowlist of the repository.

## Where a rule applies (`refs`)

| `refs` | Where | What counts as a change |
| --- | --- | --- |
| `all` (default) | every branch and tag | in default and protected branches - the difference of the branch content before and after the push, in other branches - the new commits of the push |
| `protected` | the default branch, protected branches and protected tags of the project | the content difference before and after the push |
| `default` | only the default branch | the content difference before and after the push |
| a list, for example `["refs/heads/release/*"]` | branches and tags from the list, even if GitLab does not protect them | the content difference before and after the push |

In a list, branches and tags are written by their full git name: the branch `main` is `refs/heads/main`, the branch `release/1.2` is `refs/heads/release/1.2`, the tag `v1.0.0` is `refs/tags/v1.0.0`. A short `main` in a list is an error. `*` stands for any characters except `/`. So `refs/heads/*` matches `refs/heads/main` but not `refs/heads/feature/x`.

Why it differs. In default and protected branches the decision depends on who makes the change to the branch itself: a merge into `release/1` of a commit from somebody else's branch needs the right of whoever pressed merge, even if the commit already sits in another branch. Reverting a file with a force push is a change too. In other branches only the new commits of the push are checked: a rebase of a feature branch onto a fresh `main`, where the platform team changed CI, does not turn into somebody else's change, and a branch from an already checked commit changes nothing.

A consequence worth remembering: a merge into an unprotected branch brings no new commits, and the hook does not check it. Protect the branches that merges go into, or list them in `refs`.

The price of `all` is that a Developer without the right cannot push even a proposal branch with a protected path. Ways to work:
- the owner of the path creates the branch with the change
- for paths where proposals as branches matter more than strictness, `refs: protected`. The rule then checks merges into default and protected branches
- a merge request from a fork in a personal namespace. The hook does not check personal projects, and the merge into upstream is checked as usual

## Admins, the first policy and recovery

An **admin** is a user from `admins` who is active and has at least the Maintainer role in the project (inheritance from groups included). An admin demoted to Developer does not count as an admin.

**A change of PATHPOLICY itself** (creation, edit, deletion, rename) is checked in every branch, regardless of the rules of the file.

| Who changes PATHPOLICY | Effective admins by the anchor policy | Result |
| --- | --- | --- |
| an admin | any | allowed |
| a project Owner (not an admin) | at least one | denied, `policy_admin_violation` |
| a project Owner (not an admin) | none, confirmed by the API | allowed: **recovery** |
| a project Owner (not an admin) | the API did not answer | denied, `authorization_backend_failure` |
| a deploy key or an unparsable `GL_ID` | any | denied, `unknown_principal`: only a person may change PATHPOLICY |
| anyone else | any | denied, `policy_admin_violation` |

**The first PATHPOLICY (bootstrap).** If the default branch has no PATHPOLICY, a user with the Maintainer role or higher may add it there. A malicious bootstrap is an inconvenience, not an escalation: PATHPOLICY only narrows permissions, and an Owner can remove the membership of whoever took over the policy, after which recovery applies.

**A new version must have an admin.** If no admin of the new version is active and Maintainer+, the push is denied with `lockout_prevented`. This catches a typo in a name.

**Recovery** turns on only when the API confirmed that there are no effective admins: all were removed from the project, blocked or demoted. The right goes to an effective Owner of the project, including an Owner of a parent group at any depth. An API failure never turns recovery on. An instance administrator is not a project member and does not get recovery: the way for them is to become an Owner (GitLab audits this) or break-glass ([operations.en.md](operations.en.md)).

A recovery push changes only PATHPOLICY. Protected paths in the same push are checked by the old rules, and their denial suggests to get the new PATHPOLICY into the default branch first. The Owner sees `PATHPOLICY is changed by Owner <username> because it has no working admins. This is logged.` in the push output. Every accepted recovery writes the audit event `policy_recovery`; set an alert on it. The `policy_admin_violation` denial does not mention recovery: everyone who is not an admin sees it, and recovery is open only to an Owner and only when there are no admins.

**An invalid PATHPOLICY in the default branch** (this happens only around the hook, for example through break-glass or `mode: audit`) blocks any change of the content of branches and tags of the project, because the rules are unknown. Deletion of branches and tags, break-glass and an Owner who fixes or deletes only PATHPOLICY still pass.

## Acceptance of a new version

A new version of any policy file is checked in any branch. An error denies the push with `invalid_policy_update` unless stated otherwise.

| Check | PATHPOLICY | COMMITPOLICY | TAGPOLICY | BRANCHPOLICY |
| --- | --- | --- | --- | --- |
| a regular file (not a directory, symlink or submodule), size within the limit | yes | yes | yes | yes |
| `version`, schema, unknown keys, limits | yes | yes | yes | yes |
| patterns and regular expressions compile, `pattern` examples pass | yes | yes | `tags` | yes, and the order of the rules ([branchpolicy.en.md](branchpolicy.en.md)) |
| usernames added in this version exist and are active (one GraphQL request) | yes | `except.users` | no | no |
| signing keys | no | no | yes | no |
| at least one effective admin | `lockout_prevented` | no | no | no |
| the API is unavailable during the checks | `authorization_backend_failure` | `authorization_backend_failure` | no | no |
| a delegated file is not covered by a PATHPOLICY rule | no | warning | warning | warning |

Usernames that were already in the anchor version are not checked again. `settings-check` finds principals that are gone ([operations.en.md](operations.en.md)).

Deleting a policy file is not a new version but a return to the state without the policy:

| What is deleted | Who may | After the merge into the default branch |
| --- | --- | --- |
| PATHPOLICY | an admin, in recovery an Owner | no rules; the next PATHPOLICY is added by a Maintainer+ |
| COMMITPOLICY, TAGPOLICY, BRANCHPOLICY | whoever the anchor PATHPOLICY rule allows; without a rule, anyone with push access | no checks of commit format, tag signatures or branch names |

If PATHPOLICY is deleted while COMMITPOLICY, TAGPOLICY or BRANCHPOLICY stay, anyone with push access can change them. The hook warns about this on acceptance. A directory `PATHPOLICY/` in place of the file is a policy change too (a deletion and an invalid new version).

## Denials

| Code | Title in the output | When |
| --- | --- | --- |
| `protected_path_violation` | you may not change these files | a path from a rule is changed by someone not in `allow` |
| `policy_admin_violation` | only PATHPOLICY admins may change PATHPOLICY | PATHPOLICY is changed by someone who is not an admin |
| `lockout_prevented` | the changed PATHPOLICY would have no working admin | the new version has no effective admin |
| `invalid_policy` | PATHPOLICY in main is broken, so no changes are accepted | PATHPOLICY in the default branch is invalid |
| `invalid_policy_update` | the changed PATHPOLICY is invalid | a new version of a policy file failed acceptance |
| `authorization_backend_failure` | could not check permissions: the GitLab API did not answer; this project is not connected to gitlab-repopolicy correctly | the GitLab API did not answer or the token expired; the service account does not see the project or its role is below Maintainer |
| `unknown_principal` | only a person may change these files | a deploy key or an unparsable `GL_ID` on a protected path or in PATHPOLICY |
| `tag_object_invalid` | the tag does not point to a commit, so its files cannot be checked | a tag under a PATHPOLICY rule points to a tree or a blob, not a commit |
| `limit_exceeded` | the check did not finish within the server limits | a limit of time, API calls or commits |
| `internal_error` | internal error: the push was not checked and is rejected | an internal error; the output has an id to look up in audit |

PATHPOLICY guards access. So when the hook cannot get the answer it needs (the GitLab API did not answer, the file in `main` is broken, an internal error), a push that touched protected files is denied, not let through. This is called fail closed. A push that touched no rule does not depend on the API and goes through.

How to read the text of a denial - [above](#how-to-read-a-denial).

## Checking without a push

| Command | What it does |
| --- | --- |
| `gitlab-repopolicy validate -f PATHPOLICY` | the same checks as on acceptance, without GitLab |
| `gitlab-repopolicy validate -f PATHPOLICY --online --config FILE --project P` | also checks through the API that usernames exist and the role of admins in the project. Every admin who is not an active user with Maintainer or higher gets a warning, plus the summary `N of M admins work`. The file stays valid, exit code 0 |
| `gitlab-repopolicy explain --project P --user U --path X [--ref R]` | which rules match the path and whether the user satisfies them. Exit code 0 means allowed or no rule matched, 1 means denied or an error (the error text is on stderr), 2 means `--project`, `--user` or `--path` is missing |
| `gitlab-repopolicy admins --project P` | admins from the file, effective admins and whether recovery is on |

`validate` without `--online` works anywhere the binary and the file are: locally, in a merge request CI. `explain`, `admins` and `validate --online` call the GitLab API with the token of the hook. They run on a Gitaly node, as a user who can read the server config (`--config`, by default `/etc/gitlab-repopolicy/config.yaml`) and the token file from it. `explain` and `admins` read PATHPOLICY from the default branch of the project, that is the version the next push will be evaluated by.

The `gitlab-repopolicy test` command reproduces the denial for a specific push ([operations.en.md](operations.en.md#check-a-push-without-pushing)). It needs:
- `--user` and `--gl-id user-<id>` of whoever pushed. Without `--gl-id` the command looks the ID up through the API, and without a working token it fails with `--gl-id is not given and user bob cannot be looked up`
- `--protocol` - `ssh` (default), `http` or `web`. The `checked as:` line depends on it
- the GitLab API if the branch is not the default one, because the hook asks whether it is protected

An example of a `validate` error. The hook prints the line of the file where the error is:

```text
$ gitlab-repopolicy validate -f PATHPOLICY
PATHPOLICY: invalid
  line 6: rule ci: paths: "/ci/**": patterns are relative to the repository root, remove the leading /
```

## What PATHPOLICY does not protect

The hook sees only changes of branches and tags. Project settings (the CI configuration path, protected branches, the default branch, job token push) change in Settings, and the hook does not see them. `settings-check` compares them ([operations.en.md](operations.en.md)); the full list of limitations is in [threat-model.en.md](threat-model.en.md).

The hook does not check projects in personal namespaces: they do not deploy and do not hold team code, and changes from them reach group projects through a merge, which is checked.
