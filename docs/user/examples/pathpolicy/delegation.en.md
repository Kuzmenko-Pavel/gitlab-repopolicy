[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](delegation.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](delegation.en.md)

# Delegating policy ownership

alice manages PATHPOLICY, dev-lead owns commit formatting, and release-lead owns tag keys and rules. Owners need to edit their policy files without gaining permission to change PATHPOLICY itself.

## Assigning owners

Save this as `PATHPOLICY` in the root of the default branch. All usernames must exist; alice must be an active Maintainer or Owner. Run `gitlab-repopolicy validate -f PATHPOLICY` before initial activation.

```yaml
# examples/delegation/PATHPOLICY
# PATHPOLICY stays with alice. The other policy files and the GitLab agent
# configuration are delegated to their owners by path rules.
version: 1

admins: [alice]

rules:
  commit-policy:
    match: all
    paths: ["COMMITPOLICY"]
    allow: [dev-lead, alice]
    message: COMMITPOLICY is changed by dev-lead or alice.

  tag-policy:
    match: all
    paths: ["TAGPOLICY"]
    allow: [release-lead, alice]
    message: TAGPOLICY is changed by release-lead or alice.

  kubernetes-agents:
    match: default
    paths: [".gitlab/agents/**"]
    allow: [alice]
    message: >-
      The agent configuration is read only from the default branch.
      Change it in your branch and ask alice to merge it.
```

`admins` protects PATHPOLICY itself. Other policy files have no automatic owner: path rules govern access. Here `match: all` protects drafts as well as file deletion. The agent rule is independent of delegation.

| Who | What | Result | Why |
| --- | --- | --- | --- |
| dev-lead | changes or deletes `COMMITPOLICY` | allowed | dev-lead is in the `allow` of the rule `commit-policy` |
| bob | changes or deletes `COMMITPOLICY` | denied | bob is not in `allow` |
| dev-lead | adds a rule for themselves to `PATHPOLICY` | denied | only `admins` change `PATHPOLICY` |

Without such a rule anyone who can push may change a policy file. The hook accepts such a version but warns about it.

The table assumes global `enforce`, regular GitLab access and a valid new file version. Even dev-lead cannot add an invalid COMMITPOLICY: authorization and file acceptance are separate checks.

## Extending and verifying

To cover BRANCHPOLICY, add a complete rule of the same form with path `BRANCHPOLICY` and the required `allow`. Do not expect `admins: [alice]` to protect other policy files automatically.

In a test project, have dev-lead change a valid COMMITPOLICY, then let bob attempt the same change. The first push should pass; the second should produce `protected_path_violation`. dev-lead changing PATHPOLICY produces `policy_admin_violation`.

[PATHPOLICY reference](../../policies/pathpolicy.en.md)
