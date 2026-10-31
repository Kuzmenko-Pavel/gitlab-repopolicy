[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](strict-names.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](strict-names.en.md)

# Forbidding every unlisted name

The project permits only release branches and `<prefix>/<name>` work branches. Every other new name must be denied rather than pass without a match.

## Project file

Save this as `BRANCHPOLICY` in the repository root. Run `gitlab-repopolicy validate -f BRANCHPOLICY` before merging it into the default branch. The table assumes default branch `main`, GitLab protection for `main` and `release/*`, and global `enforce`.

```yaml
# examples/strict-branches/BRANCHPOLICY
# Strict mode: only release and work branches may be created. The last rule
# has match: all and deny, so a name of no other class is refused instead of
# passing unchecked.
version: 1

rules:
  release:
    match: ['/^release\/v\d+\.\d+\.\d+$/']
    priority: 10
    protected: true
    message: Release branches look like release/v1.4.0.
  work:
    match: ['/^[a-z0-9.]+\/[a-z0-9._-]+$/']
    priority: 20
    message: Work branches are <prefix>/<name>, for example kpn/disable_auth.
  other:
    match: all
    priority: 1000
    deny: true
    message: Use release/vX.Y.Z or <prefix>/<name>.
```

<!-- check: branch-push examples/strict-branches/BRANCHPOLICY main,release/* -->
| New branch | Who creates it | Rule | Result | Why |
| --- | --- | --- | --- | --- |
| `release/v1.4.0` | bob | `release` | allowed | the name follows the standard, `release/*` is protected |
| `bob/disable_auth` | bob | `work` | allowed | the name is `<prefix>/<name>` |
| `fix-login` | bob | `other` | denied | no other rule matched |
| `master` | bob | `other` | denied | the same |

The rule with `match: all` must be the last: `validate` rejects a file with more rules after it.

The final `other` rule has the highest `priority` number and `match: all`. It catches only names not accepted by earlier rules. This example's work rule checks the prefix's shape, not whether it is a GitLab username: `user_prefix` is unset.

Run `validate --branch fix-login` for diagnosis, then make a control push creating `fix-login` in a test project. Under global `enforce`, expect `branch_name_violation`. `validate --branch` is not itself a CI permission check.

Do not place rules after `match: all`: the validator rejects unreachable rules. Do not apply this policy to a bulk import of old names without checking them first: one invalid new branch rejects the entire push.

[BRANCHPOLICY reference](../../policies/branchpolicy.en.md)
