[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](protected-branches.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](protected-branches.en.md)

# Protecting a release branch before its first push

A release pipeline needs protected CI/CD variables or protected runners. If branch protection was forgotten, those resources may be unavailable. `protected: true` detects missing protection when a branch is created.

## Configuration and preparation

Store this `BRANCHPOLICY` on the default branch after running `gitlab-repopolicy validate -f BRANCHPOLICY`. It checks whether protection exists, not the specific roles suggested in `message`.

```yaml
version: 1
rules:
  release:
    match: ['/^release\/v\d+\.\d+\.\d+$/']
    priority: 100
    protected: true
    message: Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
```

Before creating a release branch, a Maintainer adds `release/*` in Settings > Repository > Protected branches with the roles the project needs. Protected branches are a GitLab setting; the policy's `protected` field does not create that setting.

```text
remote: BRANCHPOLICY rule "release", branch release/v1.4.0: the branch must be protected, but the project does not protect it
remote:   Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
remote:   how to fix: a Maintainer protects release/v1.4.0 in Settings > Repository > Protected branches, then push again
```

Under global `enforce`, a new `release/v1.4.0` receives `branch_not_protected` if the API confirms missing protection. Configure protection and push again: this denial should disappear. User permissions and other GitLab checks still apply. If the API does not answer, BRANCHPOLICY skips the check with a warning.

## What to check after creation

Removing protection from an existing branch is not checked on every subsequent commit: BRANCHPOLICY evaluates creation only. Run `settings-check` with the server configuration and token to detect such drift on existing branches. The service account needs project access.

`validate --branch release/v1.4.0` states the protection requirement but does not ask GitLab whether it holds, even with `--online`. Do not use its exit code to verify settings.

[BRANCHPOLICY reference](../../policies/branchpolicy.en.md)
