[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](organization-preset.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](organization-preset.en.md)

# Shared organization rules

The platform team maintains CI rules in one server preset. A project includes them with `extends`, adds its maintainer kpn to the permitted users and removes the rule for deploy scripts it does not have.

## Server and project

An administrator adds this fragment to the server `config.yaml` on every Gitaly node. For local validation, save it as `presets.yaml`: offline commands need no other server keys.

```yaml
presets:
  pathpolicy:
    org-ci:
      rules:
        ci-config:
          match: all
          paths: [".gitlab-ci.yml", ".gitlab/ci/**"]
          allow: [platform-lead, sre-lead]
          message: The platform team changes the CI/CD configuration.
          help:
            contact: "#platform"
        deploy:
          match: all
          paths: ["scripts/deploy/**"]
          allow: [platform-lead]
          message: The deploy scripts belong to platform-lead.
```

Create `PATHPOLICY` in the repository root:

```yaml
# examples/org-ci/PATHPOLICY
# The CI rules of the organization come from the preset org-ci. This project
# lets its own maintainer kpn change the CI configuration too, and drops the
# rule for deploy scripts, which it does not have.
version: 1
extends: org-ci

admins: [platform-lead]

rules:
  ci-config:
    allow: [platform-lead, sre-lead, kpn]
  deploy: null
```

platform-lead must be an active Maintainer or Owner; the other usernames must exist. Run `gitlab-repopolicy validate --config presets.yaml -f PATHPOLICY` before a Maintainer or Owner merges it.

`allow` in the file replaces the complete list, so platform-lead and sre-lead are listed again. `deploy: null` removes the rule. `match`, `paths`, `message` and `help` of `ci-config` remain inherited from the preset.

`validate` shows the result of the merge:

```text
$ gitlab-repopolicy validate --config presets.yaml
PATHPOLICY: valid
  extends: org-ci
  rule ci-config: preset:org-ci, file
    allow: file
    help: preset:org-ci
    help.contact: preset:org-ci
    match: preset:org-ci
    message: preset:org-ci
    paths: preset:org-ci
```

`explain` on a Gitaly node shows the same for a given path and person:

```text
$ gitlab-repopolicy explain --project acme/platform/app --user kpn --path .gitlab-ci.yml --ref feature
acme/platform/app, PATHPOLICY of main; path .gitlab-ci.yml, ref refs/heads/feature, user kpn
  ci-config (match: all; from preset:org-ci, file): allowed; allow: platform-lead, sre-lead, kpn
    fields from: allow file; help preset:org-ci; help.contact preset:org-ci; match preset:org-ci; message preset:org-ci; paths preset:org-ci
result: kpn may change .gitlab-ci.yml on refs/heads/feature (as a person: a deploy key never may)
```

A typical mistake is to write only `[kpn]` in `allow` to "add" kpn. The list is not extended but replaced: after the merge only kpn would change CI.

## Effects on the team

Under global `enforce`, kpn may push `.gitlab-ci.yml` changes and bob may not, provided other GitLab checks permit the action. Deploy scripts are no longer protected by this file: verify that the project really does not use them.

Writing only `allow: [kpn]` would remove platform-lead and sre-lead instead of adding a user. Renaming or deleting a server preset is also not a local change: every PATHPOLICY with that `extends` becomes unusable. Validate project files before delivery and run `selftest` afterwards; roll back by restoring a consistent preset version on every node.

[PATHPOLICY reference](../../policies/pathpolicy.en.md)
