[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](architecture.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](architecture.en.md)

# Architecture

## Overview

gitlab-repopolicy is a Go binary for the global `pre-receive` server hook of Gitaly. On every push it applies rules that live in the repository itself:

| File | What it checks |
| --- | --- |
| `PATHPOLICY` | who may change certain files: CI configuration, deploy and infra configuration, other sensitive paths |
| `COMMITPOLICY` | the format of commit messages in branches |
| `TAGPOLICY` | who may release: a tag under a rule is accepted only with a signature of an allowed OpenPGP or SSH key |

GitLab Self-Managed has no mechanism that limits changes of specific files to specific people on every way a change can arrive (push, merge, Web IDE, API), and no structural server-side check of commit format. Protected branches work at the level of a branch, Code Owners only in a merge request and are bypassed by a direct push, push rules exist only in Premium and do not know who makes the change. Client-side tools (commitlint, pre-commit) are bypassed with `--no-verify` and do not see merges and the Web IDE.

Principles:
- **hard enforcement on the server**: a rule cannot be bypassed by a local setting, another client or the merge button
- **rules in the repository**: policy files are versioned and reviewed like code
- **GitLab AND gitlab-repopolicy**: the hook can only deny, it grants no new rights
- **different owners for different rules**: PATHPOLICY delegates the other policy files to their owners
- **a fast path without network**: a push without a policy or without touched rules does not call the API
- **no permanent lockout**: losing all admins is fixed by an explicit, audited recovery
- **clear messages**: a denial says what is broken and how to fix it

## System context

```text
 git push, merge, Web IDE, API
              |
              v
     GitLab Rails / Workhorse ----- /internal/allowed: regular GitLab permissions
              |
              v
           Gitaly --- custom_hooks_dir/pre-receive.d/50-gitlab-repopolicy
              |                      |
              |                      v
              |            gitlab-repopolicy hook pre-receive
              |              |            |              |
              |      git (Gitaly)   REST/GraphQL API   audit.log
              |              |      (only when needed)
              v              v
       repository and quarantine objects of the push
```

Gitaly runs the hook only after the regular GitLab permission check. The hook gets the branch and tag updates on stdin and the identity of whoever makes the change in the `GL_*` variables, and reads the repository with the same git as Gitaly. It talks to the outside only through the GitLab API and only when the decision depends on the role of a user, the type of an SSH key or whether a branch is protected.

## Responsibilities

In scope:
- an allow or deny decision for every push by the policy files from the default branch
- acceptance of new versions of policy files in any branch
- one aggregated message per push and an audit event per decision
- helper commands: installing the hook, self-test, token rotation, comparison of project settings, emergency bypass

Out of scope:
- project settings (the CI configuration path, protected branches): the hook does not see them, `settings-check` only detects changes. The hook compares the name of the default branch with `default_branch` on every push
- the quality of changes: the project checks who and in which format, not what
- history from before a policy appeared
- projects in personal namespaces
- GitLab.com and GitLab Dedicated

## Main components

```text
cmd/gitlab-repopolicy     main: signals, exit code
internal/cli              subcommands, assembly of config, clients and modules
internal/config           server config
internal/core             anchor, policy reading, acceptance, module order, budgets, aggregation
internal/module           module contract: Module, Policy, Input, Result
internal/modules          the module list of the hook
internal/modules/path     PATHPOLICY
internal/modules/commit   COMMITPOLICY
internal/modules/tag      TAGPOLICY
internal/changes          which paths and commits a push changes
internal/gitx             child git processes in an isolated environment
internal/hookenv          the environment and stdin that Gitaly passes to the hook
internal/refs             default, protected or another branch
internal/identity         who makes the change, SSH key type, namespace type
internal/gitlab           REST and GraphQL client, call budget
internal/report           messages to the user, GL-HOOK-ERR
internal/audit            JSON events
internal/strictyaml       strict YAML parsing: no anchors, unknown keys or duplicates
internal/spec, schema     the format of policy files, its validator and JSON Schema
internal/buildinfo        version and commit of the build
internal/install, settings, coverage, token, breakglass   helper commands
internal/testmodule       a test module for checks of the core, only in tests
```

The core does not know what a module checks, a module does not know how changes and identity are obtained. A module declares its file, its behaviour on an invalid policy (fail closed or skip) and the data it needs; the core computes only that. The linter (`depguard`, `forbidigo`) in `make code-lint` checks the dependency rules between packages.

A new kind of rules is a new module and a new file; the core, the trust model and operations do not change. The module file automatically becomes a protected path, and a PATHPOLICY path rule sets its owner.

## External dependencies

| Dependency | What for | When |
| --- | --- | --- |
| the git that Gitaly passes in the `PATH` of the hook | reading policies from the anchor, paths, commits, tag objects | on every push to a project |
| GitLab REST API v4 | the role of a user in the project, personal or deploy SSH key, protected branches and tags, project visibility for the service account | only when a rule is touched, on policy acceptance and on bootstrap. TAGPOLICY - only on the creation of a tag under a rule with `protected: true` |
| GitLab GraphQL | whether usernames added to a policy exist and are active | acceptance of a new version, counting admins, `settings-check` |

The API is reached through the Workhorse unix socket on the same node or over HTTPS if Gitaly runs on a separate node. One request takes up to 3 s, a push up to 30 calls. There is no cache between calls: revoked rights apply at once.

The Go dependencies of the binary are `go.yaml.in/yaml/v3`, `github.com/bmatcuk/doublestar/v4`, `github.com/dlclark/regexp2/v2` (only `pattern` in COMMITPOLICY), `github.com/ProtonMail/go-crypto` and `golang.org/x/crypto` (only tag signatures). The full list of versions is `go.mod` and the SBOM of the release. The build is static, `CGO_ENABLED=0`.

## Data flow

One push:

1. **Entry.** The repository is not a project (wiki, design, snippet) - allowed without a log. `mode: off` - allowed
2. **Anchor.** The default branch before the push. Every policy file is read only from here: a push cannot weaken the rules for itself, and a branch with a weaker policy does not authorize itself
3. **Break-glass.** A valid grant for the project - allowed with a `breakglass` event
4. **Default branch.** If `default_branch` is set in the config and the default branch of the project has another name, the push is denied with `default_branch_not_allowed`. The modules do not run: policies are not read from such a branch. The check applies to every project of the instance, including those without policies
5. **Acceptance.** Every changed policy file in any branch is checked as a new version, even if the anchor has no policy yet: an invalid new file is denied with `invalid_policy_update`
6. **No policy.** If the anchor has no policy files, the modules do not run. A push that adds the first PATHPOLICY to the default branch goes through the bootstrap check (Maintainer+)
7. **Modules.** PATHPOLICY first, then COMMITPOLICY and TAGPOLICY. Only for branches and tags that touch rules: paths, new and incoming commits. In default and protected branches - the content difference before and after the push, in others - the new commits that no branch or tag has yet (details - [pathpolicy.en.md](pathpolicy.en.md#where-a-rule-applies-refs)). The API only if the decision depends on it
8. **Decision.** AND over modules; one message per push on stderr, one problem is one block; an audit event per decision with the `receive_id` of the run. Policy change events (`policy_change`, `policy_bootstrap`, `policy_recovery`) are written only if the push is accepted

A typical run takes about 10 ms for a project without a policy and 15-50 ms when there is a policy but no rule is touched: most of it is starting git processes. A call to the API adds 50-300 ms and is rare.

Limits that protect against DoS:

| Resource | Limit | What happens |
| --- | --- | --- |
| policy file size | 64 KiB, TAGPOLICY - 256 KiB | the policy is invalid |
| path rules / patterns per rule | 100 / 50 | the policy is invalid |
| time of one hook run | 10 s | `limit_exceeded` |
| API calls per push | 30 | `limit_exceeded` |
| new commits (PATHPOLICY) | 50 000 | `limit_exceeded` |
| incoming commits (COMMITPOLICY) | 10 000 | `limit_exceeded` by the `mode` of the file |
| `pattern` per message / per push | 100 ms / 2 s | `commit_check_timeout` |
| output to the user | 8 KiB | truncated |

## Deployment model

- the binary and the config on every Gitaly node; the hook is connected with `install-hook` into `custom_hooks_dir`
- the deb or rpm package puts files only into its own directories and does not change GitLab
- a separate process for every push, no daemon and no state between runs
- the hook account is an instance service account with the Maintainer role in every top-level group and a `read_api` + `self_rotate` token

Step by step - [install.en.md](install.en.md), operations - [operations.en.md](operations.en.md).

## Security considerations

- **Trust model.** A policy is read only from the default branch before the push. PATHPOLICY protects itself (`admins`) and, through path rules, the other policy files
- **Identity.** Only from `GL_ID` and `GL_USERNAME`, which GitLab sets. Author, committer and commit signatures are not used for authorization. A deploy key is not a person. Client push options do not affect the decision
- **Deny under uncertainty.** PATHPOLICY and TAGPOLICY fail closed: an API failure, an invalid policy, an internal error deny a protected change. For TAGPOLICY this concerns the signature: the `protected` check gives a warning on an API failure, because it is hygiene, not access. COMMITPOLICY skips the check with a warning on a failure: it is hygiene, not access. The exception is the acceptance of a new COMMITPOLICY version with `except.users`: if the API did not answer, the push is denied with `authorization_backend_failure`, because it is unknown whether these users exist. An API failure never turns recovery on
- **Git isolation.** Child git processes run without replace refs and without system and global config: `refs/replace/*` cannot hide a change from the check
- **Untrusted input.** Policy files, stdin, commit messages and tag objects are parsed strictly and with limits; the parsers are covered by fuzz tests
- **Confidential messages.** The user sees rules from the policy file (they are in the repository anyway), but not API results: roles, account state, error details. These go only to audit
- **Minimal rights.** The service account can only read and rotate its own token, it has no administrator rights

Actors, bypasses and what is out of scope are described in [threat-model.en.md](threat-model.en.md).

## Known limitations

- The hook sees only changes of branches and tags. A Maintainer changes project settings (the CI configuration path, the default branch, protected branches, push with `CI_JOB_TOKEN`) around it; `settings-check` only detects them on its schedule. The exception is the default branch: if `default_branch` is set, a push to a project with another default branch is denied at once
- A merge into an unprotected branch brings no new commits. PATHPOLICY does not check it, COMMITPOLICY checks only the merge commit itself (unless `except.merge_commits` is on), not the merged commits that are already in the repository. A merge into the default branch is checked fully by both modules, into protected branches - by PATHPOLICY by content and by COMMITPOLICY by the `refs` of the ruleset ([commitpolicy.en.md](commitpolicy.en.md#which-commits-are-checked))
- A new branch or tag on an old commit that is already in a protected branch is not checked by PATHPOLICY and COMMITPOLICY: the content was checked before
- A username can become free after a rename or deletion of a user and be taken by another person. `settings-check` reports principals that no longer exist
- The service account token does not see blocked or banned users. A personal project of such a user is treated as a group project (fail closed)
- Web operations (the merge button) hold a Puma worker while the hook calls back into Rails. Under peak load on one node this can give `authorization_backend_failure` by timeout
- The Helm chart (Kubernetes) is not supported
