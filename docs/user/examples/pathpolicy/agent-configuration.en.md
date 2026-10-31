[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](agent-configuration.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](agent-configuration.en.md)

# GitLab Agent configuration

Anyone with push access may propose `.gitlab/agents/**` configuration in their own branch. Only alice may accept it into the default branch, because GitLab reads the agent configuration from there.

## File and check scope

Add this `PATHPOLICY` to the repository root, run `gitlab-repopolicy validate -f PATHPOLICY` and have a Maintainer or Owner merge its first version. All usernames must exist; alice must be an active Maintainer or Owner. The `commit-policy` and `tag-policy` rules in the same file delegate other policies; `kubernetes-agents` governs this scenario.

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

| Who | What | Result | Why |
| --- | --- | --- | --- |
| bob | changes `.gitlab/agents/prod/config.yaml` in `feature` | allowed | the rule applies only to the default branch |
| bob | merges `feature` into `main` | denied | bob makes the change in `main` |
| alice | merges `feature` into `main` | allowed | alice is in `allow` |

The table assumes default branch `main` and global `enforce`; GitLab permissions and other policies must permit the action. A merge checks whoever accepts the change, regardless of commit authorship.

## Verifying the result

bob pushes `feature` with a change to `.gitlab/agents/prod/config.yaml`. bob's attempt to merge it into `main` should produce `protected_path_violation`; alice may accept the same merge. Changing the rule and configuration in one push still uses the old policy for that push.

Do not use this `match` choice for CI files in every branch. `default` fits here because of where the agent reads its configuration. To block drafts too, use `all`, accepting that bob can no longer push a proposal.

[PATHPOLICY reference](../../policies/pathpolicy.en.md)
