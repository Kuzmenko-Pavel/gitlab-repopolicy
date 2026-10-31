[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](deny-family.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](deny-family.en.md)

# An exact rule and a denial for the remaining family

`release/v1.4.0` is valid; `release/foo` is a mistake. One exact regex does not forbid the second name: unmatched names pass unchecked. A following rule for the whole `release/` family is needed to deny it.

## Complete configuration

Save this as `BRANCHPOLICY`, run `gitlab-repopolicy validate -f BRANCHPOLICY` and activate it in the default branch. A Maintainer must protect `release/*` before creating a valid release branch.

```yaml
version: 1
rules:
  release:
    match: ['/^release\/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/']
    priority: 100
    protected: true
    message: Use release/vX.Y.Z and protect release/* in GitLab.
  release-bad:
    match: ['/^release\//']
    priority: 110
    deny: true
    message: Release branches look like release/v1.4.0.
```

| Name | Selected rule | Result in enforce |
| --- | --- | --- |
| `release/v1.4.0` | `release` | allowed if GitLab protects the branch |
| `release/foo` | `release-bad` | `branch_name_violation` |
| `feature/login` | none | name not checked |

The exact rule must have a lower `priority`; otherwise the family denial also rejects valid names. Priorities must remain unique after preset merging.

```text
remote: BRANCHPOLICY rule "release-bad", branch release/foo: creating this branch is forbidden
remote:   Release branches look like release/v1.4.0.
```

Inspect selection with `validate --branch release/foo` and `validate --branch release/v1.4.0`. Both informational queries can exit 0. Check actual rejection with a control push creating a branch under global `enforce`.

Without the family denial, a name can fall through to a broad `work` rule that treats `release` as a username. A useful denial explains the required release name rather than sending a developer to look for a nonexistent user.

[BRANCHPOLICY reference](../../policies/branchpolicy.en.md)
