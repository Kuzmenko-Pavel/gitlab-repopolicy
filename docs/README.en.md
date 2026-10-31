[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](README.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](README.en.md)

# gitlab-repopolicy documentation

If you are new to the application, the [project README](../README.en.md) explains its capabilities and shows a denial example. Then choose material for your task.

## Policy users

Policies are YAML files in the repository root. The active version is read from the default branch before the push; changing a rule does not weaken checks for that same push. GitLab settings define the default branch; it is not necessarily named `main`.

| Task | Material |
| --- | --- |
| Restrict file changes to selected users | [PATHPOLICY](user/policies/pathpolicy.en.md) and [worked scenarios](user/policies/pathpolicy.en.md#examples) |
| Set a commit message format | [COMMITPOLICY](user/policies/commitpolicy.en.md), [message structure](user/policies/commitpolicy.en.md#what-a-commit-message-consists-of) and [scenarios](user/policies/commitpolicy.en.md#examples) |
| Require tag signatures | [TAGPOLICY](user/policies/tagpolicy.en.md) and [release scenarios](user/policies/tagpolicy.en.md#examples) |
| Check new branch names | [BRANCHPOLICY](user/policies/branchpolicy.en.md) and [standard examples](user/policies/branchpolicy.en.md#examples) |
| Select branches or tags for a rule | [The match field](user/policies/match.en.md) |
| Check a file before pushing | [Local validation](user/reference/validate.en.md) |
| Choose a diagnostic tool | [CLI reference](user/reference/cli.en.md#which-command-to-use) |

For a rejected push, find the policy name in its message and open "Denials" in that reference. `checked as` identifies the user whose action the hook checked; this can differ from the commit author.

## GitLab administrators

[Installation](user/administration/install.en.md) prepares the service account, server files and Gitaly hook. For production, follow it with the [integration guide](user/administration/integration-guide.en.md): observe in `audit`, then enable denials in a controlled rollout.

[Server configuration](user/administration/configuration.en.md) distinguishes minimal startup, the packaged file and program defaults. [Operations](user/administration/operations.en.md) covers audit logging, alerts, tokens, recovery and diagnosis. To assess protection boundaries, read the [threat model](user/administration/threat-model.en.md) and [check sequence](development/architecture.en.md#how-changes-are-checked).

## gitlab-repopolicy developers

The [developer documentation entry point](development/README.en.md) leads from architecture to local checks and releases. [CONTRIBUTING](../CONTRIBUTING.en.md) defines contribution, DCO and review rules; [SECURITY](../SECURITY.en.md) explains private vulnerability reporting.
