[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](README.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](README.en.md)

# Developing gitlab-repopolicy

Start with [architecture](architecture.en.md) to understand active policy sources, check order and the responsibilities of the core and modules. The [development guide](development.en.md) then covers tools and local checks through the Makefile.

Before changing a policy, read its [user reference](../README.en.md#policy-users) and worked scenarios. PATHPOLICY and TAGPOLICY `version: 1` schemas, denial codes and the audit format are public contracts; documentation work does not justify changing them.

## Checking a contribution

[CONTRIBUTING](../../CONTRIBUTING.en.md) defines commits, DCO and review. Required checks are `make code-lint test-unit test-integration`; identity, API-client or installation changes also need `make test-e2e`. The [testing section](development.en.md#tests) explains the corpus, executable documentation examples and GitLab stand.

Maintainers preparing a signed tag and artifacts need the [release guide](release.en.md). Production setup and emergency procedures belong to the separate [administration documentation](../README.en.md#gitlab-administrators).
