[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](CONTRIBUTING.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](CONTRIBUTING.en.md)

# Contributing

Report bugs or propose changes in [issues](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/issues). Open a pull request for a fix. Report vulnerabilities privately following [SECURITY](SECURITY.en.md).

## Before a pull request

- Discuss a larger change (a new module, a new COMMITPOLICY rule, a change of hook behaviour) in an issue first
- Prepare tools using the [development guide](docs/development/development.en.md). GNU make 4 or newer is required; `make help` lists available operations
- A change is done when `make code-lint test-unit test-integration` pass. Changes to identity, the GitLab API client or installation also need `make test-e2e`; label such a pull request `e2e`. ci and e2e run manually on any branch: Actions > e2e > Run workflow

## Commits

- [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/): `type(scope): subject`, types `feat`, `fix`, `build`, `chore`, `ci`, `docs`, `perf`, `refactor`, `revert`, `style`, `test`, the subject starts with a lowercase letter and has no full stop at the end, the header is up to 100 characters
- Every commit is signed off under the [DCO](https://developercertificate.org/): `git commit -s` adds `Signed-off-by`. This confirms that you have the right to contribute the code under the license of the project. There is no CLA
- Messages are in English

The rules are in [COMMITPOLICY](COMMITPOLICY), and CI checks the commits and the pull request title against them with the same binary as on GitLab. Locally:

```sh
make code-lint-commits BASE_REF=origin/main
```

A pull request is merged with squash: the PR title becomes the header of the commit, so it must follow Conventional Commits too.

## What to know about the code

- Code, comments, godoc and the output of the binary are in English
- A new Go dependency only with a written justification in the pull request: every dependency ends up in a binary that runs on GitLab servers
- Follow the [package responsibilities](docs/development/architecture.en.md#main-components); the linter checks dependencies
- The PATHPOLICY and TAGPOLICY `version: 1` schemas are frozen, denial codes and the audit event format are a public contract
- `tests/corpus/*/valid/` only grows: every next version must accept these files

## Documentation

Write documentation in Ukrainian (`<name>.md`) with an English translation alongside it (`<name>.en.md`). Update both files in the same pull request and check that conditions, examples and expected results agree.

Use `make test-docs` for a short feedback cycle. It recursively checks language pairs, relative links and anchors, complete policy blocks and configuration quotes, and executes marked tables and CLI examples. A quote starting with `# examples/...` or `# config/...` must exactly match that file. For diagrams, run `make test-docs-mermaid` with Mermaid CLI (`mmdc`) installed. Run the complete required checks before review too.

## Review

A maintainer reviews the pull request (see `.github/CODEOWNERS`). It is merged after green CI and an approval.

## Agents

Rules for AI agents working with the repository are in [AGENTS.md](AGENTS.md).
