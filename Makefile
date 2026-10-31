SHELL := /bin/bash
.SHELLFLAGS := -euo pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-builtin-rules --warn-undefined-variables
-include .env
export

# GitHub event variables; empty locally.
GITHUB_REF_TYPE   ?=
GITHUB_REF_NAME   ?=
GITHUB_RUN_ID     ?=
GITHUB_EVENT_PATH ?=
# Set by GitHub Actions and by act; release-publish runs only on GitHub.
GITHUB_ACTIONS    ?=
ACT               ?=

# Version. The tag is vX.Y.Z (Go modules requirement); the binary, packages and audit use X.Y.Z.
# In a release the tag comes from the GitHub event, locally from GIT_STAMP of the standard.
GIT_STAMP   ?= $(shell git describe 2>/dev/null || echo v0.1.0)
RELEASE_TAG ?= $(if $(filter tag,$(GITHUB_REF_TYPE)),$(GITHUB_REF_NAME),$(GIT_STAMP))
VERSION     ?= $(patsubst v%,%,$(RELEASE_TAG))
COMMIT      ?= $(shell git rev-parse --short=12 HEAD)
# Commit date, not the clock: the build is reproducible.
BUILD_DATE  ?= $(shell git log -1 --format=%cI)
# Evaluate once: ?= creates a recursive variable, and with export
# $(shell) would run on every recipe line.
GIT_STAMP   := $(GIT_STAMP)
RELEASE_TAG := $(RELEASE_TAG)
VERSION     := $(VERSION)
COMMIT      := $(COMMIT)
BUILD_DATE  := $(BUILD_DATE)

# Module path from go.mod, not from "go list -m": help and other targets do not need
# a Go toolchain, and $(shell) of make 4.3 does not see exported GOTOOLCHAIN.
GO_MODULE := $(shell sed -n 's/^module //p' go.mod)
TOOLS_BIN := $(CURDIR)/bin/tools
export PATH := $(TOOLS_BIN):$(PATH)
LDFLAGS   = -s -w -X $(GO_MODULE)/internal/buildinfo.Version=$(VERSION) \
            -X $(GO_MODULE)/internal/buildinfo.Commit=$(COMMIT) \
            -X $(GO_MODULE)/internal/buildinfo.Date=$(BUILD_DATE)
# Not exported: only the go build lines below use them.
unexport GO_MODULE LDFLAGS

# Start of the range for code-lint-commits; set by the workflow in CI.
BASE_REF       ?= origin/main
# git for integration: system git or the git bundled with Gitaly.
GIT_BIN        ?= git
# Optional Mermaid CLI arguments, for example a local Puppeteer configuration.
MERMAID_FLAGS  ?=
# Tests run from their package directories, so a path in GIT_BIN is made absolute.
GIT_BIN_ABS     = $(if $(findstring /,$(GIT_BIN)),$(abspath $(GIT_BIN)),$(GIT_BIN))
# gitlab/gitlab-ce image for e2e.
GITLAB_VERSION ?= 19.4.2-ce.0
FUZZTIME       ?= 30s
# Previous minor version for the rollback check, without v; computed from
# tags v* when not given: the last release, not rc, of a lower minor version.
COMPAT_PREV_VERSION ?=
COMPAT_PREV_TAG     := v$(COMPAT_PREV_VERSION)
# Local release check: directories with already downloaded archives and checksums.txt.
COMPAT_PREV_DIR     ?=
RELEASE_ASSETS_DIR  ?=
# Version for make release, without v; asked interactively when empty.
RELEASE_VERSION ?=
COMPOSE_PROJECT_NAME ?= gitlab-repopolicy-$(or $(GITHUB_RUN_ID),local)-$(subst .,-,$(GITLAB_VERSION))
# e2e stand: runner image, ports on 127.0.0.1, state (test CA, SSH keys, tokens) outside the repo volumes.
GITLAB_RUNNER_VERSION ?= v19.4.1
E2E_HTTPS_PORT ?= 8443
E2E_SSH_PORT   ?= 2222
E2E_STATE_ROOT ?= $(CURDIR)/bin/e2e-stand
E2E_STATE_DIR  := $(abspath $(E2E_STATE_ROOT))/$(COMPOSE_PROJECT_NAME)
# Linux build of the binary for the stand, mounted read-only as /usr/bin/gitlab-repopolicy.
E2E_BINARY     := $(CURDIR)/bin/e2e/gitlab-repopolicy
# E2E_NIGHTLY=1 adds the nightly scenarios (parallel merges) to test-e2e.
E2E_NIGHTLY    ?=
comma          := ,
E2E_TAGS       := e2e$(if $(filter 1,$(E2E_NIGHTLY)),$(comma)nightly)

# yamllint is not a Go tool; its version is pinned here and run through pipx.
YAMLLINT_VERSION := 1.38.0
# Go tools, one module per tool under tools/<name>.
TOOLS  := actionlint benchstat cosign deadcode gitleaks golangci-lint goreleaser govulncheck syft
BINARY := bin/gitlab-repopolicy
# Dependabot capitalizes "Bump" from the commit history and resets the PR title on
# every rebase, so its commits and PR titles are not linted. The squash commit on
# main is linted on push; merge with gh pr merge --subject in lower case.
DEPENDABOT_EMAIL := 49699333+dependabot[bot]@users.noreply.github.com
DEPENDABOT_LOGIN := dependabot[bot]
# Repository slug for release URLs and keyless signature identity.
GITHUB_REPOSITORY_SLUG := Kuzmenko-Pavel/gitlab-repopolicy
# Consumer verification of release assets in the current directory.
VERIFY_ASSETS := cosign verify-blob checksums.txt --bundle checksums.txt.sigstore.json \
	--certificate-identity-regexp '^https://github.com/$(GITHUB_REPOSITORY_SLUG)/\.github/workflows/release\.yml@refs/tags/v' \
	--certificate-oidc-issuer https://token.actions.githubusercontent.com \
	&& sha256sum --check --ignore-missing checksums.txt

# Colors for help.
GREEN  := $(shell tput -Txterm setaf 2 2>/dev/null || true)
YELLOW := $(shell tput -Txterm setaf 3 2>/dev/null || true)
WHITE  := $(shell tput -Txterm setaf 7 2>/dev/null || true)
RESET  := $(shell tput -Txterm sgr0  2>/dev/null || true)
GRAY   := $(shell tput -Txterm setaf 6 2>/dev/null || true)
TARGET_MAX_CHAR_NUM ?= 30

# Tool binaries, one module per tool under tools/<name>.
$(TOOLS_BIN)/%: tools/%/go.mod tools/%/go.sum
	go -C tools/$* build -o $(TOOLS_BIN)/ tool
	touch $@

# release-notes is a program of this repository, not a third-party tool.
$(TOOLS_BIN)/release-notes: tools/release-notes/go.mod $(wildcard tools/release-notes/*.go)
	go -C tools/release-notes build -o $(TOOLS_BIN)/ .

.PHONY: code-fmt
## Format Go code. | Code
code-fmt: $(TOOLS_BIN)/golangci-lint
	golangci-lint fmt

.PHONY: code-lint
## Lint code, modules, workflows and YAML.
code-lint: $(TOOLS_BIN)/golangci-lint $(TOOLS_BIN)/actionlint code-deadcode
	golangci-lint config verify
	golangci-lint run
	golangci-lint fmt --diff
	cd tools/release-notes && golangci-lint run --config $(CURDIR)/.golangci.yml ./...
	go mod tidy -diff
	for t in $(TOOLS) release-notes; do go -C tools/$$t mod tidy -diff; done
	go test -count=1 -run '^TestSchemas$$' ./internal/schema
	actionlint
	pipx run yamllint==$(YAMLLINT_VERSION) --strict .

.PHONY: code-deadcode
## Find functions that no main reaches: the hook, the test hook, release-notes.
code-deadcode: $(TOOLS_BIN)/deadcode
	dead="$$(deadcode ./cmd/... ./tests/integration/internal/testhook; cd tools/release-notes && deadcode .)"; \
	if [ -n "$$dead" ]; then echo "$$dead"; echo "code-deadcode: delete the functions above or call them" >&2; exit 1; fi

.PHONY: code-lint-commits
## Check commit messages and the PR title against COMMITPOLICY from the base.
code-lint-commits: code-build
	zero='^0+$$'; \
	if [ "$(GITHUB_REF_TYPE)" = tag ]; then \
		if ! [[ "$$RELEASE_TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.(0|[1-9][0-9]*))?$$ ]]; then \
			echo "code-lint-commits: the tag does not match vX.Y.Z or vX.Y.Z-rc.N" >&2; exit 1; \
		fi; \
		git fetch --force --tags origin; \
		tip=$$(git rev-parse --verify "refs/tags/$(RELEASE_TAG)^{commit}"); \
		policy=$$tip; \
		if prev=$$(git describe --abbrev=0 --match 'v*' --exclude "$(RELEASE_TAG)" "$$tip" 2>/dev/null); then \
			range="$$prev..$$tip"; \
		else \
			range=$$tip; \
		fi; \
	elif [[ "$(BASE_REF)" =~ $$zero ]]; then \
		tip=$$(git rev-parse --verify HEAD); policy=$$tip; range=$$tip; \
	else \
		tip=$$(git rev-parse --verify HEAD); policy=$(BASE_REF); range="$(BASE_REF)..$$tip"; \
	fi; \
	echo "code-lint-commits: range $$range, COMMITPOLICY from $$policy"; \
	git show "$$policy:COMMITPOLICY" > bin/COMMITPOLICY.base; \
	$(BINARY) lint-commit --range "$$range" -f bin/COMMITPOLICY.base --skip-author '$(DEPENDABOT_EMAIL)'; \
	if [ -n "$(GITHUB_EVENT_PATH)" ] && jq -e '.pull_request' "$(GITHUB_EVENT_PATH)" > /dev/null; then \
		if [ "$$(jq -r '.pull_request.user.login // ""' "$(GITHUB_EVENT_PATH)")" = '$(DEPENDABOT_LOGIN)' ]; then \
			echo "code-lint-commits: pull request of $(DEPENDABOT_LOGIN), the title is linted as the squash commit on main"; \
			exit 0; \
		fi; \
		title=$$(jq -r '.pull_request.title' "$(GITHUB_EVENT_PATH)"); \
		number=$$(jq -r '.pull_request.number' "$(GITHUB_EVENT_PATH)"); \
		msg="$$title (#$$number)"; \
		for c in $$(git rev-list --reverse "$$range"); do \
			msg+=$$'\n\n'"* $$(git log -1 --format=%B "$$c")"; \
		done; \
		echo "code-lint-commits: squash message of pull request #$$number"; \
		$(BINARY) lint-commit -m "$$msg" -f bin/COMMITPOLICY.base; \
	fi

.PHONY: code-generate
## Regenerate schemas/ from the specs of the modules and the golden files of push output.
code-generate:
	go test ./internal/schema -run '^TestSchemas$$' -update
	go test ./internal/report -run '^TestGolden$$' -update
	go test ./internal/audit -run '^TestGolden$$' -update

.PHONY: code-deps-update
## Update dependencies of the product and of every tool module.
code-deps-update:
	go get -u ./...
	go mod tidy
	for t in $(TOOLS); do go -C tools/$$t get -u tool && go -C tools/$$t mod tidy; done

.PHONY: code-build
## Build the binary for the current platform into bin/. | Build
code-build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/gitlab-repopolicy

.PHONY: code-build-info
## Print build metadata: version, commit, date, Go version.
code-build-info:
	@echo "version: $(VERSION)"
	@echo "commit:  $(COMMIT)"
	@echo "date:    $(BUILD_DATE)"
	@echo "go:      $$(go env GOVERSION)"

.PHONY: clean
## Remove bin/, dist/, coverage/ and the test cache.
clean:
	rm -rf bin dist coverage
	go clean -testcache

.PHONY: test-unit
## Run unit tests with the race detector. | Tests
test-unit:
	go test -race -shuffle=on ./...
	go -C tools/release-notes test -race -shuffle=on ./...

.PHONY: test-docs
## Check documentation links, language pairs, examples, tables and CLI output.
test-docs:
	GIT_BIN="$(GIT_BIN_ABS)" go test -count=1 -race -tags integration ./tests/integration/... -run '^Test(Docs|MatchDocs|CLIDocs|ValidateGuide|Examples|PathPolicyExamples)'

.PHONY: test-docs-mermaid
## Render documentation diagrams; requires mmdc (Mermaid CLI) in PATH.
test-docs-mermaid:
	find docs -name '*.md' -exec grep -l '^```mermaid' {} + | while IFS= read -r doc; do \
		mkdir -p "bin/docs-mermaid/$$(dirname "$$doc")"; \
		mmdc --input "$$doc" --output "bin/docs-mermaid/$$doc" $(MERMAID_FLAGS); \
	done

.PHONY: test-integration
## Run integration tests on a real git receive-pack with GIT_BIN.
test-integration: $(TOOLS_BIN)/release-notes
	if [ -z "$$(go list -tags integration ./tests/integration/... 2>/dev/null)" ]; then \
		echo "test-integration: no integration test packages under tests/integration" >&2; exit 1; \
	fi
	GIT_BIN="$(GIT_BIN_ABS)" go test -count=1 -race -shuffle=on -tags integration ./tests/integration/...
	prev="$(COMPAT_PREV_VERSION)"; \
	if [ -z "$$prev" ]; then \
		if [ "$(GITHUB_REF_TYPE)" = tag ] && ! git fetch --quiet --force --tags origin; then \
			echo "test-integration: cannot fetch the tags for the rollback check" >&2; exit 1; \
		fi; \
		prev=$$(release-notes previous-minor "$(VERSION)"); \
	fi; \
	if [ -z "$$prev" ]; then \
		echo "test-integration: rollback check skipped: no release of a lower minor version than $(VERSION) among the tags v*"; \
	else \
		$(MAKE) --no-print-directory test-integration-compat COMPAT_PREV_VERSION="$$prev"; \
	fi

# The rollback check of test-integration: the previous minor
# release, verified like a consumer does, accepts every valid PATHPOLICY and
# TAGPOLICY of the corpus and of examples/.
.PHONY: test-integration-compat
test-integration-compat: $(TOOLS_BIN)/cosign
	dir=bin/compat/$(COMPAT_PREV_VERSION); rm -rf "$$dir"; mkdir -p "$$dir"; \
	case "$$(uname -m)" in x86_64) arch=amd64 ;; aarch64 | arm64) arch=arm64 ;; *) echo "test-integration: unsupported arch $$(uname -m)" >&2; exit 1 ;; esac; \
	archive="gitlab-repopolicy_$(COMPAT_PREV_VERSION)_linux_$$arch.tar.gz"; \
	if [ -n "$(COMPAT_PREV_DIR)" ]; then \
		cp "$(COMPAT_PREV_DIR)/checksums.txt" "$(COMPAT_PREV_DIR)/$$archive" "$$dir/"; \
		echo "test-integration: $(COMPAT_PREV_TAG) from COMPAT_PREV_DIR, cosign verify-blob skipped"; \
		(cd "$$dir" && sha256sum --check --ignore-missing checksums.txt); \
	else \
		base="https://github.com/$(GITHUB_REPOSITORY_SLUG)/releases/download/$(COMPAT_PREV_TAG)"; \
		for f in checksums.txt checksums.txt.sigstore.json "$$archive"; do \
			curl --fail --silent --show-error --location --output "$$dir/$$f" "$$base/$$f"; \
		done; \
		(cd "$$dir" && $(VERIFY_ASSETS)); \
	fi; \
	tar -xzf "$$dir/$$archive" -C "$$dir" gitlab-repopolicy; \
	"$$dir/gitlab-repopolicy" version; \
	n=0; \
	for module in pathpolicy tagpolicy; do \
		file=$$(echo "$$module" | tr '[:lower:]' '[:upper:]'); \
		for f in tests/corpus/$$module/valid/* $$(find examples -type f -name "$$file" 2>/dev/null | sort); do \
			"$$dir/gitlab-repopolicy" validate -f "$$f" --module "$$module" > /dev/null || { \
				echo "test-integration: $(COMPAT_PREV_VERSION) rejects $$f" >&2; exit 1; }; \
			n=$$((n + 1)); \
		done; \
	done; \
	echo "test-integration: $(COMPAT_PREV_VERSION) accepts all $$n valid PATHPOLICY and TAGPOLICY files"

.PHONY: test-coverage
## Run unit tests with coverage into coverage/; integration tests are not counted.
test-coverage:
	rm -rf coverage; mkdir -p coverage/unit
	go test -count=1 -cover -covermode=atomic -coverpkg=./... ./... -args -test.gocoverdir="$(CURDIR)/coverage/unit"
	go tool covdata textfmt -i=coverage/unit -o coverage/coverage.out
	go tool cover -func=coverage/coverage.out | tail -n 1

.PHONY: test-fuzz
## Run every fuzz target for FUZZTIME each.
test-fuzz:
	targets=0; \
	for p in $$(go list ./...); do \
		for f in $$(go test -list '^Fuzz' "$$p" | grep '^Fuzz' || true); do \
			targets=$$((targets + 1)); \
			echo "test-fuzz: $$p $$f ($(FUZZTIME))"; \
			go test -run '^$$' -fuzz "^$$f$$" -fuzztime "$(FUZZTIME)" "$$p"; \
		done; \
	done; \
	if [ "$$targets" -eq 0 ]; then echo "test-fuzz: no fuzz targets found" >&2; exit 1; fi

.PHONY: test-load
## Run benchmarks and compare them with BASE_REF.
test-load: $(TOOLS_BIN)/benchstat
	if [ -z "$$(go test -tags integration -list '^Benchmark' ./tests/integration/... 2>/dev/null | grep '^Benchmark' || true)" ]; then \
		echo "test-load: no benchmarks under tests/integration" >&2; exit 1; \
	fi
	mkdir -p bin/bench
	go test -tags integration -run '^$$' -bench . -benchmem -count 6 ./tests/integration/... | tee bin/bench/new.txt
	base=$$(git merge-base HEAD "$(BASE_REF)"); \
	rm -rf bin/bench/base; git worktree add --detach bin/bench/base "$$base" > /dev/null; \
	trap 'git worktree remove --force bin/bench/base' EXIT; \
	if [ -z "$$(cd bin/bench/base && go test -tags integration -list '^Benchmark' ./tests/integration/... 2>/dev/null | grep '^Benchmark' || true)" ]; then \
		echo "test-load: $(BASE_REF) has no benchmarks, nothing to compare"; \
	else \
		(cd bin/bench/base && go test -tags integration -run '^$$' -bench . -benchmem -count 6 ./tests/integration/...) > bin/bench/base.txt; \
		benchstat bin/bench/base.txt bin/bench/new.txt; \
	fi

.PHONY: test-e2e
## Run e2e tests on the GitLab CE GITLAB_VERSION stand in docker compose. | E2e stand
test-e2e:
	mkdir -p bin/e2e
	CGO_ENABLED=0 GOOS=linux GOARCH=$$(docker version --format '{{.Server.Arch}}') \
		go build -trimpath -ldflags "$(LDFLAGS)" -o $(E2E_BINARY).new ./cmd/gitlab-repopolicy
	# Overwrite in place: the file is bind-mounted, a new inode would not reach the container.
	cmp -s $(E2E_BINARY).new $(E2E_BINARY) || cp $(E2E_BINARY).new $(E2E_BINARY)
	rm -f $(E2E_BINARY).new
	d="$(E2E_STATE_DIR)/tls"; \
	if [ ! -f "$$d/gitlab.crt" ]; then \
		mkdir -p "$$d" && chmod 700 "$(E2E_STATE_DIR)"; \
		openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 365 \
			-subj "/CN=gitlab-repopolicy e2e CA" -keyout "$$d/ca.key" -out "$$d/ca.crt" \
			-addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign 2> /dev/null; \
		openssl req -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -subj /CN=localhost \
			-keyout "$$d/gitlab.key" -out "$$d/gitlab.csr" 2> /dev/null; \
		openssl x509 -req -in "$$d/gitlab.csr" -CA "$$d/ca.crt" -CAkey "$$d/ca.key" -days 365 \
			-extfile <(printf 'subjectAltName=DNS:localhost,DNS:gitlab,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n') \
			-out "$$d/gitlab.crt" 2> /dev/null; \
	fi
	docker compose up --detach --wait --wait-timeout 1200
	go test -tags $(E2E_TAGS) -count=1 -v -timeout 60m ./tests/e2e/...

.PHONY: test-e2e-logs
## Copy the audit log and the GitLab logs of the e2e stand into bin/e2e-logs/.
test-e2e-logs:
	rm -rf bin/e2e-logs; mkdir -p bin/e2e-logs
	for f in /var/log/gitlab-repopolicy/audit.log /var/log/gitlab/gitaly/current /var/log/gitlab/gitlab-rails/production_json.log \
		/var/log/gitlab/gitlab-rails/api_json.log /var/log/gitlab/sidekiq/current /var/log/gitlab/gitlab-workhorse/current; do \
		docker compose cp "gitlab:$$f" "bin/e2e-logs/$$(echo "$${f#/var/log/}" | tr / -)" || echo "test-e2e-logs: $$f is not copied" >&2; \
	done

.PHONY: test-e2e-down
## Remove the e2e stand COMPOSE_PROJECT_NAME with its volumes.
test-e2e-down:
	docker compose down --volumes --remove-orphans
	rm -rf "$(E2E_STATE_DIR)"

.PHONY: security-sast
## Static analysis with gosec, SARIF report in bin/reports/. | Security
security-sast: $(TOOLS_BIN)/golangci-lint
	mkdir -p bin/reports
	golangci-lint run --enable-only gosec --output.text.path=stdout --output.sarif.path=bin/reports/gosec.sarif

.PHONY: security-dependency-scan
## Check reachable code for known vulnerabilities.
security-dependency-scan: $(TOOLS_BIN)/govulncheck
	govulncheck ./...

.PHONY: security-secret-scan
## Scan the branch history for secrets.
security-secret-scan: $(TOOLS_BIN)/gitleaks
	gitleaks git --redact --no-banner --verbose .

.PHONY: version
## Print the product version (without the v prefix). | Release
version:
	@echo "$(VERSION)"

.PHONY: release
## Create the signed tag vRELEASE_VERSION on main with a changelog from Conventional Commits.
release: $(TOOLS_BIN)/release-notes
	@if [ -n "$$(git status --porcelain)" ]; then \
		echo "$(YELLOW)release: there are uncommitted changes; commit them before creating the release tag$(RESET)" >&2; exit 1; \
	fi
	@branch=$$(git symbolic-ref --short -q HEAD || true); \
	if [ "$$branch" != main ]; then \
		echo "$(YELLOW)release: run on main (current: $${branch:-detached HEAD})$(RESET)" >&2; exit 1; \
	fi
	@git fetch --quiet --tags origin main
	@if [ "$$(git rev-parse HEAD)" != "$$(git rev-parse FETCH_HEAD)" ]; then \
		echo "$(YELLOW)release: local main differs from origin/main; pull or push first$(RESET)" >&2; exit 1; \
	fi
	@prev=$$(git describe --abbrev=0 --match 'v*' 2>/dev/null || true); \
	version="$(RELEASE_VERSION)"; \
	if [ -z "$$version" ]; then \
		printf 'Version without v (previous release %s): ' "$${prev:-none}"; read -r version; \
	fi; \
	if ! [[ "$$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.(0|[1-9][0-9]*))?$$ ]]; then \
		echo "$(YELLOW)release: $$version is not X.Y.Z or X.Y.Z-rc.N$(RESET)" >&2; exit 1; \
	fi; \
	tag="v$$version"; \
	if git rev-parse --quiet --verify "refs/tags/$$tag" > /dev/null; then \
		echo "$(YELLOW)release: tag $$tag already exists$(RESET)" >&2; exit 1; \
	fi; \
	mkdir -p bin; notes="bin/release-notes-$$version.txt"; \
	release-notes notes "$$version" > "$$notes"; \
	git tag -s -a -e -F "$$notes" "$$tag"; \
	echo "$(GREEN)release: created $$tag. Publish it with: git push origin $$tag$(RESET)"

.PHONY: release-snapshot
## Build archives and packages without signing or publishing.
release-snapshot: $(TOOLS_BIN)/goreleaser $(TOOLS_BIN)/syft
	goreleaser release --snapshot --clean --skip=sign

# The format of RELEASE_TAG is checked through the environment before any
# recipe puts the tag into a command line.
.PHONY: release-tag-format
release-tag-format:
	if ! [[ "$$RELEASE_TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.(0|[1-9][0-9]*))?$$ ]]; then \
		printf 'release: RELEASE_TAG %q does not match vX.Y.Z or vX.Y.Z-rc.N\n' "$$RELEASE_TAG" >&2; exit 1; \
	fi

.PHONY: release-verify-tag
## Verify the signature of RELEASE_TAG against the project TAGPOLICY.
release-verify-tag: release-tag-format code-build
	git fetch --force origin "+refs/tags/$(RELEASE_TAG):refs/tags/$(RELEASE_TAG)"
	if [ "$$(git cat-file -t "refs/tags/$(RELEASE_TAG)")" != tag ]; then \
		echo "release-verify-tag: $(RELEASE_TAG) is not an annotated tag" >&2; exit 1; \
	fi
	git fetch --force origin "+refs/heads/main:refs/remotes/origin/main"
	if ! git merge-base --is-ancestor "refs/tags/$(RELEASE_TAG)^{commit}" refs/remotes/origin/main; then \
		echo "release-verify-tag: the commit of $(RELEASE_TAG) is not in main" >&2; exit 1; \
	fi
	tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
	git init --quiet --bare --object-format="$$(git rev-parse --show-object-format)" "$$tmp"; \
	git -C "$$tmp" fetch --quiet "$(CURDIR)" \
		"refs/remotes/origin/main:refs/heads/main" "refs/tags/$(RELEASE_TAG):refs/tags/$(RELEASE_TAG)"; \
	git -C "$$tmp" symbolic-ref HEAD refs/heads/main; \
	new=$$(git rev-parse "refs/tags/$(RELEASE_TAG)"); \
	old=$$(printf '0%.0s' $$(seq $${#new})); \
	$(BINARY) test --repo "$$tmp" --old "$$old" --new "$$new" --ref "refs/tags/$(RELEASE_TAG)" \
		--user release --config config/release-verify.yaml | tee bin/release-verify.audit; \
	if ! jq -e --arg ref "refs/tags/$(RELEASE_TAG)" \
		'select(.event == "decision" and .module == "tagpolicy" and .decision == "allow" and .ref == $$ref)' \
		bin/release-verify.audit > /dev/null; then \
		echo "release-verify-tag: no tagpolicy allow decision for $(RELEASE_TAG); TAGPOLICY is missing or no rule matched" >&2; exit 1; \
	fi

.PHONY: release-reproduce
## Rebuild the latest release tag and compare binaries with the release archives.
release-reproduce: $(TOOLS_BIN)/goreleaser $(TOOLS_BIN)/cosign
	tag="$(RELEASE_TAG)"; \
	if ! git rev-parse --quiet --verify "refs/tags/$$tag" > /dev/null; then \
		if ! tag=$$(git describe --abbrev=0 --match 'v*' 2>/dev/null); then \
			echo "release-reproduce: no release tag v* reachable from HEAD" >&2; exit 1; \
		fi; \
	fi; \
	version=$${tag#v}; \
	echo "release-reproduce: $$tag"; \
	rm -rf bin/reproduce; mkdir -p bin/reproduce/assets bin/reproduce/release; \
	if [ -n "$(RELEASE_ASSETS_DIR)" ]; then \
		cp "$(RELEASE_ASSETS_DIR)"/checksums.txt "$(RELEASE_ASSETS_DIR)"/gitlab-repopolicy_"$$version"_linux_*.tar.gz bin/reproduce/assets/; \
		echo "release-reproduce: local assets from RELEASE_ASSETS_DIR, cosign verify-blob skipped"; \
		(cd bin/reproduce/assets && sha256sum --check --ignore-missing checksums.txt); \
	else \
		base="https://github.com/$(GITHUB_REPOSITORY_SLUG)/releases/download/$$tag"; \
		for f in checksums.txt checksums.txt.sigstore.json gitlab-repopolicy_"$$version"_linux_amd64.tar.gz gitlab-repopolicy_"$$version"_linux_arm64.tar.gz; do \
			curl --fail --silent --show-error --location --output "bin/reproduce/assets/$$f" "$$base/$$f"; \
		done; \
		(cd bin/reproduce/assets && $(VERIFY_ASSETS)); \
	fi; \
	git worktree add --detach bin/reproduce/src "$$tag" > /dev/null; \
	trap 'git worktree remove --force bin/reproduce/src' EXIT; \
	(cd bin/reproduce/src && GORELEASER_CURRENT_TAG="$$tag" goreleaser build --clean); \
	for arch in amd64 arm64; do \
		mkdir -p "bin/reproduce/release/$$arch"; \
		tar -xzf "bin/reproduce/assets/gitlab-repopolicy_$${version}_linux_$$arch.tar.gz" -C "bin/reproduce/release/$$arch" gitlab-repopolicy; \
		want=$$(sha256sum "bin/reproduce/release/$$arch/gitlab-repopolicy" | cut -d' ' -f1); \
		got=$$(sha256sum bin/reproduce/src/dist/gitlab-repopolicy_linux_$$arch*/gitlab-repopolicy | cut -d' ' -f1); \
		if [ "$$want" != "$$got" ]; then \
			echo "release-reproduce: linux/$$arch differs: release $$want, rebuilt $$got" >&2; exit 1; \
		fi; \
		echo "release-reproduce: linux/$$arch $$want matches"; \
	done

.PHONY: release-publish
## Publish the GitHub Release for RELEASE_TAG and verify its assets.
release-publish: release-tag-format $(TOOLS_BIN)/goreleaser $(TOOLS_BIN)/syft $(TOOLS_BIN)/cosign
	if [ "$(GITHUB_ACTIONS)" != true ] || [ "$(GITHUB_REF_TYPE)" != tag ] || [ -n "$(ACT)" ]; then \
		echo "release-publish: runs only in GitHub Actions on a tag push (GITHUB_ACTIONS=true, GITHUB_REF_TYPE=tag, ACT unset)" >&2; exit 1; \
	fi
	git fetch --force origin "+refs/tags/$(RELEASE_TAG):refs/tags/$(RELEASE_TAG)"
	mkdir -p bin
	git for-each-ref --format='%(contents:subject)%0a%0a%(contents:body)' "refs/tags/$(RELEASE_TAG)" > bin/release-notes.md
	GORELEASER_CURRENT_TAG="$(RELEASE_TAG)" goreleaser release --clean --release-notes bin/release-notes.md
	rm -rf bin/published; mkdir -p bin/published
	gh release download "$(RELEASE_TAG)" --repo "$(GITHUB_REPOSITORY_SLUG)" --dir bin/published
	cd bin/published && $(VERIFY_ASSETS)

.PHONY: help
## Shows help. | Help
help:
	@echo ''
	@echo 'Usage:'
	@echo ''
	@echo '  $(YELLOW)make$(RESET) $(GREEN)<target>$(RESET)'
	@echo ''
	@echo 'Targets:'
	@awk '/^[a-zA-Z0-9\-_]+:/ { \
		helpMessage = match(lastLine, /^## (.*)/); \
		if (helpMessage) { \
			if (index(lastLine, "|") != 0) { \
				stage = substr(lastLine, index(lastLine, "|") + 1); \
				printf "\n $(GRAY)%s: \n\n", stage; \
			} \
			helpCommand = substr($$1, 0, index($$1, ":")-1); \
			helpMessage = substr(lastLine, RSTART + 3, RLENGTH); \
			if (index(lastLine, "|") != 0) { \
				helpMessage = substr(helpMessage, 0, index(helpMessage, "|")-1); \
			} \
			printf "  $(YELLOW)%-$(TARGET_MAX_CHAR_NUM)s$(RESET) $(GREEN)%s$(RESET)\n", helpCommand, helpMessage; \
		} \
	} \
	{ lastLine = $$0 }' $(MAKEFILE_LIST)
	@echo ''
