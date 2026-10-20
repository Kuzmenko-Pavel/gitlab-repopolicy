[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](branchpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](branchpolicy.en.md)

# BRANCHPOLICY

BRANCHPOLICY checks the name of every new branch and that branches of some kinds are protected in GitLab. The file lies in the root of the repository with the name `BRANCHPOLICY`, its content is YAML.

First, what a branch name is and how to change it. Then, how BRANCHPOLICY checks it.

## Branch name

A branch is a moving name for a line of commits, every new commit in the branch moves it forward. A branch name may contain `/`, and teams use this so that the name tells at once what kind of branch it is:
- `release/v1.4.0` - the release 1.4.0
- `hotfix/v1.4.1` - an urgent fix of a release
- `kpn/disable_auth` - a work branch of the developer `kpn`
- `gks/INFRA-1719/change_logic` - a work branch of the developer `gks` for the issue `INFRA-1719`

The part before the first `/` is called the prefix. BRANCHPOLICY splits branches into classes by such a prefix or by another name pattern and sets its own requirements for every class.

### How to create, rename and push a branch

```sh
git switch -c kpn/disable_auth        # create a branch locally and switch to it
git branch -m kpn/disable_auth         # rename the current branch
git branch -m old-name kpn/disable_auth  # rename another branch
git push -u origin kpn/disable_auth    # create the branch on the server
```

BRANCHPOLICY checks a branch only when it appears on the server, that is on the first `git push` with the new name. Locally a branch can have any name.

### What to do after a denial

The hook rejects the push, so the branch with the wrong name is not on the server, it exists only on your machine. Rename it locally as the denial suggests and push again:

```sh
git branch -m release/foo release/v1.4.0
git push -u origin release/v1.4.0
```

Nothing needs to be deleted on the server. Deleting a branch from the server (`git push origin --delete <name>`) is needed only when a wrongly named branch was already there before BRANCHPOLICY appeared and it is to be removed.

## How BRANCHPOLICY checks it

The file consists of rules. A rule describes a class of branches. `match` says which names fall into the class, the other fields say what is required of them. A new branch falls into a class by the first rule whose `match` fits the name.

Why this is needed:
- protection. Branches `release/*` or `epic/*` deploy, and the pipeline of an unprotected branch does not see protected CI/CD variables (passwords, deploy tokens) and protected runners. The branch is created, CI deploys nothing, and people search for the reason for a long time. The hook catches this at once, when the branch is created, and says what to set up
- a name standard. An agreement of the team becomes a rule on the server, not an oral tradition

BRANCHPOLICY keeps order, it does not guard access. So when the check cannot be done (the file in the default branch is broken, the GitLab API did not answer), the branch is created with a warning instead of being blocked, as in COMMITPOLICY. Who may change BRANCHPOLICY itself is set by a PATHPOLICY rule ([pathpolicy.en.md](pathpolicy.en.md)). Without such a rule anyone who can push changes the file.

## Example

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

How to read it:
- `release/v1.4.0` falls into the class `release`, because `match: "release/*"` fits. The name matches `pattern`, so the branch passes if the project protects `release/*` (Settings > Repository > Protected branches)
- `release/foo` also falls into the class `release` and is denied there, because it does not match `pattern`. It does not move on to the class `work`, only the first matching rule sets the class
- `kpn/disable_auth` and `gks/INFRA-1719/change_logic` do not fit `release/*`, `hotfix/*`, `epic/*`, `demo/*` and fall into `work`, where they pass `pattern`
- `123-fix-login` also falls into `work`, but does not pass `pattern`, because it has no prefix with `/`
- a name that fits no class is denied. There is no such name here, `work` with `match: "*"` catches everything

`*` in `match` matches any characters, including `/`, as in GitLab protected branches. `release/*` covers both `release/v1.4.0` and `release/v1.4/rc1`, and `*` covers any name. In the `refs` field of COMMITPOLICY `*` behaves differently and stops at `/` ([commitpolicy.en.md](commitpolicy.en.md#which-branches-a-ruleset-applies-to-refs)).

`pattern` is a regex (RE2 syntax) that the whole branch name must match. The raw regex is not shown to a person in a denial, they see the `message` text, so it holds an example of a correct name.

## Presets (`extends`)

A preset is a named set of rules in the server config. A name standard shared by many projects is best kept there, and projects take it with one line:

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

So a project adds its own classes before the standard and may override the last rule of the standard, which catches everything (`match: "*"`), and it stays last. For example, a project on `main` where `master` may not be created:

```yaml
version: 1
extends: org-branches
rules:
  - name: no-master
    match: "master"
    deny: true
    message: This project uses main as the default branch; master must not be created.
```

A strict project, where only the classes of the standard are allowed and everything else is forbidden. The rule named `work` replaces the preset rule `work` with `match: "*"`:

```yaml
version: 1
extends: org-branches
rules:
  - name: work
    match: "*"
    deny: true
    message: In this project only release/* branches are created.
```

Moving from `master` to `main`. The new main branch is allowed explicitly, and the rule is removed after the move:

```yaml
version: 1
extends: org-branches
rules:
  - name: migration-main
    match: "main"
    protected: true
```

A project without a preset writes all rules itself. If there is no rule with `match: "*"`, everything that is not described is forbidden.

### Forbid everything that is not described

There is no rule `match: "*"`, so a name without a class is denied with `branch_name_violation`. The main branch `main` is the default branch of the project, the hook checks its name against `default_branch` of the server config, so it needs no rule of its own:

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
| `master` | denied, rule `no-master` |
| `release/v1.4.0` | passes if the project protects `release/*` |
| `release/foo` | denied, class `release`, the name does not match `pattern`. It does not move on to the class `user` |
| `jdoe/disable_auth`, pushed by `jdoe` | passes, the prefix is the username of the pusher |
| `fix-login`, `tmp` | denied, no `match` fits (`*/*` requires `/`) |

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
| `match` | yes | which names fall into the class. A pattern as in GitLab protected branches, `*` matches any characters, including `/`. Up to 255 bytes |
| `deny` | no | `true` - branches of this class may not be created. Together with `pattern`, `user_prefix`, `protected` or `examples` it is an error |
| `pattern` | no | a regex (RE2 syntax) that the whole branch name must match. Without `pattern` any name in the class is allowed. Up to 256 bytes |
| `user_prefix` | no | `true` - the part of the name before the first `/` is the username of an active GitLab user |
| `protected` | no | `true` - the project must protect a new branch of this class in GitLab (Settings > Repository > Protected branches) |
| `examples` | no | only with `pattern`, `{valid: [...], invalid: [...]}`. Names on which the hook checks the regex itself on acceptance of the file and in `validate`. Not printed in a denial |
| `message` | no | the text of a denial, up to 1000 characters. It holds an example name for a person and, for `protected`, which roles to set in Protected branches |

Unknown keys, YAML anchors and aliases are an error. The file size is up to 64 KiB.

Checks of the list of rules together with the preset, on acceptance of a new version and in `validate`:
- there are no rules after the rule with `match: "*"`: they are never reached
- `match` is not repeated
- every `examples.valid` passes the `pattern` of its rule and falls into this very rule. This shows when a rule above takes the names of another one. For example, a project added `match: "*/*"` before the preset, and `release/v1.4.0` went to another rule
- every `examples.invalid` does not pass the `pattern` of its rule

Without `examples` only the first two checks catch such taking.

## What is checked

Only the creation of a branch. A new commit in an existing branch and its deletion are not checked, existing branches stay as they are, even with a name outside the standard. The default branch is not checked, the hook checks its name against `default_branch` ([configuration.en.md](configuration.en.md#default_branch)). Tags are not checked, the protection of release tags is set by the field `protected` in TAGPOLICY ([tagpolicy.en.md](tagpolicy.en.md)).

For a new branch, in order:
1. The class is the first rule whose `match` fits the name. No such rule - denied with `branch_name_violation`
2. `deny: true` - denied with `branch_name_violation`
3. `pattern` does not match - denied with `branch_name_violation`
4. `user_prefix: true` - the part before the first `/` must be a username. If a person creates the branch with their own username as the prefix, no API is needed, because GitLab has just authenticated them. Otherwise the hook asks GitLab once whether there is an active user with such a name, ignoring case. If not - denied with `branch_name_violation`. So a branch can be created for a colleague. `olga/fix` by `alice` passes if `olga` exists
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
| BRANCHPOLICY in the default branch is invalid | the module is skipped with a warning, only for pushes that create a branch. The audit event `invalid_branch_policy` |
| a push changes BRANCHPOLICY to an invalid version | denied with `invalid_policy_update` |
| a personal namespace | not checked |

The audit event `decision` is written only for a violation or a warning, otherwise every branch creation in hundreds of projects would leave a record.

| Code | Title in the output | When |
| --- | --- | --- |
| `branch_name_violation` | the name does not follow the branch standard | the name does not match `pattern` or `user_prefix` of its class |
| `branch_name_violation` | creating this branch is forbidden | a class with `deny: true` |
| `branch_name_violation` | no rule of BRANCHPOLICY allows this name | the name fell into no class |
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

How to read it:
- the title - which rule, which branch and what the problem is
- then the `message` of the rule and what to do (`how to fix`)
- `rules` - from which commit of the default branch the hook read BRANCHPOLICY
- `checked as` - who pushed and how (HTTPS, SSH, the GitLab UI)
- `GL-HOOK-ERR` - a short line for the GitLab UI

Without `message` the denial still names the rule, the branch and the reason. For a name without a class the denial is:

```text
remote: BRANCHPOLICY, branch fix-login: no rule of BRANCHPOLICY allows this name
remote:   how to fix: rename the branch locally (git branch -m) to a name the rules of this project allow, and push again
```

## How to roll out

1. A preset in the server config, BRANCHPOLICY with `extends` and `mode: warn` in the projects, a PATHPOLICY rule on the file `BRANCHPOLICY`
2. A week or two of warnings. Look at the `decision` events of the module `branchpolicy` in audit to see which names break the standard
3. `mode: enforce`

Pitfalls:
- `git push --mirror` of an old repository creates all branches at once and is denied entirely if even one name is outside the standard. Add BRANCHPOLICY after the import
- in a project without PATHPOLICY, adding BRANCHPOLICY gives a warning that nothing protects the file. This is expected
- `settings-check` checks, for projects with BRANCHPOLICY, that the project protects the patterns of rules with `protected: true` ([operations.en.md](operations.en.md#settings-check))

## Checking without a push

```sh
gitlab-repopolicy validate -f BRANCHPOLICY
gitlab-repopolicy validate -f BRANCHPOLICY --config presets.yaml
```

`validate` makes the same checks as the acceptance of a new version of the file, that is the schema, regexes, the order of the rules and `examples`. A file with `extends` needs `--config` with `branchpolicy_presets`, other config keys are not needed.
