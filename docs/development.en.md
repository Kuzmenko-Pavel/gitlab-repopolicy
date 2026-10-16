[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](development.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](development.en.md)

# Development

## What you need

| Tool | What for |
| --- | --- |
| Go | the version is set by `toolchain` in `go.mod`; with `GOTOOLCHAIN=auto` (the default) Go downloads it itself |
| GNU make 4 or newer | every operation of the project. The default make 3.81 on macOS lacks `.SHELLFLAGS` and does not work |
| git | integration tests make a real push |
| pipx | `yamllint` in `make code-lint` |
| Docker with compose | only for e2e (`make test-e2e`) |

The other tools (golangci-lint, gitleaks, govulncheck, goreleaser, syft, cosign, actionlint, benchstat) make builds itself into `bin/tools/` from the pinned versions in `tools/`. Tests need neither `gpg` nor `ssh-keygen`: they create keys and signatures themselves.

## Make is the only entry point

```sh
make help
```

Build, check and test the project only through make targets: CI calls them the same way. A change is done when these pass:

```sh
make code-lint test-unit test-integration
```

Changes to identity, the GitLab API client or installation also need `make test-e2e`.

| Target | What it does |
| --- | --- |
| `code-fmt` | formats Go code |
| `code-lint` | golangci-lint with the package dependency rules, tidy of every module, actionlint, yamllint, `schemas/` matching the code, documentation language pairs and links |
| `code-lint-commits` | commit messages since `BASE_REF` against `COMMITPOLICY` from `BASE_REF` |
| `code-generate` | regenerates `schemas/` and the golden files of push output |
| `code-build` | the binary in `bin/gitlab-repopolicy` |
| `test-unit` | unit tests with the race detector |
| `test-integration` | scenarios on a real `git receive-pack` with the git from `GIT_BIN`, the policy corpus |
| `test-coverage` | unit and integration with coverage into `coverage/` |
| `test-fuzz` | every fuzz target for `FUZZTIME` |
| `test-load` | hook benchmarks |
| `test-e2e`, `test-e2e-down` | the GitLab CE stand and scenarios on it |
| `security-sast`, `security-dependency-scan`, `security-secret-scan` | gosec, govulncheck, gitleaks |
| `release-snapshot` | archives and packages without signing and publishing |

Local values of make variables (`GIT_BIN`, `GITLAB_VERSION`, `FUZZTIME` and others) can go into `.env`, a sample is `.env.example`. `.env` is not committed.

## Repository layout

```text
cmd/gitlab-repopolicy/   main
internal/                all product code, the package map is in architecture.en.md
schemas/                 JSON Schema of policy files, generated from code
config/                  config.yaml of the package and the config of the release tag check
packaging/               package scripts and systemd units
examples/                policy file examples; part of the test corpus
tests/integration/       scenarios on a real git receive-pack
tests/e2e/               scenarios on GitLab CE
tests/corpus/            the policy compatibility corpus: valid and invalid by module
tools/                   versions of development tools
docs/                    documentation
```

## Tests

**Unit** tests live next to the code. The GitLab API is replaced by a fake server, git by interfaces, time is fixed.

**Integration** tests (`tests/integration`) make a real push: git itself creates the quarantine, passes stdin and calls the hook, only the GitLab API is replaced. The git version affects the result, so repeat the run with the git that Gitaly ships in your GitLab versions:

```sh
make test-integration GIT_BIN=/path/to/git
```

How to get git from the GitLab image (amd64):

```sh
v=19.4.2-ce.0; d=bin/gitaly-git/$v
mkdir -p "$d/lib"
id=$(docker create gitlab/gitlab-ce:$v)
docker cp "$id:/opt/gitlab/embedded/bin/gitaly-git-prev" "$d/"
for l in libiconv.so.2 libpcre2-8.so.0 libz.so.1 libcrypto.so.3; do
  docker cp -L "$id:/opt/gitlab/embedded/lib/$l" "$d/lib/"
done
docker rm "$id"
cat > "$d/git" <<'EOF'
#!/bin/sh
dir=$(dirname "$(readlink -f "$0")")
exec /lib64/ld-linux-x86-64.so.2 --library-path "$dir/lib" --argv0 "$0" "$dir/gitaly-git-prev" "$@"
EOF
chmod +x "$d/git"
for n in git-receive-pack git-upload-pack; do ln -sf git "$d/$n"; done
make test-integration GIT_BIN=$d/git
```

Gitaly has several git builds; the hook gets `gitaly-git-prev`. The wrapper passes the libraries to the loader without changing the environment of git. The test checks that the hook really sees the git from `GIT_BIN`.

**Compatibility corpus.** `tests/corpus/<module>/valid` holds files that the current and every next version must accept, `invalid` holds files they must reject. Together with `examples/` the corpus only grows: files in `valid` are never deleted or changed. A release checks the previous minor version against the same PATHPOLICY and TAGPOLICY files. Every policy example in the documentation matches a file from `examples/` word for word: a test requires it.

**Fuzz.** The targets live in the packages next to the code, the corpus in `testdata/fuzz/`. `make test-fuzz` runs each one for `FUZZTIME` (30 s by default, 10 minutes in nightly). A crash found stays in `testdata/fuzz/` together with the fix.

**Load.** `make test-load` runs hook benchmarks on synthetic repositories: 100 thousand files, a diff of 50 thousand paths, a merge request of 500 commits, the fast path without a policy.

## e2e stand

```sh
make test-e2e
make test-e2e-down
```

`test-e2e` builds the binary for Linux, starts GitLab CE `GITLAB_VERSION` and gitlab-runner in docker compose, prepares the stand through the API (users, groups, service account, projects, SSH keys, runner), installs the hook with `install-hook` and runs the scenarios from `tests/e2e`. The first start of GitLab takes a few minutes; the stand lives between runs, a repeated `make test-e2e` only prepares the state again. `test-e2e-down` removes the stand with its volumes.

| Variable | Default | Meaning |
| --- | --- | --- |
| `GITLAB_VERSION` | the current minor version | the `gitlab/gitlab-ce` image tag |
| `E2E_HTTPS_PORT`, `E2E_SSH_PORT` | `8443`, `2222` | ports of the stand on `127.0.0.1` |
| `E2E_STATE_ROOT` | `bin/e2e-stand` | the test CA, SSH keys and tokens of the stand, a directory per stand |
| `E2E_NIGHTLY` | empty | `1` adds the nightly scenario with parallel merges |
| `COMPOSE_PROJECT_NAME` | from the GitLab version | the stand name; every version has its own |

Keep one stand at a time: GitLab CE needs several GB of memory.

## The project checks itself with its own tool

The project checks itself with its own binary: commits against [COMMITPOLICY](../COMMITPOLICY), release tags against [TAGPOLICY](../TAGPOLICY).

```sh
make code-lint-commits BASE_REF=origin/main
```

How to shape commits and pull requests - [CONTRIBUTING.en.md](../CONTRIBUTING.en.md).
