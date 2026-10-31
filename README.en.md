[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](README.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](README.en.md)

[![CI](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/ci.yml?query=branch%3Amain)
[![e2e](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/e2e.yml/badge.svg?branch=main)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/e2e.yml?query=branch%3Amain)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Kuzmenko-Pavel/gitlab-repopolicy/badge)](https://scorecard.dev/viewer/?uri=github.com/Kuzmenko-Pavel/gitlab-repopolicy)
[![Coverage](https://codecov.io/gh/Kuzmenko-Pavel/gitlab-repopolicy/graph/badge.svg?branch=main)](https://codecov.io/gh/Kuzmenko-Pavel/gitlab-repopolicy)
[![Release](https://img.shields.io/github/v/release/Kuzmenko-Pavel/gitlab-repopolicy?sort=semver&include_prereleases)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases)
[![License](https://img.shields.io/github/license/Kuzmenko-Pavel/gitlab-repopolicy)](./LICENSE)
[![Docs](https://img.shields.io/badge/docs-latest-blue)](docs/README.en.md)

# gitlab-repopolicy

**gitlab-repopolicy** is an application for GitLab Self-Managed that implements a server-side Git hook and lets teams define repository management policies directly in their GitLab projects.

Policies determine who may change particular files, set branch naming rules, define commit message requirements and control tag creation.

Rules are written in YAML files stored in the repository alongside the project code.

When changes are sent to GitLab, the application checks them against the configured policies. If changes violate the rules, GitLab can reject them with an explanation and guidance on how to fix the problem.

## Where to start

The application checks group project repositories. An administrator connects the hook on Gitaly nodes; project owners then keep the policies they need alongside their code.

| I want to | Next step |
| --- | --- |
| Understand capabilities and protection boundaries | [Policy overview](#policies) and [threat model](docs/user/administration/threat-model.en.md) |
| Install the application | [First run](#first-run) and [full instructions](docs/user/administration/install.en.md) |
| Configure a policy | Choose a file in the [policy table](#policies) and start with its short example |
| Find a worked solution | Catalogs for [files](docs/user/policies/pathpolicy.en.md#examples), [commits](docs/user/policies/commitpolicy.en.md#examples), [tags](docs/user/policies/tagpolicy.en.md#examples) and [branches](docs/user/policies/branchpolicy.en.md#examples) |
| Check configuration before pushing | [Local policy validation](docs/user/reference/validate.en.md) or [server selftest](docs/user/reference/cli.en.md#selftest) |
| Understand a denial | The named policy's "Denials" section; for server failures, [diagnosis](docs/user/administration/operations.en.md#common-problems) |
| Contribute to development | [Developer documentation](docs/development/README.en.md) and [CONTRIBUTING](CONTRIBUTING.en.md) |

## Policies

Each policy is a separate YAML file in the root of the repository.

| File | What it decides |
| --- | --- |
| [PATHPOLICY](docs/user/policies/pathpolicy.en.md) | who may change certain files: CI configuration, deploy logic, prod values |
| [COMMITPOLICY](docs/user/policies/commitpolicy.en.md) | what format commit messages must have |
| [TAGPOLICY](docs/user/policies/tagpolicy.en.md) | who may release: a tag is accepted only with a signature of an allowed OpenPGP or SSH key |
| [BRANCHPOLICY](docs/user/policies/branchpolicy.en.md) | how new branches are named and which of them must be protected in GitLab |

## Why

File policies are useful when branch access is broader than access to its CI configuration or deployment scripts. For example, developers may change code in their branches while CI remains under platform team control. Server-side checks apply that requirement independently of local Git-hook settings.

gitlab-repopolicy checks a change on the server, so it does not matter how it was made: a push over HTTPS or SSH, the merge button, auto-merge, the Web IDE, the API, a push from CI. The rules are versioned and reviewed together with the code. The hook can only deny, it does not extend GitLab permissions.

When the hook checks nothing:
- a push to a project in a user's personal namespace (`alice/my-project`). Only group projects are checked
- a push to wiki, design and snippet repositories
- `mode: off` in the server configuration

With `mode: audit` the hook runs the same checks and logs its decisions, but policy violations do not block a push. Startup errors can still cause rejection; see [configuration](docs/user/administration/configuration.en.md#mode) for details and other modes.

An example from the file to the fix. The platform team (alice and sre-lead) wants only themselves to change the CI configuration, in any branch:

```yaml
# examples/ci-config/PATHPOLICY
# The platform team, alice and sre-lead, owns the CI configuration and the
# deploy scripts in every branch, where only new content counts, and the
# production values in main and release branches, where every commit that
# enters the branch counts. Everything else is authorized by GitLab as usual.
version: 1

admins: [alice, sre-lead]

rules:
  ci-config:
    match: all
    check_history: new-content
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
    check_history: incoming
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

[The PATHPOLICY denial guide](docs/user/policies/pathpolicy.en.md#denials) explains each line of the message and how to resolve the problem.

## Prerequisites

Before the installation you need:
- GitLab Self-Managed, CE or EE, of a version from the table below
- root on every node where Gitaly runs
- an instance service account with a token (`read_api`, `self_rotate`), Maintainer in every top-level group ([install.en.md](docs/user/administration/install.en.md#3-service-account-and-token))
- `custom_hooks_dir` in the Gitaly configuration, for example `/var/opt/gitlab/gitaly/custom_hooks` in `gitlab.rb` and `gitlab-ctl reconfigure`. Without it Gitaly does not run global hooks
- a prepared `.deb` or `.rpm` package. Build one for local checks with `make release-snapshot`; [installation](docs/user/administration/install.en.md#1-prepare-and-verify-artifacts) explains verification of signed artifacts

## First run

This example installs and starts gitlab-repopolicy on a single server running GitLab Self-Managed from the Linux package (Omnibus).

Before starting, create an instance service account in GitLab, obtain its access token and configure `custom_hooks_dir` in Gitaly. Prepare the package following the [installation instructions](docs/user/administration/install.en.md#1-prepare-and-verify-artifacts). `1.0.0` below is an example version; substitute the actual filename of your built package.

For the first run on a production server, use `audit` mode. It checks changes and logs results without blocking pushes for policy violations. After installing the package and before connecting the hook, open the configuration and set `mode: audit`.

The installation and first checks can look like this:

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb
sudoedit /etc/gitlab-repopolicy/config.yaml
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

Change `mode` in the editor before running the remaining commands. Also review the shipped `default_branch: [main, master]`: it restricts the default branch name of every group project, including those without policies. Set `default_branch: []` to start observing without this restriction.

After these commands:

- The package installs the application and creates the required directories and configuration files
- `sudoedit` lets you change the mode before connecting the hook
- `cat` saves the service account token in `/etc/gitlab-repopolicy/token`. Enter the token, press Enter, then Ctrl+D. For a new file, `umask` sets mode 0440
- `install-hook` creates the wrapper in `pre-receive.d/` and gives the `git` user access to the configuration, token and log. Gitaly starts invoking the checks from this point
- `selftest` checks installation and configuration. Run it as both `root` and `git`, because Gitaly runs the hook as `git`. Any `FAIL` line means exit code 1

A successful check confirms access to the API and local files. It does not prove that Gitaly invokes the wrapper or that all groups are covered. Fix any `selftest` failures, check group coverage with `coverage` and make a control push in a test project. In `audit` mode, look for the expected denial in `/var/log/gitlab-repopolicy/audit.log` with `"enforced": false`. Startup failures, including an unreadable configuration, can still block a push in `audit` mode.

This is a basic single-server installation. The [installation guide](docs/user/administration/install.en.md) covers release verification, service account setup, Docker and separate Gitaly nodes. Follow the [integration guide](docs/user/administration/integration-guide.en.md) to move from observation to enforcement.

## Supported GitLab versions

| GitLab CE | e2e result |
| --- | --- |
| 19.4 (19.4.2) | supported |
| 19.3 (19.3.5) | supported |
| 19.2 (19.2.8) | supported |

The e2e matrix runs on the current and the two previous minor versions of GitLab CE, nightly on `latest`. EE and any tier are supported by the same code: only Free features are used. GitLab.com and GitLab Dedicated are not supported.

## Performance

The hook runs inside the push, so a push with it takes longer. How much longer depends on whether the hook needs the GitLab API.

How it was measured. GitLab CE 19.4.2 in Docker and the git client on one machine, pushes over HTTPS and SSH to `localhost`. Every scenario was pushed without the hook (after `uninstall-hook`) and with the hook (`install-hook`, `mode: enforce`). The modes alternated in blocks: without, with, without, with, without, with. In every block, for every scenario and protocol, 3 warm-up pushes and 10 measured ones, 30 measurements per cell in total. The time is the full `git push` on the client, the table shows the median in milliseconds.

| Scenario | HTTPS: (without hook / with hook), ms | SSH: (without hook / with hook), ms |
| --- | --- | --- |
| no policy files, one commit | 274 / 329 (+55) | 372 / 400 (+28) |
| PATHPOLICY, the commit does not change the files of a rule | 297 / 315 (+18) | 379 / 405 (+25) |
| PATHPOLICY, the commit changes a file of a rule, the pusher is in `allow` | 297 / 448 (+151) | 376 / 600 (+223) |
| COMMITPOLICY, one commit | 278 / 328 (+51) | 385 / 414 (+29) |
| COMMITPOLICY, a new branch with 50 commits | 309 / 342 (+33) | 385 / 441 (+55) |
| BRANCHPOLICY, a new branch by `match` | 285 / 302 (+16) | 375 / 388 (+14) |
| BRANCHPOLICY, a new branch with `user_prefix` | 281 / 299 (+17) | 388 / 465 (+77) |
| BRANCHPOLICY, a new branch with `protected: true` | 290 / 398 (+108) | 384 / 477 (+93) |
| TAGPOLICY, a signed tag | 293 / 320 (+27) | 375 / 409 (+35) |
| TAGPOLICY, a signed tag with `protected: true` | 312 / 410 (+98) | 387 / 502 (+115) |
| all four policies, a commit in a new `alice/...` branch | 313 / 342 (+29) | 394 / 503 (+110) |

What this shows:
- when the hook does not need the GitLab API, a push takes 14-55 ms longer. The hook itself, by the `duration_ms` field of the audit, runs 12-46 ms (median)
- it takes noticeably longer where the hook asks GitLab. When a commit changes a file of a PATHPOLICY rule, the hook requests the members of the project to check `allow`, that is +151-223 ms. For `protected: true` in BRANCHPOLICY and TAGPOLICY the hook requests the protected branches or tags of the project, that is +93-115 ms
- over SSH the hook also checks that the key is a personal key and not a deploy key. That is why SSH costs more in the scenarios with `user_prefix` and with all policies

Parallel load. 10 clients at once, each makes 30 pushes to its own branches of one project with all four policies, every tenth push with a signed tag.

| Protocol | Without hook: (p50 / p90), ms | With hook: (p50 / p90), ms | Total time: (without hook / with hook), s |
| --- | --- | --- | --- |
| HTTPS | 324 / 357 | 347 / 372 | 10.2 / 10.7 |
| SSH | 400 / 428 | 445 / 550 | 12.3 / 14.2 |

No push was rejected. Under this load the hook runs as fast as with sequential pushes: `duration_ms` median 18 ms, p90 24 ms. All 300 pushes of one measurement come from one user to one project, about 1500 per minute. This is above the GitLab limit `gitlab_shell_operation_limit` (600 operations per minute for a user and a project by default), so the limit was lifted for the measurement. With the limit, GitLab itself rejects the excess pushes with `GitLab: rate limited` before the hook.

Limits of the measurement:
- the client and GitLab are on one machine, there is no network. If Gitaly is on separate nodes, every API request of the hook also adds the network latency between Gitaly and GitLab Rails
- one user, small repositories, every push changes one or a few files
- one machine. On other hardware the absolute time differs, compare the difference without and with the hook

Machine: Intel Core i9-14900HX (32 threads, governor `performance`), 62 GiB RAM, NVMe WD PC SN560 1 TB, Linux 6.17, Docker 29.9.2 without CPU and memory limits for the GitLab container, git 2.43.0 on the client, gitlab-repopolicy from commit `ffcda2e`. Measured on 2026-10-28.

## Editor

Policy files have no extension, so the editor has to be told that they are YAML with a schema. The schemas are in the directory `schemas/` of the repository. The links below point to the branch `main`. For the editor to check files against the schema of a given release, replace `main` in the link with the tag of the release, for example `v1.0.0`.

**VS Code** with the YAML extension (`yaml-language-server`), in `settings.json`:

```json
{
  "files.associations": { "PATHPOLICY": "yaml", "COMMITPOLICY": "yaml", "TAGPOLICY": "yaml", "BRANCHPOLICY": "yaml" },
  "yaml.schemas": {
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/pathpolicy.schema.json": "PATHPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/commitpolicy.schema.json": "COMMITPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/tagpolicy.schema.json": "TAGPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/branchpolicy.schema.json": "BRANCHPOLICY"
  }
}
```

**PyCharm, GoLand, IntelliJ IDEA** and other JetBrains IDEs check YAML against a schema without plugins. The schemas follow JSON Schema draft 2020-12.
1. Settings > Editor > File Types > YAML, add `PATHPOLICY`, `COMMITPOLICY`, `TAGPOLICY`, `BRANCHPOLICY` to "File name patterns". This is a setting of the IDE itself, everyone makes it once on their machine
2. Settings > Languages & Frameworks > Schemas and DTDs > JSON Schema Mappings, the button `+` for every file:
   - Name: `PATHPOLICY`
   - Schema file or URL: `https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/pathpolicy.schema.json`
   - Schema version: JSON Schema 2020-12
   - with the button `+` under the list add the File path pattern `PATHPOLICY`

   The same for `COMMITPOLICY`, `TAGPOLICY` and `BRANCHPOLICY`. The IDE keeps these mappings in the project, in `.idea/jsonSchemas.xml`: commit the file, and the schemas connect for the whole team by themselves

After that the IDE highlights unknown keys and wrong values and shows hints from the descriptions of the fields.

Check a file locally with the same code as the hook with `gitlab-repopolicy validate` ([validate.en.md](docs/user/reference/validate.en.md)), commit messages with `gitlab-repopolicy lint-commit`. The editor knows only the schema, while `validate` also checks the merge with presets, regular expressions, the order of the rules of BRANCHPOLICY and the keys of TAGPOLICY.

## Contributing

Before contributing, read the [development and review requirements](CONTRIBUTING.en.md) and the [code of conduct](CODE_OF_CONDUCT.en.md). Report vulnerabilities privately using the [SECURITY procedure](SECURITY.en.md).

## License

[Apache-2.0](LICENSE)
