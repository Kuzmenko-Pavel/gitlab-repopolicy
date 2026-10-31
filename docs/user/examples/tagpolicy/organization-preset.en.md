[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](organization-preset.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](organization-preset.en.md)

# Preset rules and project-owned keys

The organization shares staging/prod rules while each project controls its signing keys. The server preset names signer labels; the project's TAGPOLICY defines the public keys behind them.

## Both configurations

An administrator adds this fragment to the server `config.yaml`. Save the same fragment as `presets.yaml` for local checks.

```yaml
presets:
  tagpolicy:
    org-tags:
      rules:
        staging:
          match: ['/^v\d+\.\d+\.\d+-staging$/']
          signers: [release-lead, dev-lead]
          message: Staging releases are signed by release-lead or dev-lead.
        prod:
          match: ['/^v\d+\.\d+\.\d+-prod$/']
          signers: [release-lead]
          protected: true
          message: Prod releases are signed by release-lead. Protect v*-prod in Settings > Repository > Protected tags.
```

The project stores the following `TAGPOLICY` in its root. These are test keys; actual use requires your public keys and signers with access to the corresponding private keys.

```yaml
# examples/org-tags/TAGPOLICY
# The tag rules of the organization come from the preset org-tags: it names
# the signers by label. The keys behind the labels live in this file, so
# replacing or revoking a key is a reviewed change of the project.
version: 1
extends: org-tags

keys:
  release-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl release-lead
  dev-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE dev-lead
```

Run `gitlab-repopolicy validate --config presets.yaml -f TAGPOLICY` before merging into the default branch. To create prod tags, a Maintainer also protects `v*-prod` in GitLab.

## Why keys remain local

A preset contains only `rules`. Replacing or removing a key is a reviewed project TAGPOLICY change effective for subsequent invocations after merge. The file records which key could sign this project's releases. A preset cannot contain `keys`.

Every inherited `signers` label must exist in `keys`. Removing dev-lead only from `keys` makes the new file invalid; revocation must also update every list referencing that label. Check `validate --config presets.yaml --tag v1.4.0-staging` before merging.

Under global `enforce`, release-lead's signature permits a prod tag when the remaining requirements hold. After revocation, subsequent pushes of that tag must not be authorized by the removed key; existing tags are not automatically re-evaluated.

[TAGPOLICY reference](../../policies/tagpolicy.en.md)
