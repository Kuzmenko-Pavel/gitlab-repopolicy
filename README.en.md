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

Server-side checks of repository rules for GitLab Self-Managed. A global `pre-receive` hook of Gitaly checks every push against policy files that live in the repository itself:

| File | What it decides |
| --- | --- |
| [PATHPOLICY](docs/pathpolicy.en.md) | who may change certain files: CI configuration, deploy logic, prod values |
| [COMMITPOLICY](docs/commitpolicy.en.md) | what format commit messages must have |
| [TAGPOLICY](docs/tagpolicy.en.md) | who may release: a tag is accepted only with a signature of an allowed OpenPGP or SSH key |
| [BRANCHPOLICY](docs/branchpolicy.en.md) | how new branches are named and which of them must be protected in GitLab |

## Why

GitLab has no way to let only specific people change specific files on every way a change can arrive. Protected branches protect a whole branch, Code Owners work only in a merge request and are bypassed by a direct push, push rules exist only in Premium and do not know who makes the change. Client-side checks (commitlint, pre-commit) are bypassed with `--no-verify` and do not see the merge button and the Web IDE.

gitlab-repopolicy checks a change on the server, however it was made: a push over HTTPS or SSH, a merge, auto-merge, the Web IDE, the API, a push from CI. The rules are versioned and reviewed together with the code. The hook can only deny: it does not extend GitLab permissions.

```yaml
# examples/ci-config/PATHPOLICY
# The platform team owns the CI configuration, the deploy logic and the
# production values; everything else is authorized by GitLab as usual.
version: 1

admins: [platform-lead, sre-lead]

rules:
  - name: ci-config
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "Makefile"
      - "scripts/deploy/**"
    allow: [platform-lead, sre-lead, ci-maintainer]
    message: >-
      The platform team changes the CI/CD configuration and the deploy logic.
      Ask platform-lead, sre-lead or ci-maintainer to make this change.

  - name: prod-values
    paths: ["helm/**/values-prod.yaml"]
    refs: protected
    allow: [platform-lead, release-manager]
```

## Install in five commands

Before that you need an instance service account with a token and `custom_hooks_dir` in the Gitaly configuration. Step by step, including verification of the release signature - [docs/install.en.md](docs/install.en.md).

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'     # service account token, Ctrl-D
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

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

Check a file locally with the same code as the hook: `gitlab-repopolicy validate`, commit messages with `gitlab-repopolicy lint-commit`.

## Documentation

| Document | About |
| --- | --- |
| [install.en.md](docs/install.en.md) | installation by installation type, release verification, service account |
| [configuration.en.md](docs/configuration.en.md) | the server `config.yaml` |
| [operations.en.md](docs/operations.en.md) | audit and alerts, recovery, break-glass, token rotation, `settings-check`, upgrades, common problems |
| [pathpolicy.en.md](docs/pathpolicy.en.md), [commitpolicy.en.md](docs/commitpolicy.en.md), [tagpolicy.en.md](docs/tagpolicy.en.md), [branchpolicy.en.md](docs/branchpolicy.en.md) | policy file reference |
| [integration-guide.en.md](docs/integration-guide.en.md) | the order of turning it on in a production instance |
| [architecture.en.md](docs/architecture.en.md) | how it works |
| [threat-model.en.md](docs/threat-model.en.md) | what it protects from and what not |
| [development.en.md](docs/development.en.md), [release.en.md](docs/release.en.md) | development and releasing |

## Contributing

[CONTRIBUTING.en.md](CONTRIBUTING.en.md), [CODE_OF_CONDUCT.en.md](CODE_OF_CONDUCT.en.md). Report vulnerabilities privately, see [SECURITY.en.md](SECURITY.en.md).

## License

[Apache-2.0](LICENSE)
