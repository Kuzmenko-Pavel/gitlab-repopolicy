[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](README.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](README.en.md)

[![CI](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/ci.yml?query=branch%3Amain)
[![e2e](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/e2e.yml/badge.svg?branch=main)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/e2e.yml?query=branch%3Amain)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Kuzmenko-Pavel/gitlab-repopolicy/badge)](https://scorecard.dev/viewer/?uri=github.com/Kuzmenko-Pavel/gitlab-repopolicy)
[![Coverage](https://codecov.io/gh/Kuzmenko-Pavel/gitlab-repopolicy/graph/badge.svg?branch=main)](https://codecov.io/gh/Kuzmenko-Pavel/gitlab-repopolicy)
[![Release](https://img.shields.io/github/v/release/Kuzmenko-Pavel/gitlab-repopolicy?sort=semver&include_prereleases)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases)
[![License](https://img.shields.io/github/license/Kuzmenko-Pavel/gitlab-repopolicy)](./LICENSE)
[![Docs](https://img.shields.io/badge/docs-latest-blue)](#documentation)

# gitlab-repopolicy

Server-side checks of repository rules for GitLab Self-Managed. A global `pre-receive` hook of Gitaly checks pushes to project repositories against policy files that live in the repository itself. If a push breaks the rules, GitLab rejects it and shows what is wrong and how to fix it.

## Where to start

| Who you are | What you need | Where to look |
| --- | --- | --- |
| a developer whose push GitLab rejected | understand the denial and fix the commit, tag or branch | the policy document named in the denial: [COMMITPOLICY](docs/commitpolicy.en.md), [TAGPOLICY](docs/tagpolicy.en.md), [BRANCHPOLICY](docs/branchpolicy.en.md), [PATHPOLICY](docs/pathpolicy.en.md) |
| a project Maintainer | write a policy file, check it locally and turn it on without stopping the team | the same policy documents, [validate.en.md](docs/validate.en.md), then [integration-guide.en.md](docs/integration-guide.en.md) |
| a GitLab administrator | install the hook, configure the server, watch the audit | [install.en.md](docs/install.en.md), [configuration.en.md](docs/configuration.en.md), [operations.en.md](docs/operations.en.md), [integration-guide.en.md](docs/integration-guide.en.md) |

## Policies

Each policy is a separate YAML file in the root of the repository.

| File | What it decides |
| --- | --- |
| [PATHPOLICY](docs/pathpolicy.en.md) | who may change certain files: CI configuration, deploy logic, prod values |
| [COMMITPOLICY](docs/commitpolicy.en.md) | what format commit messages must have |
| [TAGPOLICY](docs/tagpolicy.en.md) | who may release: a tag is accepted only with a signature of an allowed OpenPGP or SSH key |
| [BRANCHPOLICY](docs/branchpolicy.en.md) | how new branches are named and which of them must be protected in GitLab |

## Why

GitLab has no way to let only specific people change specific files on every way a change can arrive. Protected branches protect a whole branch, Code Owners work only in a merge request and are bypassed by a direct push, push rules exist only in Premium and do not know who makes the change. Client-side checks (commitlint, pre-commit) are bypassed with `--no-verify` and do not see the merge button and the Web IDE.

gitlab-repopolicy checks a change on the server, so it does not matter how it was made: a push over HTTPS or SSH, the merge button, auto-merge, the Web IDE, the API, a push from CI. The rules are versioned and reviewed together with the code. The hook can only deny, it does not extend GitLab permissions.

When the hook checks nothing:
- a push to a project in a user's personal namespace (`alice/my-project`). Only group projects are checked
- a push to wiki, design and snippet repositories
- `mode: off` in the server configuration

With `mode: audit` the hook checks everything, but only writes the decision to the audit log and denies nobody ([configuration.en.md](docs/configuration.en.md#mode)).

An example from the file to the fix. The platform team (alice and sre-lead) wants only themselves to change the CI configuration, in any branch:

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

bob, who is not in `allow`, changes `.gitlab-ci.yml` in their branch `feature` and pushes. GitLab rejects the push:

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: PATHPOLICY rule "ci-config", branch feature: you may not change these files
remote:   may change them: alice, sre-lead
remote:   files:
remote:     .gitlab-ci.yml
remote:   The platform team changes the CI/CD configuration and the deploy scripts. Ask alice or sre-lead to push this change.
remote:   the content of .gitlab-ci.yml in commit 11e97b832 differs both from this branch and from main; new content is pushed by one of them
remote:   who can help with this rule: #platform
remote:
remote: rules: PATHPOLICY in main (f50c5a0)
remote: checked as: bob (SSH)
remote: GL-HOOK-ERR: gitlab-repopolicy: you may not change .gitlab-ci.yml (PATHPOLICY rule ci-config; may change: alice, sre-lead)
```

What bob does: asks alice to make the change, or removes it from their commits and pushes again:

```sh
git restore --source=origin/main -- .gitlab-ci.yml
git commit --amend --no-edit
git push --force-with-lease origin feature
```

How to read the denial line by line - [pathpolicy.en.md](docs/pathpolicy.en.md#denials).

## Prerequisites

Before the installation you need:
- GitLab Self-Managed, CE or EE, of a version from the table below
- root on every node where Gitaly runs
- an instance service account with a token (`read_api`, `self_rotate`), Maintainer in every top-level group ([install.en.md](docs/install.en.md#3-service-account-and-token))
- `custom_hooks_dir` in the Gitaly configuration, for example `/var/opt/gitlab/gitaly/custom_hooks` in `gitlab.rb` and `gitlab-ctl reconfigure`. Without it Gitaly does not run global hooks
- a `.deb` or `.rpm` package from [GitHub Releases](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases) with a verified signature ([install.en.md](docs/install.en.md#1-download-and-verify-the-release))

## First run

In short, for the Linux package on one node. Docker, separate Gitaly nodes and all the steps in detail - [docs/install.en.md](docs/install.en.md).

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'     # service account token, Enter, Ctrl-D
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

What you should get:
- the package installs the binary `/usr/bin/gitlab-repopolicy`, the configuration `/etc/gitlab-repopolicy/config.yaml` and disabled timers
- `cat` prints nothing, the token goes to a file with mode 0440
- `install-hook` prints `.../pre-receive.d/50-gitlab-repopolicy: created`. From this moment the hook checks pushes
- `selftest` prints `ok`, `info` or `warn` lines, and any `FAIL` gives exit code 1. The result must be the same as root and as `git`, because the hook runs as `git`

For a production instance set `mode: audit` in the configuration before `install-hook`. Then the hook denies nobody and only writes whom it would have denied ([integration-guide.en.md](docs/integration-guide.en.md)).

To check that the hook really works, use a test project. Add a PATHPOLICY to it and make a push that breaks it ([pathpolicy.en.md](docs/pathpolicy.en.md)). The denial is visible in the output of `git push`, the decision in `/var/log/gitlab-repopolicy/audit.log`.

## Supported GitLab versions

| GitLab CE | e2e result |
| --- | --- |
| 19.4 (19.4.2) | supported |
| 19.3 (19.3.5) | supported |
| 19.2 (19.2.8) | supported |

The e2e matrix runs on the current and the two previous minor versions of GitLab CE, nightly on `latest`. EE and any tier are supported by the same code: only Free features are used. GitLab.com and GitLab Dedicated are not supported.

## Editor

Policy files have no extension, so the editor has to be told that they are YAML with a schema. For VS Code with the YAML extension (`yaml-language-server`):

```json
{
  "files.associations": { "PATHPOLICY": "yaml", "COMMITPOLICY": "yaml", "TAGPOLICY": "yaml", "BRANCHPOLICY": "yaml" },
  "yaml.schemas": {
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/v1.0.0/schemas/pathpolicy.schema.json": "PATHPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/v1.0.0/schemas/commitpolicy.schema.json": "COMMITPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/v1.0.0/schemas/tagpolicy.schema.json": "TAGPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/v1.0.0/schemas/branchpolicy.schema.json": "BRANCHPOLICY"
  }
}
```

Check a file locally with the same code as the hook with `gitlab-repopolicy validate` ([validate.en.md](docs/validate.en.md)), commit messages with `gitlab-repopolicy lint-commit`.

## Documentation

| Document | About |
| --- | --- |
| [install.en.md](docs/install.en.md) | installation by installation type, release verification, service account |
| [configuration.en.md](docs/configuration.en.md) | the server `config.yaml` |
| [operations.en.md](docs/operations.en.md) | audit and alerts, recovery, break-glass, token rotation, `settings-check`, upgrades, common problems |
| [pathpolicy.en.md](docs/pathpolicy.en.md), [commitpolicy.en.md](docs/commitpolicy.en.md), [tagpolicy.en.md](docs/tagpolicy.en.md), [branchpolicy.en.md](docs/branchpolicy.en.md) | policy file reference |
| [match.en.md](docs/match.en.md) | the field `match`: which branches and tags a rule applies to; the notation of the diagrams |
| [validate.en.md](docs/validate.en.md) | checking policy files without a push, step by step |
| [cli.en.md](docs/cli.en.md) | all commands and flags |
| [integration-guide.en.md](docs/integration-guide.en.md) | the order of turning it on in a production instance |
| [architecture.en.md](docs/architecture.en.md) | how it works |
| [threat-model.en.md](docs/threat-model.en.md) | what it protects from and what not |
| [development.en.md](docs/development.en.md), [release.en.md](docs/release.en.md) | development and releasing |

## Contributing

[CONTRIBUTING.en.md](CONTRIBUTING.en.md), [CODE_OF_CONDUCT.en.md](CODE_OF_CONDUCT.en.md). Report vulnerabilities privately, see [SECURITY.en.md](SECURITY.en.md).

## License

[Apache-2.0](LICENSE)
