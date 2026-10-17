[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](branchpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](branchpolicy.en.md)

# BRANCHPOLICY

BRANCHPOLICY checks the name of every new branch and that branches of some classes are protected in GitLab. The file lies in the root of the repository with the name `BRANCHPOLICY`, its content is YAML.

Why. Branches `release/*` or `epic/*` deploy, and the pipeline of an unprotected branch does not see protected CI/CD variables and protected runners. The branch is created, CI deploys nothing, and people search for the reason for a long time. The hook catches this at once, when the branch is created, and says what to set up. The second goal is a branch name standard: an agreement of the team becomes a rule on the server, not an oral tradition.

It is hygiene, not access. So BRANCHPOLICY skips the check with a warning on a failure (fail open), like COMMITPOLICY. Who changes BRANCHPOLICY itself is set by a PATHPOLICY path rule ([pathpolicy.en.md](pathpolicy.en.md)). Without such a rule anyone who can push changes the file.

## Example

```yaml
# A branch standard: release and hotfix branches are release/vX.Y.Z and
# hotfix/vX.Y.Z, epic and demo branches are created by any developer, and all
# four are protected; work branches are <prefix>/[<issue>/]<name>. A name
# that fits no class is denied. In an installation this list usually lives in
# branchpolicy_presets of the server config, and a project only extends it.
version: 1

rules:
  - name: release
    match: "release/*"
    pattern: '^release/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$'
    examples:
      valid: [release/v1.4.0, release/v1.4.0-staging]
      invalid: [release/foo, release/v1.2, release/v01.2.3]
    protected: true
    message: Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
  - name: hotfix
    match: "hotfix/*"
    pattern: '^hotfix/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[a-z][a-z0-9]*)?$'
    examples:
      valid: [hotfix/v1.4.1]
      invalid: [hotfix/foo, hotfix/v1.4]
    protected: true
  - name: epic
    match: "epic/*"
    pattern: '^epic/[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$'
    examples:
      valid: [epic/payments]
      invalid: [epic/, epic/Payments]
    protected: true
  - name: demo
    match: "demo/*"
    pattern: '^demo/[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*$'
    protected: true
  - name: work
    match: "*"
    pattern: '^[a-z0-9][a-z0-9._-]*/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)(/[a-z0-9][a-z0-9._-]*)*|[a-z0-9][a-z0-9._-]*(/[a-z0-9][a-z0-9._-]*)*)$'
    examples:
      valid: [kpn/disable_auth, gks/INFRA-1719/change_logic, dmitry.mashoshin/123, dmitry.mashoshin/355/change_proce/auth]
      invalid: [kpn, kpn/, 123-fix-login, Kuzmenko.Pavel-main-patch-48213, revert-1a2b3c4d]
    message: Work branches are <prefix>/[<issue>/]<name>, for example kpn/disable_auth.
```

How to read it:
- a new branch falls into a class by the first rule whose `match` fits the name. `release/foo` falls into the class `release` and is denied there, because it does not match `pattern`: it does not move on to the class `work`
- `kpn/disable_auth` and `gks/INFRA-1719/change_logic` pass the rule `work`, `123-fix-login` does not
- `release/v1.4.0` passes only if the project protects `release/*` (Settings > Repository > Protected branches)
- a name that fits no class is denied. There is no such name here: `work` with `match: "*"` catches everything

## Presets (`extends`)

The standard of an installation is best kept in the server config, and projects take it with one line:

```yaml
# /etc/gitlab-repopolicy/config.yaml
branchpolicy_presets:
  org-branches:
    rules:
      - name: release
        match: "release/*"
        pattern: '^release/v[0-9]+\.[0-9]+\.[0-9]+$'
        protected: true
      - name: work
        match: "*"
        pattern: '^[a-z0-9.]+/[a-z0-9._/-]+$'
```

```yaml
# BRANCHPOLICY of a project
version: 1
extends: org-branches
```

How the list of rules is built:
1. First the new rules of the project, whose names are not in the preset, in the order of the file
2. Then the rules of the preset in its order. A rule of the project with the `name` of a preset rule takes the place of the preset rule

So a project adds its own classes before the standard and overrides the catch-all of the standard, which stays last. For example, a project on `main` where `master` may not be created:

```yaml
version: 1
extends: org-branches
rules:
  - name: no-master
    match: "master"
    deny: true
    message: This project uses main as the default branch; master must not be created.
```

A strict project: only the classes of the standard, everything else is forbidden. The rule named `work` replaces the catch-all of the preset:

```yaml
version: 1
extends: org-branches
rules:
  - name: work
    match: "*"
    deny: true
    message: In this project only release/* branches are created.
```

Migration from `master` to `main`: the second main branch is allowed explicitly, the rule is removed after the migration:

```yaml
version: 1
extends: org-branches
rules:
  - name: migration-main
    match: "main"
    protected: true
```

A project without a preset writes all rules itself. Without a catch-all everything that is not described is forbidden.

### Forbid everything that is not described

There is no rule `match: "*"`, so a name without a class is denied with `branch_name_violation`. The main branch `main` is the default branch, the core checks its name by `default_branch`, so it needs no rule of its own:

```yaml
version: 1
rules:
  - name: no-master
    match: "master"
    deny: true
    message: This project uses main; master must not be created.
  - name: release
    match: "release/*"
    pattern: '^release/v[0-9]+\.[0-9]+\.[0-9]+(-[a-z][a-z0-9]*)?$'
    protected: true
  - name: hotfix
    match: "hotfix/*"
    pattern: '^hotfix/v[0-9]+\.[0-9]+\.[0-9]+(-[a-z][a-z0-9]*)?$'
    protected: true
  - name: user
    match: "*/*"
    user_prefix: true
```

| New branch | Result |
| --- | --- |
| `master` | denied: rule `no-master` |
| `release/v1.4.0` | passes if the project protects `release/*` |
| `release/foo` | denied: class `release`, the name does not match `pattern`. It does not move on to the class `user` |
| `jdoe/disable_auth`, pushed by `jdoe` | passes: the prefix is the username of the pusher |
| `fix-login`, `tmp` | denied: no `match` fits |

## File structure

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | `1` |
| `extends` | no | the name of a preset from `branchpolicy_presets` of the server config ([configuration.en.md](configuration.en.md#branchpolicy_presets)) |
| `mode` | no | `enforce` (default) or `warn`. `warn` turns every denial into a warning |
| `rules` | no | rules, up to 50 together with the rules of the preset |

Fields of a rule:

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | yes | `[a-z0-9-]{1,63}`, unique in the file. The name of a preset rule means a replacement |
| `match` | yes | the class of a branch, a pattern as in GitLab protected branches: `*` matches any characters, including `/`. Up to 255 bytes |
| `deny` | no | `true` - branches of this class may not be created. Together with `pattern`, `user_prefix`, `protected` or `examples` it is an error |
| `pattern` | no | RE2 regex that the whole branch name must match. Without `pattern` any name in the class is allowed. Up to 256 bytes |
| `user_prefix` | no | `true` - the part of the name before the first `/` is the username of an active GitLab user |
| `protected` | no | `true` - a new branch of this class must be a protected branch of the project in GitLab |
| `examples` | no | only with `pattern`: `{valid: [...], invalid: [...]}`. A self-check of the regex on acceptance of the file and in `validate`, not printed in a denial |
| `message` | no | the text of a denial, up to 1000 characters. It holds an example name for a person and, for `protected`, which roles to set in Protected branches |

Unknown keys, YAML anchors and aliases are an error. The file size is up to 64 KiB.

Checks of the list of rules together with the preset, on acceptance of a new version and in `validate`:
- there are no rules after the rule with `match: "*"`: they are never reached
- `match` is not repeated
- every `examples.valid` passes the `pattern` of its rule and falls into this very rule. This shows shadowing: for example, a project added `match: "*/*"` before the preset, and `release/v1.4.0` went to another rule
- every `examples.invalid` does not pass the `pattern` of its rule

Without `examples` only the first two checks catch shadowing.

## What is checked

Only the creation of a branch (`refs/heads/*`). Updates and deletions of existing branches are not checked: they stay as they are, even with a name outside the standard. The default branch is not checked: the core checks its name by `default_branch` ([configuration.en.md](configuration.en.md#default_branch)). Tags are not checked: the protection of release tags is the field `protected` in TAGPOLICY ([tagpolicy.en.md](tagpolicy.en.md)).

For a new branch, in order:
1. The class is the first rule whose `match` fits the name. No such rule - denied with `branch_name_violation`
2. `deny: true` - denied with `branch_name_violation`
3. `pattern` does not match - denied with `branch_name_violation`
4. `user_prefix: true` - the part before the first `/` must be a username. If a person creates the branch with their own username as the prefix, no API is needed: GitLab has just authenticated them. Otherwise the hook asks GitLab once whether there is an active user with such a name, ignoring case. If not - denied with `branch_name_violation`. So a branch can be created for a colleague: `olga/fix` by `alice` passes if `olga` exists
5. `protected: true` - the hook asks GitLab whether the project protects this branch. If not - denied with `branch_not_protected`

The name check is local, without the API. The API is needed only by steps 4 and 5 and only for rules with these fields.

## GitLab UI

A branch created in the GitLab UI or API (the New branch button, a branch from an issue, a Web IDE commit to a new branch, revert and cherry-pick with a new MR) is checked the same way. The names GitLab suggests (`123-title`, `user-main-patch-48213`, `revert-1a2b3c4d`) usually do not follow a standard. Rename the branch in the dialog or create a branch by the standard beforehand and choose it as the target.

## Modes and denials

| Situation | Result |
| --- | --- |
| `mode: enforce`, a violation | denied with the code of the violation |
| `mode: warn`, a violation | the push passes, a warning in the output and in audit |
| the API did not answer during `user_prefix` or `protected` | the check is skipped, a warning `authorization_backend_failure` or `limit_exceeded` |
| the preset of `extends` is not in the server config, or the list of rules fails its checks | the module is skipped with a warning, only for pushes that create a branch |
| BRANCHPOLICY in the default branch is invalid | the module is skipped with a warning, only for pushes that create a branch; audit `invalid_policy` with `decision: warn` |
| a push changes BRANCHPOLICY to an invalid version | denied with `invalid_policy_update` |
| a personal namespace | not checked |

The audit event `decision` is written only for a violation or a warning, otherwise every branch creation in hundreds of projects would leave a record.

| Code | Title in the output | When |
| --- | --- | --- |
| `branch_name_violation` | the name does not follow the branch standard, creating this branch is forbidden, no rule of BRANCHPOLICY allows this name | the name does not fit the class, the class is forbidden or there is no class |
| `branch_not_protected` | the branch must be protected, but the project does not protect it | a new branch of a class with `protected: true`, and the project does not protect it |

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: BRANCHPOLICY rule "release", branch release/foo: the name does not follow the branch standard
remote:   Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
remote:   how to fix: rename the branch locally (git branch -m) and push again
remote:
remote: rules: BRANCHPOLICY in main (9f1c2ab)
remote: checked as: alice (HTTPS)
remote: GL-HOOK-ERR: gitlab-repopolicy: branch release/foo breaks BRANCHPOLICY rule release; rename it (git branch -m) and push again
```

The raw regex is not shown to the user: a person sees the name format from `message`. Without `message` the denial still names the rule, the branch and the reason.

## How to roll out

1. A preset in the server config; BRANCHPOLICY with `extends` and `mode: warn` in the projects; a PATHPOLICY path rule on `BRANCHPOLICY`
2. A week or two of warnings: look at the `decision` events of the module `branchpolicy` in audit
3. `mode: enforce`

Pitfalls:
- `git push --mirror` of a legacy repository creates all branches at once and is denied entirely if even one name is outside the standard. Add BRANCHPOLICY after the import
- in a project without PATHPOLICY, adding BRANCHPOLICY gives a warning that nothing protects the file. This is expected
- `settings-check` checks, for projects with BRANCHPOLICY, that the project protects the patterns of rules with `protected: true` ([operations.en.md](operations.en.md#settings-check))

## Checking without a push

```sh
gitlab-repopolicy validate -f BRANCHPOLICY
gitlab-repopolicy validate -f BRANCHPOLICY --config presets.yaml
```

`validate` makes the same checks as acceptance: the schema, regexes, the order of the rules and `examples`. A file with `extends` needs `--config` with `branchpolicy_presets`; other config keys are not needed.
