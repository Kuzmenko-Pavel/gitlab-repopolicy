[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](threat-model.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](threat-model.en.md)

# Threat model

gitlab-repopolicy protects the content of protected paths, the format of history and release tags from everyone who can push but does not manage the project. Project settings are out of its reach. This is the main residual risk, and only detection (`settings-check`) compensates for it.

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

## Bypasses through project settings

The hook is called only when a branch or a tag changes. Changes in Settings are writes to the GitLab database, and the hook does not see them. Every bypass needs the Maintainer role:

| Maintainer action | With `refs: all` | With `refs: protected` | Compensation |
| --- | --- | --- | --- |
| "CI/CD configuration file" to an unprotected path, another project or a URL | **bypass** | **bypass** | `settings-check` |
| push a changed CI to an ordinary branch, then protect the branch | closed | **bypass** | `refs: all` by default |
| make a branch without policy files or with old versions the default | closed if `default_branch` is set | closed if `default_branch` is set | the hook denies every push to a project whose default branch is not in `default_branch`; effects without a push (a manual pipeline run, `CI_DEFAULT_BRANCH`) are detected by `settings-check` |
| turn on push with `CI_JOB_TOKEN` | **bypass**: the job pushes as the user who started the pipeline | **bypass** | `settings-check` |
| pipeline variables to change the behaviour of a job | out of scope | out of scope | a minimal role for pipeline variables |

If `default_branch` has several names, for example `[main, master]` during a migration, switching the default between them is an accepted risk: the installation named both branches allowed itself.

`settings-check` is detection on a schedule (once a day by default), not prevention: it does not see a change that was reverted between two runs. Only an organizational measure closes this group of bypasses fully: the Maintainer role only for the owners of protected paths. Where this is not possible, `settings-check` is required.

## What is protected

- **A change of protected paths** in any way: a push over HTTPS and SSH, the merge button, auto-merge, the Web IDE, the API, revert and cherry-pick in the UI, a push from CI. The hook sees the identity of whoever makes the change, not the commit author
- **Weakening the rules in the same push.** A policy is read from the default branch before the push; a new version of a policy cannot be used for itself
- **A branch with its own weaker policy.** One anchor for the whole push
- **A forged author.** Author, committer and commit signatures are not used for authorization
- **A deploy key created by an admin.** Not a person, denied on a protected path
- **Client push options** do not affect the decision
- **`refs/replace/*`.** Child git processes ignore replace refs, so a replaced object cannot hide a change
- **Reverting a file with a force push** in default and protected branches is a change too, and it is checked
- **Evil merge.** A path changed in the merge commit itself shows as a change
- **A signed staging tag under a prod tag name.** The name inside the tag object is checked against the name it is pushed under
- **Uncertainty.** An API failure, an expired token, a service account without access, an invalid policy, an internal error deny a protected change. An API failure never turns recovery on
- **DoS through input.** Limits on size, number of rules, time and API calls; regular expressions in TAGPOLICY and in COMMITPOLICY references are RE2 with linear time, `pattern` runs with a timeout

## Out of scope

- Projects and forks in personal namespaces are not checked. Changes from them reach group projects through a merge, which is checked
- Import and export, restore from a backup, project transfer are operations of an Owner and an administrator
- Between reading the anchor and updating the branch there are milliseconds. For the default branch git itself rejects an outdated state
- The behaviour of the code that the protected CI builds and deploys: gitlab-repopolicy protects the pipeline, not the application
- A rename or deletion of a user frees the username. If another person takes it, they get the rights from the policy until the policy is fixed. `settings-check` reports principals that no longer exist
- A new branch or tag on an old commit that is already in a protected branch is not checked by PATHPOLICY and COMMITPOLICY. TAGPOLICY checks the signature of such a tag
- A merge into an unprotected branch brings no new commits: PATHPOLICY does not check it, COMMITPOLICY checks only the merge commit, not the merged commits that are already in the repository
- A compromised private signing key: until the merge of the TAGPOLICY change that removes the key, it can sign a release. An SSH key also has no expiry: its lifetime and a separate key for signing are the responsibility of whoever adds the key to TAGPOLICY
- The service account token reads the code, metadata and values of CI/CD variables of every group. On one Linux package node this is no more than the `git` user, which runs Rails and Gitaly, already has. With separate Gitaly nodes it is an accepted risk: protect the token file and the nodes
- The hook does not protect the GitHub repository of the project itself: here repository rules and artifact signatures keep releases intact ([release.en.md](release.en.md))

## Reporting vulnerabilities

[SECURITY.en.md](../SECURITY.en.md).
