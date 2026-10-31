[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](branchpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](branchpolicy.en.md)

# BRANCHPOLICY

## In short

BRANCHPOLICY checks the name of every new branch and that branches of certain classes are protected in GitLab. The agreement of the team on names becomes a rule on the server, and a release branch nobody remembered to protect does not stay without protected CI/CD variables and runners. The file is in the root of the repository under the name `BRANCHPOLICY`, the content is YAML. The smallest working file:

```yaml
version: 1
rules:
  no-master:
    match: [master]
    priority: 10
    deny: true
    message: This project uses main; master must not be created.
```

Here the branch `master` must not be created. All other names are not checked.

## Quick start

1. Describe the classes of branches as rules with `priority` and check the file: `gitlab-repopolicy validate --branch <name>` shows which rule applies to a name ([below](#checking-without-a-push))
2. Start with `mode: warn`: violations are only shown and written to audit
3. Merge the file into the default branch. Only new branches are checked, existing ones stay as they are
4. When there are almost no warnings, remove `mode: warn`

Who may change BRANCHPOLICY itself is decided by a rule of PATHPOLICY ([pathpolicy.en.md](../examples/pathpolicy/delegation.en.md)).

## The name of a branch

The name of a branch may contain `/`, and teams use that so the name tells at once what the branch is:
- `release/v1.4.0` - the release 1.4.0
- `hotfix/v1.4.1` - an urgent fix of a release
- `kpn/disable_auth` - a work branch of the developer `kpn`
- `kpn/PAY-12/refunds` - a work branch of `kpn` for the issue `PAY-12`

BRANCHPOLICY checks a branch when it appears on the server: at the first `git push` with a new name or at a creation in the GitLab UI. Locally a branch may have any name.

```sh
git switch -c kpn/disable_auth            # create a branch and switch to it
git branch -m old-name kpn/disable_auth   # rename a branch
git push -u origin kpn/disable_auth       # create the branch on the server
```

After a denial the branch is not on the server, only on your machine. Rename it as the denial suggests and push again, nothing has to be deleted on the server.

## Field reference

| Field | Required | Value |
| --- | --- | --- |
| `version` | yes | `1` |
| `mode` | no | `enforce` (the default) or `warn`. `warn` turns every denial into a warning |
| `extends` | no | the name of a preset of the server config or a list of names ([configuration.en.md](../administration/configuration.en.md#presets)) |
| `rules` | no | rules, up to 50: rule name → rule |
| `rules.<name>.match` | yes | the names of branches of the class ([match.en.md](match.en.md)). There is no `default` here |
| `rules.<name>.priority` | yes | the order of the rules, an integer from 0 to 1 000 000. A new branch falls into the rule with the lowest `priority` whose `match` matched. Unique after the merge with presets |
| `rules.<name>.deny` | no | `true` forbids branch creation; `false` (the default) adds no such prohibition. A forbidden branch has nothing to check, so with `deny: true` the fields `user_prefix` and `protected` cannot be `true`; `false` is allowed |
| `rules.<name>.user_prefix` | no | `true` requires an active GitLab username before the first `/`; `false` (the default) disables the prefix check |
| `rules.<name>.protected` | no | `true` requires the new branch to be protected in GitLab (Settings > Repository > Protected branches); `false` (the default) does not require that status |
| `rules.<name>.message` | yes | text in a denial, up to 1000 characters. An example of a right name, and for `protected` which roles to set |
| `rules.<name>.help` | no | `contact` and `docs_url`, as in [PATHPOLICY](pathpolicy.en.md#field-reference) |

Defaults apply to the rule after merging with presets. A parameter omitted from the file is inherited; explicit `false` disables the corresponding preset requirement. The conflict with `deny: true` is checked after merging: if a preset sets `protected: true` and the file forbids the class, the file writes `deny: true` and `protected: false`, without `null`. `protected: false` does not change branch protection settings in GitLab.

Checks of the list of rules after the merge with presets, at the acceptance of a new version and in `validate`:
- `priority` does not repeat: otherwise the order of the rules would be random
- no rules come after a rule with `match: all`: they would never apply
- two rules with the same `match` (as sets) are an error: the second would never apply

Unknown keys, YAML anchors and aliases are an error. The file is up to 64 KiB.

## What is checked

Only the creation of a branch. A new commit in an existing branch and its deletion are not checked. The default branch is not checked, the hook compares its name with `default_branch` of the server config ([configuration.en.md](../administration/configuration.en.md#default_branch)). Tags are not checked.

For a new branch, in order:
1. The rule is the one with the lowest `priority` among the rules whose `match` matches the name. No rule matches - the name is not checked
2. `deny: true` - denied with `branch_name_violation`
3. `user_prefix: true` - the part before the first `/` must be a username. When a person creates a branch with their own username as the prefix, the API is not needed. Otherwise the hook asks GitLab once whether an active user with this name exists. None - denied with `branch_name_violation`. A branch for a colleague can be created: `bob/fix` by alice passes when bob exists
4. `protected: true` - the hook asks GitLab whether the project protects this branch. No - denied with `branch_not_protected`

A name of no class is not checked on purpose: a standard describes only the classes that matter. To forbid everything that is not described, add a last rule with `match: all` and `deny: true` ([example 3](../examples/branchpolicy/strict-names.en.md)).

A branch created in the UI or the API of GitLab (New branch, a branch from an issue, the Web IDE, revert and cherry-pick into a new MR) is checked the same way. The names GitLab suggests (`123-title`, `user-main-patch-48213`) usually do not follow a standard: rename the branch in the dialog.

## Examples

| Example | What it explains |
| --- | --- |
| [Organization standard](../examples/branchpolicy/organization-standard.en.md) | A complete preset and the behavior of branch classes |
| [Family denial](../examples/branchpolicy/deny-family.en.md) | An exact release format and `deny` for malformed names |
| [Strict names](../examples/branchpolicy/strict-names.en.md) | A final rule forbidding every unlisted name |
| [Project overrides](../examples/branchpolicy/project-overrides.en.md) | `null`, field overrides and falling into another class |
| [Username prefix](../examples/branchpolicy/user-prefix.en.md) | Own branches, a colleague's branch and a missing user |
| [Protected release branches](../examples/branchpolicy/protected-branches.en.md) | Protection before creation and subsequent settings-check |
| [Exceptions and denial](../examples/branchpolicy/exception-vs-deny.en.md) | Why `!` in match does not replace `deny` |

## Denials

| Situation | Result |
| --- | --- |
| `mode: enforce`, a violation | denied with the code of the violation |
| `mode: warn`, a violation | the push goes through, a warning in the output and in audit |
| the API did not answer during `user_prefix` or `protected` | the check is skipped, a warning |
| the preset of `extends` is missing from the server config, broken, or the rules after the merge fail the checks | the module is skipped with a warning, only for pushes that create a branch |
| BRANCHPOLICY in the default branch is invalid | the same, the audit event `invalid_branch_policy` |
| a push changes BRANCHPOLICY to an invalid version | denied with `invalid_policy_update` |

BRANCHPOLICY keeps order, it does not guard access. So when the check cannot be made, the branch is created with a warning.

| Code | Title in the output | When |
| --- | --- | --- |
| `branch_name_violation` | creating this branch is forbidden | a rule with `deny: true` |
| `branch_name_violation` | the name does not follow the branch standard | the prefix is not the username of an active user |
| `branch_not_protected` | the branch must be protected, but the project does not protect it | a new branch of a rule with `protected: true`, and the project does not protect it |

The whole denial for `master`:

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: BRANCHPOLICY rule "no-master", branch master: creating this branch is forbidden
remote:   This project uses main; master must not be created.
remote:
remote: rules: BRANCHPOLICY in main (72ad41f)
remote: checked as: alice (SSH)
remote: GL-HOOK-ERR: gitlab-repopolicy: branch master is forbidden by BRANCHPOLICY rule no-master
```

- the title - the rule, the branch and what the problem is
- then the `message` of the rule and, when there is one, what to do (`how to fix`)
- `rules` - which commit of the default branch the hook read BRANCHPOLICY from
- `checked as` - who pushed and how
- `GL-HOOK-ERR` - the short line for the GitLab UI

## Common mistakes

| Symptom | Cause | What to do |
| --- | --- | --- |
| `priority 100 is also the priority of rule ...` from `validate` | the same `priority` in two rules, often after the merge with a preset | change the `priority` of the rule in the file |
| `comes after rule other with match: all and is never used` | the rule with `match: all` is not the last | give the rule with `match: all` the highest `priority` |
| `release/foo` was created unchecked | no `deny` for the family `release/` | add the rule `release-bad` ([example 2](../examples/branchpolicy/deny-family.en.md)) |
| `tmp/x` was created although `!/^tmp\//` is in `match` | an exception only removes a name from a class | a separate rule with `deny` ([example 7](../examples/branchpolicy/exception-vs-deny.en.md)) |
| `git push --mirror` of an old repository is rejected as a whole | one branch with a wrong name rejects the whole push | add BRANCHPOLICY after the import |
| after `demo: null` branches `demo/...` need a user `demo` | the name moves to the next rule, `work` | check names with `validate --branch` |

## Checking without a push

```sh
gitlab-repopolicy validate -f BRANCHPOLICY
gitlab-repopolicy validate -f BRANCHPOLICY --config presets.yaml --branch release/v1.4.0
gitlab-repopolicy validate -f BRANCHPOLICY --config config.yaml --online --branch kpn/disable_auth
```

`validate` checks whether the file is valid. `--branch` also shows which rule applies to a name and its result. For a valid file, this informational query returns code 0 even when the answer is `denied`. With `--online`, it can check the user from the `user_prefix` through the GitLab API. The [validate guide](../reference/validate.en.md) explains these modes and their limits; the [CLI reference](../reference/cli.en.md) lists every parameter.
