[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](SECURITY.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](SECURITY.en.md)

# Security

## How to report a vulnerability

Report it privately through [GitHub Security Advisories](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/security/advisories/new). Do not open a public issue and do not describe the vulnerability in a pull request.

Useful to include:
- the gitlab-repopolicy version (`gitlab-repopolicy version`) and the GitLab version
- how to reproduce: policy files, commands, push output
- what exactly could be bypassed or what harm it does

We confirm receipt, agree with you on the timeline of the fix and the publication, and credit you in the advisory if you wish.

## Supported versions

Security fixes come out for the latest minor version. Upgrade to it: PATHPOLICY and TAGPOLICY `version: 1` schemas are frozen, and the operations guide explains how unknown COMMITPOLICY checks are handled ([docs/operations.en.md](docs/user/administration/operations.en.md#upgrade-and-rollback)).

## What counts as a vulnerability

Any way to change a protected path, weaken a policy for one's own push, bypass the check of commit format or tag signature, or make the hook decide by anything other than the policy from the default branch - yes. Known limitations described in [docs/threat-model.en.md](docs/user/administration/threat-model.en.md) (project settings, personal namespaces, actions of an Owner and an administrator) - no, but ideas on how to reduce them are welcome in an issue.

## Verifying releases

The release procedure produces a cosign keyless signature, SBOM and SLSA provenance. The [installation guide](docs/user/administration/install.en.md#1-prepare-and-verify-artifacts) explains verification of a signed artifact bundle.
