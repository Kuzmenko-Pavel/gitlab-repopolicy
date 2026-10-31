[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](threat-model.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](threat-model.en.md)

# Threat model

gitlab-repopolicy protects the content of protected paths, the format of history and release tags in project repositories from those who can make changes to them (push, merge, Web IDE, API) but do not manage the project. Wiki, design and snippet repositories, projects in a personal namespace and the whole instance with `mode: off` are not checked. Project settings are out of the reach of the hook. This is the main residual risk, and only detection (`settings-check`) compensates for it.

The security of the tool does not depend on the secrecy of its code: every limitation is described here in public.

## Trust boundary

The hook runs only for changes of branches and tags and only after the regular GitLab permission check. It can deny, but it cannot grant rights.

Outside the trust boundary are those who change GitLab or Gitaly around push acceptance:
- the Owner of a project and namespace: recovery by design, transfer, deletion, import
- a GitLab administrator: impersonation, membership management
- root on a Gitaly node and the `git` user: writes to the repository around hooks

A Maintainer is partly inside: their git changes are limited, project settings are not.

## Actors

| Actor | PATHPOLICY | COMMITPOLICY | TAGPOLICY | Comment |
| --- | --- | --- | --- | --- |
| Developer | protects | protects | protects | neither a push nor a merge gets around it; a release tag only with an allowed signature |
| Maintainer | partly | protects | partly | git changes are limited, project settings, including the default branch, are not (below) |
| owner of a delegated policy | by delegation | owns the rules | owns the rules | changes and deletes COMMITPOLICY and TAGPOLICY, but not PATHPOLICY |
| PATHPOLICY admin | by design | through PATHPOLICY | through PATHPOLICY | every change is in audit |
| project or group Owner | no | no | no | recovery by design; transfer, deletion |
| compromised account without the right | protects | protects | protects | limited by its role |
| compromised account with the right | no | no | protects | has the right by definition; a release tag also needs the private signing key. 2FA, SSO and audit help |
| CI (`CI_JOB_TOKEN`) | protects | protects | protects | a job pushes as the user who started the pipeline and is checked as that user |
| deploy key | protects | protects | protects | a deploy key is not a person, even if an admin created it; for TAGPOLICY the signature decides |
| project or group access token, service account | protects | protects | protects | the right only by an explicit username in the policy |
| GitLab administrator | no | no | no | impersonation, membership |
| root on a Gitaly node, the `git` user | no | no | no | writes around hooks |

BRANCHPOLICY is not in the table because it does not tell actors apart. The rules are the same for everyone who can create a branch. It limits which branches can be created: `deny` forbids a class, and a name that falls into no class is not checked. A rule with `match: all` and `deny: true` forbids everything that is not described. It enforces naming agreements rather than a user authorization boundary. On an API failure the `user_prefix` and `protected` checks are skipped with a warning, and existing branches are not checked.

## Bypasses through project settings

The hook is called only when a branch or a tag changes. Changes in Settings are writes to the GitLab database, and the hook does not see them. Every bypass needs the Maintainer role:

| Maintainer action | Result | Compensation |
| --- | --- | --- |
| "CI/CD configuration file" to an unprotected path, another project or a URL | **bypass** | `settings-check` |
| make a branch without policy files or with old versions the default | closed if `default_branch` is set | the hook denies every push to a project whose default branch is not in `default_branch`; effects without a push (a manual pipeline run, `CI_DEFAULT_BRANCH`) are detected by `settings-check` |
| turn on push with `CI_JOB_TOKEN` | **bypass**: the job pushes as the user who started the pipeline | `settings-check` |
| remove the protection of a release branch | the content of the branch does not change, and a rule with a `match` list applies by the name of the branch, not by the protection in GitLab | `settings-check` for BRANCHPOLICY with `protected: true` |
| pipeline variables to change the behaviour of a job | out of scope | a minimal role for pipeline variables |

If `default_branch` has several names, for example `[main, master]` during a migration, switching the default between them is an accepted risk: the installation named both branches allowed itself.

`settings-check` is detection on a schedule (once a day by default), not prevention: it does not see a change that was reverted between two runs. Only an organizational measure closes this group of bypasses fully: the Maintainer role only for the owners of protected paths. Where this is not possible, `settings-check` is required.

## What is protected

- **A change of protected paths in any way.** bob changes `.gitlab-ci.yml` with the Edit button of the UI, through the API, with a revert or a cherry-pick in the UI or with a push from CI. The hook sees whoever makes the change, not the commit author, and denies the same way as on `git push`
- **Intermediate commits.** bob pushes to `main` a commit with a changed `.gitlab-ci.yml` and the next commit that reverts it. The resulting file is the same, but the pipeline of the first commit would run the change, so in the default branch, protected branches and branches of a `match` list every commit that enters the branch is checked
- **Rewritten history in a shared branch (B+).** bob rebases a branch with a commit of alice and pushes their own new content of `ci.yml` in a rewritten commit. The hook lets through only content that this branch or the default branch already had, so new content cannot hide among rewritten commits
- **Content from another branch.** bob creates a branch with their own `ci.yml` and then brings this content into the shared branch with a rebase. The allowed versions of the file come only from the branch itself and the default branch, not from other branches and tags, so another branch does not make content "known"
- **A tag outside of branches.** bob puts a tag on a local commit with a changed `.gitlab-ci.yml` and pushes only the tag. The pipeline of the tag would run an unchecked commit, so a tag is accepted only when a branch holds its commit after the push
- **Weakening the rules in the same push.** bob removes their restriction from PATHPOLICY and changes a protected file in the same push. The rules are read from the default branch before the push, so the new version applies only from the next push
- **A branch with its own weaker policy.** bob pushes a branch where PATHPOLICY allows them everything. The rules for all branches and tags come from the default branch before the push
- **A forged author.** bob writes a commit with `--author "alice <alice@example.com>"`. Author, committer and commit signatures are not used for the decision
- **A deploy key created by an admin.** A push with a deploy key to a protected path is denied: a deploy key is not a person
- **Client push options** (`git push -o ...`) do not affect the decision
- **`refs/replace/*`.** bob pushes a replace ref so that a changed commit looks innocent. The hook ignores replace refs and sees the real object
- **Reverting a file with a force push.** bob force-pushes `main` back to a commit with an old `ci.yml`. The difference of the content of the branch before and after the push is a change too
- **Evil merge.** bob changes `ci.yml` in the merge commit itself while resolving a conflict. A path changed in the merge commit shows as a change
- **A signed staging tag under a prod tag name.** bob takes a signed staging tag and pushes it as `v1.4.0-prod`. The name inside the tag object is checked against the name it is pushed under
- **Uncertainty.** The GitLab API did not answer when the hook needs to know who bob is. An API failure, an expired token, a service account without access, a broken file or preset, an internal error deny a protected change. An API failure never turns recovery on
- **DoS through input.** bob writes a commit message on which a regular expression would run for years. All regular expressions are RE2 with linear time, plus limits on size, the number of rules and commits, time and API calls

## Out of scope

- Projects and forks in personal namespaces are not checked. Changes from them reach group projects through a merge, which is checked
- Import and export, restore from a backup, project transfer are operations of an Owner and an administrator
- Two simultaneous pushes. Each invocation reads its own anchor, without locking the whole repository for the check. Git compares the old value of the ref being updated, but this does not guarantee a current default branch for a concurrent push to another branch. Do not rely on policy changes and all other pushes being serialized
- The behaviour of the code that the protected CI builds and deploys: gitlab-repopolicy protects the pipeline, not the application
- A rename or deletion of a user frees the username. If another person takes it, they get the rights from the policy until the policy is fixed. `settings-check` reports principals that no longer exist
- PATHPOLICY and COMMITPOLICY do not check tags, so the core separately requires their commits to be reachable from branches. This does not prove old history passed current rules: it can predate them. A new branch on an existing commit is checked according to its class and `match`; TAGPOLICY independently checks signatures of matching tags
- A merge into an ordinary branch under `match: all` brings only content the repository already has: PATHPOLICY checks only new content, COMMITPOLICY only the merge commit. The change is checked when the branch is merged into `main`
- A compromised private signing key: until the merge of the TAGPOLICY change that removes the key, it can sign a release. An SSH key also has no expiry: its lifetime and a separate key for signing are the responsibility of whoever adds the key to TAGPOLICY
- The service account token reads the code, metadata and values of CI/CD variables of every group. This is a GitLab property of the Maintainer role and the `read_api` scope, the hook does not use it. On one Linux package node this is no more than the `git` user, which runs Rails and Gitaly, already has. With separate Gitaly nodes it is an accepted risk: protect the token file and the nodes
- The hook does not protect the GitHub repository of the project itself: here repository rules and artifact signatures keep releases intact ([release.en.md](../../development/release.en.md))

## Reporting vulnerabilities

If you find a bypass outside these documented limitations, report it privately following [SECURITY](../../../SECURITY.en.md). The [check architecture](../../development/architecture.en.md#how-changes-are-checked) explains where each boundary applies.
