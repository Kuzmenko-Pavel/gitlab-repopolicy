[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](inspect-tag.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](inspect-tag.en.md)

# Inspecting requirements for a tag name

Before a release, determine which rules cover `v1.4.0-prod` and which key must sign it. The name and policy file are sufficient; the tag need not exist yet.

## Reproducible query

Save this server fragment as `presets.yaml` and the following file as `TAGPOLICY` in the current directory. These are test public keys; the query needs no private key, Git repository, API or token.

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

```text
$ gitlab-repopolicy validate --config presets.yaml --tag v1.4.0-prod
TAGPOLICY: valid
  extends: org-tags
  rule prod: preset:org-tags
    match: preset:org-tags
    message: preset:org-tags
    protected: preset:org-tags
    signers: preset:org-tags
  rule staging: preset:org-tags
    match: preset:org-tags
    message: preset:org-tags
    signers: preset:org-tags
tag v1.4.0-prod: rules prod (from preset:org-tags)
  signers: release-lead (SSH)
  requires: the project protects the tag in Settings > Repository > Protected tags
  the tag must be signed by one of the signers; validate does not check signatures
```

`extends` identifies the included preset; `from` identifies rule origins. `signers` lists permitted keys after intersecting all matching rules. `requires` states a protected-tag condition without checking it in GitLab.

## What the result does not confirm

Exit 0 means a valid file and a completed informational query. It does not confirm a signature, possession of a private key or GitLab permission to create the tag. Even a result with no common signer can exit 0.

Repeat the query for `build-42`: no match means TAGPOLICY does not check that name's signature. Global commit reachability and GitLab permissions still apply. For a final decision, create the tag and use `test` on an appropriate repository copy or make a control push; the [CLI reference](../../reference/cli.en.md#test) explains required objects and access.

[TAGPOLICY reference](../../policies/tagpolicy.en.md)
