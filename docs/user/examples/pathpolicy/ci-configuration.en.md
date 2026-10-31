[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](ci-configuration.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](ci-configuration.en.md)

# Protecting CI/CD configuration

Only alice and sre-lead may change `.gitlab-ci.yml`, local includes and deploy scripts, in any branch. A pipeline runs its branch's configuration, so checking only a merge into `main` is insufficient.

## Project policy

Save this as `PATHPOLICY` in the repository root. Check it with `gitlab-repopolicy validate -f PATHPOLICY`; a Maintainer or Owner adds the first version to the default branch. The usernames must exist, and at least one `admin` must be an active Maintainer or Owner.

```yaml
# examples/ci-config/PATHPOLICY
# The platform team, alice and sre-lead, owns the CI configuration and the
# deploy scripts in every branch and the production values in main and
# release branches. Everything else is authorized by GitLab as usual.
version: 1

admins: [alice, sre-lead]

rules:
  ci-config:
    match: all
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "scripts/deploy/**"
    allow: [alice, sre-lead]
    message: >-
      The platform team changes the CI/CD configuration and the deploy scripts.
      Ask alice or sre-lead to push this change.
    help:
      contact: "#platform"

  prod-values:
    match: [main, '/^release\//']
    paths: ["helm/**/values-prod.yaml"]
    allow: [alice, release-manager]
    message: >-
      Production values are changed in main and release branches by alice or
      release-manager. Propose the change in your branch and ask them to merge it.
```

`ci-config` uses `match: all` to cover every branch. `allow` identifies whoever pushes the change, rather than its commit author. `prod-values` separately restricts production files and grants no additional access to CI.

| Who | What | Result | Why |
| --- | --- | --- | --- |
| bob | changes `README.md` | allowed | no rule is about the file |
| bob | pushes the branch `feature` with a changed `.gitlab-ci.yml` | denied | `ci-config` applies to every branch, bob is not in `allow` |
| alice | pushes the same branch | allowed | alice is in `allow` |
| bob | tags a commit of this branch | allowed | PATHPOLICY does not check tags, the commit was checked with the branch |

The table describes PATHPOLICY's contribution in a group project with default branch `main` and global `enforce`. Other policies and GitLab permissions must also permit the action. The tag in bob's row points to an already accepted branch commit; a separate TAGPOLICY can require a signature.

## Verification and common mistakes

Once active, ask bob to change only README, then a CI file in a test branch. The first push should pass and the second produce `protected_path_violation`. Have alice push that same CI change. In global `audit`, pushes pass and expected denials appear in the log.

`match: default` leaves other branches' CI outside this rule. Protecting only `.gitlab-ci.yml` is also insufficient when a job calls an unprotected `Makefile` or script: include every path that defines executed commands.

[PATHPOLICY reference](../../policies/pathpolicy.en.md)
