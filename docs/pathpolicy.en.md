[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](pathpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](pathpolicy.en.md)

# PATHPOLICY

PATHPOLICY answers one question: may the person who pushes changes to the repository change these files. The file lives in the repository root as `PATHPOLICY` (no extension, case matters), its content is YAML.

PATHPOLICY is the root of trust of a project. Its `admins` decide who changes PATHPOLICY itself, and ordinary path rules decide who changes the other policy files (`COMMITPOLICY`, `TAGPOLICY`).

## How the policy is read

- The hook reads every policy file from the **anchor**. The anchor is the commit the default branch of the project points to before this push. A push cannot weaken the rules for itself: changes in a push are always evaluated by the rules that were already in the default branch
- One anchor for all branches and tags of one push. A branch with its own, weaker PATHPOLICY does not authorize itself
- A new version of a policy file is checked when it is pushed to **any** branch, including a feature branch. The author sees the error at once, not at merge time. Details - [acceptance of a new version](#acceptance-of-a-new-version)
- GitLab permissions stay as they are. The hook runs after the regular GitLab permission check and can only deny: PATHPOLICY narrows permissions and never grants new ones

## Example

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
      Propose the change in a merge request.

  - name: prod-values
    paths: ["helm/**/values-prod.yaml"]
    refs: protected
    allow: [platform-lead, release-manager]
```

Delegating the other policy files to their owners:

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

Unknown keys, YAML anchors and aliases are an error. The file size is up to 64 KiB.

## Who makes the change

The person who makes the change is called a principal below. `admins` and `allow` hold only usernames of specific people or bots. There are no groups or roles on purpose: group membership changes outside the review of the policy file, and a person added to a group for another task would silently get the right.

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

Commit author, committer and commit signatures are not used for authorization: they are easy to forge.

## Path matching

- A pattern is a doublestar glob anchored at the repository root. `*` matches within one segment, `**` matches zero or more segments, `?`, `[abc]`
- `terraform/prod/**` does not match `x/terraform/prod/a.tf`. There is no negation (`!`), no braces `{a,b}` and no gitignore rule "a name without `/` matches in any directory"
- Matching is byte-wise and case-sensitive, the way git sees the path
- A pattern is relative: no leading `/`, no `\`, no empty, `.` or `..` segments, UTF-8, up to 256 bytes and at most four `**`
- Rename and move are a deletion of the old path and an addition of the new one: both are checked. A change of file mode, a symlink and a gitlink (submodule) are path changes too

**Every matching rule must be satisfied (AND).** If a path falls under two rules, the author of the change must be in `allow` of both. A new rule can only narrow who may change a path. A narrow rule does not widen a broad one: if `**` is allowed only to platform-lead and `docs/**` to a technical writer, `docs/a.md` may be changed only by someone who is in both lists. The `explain` command (below) shows which rules match.

A path that no rule matches is authorized by GitLab as usual. PATHPOLICY is not an allowlist of the repository.

## Where a rule applies (`refs`)

| `refs` | Where | What counts as a change |
| --- | --- | --- |
| `all` (default) | every branch and tag | in default and protected branches - the difference of the branch content before and after the push, in other branches - the new commits of the push |
| `protected` | the default branch, protected branches and protected tags of the project | the content difference before and after the push |
| `default` | only the default branch | the content difference before and after the push |
| a list, for example `["refs/heads/release/*"]` | branches and tags from the list, even if GitLab does not protect them | the content difference before and after the push |

In a list, `*` matches within one segment: `refs/heads/*` matches `refs/heads/main` but not `refs/heads/feature/x`.

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
| anyone else | any | denied, `policy_admin_violation` |

**The first PATHPOLICY (bootstrap).** If the default branch has no PATHPOLICY, a user with the Maintainer role or higher may add it there. A malicious bootstrap is an inconvenience, not an escalation: PATHPOLICY only narrows permissions, and an Owner can remove the membership of whoever took over the policy, after which recovery applies.

**A new version must have an admin.** If no admin of the new version is active and Maintainer+, the push is denied with `lockout_prevented`. This catches a typo in a name.

**Recovery** turns on only when the API confirmed that there are no effective admins: all were removed from the project, blocked or demoted. The right goes to an effective Owner of the project, including an Owner of a parent group at any depth. An API failure never turns recovery on. An instance administrator is not a project member and does not get recovery: the way for them is to become an Owner (GitLab audits this) or break-glass ([operations.en.md](operations.en.md)).

A recovery push changes only PATHPOLICY. Protected paths in the same push are checked by the old rules. Every recovery writes the audit event `policy_recovery`; set an alert on it.

**An invalid PATHPOLICY in the default branch** (this happens only around the hook, for example through break-glass or `mode: audit`) blocks any change of the content of branches and tags of the project, because the rules are unknown. Deletion of branches and tags, break-glass and an Owner who fixes or deletes only PATHPOLICY still pass.

## Acceptance of a new version

A new version of any policy file is checked in any branch. An error denies the push with `invalid_policy_update` unless stated otherwise.

| Check | PATHPOLICY | COMMITPOLICY | TAGPOLICY |
| --- | --- | --- | --- |
| a regular file (not a directory, symlink or submodule), size within the limit | yes | yes | yes |
| `version`, schema, unknown keys, limits | yes | yes | yes |
| patterns and regular expressions compile, `pattern` examples pass | yes | yes | `tags` |
| usernames added in this version exist and are active (one GraphQL request) | yes | `except.users` | no |
| signing keys | no | no | yes |
| at least one effective admin | `lockout_prevented` | no | no |
| the API is unavailable during the checks | `authorization_backend_failure` | `authorization_backend_failure` | no |
| a delegated file is not covered by a PATHPOLICY rule | warning | warning | warning |

Usernames that were already in the anchor version are not checked again. `settings-check` finds principals that are gone ([operations.en.md](operations.en.md)).

Deleting a policy file is not a new version but a return to the state without the policy:

| What is deleted | Who may | After the merge into the default branch |
| --- | --- | --- |
| PATHPOLICY | an admin, in recovery an Owner | no rules; the next PATHPOLICY is added by a Maintainer+ |
| COMMITPOLICY, TAGPOLICY | whoever the anchor PATHPOLICY rule allows; without a rule, anyone with push access | no checks of commit format or tag signatures |

If PATHPOLICY is deleted while COMMITPOLICY or TAGPOLICY stay, anyone with push access can change them. The hook warns about this on acceptance. A directory `PATHPOLICY/` in place of the file is a policy change too (a deletion and an invalid new version).

## Denials

| Code | Title in the output | When |
| --- | --- | --- |
| `protected_path_violation` | modification denied | a path from a rule is changed by someone not in `allow` |
| `policy_admin_violation` | policy change denied | PATHPOLICY is changed by someone who is not an admin |
| `lockout_prevented` | new policy has no effective administrators | the new version has no effective admin |
| `invalid_policy` | policy is invalid | PATHPOLICY in the default branch is invalid |
| `invalid_policy_update` | new policy version is invalid | a new version of a policy file failed acceptance |
| `authorization_backend_failure` | unable to verify authorization | the GitLab API did not answer, the token expired, the service account does not see the project |
| `unknown_principal` | principal type not allowed | a deploy key or an unparsable `GL_ID` on a protected path |
| `limit_exceeded`, `internal_error` | push could not be verified | a limit of time, API calls or commits; an internal error |

Under uncertainty PATHPOLICY fails closed: if the answer cannot be obtained, a push that touches protected paths is denied. A push that touches no rule does not depend on the API.

A denial in the terminal:

```text
remote: gitlab-repopolicy: push rejected (PATHPOLICY: 1 violation)
remote:
remote: PATHPOLICY [ci-config] refs/heads/feature: modification denied
remote:   The platform team changes the CI/CD configuration and the deploy logic.
remote:   Propose the change in a merge request.
remote:   files:
remote:     .gitlab-ci.yml
remote:
remote: user: bob (id 3)
remote: GL-HOOK-ERR: gitlab-repopolicy: PATHPOLICY [ci-config] modification denied. See push output.
```

In a merge request, the Web IDE and API responses GitLab shows only the `GL-HOOK-ERR` line. API results (a role, "blocked") never get into the message, only into audit.

## Checking without a push

| Command | What it does |
| --- | --- |
| `gitlab-repopolicy validate -f PATHPOLICY` | the same checks as on acceptance, without GitLab |
| `gitlab-repopolicy validate -f PATHPOLICY --online --config FILE --project P` | also checks through the API that usernames exist and the role of admins in the project |
| `gitlab-repopolicy explain --project P --user U --path X [--ref R]` | which rules match the path and whether the user satisfies them |
| `gitlab-repopolicy admins --project P` | admins from the file, effective admins and whether recovery is on |

`explain` and `admins` read PATHPOLICY from the default branch of the project through the API, that is the version the next push will be evaluated by.

A `validate` error:

```text
$ gitlab-repopolicy validate -f PATHPOLICY
PATHPOLICY: invalid
  rule ci: paths: "/ci/**": patterns are relative to the repository root, remove the leading /
```

## What PATHPOLICY does not protect

The hook sees only changes of branches and tags. Project settings (the CI configuration path, protected branches, the default branch, job token push) change in Settings, and the hook does not see them. `settings-check` compares them ([operations.en.md](operations.en.md)); the full list of limitations is in [threat-model.en.md](threat-model.en.md).

The hook does not check projects in personal namespaces: they do not deploy and do not hold team code, and changes from them reach group projects through a merge, which is checked.
