[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](validate.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](validate.en.md)

# Checking policy files locally

## Quick policy-file check

Open the project directory containing the policy files and run:

```sh
gitlab-repopolicy validate
```

The command looks for `PATHPOLICY`, `COMMITPOLICY`, `TAGPOLICY` and `BRANCHPOLICY` only in the current directory. It checks YAML, schema, regexes, `match`, BRANCHPOLICY ordering and TAGPOLICY public keys. Offline checking needs no GitLab, token or even Git repository.

For one file:

```sh
gitlab-repopolicy validate -f PATHPOLICY
```

For example, in a directory containing only the PATHPOLICY from the [CI scenario](../examples/pathpolicy/ci-configuration.en.md), output is:

```console
$ gitlab-repopolicy validate
PATHPOLICY: valid
$ gitlab-repopolicy validate -f PATHPOLICY
PATHPOLICY: valid
```

`valid` means the checks performed on the file found no errors. On error, the command prints `invalid`, the reason and a YAML position when known; it exits 1. An empty directory reports `no policy files in the current directory` and exits 0, so a successful job without explicit `-f` does not prove a policy exists.

Warnings and `not checked` lines do not make a file invalid. Review them before merging. Files using `extends` need a copy of the presets, as explained below.

## One file, drafts and multiple files

`-f` accepts one path. For several standard-named files in the project root, run `validate` without `-f`. For different directories, invoke the command separately for each required path; repeated `-f` flags do not form a file list.

The filename determines the module. For a differently named draft, specify it explicitly:

```sh
gitlab-repopolicy validate -f drafts/pathpolicy.yaml --module pathpolicy
```

Available modules are `pathpolicy`, `commitpolicy`, `tagpolicy`, `branchpolicy`. Invalid arguments exit 2. The [CLI reference](cli.en.md#validate) lists all flags.

## Presets

`extends` references a named set of rules in the server configuration. Local checking needs the same preset version; developers do not need the server token.

```sh
gitlab-repopolicy validate --config presets.yaml
```

`presets.yaml` can contain only `presets`, like the [example file](../../../examples/presets.yaml). The command merges presets and the file, displays rule origins and reports broken configuration presets even if the current file does not use them. A missing required preset makes `extends` fail.

From a clone of this project, check a ready-made example without preparing a demo repository:

```sh
gitlab-repopolicy validate -f examples/org-ci/PATHPOLICY --config examples/presets.yaml
```

## Inspecting match for a branch or tag

Use `--branch` to find the class of a new branch name, or `--tag` to list tag rules and keys:

```sh
gitlab-repopolicy validate -f BRANCHPOLICY --branch release/v1.4.0
gitlab-repopolicy validate -f TAGPOLICY --tag v1.4.0-prod
```

These are informational queries after file validation. `result: denied`, no match or no common signer does not turn exit 0 into 1. `--tag` does not check an actual tag signature. A `protected` requirement is only displayed, even with `--online`.

The [branch-name scenario](../examples/branchpolicy/organization-standard.en.md) and [tag-name query](../examples/tagpolicy/inspect-tag.en.md) include complete configurations. [match](../policies/match.en.md) explains the shared matching syntax.

## Online user and admin checks

On a Gitaly node, a user who can read the server configuration and token can add `--online`. The command calls the GitLab API; `--config` is required and loaded in full.

```sh
sudo -u git gitlab-repopolicy validate -f PATHPOLICY   --online --config /etc/gitlab-repopolicy/config.yaml   --project group/project
```

Without `--project`, usernames are checked for existence and activity, but admin roles are not. With `--project`, PATHPOLICY admin roles are checked: at least one active Maintainer or Owner is required. An ineffective admin alongside a working one warns; no working admin is an error. `--online --branch` can also check the username prefix.

An unavailable check can produce `not checked` and exit 0; failure to read an explicitly named project exits 1. CI therefore cannot treat exit 0 alone as confirmation of every permission. `explain` answers a different question: whether path rules permit a particular user to change a path.

## validate in the CI of a merge request

The job needs a verified release binary or an image containing it. Verify artifact signatures and checksums using the [installation instructions](../administration/install.en.md#1-prepare-and-verify-artifacts) when preparing the image. A checksum downloaded alongside the binary alone does not establish its origin.

```yaml
# .gitlab-ci.yml; the runner image already contains the verified binary
validate-policies:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
      changes: [PATHPOLICY, COMMITPOLICY, TAGPOLICY, BRANCHPOLICY]
  script:
    - gitlab-repopolicy validate
```

For `extends`, deliver the same `presets.yaml` copy as on the server and add `--config presets.yaml`. To require a particular file to exist, invoke a separate command with `-f`. Offline checking needs no server token.

## What validate does not know

| Question | Tool or limitation |
| --- | --- |
| Is the file valid | `validate`; without online it does not confirm users and roles |
| Does a message follow the rules | `lint-commit`; without `--rule`, it applies every rule regardless of match |
| Who may change a path | `explain`; reads active PATHPOLICY through the API, without analyzing a whole push |
| Will the hook allow a specified ref update | `test`; needs appropriate Git objects, refs, configuration and API when required |

Offline `validate` cannot see the anchor version, actual pusher, GitLab settings or Git file type. A symlink can be read locally but cannot be accepted as a Git policy file. Acceptance checks new usernames and working admins. An unchanged expired OpenPGP key can warn during a push even though validating the file alone fails.

For a full walkthrough of YAML errors, presets, expired keys and exact output, open the [validate demo directory](../examples/validation-walkthrough.en.md). It is a separate practical exercise; the commands above suffice for daily checks.
