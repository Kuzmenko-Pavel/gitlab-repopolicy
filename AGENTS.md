# AGENTS.md

Operational contract for AI and code agents working in this repository. Product documentation lives in [README.md](README.md) and [docs/](docs/); this file does not repeat it.

## Project

`gitlab-repopolicy` is a Go binary for the global `pre-receive.d` hook of GitLab Gitaly. It enforces repository-local policies from `PATHPOLICY`, `COMMITPOLICY`, `TAGPOLICY` and `BRANCHPOLICY` on every push. Architecture: [docs/architecture.md](docs/architecture.md).

## Commands

`make help` is the only entry point to build, check and test the project. Do not call `go test`, `golangci-lint` or other tools directly in instructions, scripts or CI; add or change a make target instead.

A change is done when these targets pass:

```sh
make code-lint test-unit test-integration
```

Changes to identity, the GitLab API client or installation also need `make test-e2e`.

## Packages and dependency rules

| Package | Responsibility |
| --- | --- |
| `cmd/gitlab-repopolicy` | `main`: signals, `cli.Run`, exit code |
| `internal/cli` | subcommands, composition root: config, clients, module list |
| `internal/core` | anchor, policy loading, acceptance, module order, budgets, aggregation |
| `internal/module` | module contract: `Module`, `Policy`, `Input`, `Result` |
| `internal/modules` | `Default(Settings)`, the module list of the hook |
| `internal/modules/{path,commit,tag,branch}` | PATHPOLICY, COMMITPOLICY, TAGPOLICY, BRANCHPOLICY |
| `internal/{changes,gitx,refs,identity,gitlab,hookenv,config,strictyaml,report,audit}` | core infrastructure |
| `internal/{install,settings,coverage,token,breakglass}` | operational commands, called only from `cli` |
| `internal/{spec,schema,buildinfo}` | format of policy files and its validator, JSON Schema generation, build metadata |
| `internal/testmodule` | policy module of the tests of the core, imported only by tests and by the test hook `tests/integration/internal/testhook` |

Rules, enforced by `depguard` and `forbidigo` in `make code-lint`:

- `module` depends only on the standard library and `audit`
- `modules/*` do not import `core`, `gitx`, `gitlab`, `config`, `cli`; git and the API reach a module only through `Input`, settings through `Settings`
- `core` does not import `modules/*`
- `cli` calls `modules/path` and `modules/commit` directly for `explain`, `admins`, `lint-commit` and `selftest`, `modules/branch` and `modules/tag` for `validate --branch` and `--tag`; everything else gets modules from `modules.Default`
- `go-crypto` and `x/crypto/ssh` only in `modules/tag`; `yaml` only in `strictyaml`; `doublestar` only in `modules/path`
- `os.Exit`, `os.Args`, `os.Getenv`, `os.LookupEnv`, `os.Environ` only in `cmd` and `cli`
- `time.Now` only through the injected clock, except in `cmd` and `cli`
- these `forbidigo` rules do not apply to tests, `tests/integration/internal/testhook` and `tools/release-notes`

## Invariants

Do not weaken these, not even in test configuration:

- PATHPOLICY and TAGPOLICY fail closed; COMMITPOLICY and BRANCHPOLICY fail open only where the documentation says so
- policy is read only from the anchor: the tip of the default branch before the push
- `version: 1` schemas of PATHPOLICY and TAGPOLICY are frozen; COMMITPOLICY only gains new rules in its catalogue
- refusal codes and the audit event format are a public contract
- no new Go dependency without a written justification in the pull request

## Do not change without an explicit request

- `TAGPOLICY`, `COMMITPOLICY`
- `.github/workflows/`
- `tools/`
- `tests/corpus/*/valid/`

## Language

Code, comments, godoc, commit messages and binary output are in English. Documentation is Ukrainian (`<name>.md`) with an English translation (`<name>.en.md`); a change to a document updates both files in the same pull request. This file has no translation.
