[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](overlapping-rules.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](overlapping-rules.en.md)

# One tag matching multiple rules

General release tags allow release-lead and dev-lead. Production adds a requirement allowing release-lead or sec-lead. This explains why only release-lead can sign a production tag.

## Policy with an intersection

Save this complete file as `TAGPOLICY`, check it with `validate -f TAGPOLICY` and activate it in the default branch. These are test public keys; replace them with your public keys for actual releases.

```yaml
# examples/release-overlap/TAGPOLICY
# Every release tag is signed by release-lead or dev-lead; a prod release
# also matches the prod rule, signed by release-lead or sec-lead. A tag that
# matches both rules is signed only by a key that both rules list.
version: 1

rules:
  releases:
    match: ['/^v\d+\.\d+\.\d+/']
    signers: [release-lead, dev-lead]
    message: Release tags are signed by release-lead or dev-lead.
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [release-lead, sec-lead]
    message: Prod releases are also signed by release-lead or sec-lead.

keys:
  release-lead: |
    -----BEGIN PGP PUBLIC KEY BLOCK-----
    
    mDMEas8yyBYJKwYBBAHaRw8BAQdARgQtR6FZCckYy8pf9+9snMoIb3gcbH9QVotM
    2CEt2za0H1JlbGVhc2UgTGVhZCA8bGVhZEBleGFtcGxlLmNvbT6IkwQTFgoAOxYh
    BGx5V+qHOlRnp48dB1l3WaOCcpNZBQJqzzLIAhsDBQsJCAcCAiICBhUKCQgLAgQW
    AgMBAh4HAheAAAoJEFl3WaOCcpNZf7IBAO02jJtCe9KhJlLAOTz/XX+kpgyY8mth
    6Cku2bukuF6bAP40EeqZQ1ISoo/Agcdncm5Z+KysHKFd7y49xzE0q6ObDA==
    =3XJY
    -----END PGP PUBLIC KEY BLOCK-----
  dev-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE dev-lead
  sec-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl sec-lead
```

<!-- check: tag-rules examples/release-overlap/TAGPOLICY -->
| Tag | Rules | Who may sign |
| --- | --- | --- |
| `v1.4.0` | `releases` | release-lead, dev-lead |
| `v1.4.0-prod` | `releases`, `prod` | release-lead |
| `build-42` | - | not checked |

Neither dev-lead nor sec-lead may sign `v1.4.0-prod`: each of them is in only one list. A tag has one signature, so several rules for one tag only narrow the circle of signers. When the rules have no common signer, such a tag cannot be created, and `validate --tag` writes `signers: none, the matching rules have no signer in common`.

`releases` deliberately has no trailing `$`, so it also matches the `-prod` suffix. One tag requires one key in the intersection of all lists. This policy neither requires nor implements separate signatures by two different people.

## Checking expectations

Run `gitlab-repopolicy validate -f TAGPOLICY --tag v1.4.0-prod`: the response should list `prod`, `releases` and the common label release-lead. The implementation determines rule order; the meaning is the intersection. Exit 0 confirms file validity and a completed query, not an actual tag signature.

Checking a signature requires an existing tag and `test` or a control push under global `enforce`. dev-lead and sec-lead individually receive `tag_signer_not_allowed`. Removing release-lead from one list leaves no common key and makes such a tag impossible for any signer.

[TAGPOLICY reference](../../policies/tagpolicy.en.md)
