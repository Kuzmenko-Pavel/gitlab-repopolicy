[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](tagpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](tagpolicy.en.md)

# TAGPOLICY

TAGPOLICY answers one question: is the tag signed by a key that may release tags with this name. The file lives in the repository root as `TAGPOLICY`, its content is YAML.

A release is a signed tag, and different environments are released by different people. A rule ties a regex of tag names to a set of OpenPGP or SSH public keys. The check relies only on the tag object and the keys in the file itself: neither the GitLab API nor `gpg` or `ssh-keygen` on the server is needed.

Who changes TAGPOLICY itself is set by a PATHPOLICY path rule ([pathpolicy.en.md](pathpolicy.en.md)). The history of release rights is the history of this file.

## Example

```yaml
# examples/signed-releases/TAGPOLICY
# Release tags: staging is signed by release-lead or dev-lead, prod only by
# release-lead. Tags that match no rule, such as build-42, are not checked.
version: 1

rules:
  - name: staging
    tags: 'v\d+\.\d+\.\d+-staging'
    signers: [release-lead, dev-lead]
  - name: prod
    tags: 'v\d+\.\d+\.\d+-prod'
    signers: [release-lead]
    message: Prod releases are signed by release-lead.

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
  dev-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE ed25519
```

## Fields

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | `1`. The `version: 1` schema is frozen |
| `rules` | no | up to 50 rules |
| `rules[].name` | yes | `[a-z0-9-]{1,63}`, unique in the file |
| `rules[].tags` | yes | an RE2 regex, up to 256 bytes. It must match the whole tag name without `refs/tags/`, there is no need to add `^` and `$` |
| `rules[].signers` | yes | 1-32 key labels from `keys` |
| `rules[].message` | no | text for the person who is denied, up to 1000 characters |
| `keys` | yes | 1-32 entries `label: key` |

A key is exactly one public key per entry:
- OpenPGP - an armored block `-----BEGIN PGP PUBLIC KEY BLOCK-----`
- SSH - an OpenSSH public key line: `ssh-ed25519`, `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`, `ssh-rsa`, `sk-ssh-ed25519@openssh.com`, `sk-ecdsa-sha2-nistp256@openssh.com`. No options and no certificates

A label is the name of a key inside the file (`[A-Za-z0-9_.-]`, up to 255 characters), not a GitLab user. The hook does not check it against GitLab and does not connect whoever signed with whoever pushes. Using the username of the key owner keeps audit easy to read.

The file size is up to 256 KiB, because armored keys are large.

## How tags are matched

- A tag that matches no rule is not checked by the module. In the example above `build-42` passes without a signature
- A tag that matches several rules must satisfy all of them (AND): the signing key must be in `signers` of each. A tag has one signature, so overlapping rules give the intersection of the sets. Write rules without overlaps
- A regex, not a glob, because a glob does not tell `v1.2.3` from `v1.2.3-prod`. A regex can be taken from the CI rules (`$CI_COMMIT_TAG =~ ...`) as is
- A rule must cover everything the CI of this environment deploys. Then a mismatch gives an extra check, not a deployment of an unchecked tag

## What is checked

For every new or moved tag whose name matches a rule:

1. The tag is annotated. A lightweight tag is denied with `tag_not_signed`
2. The name inside the tag object equals the name it is pushed under. Otherwise a signed staging tag could be pushed under a prod tag name: the signature of the object stays correct and `git verify-tag` accepts it. A mismatch is denied with `tag_object_invalid`
3. The tag points directly to a commit. A tag of a tag, a tree or a blob is denied with `tag_object_invalid`
4. There is a signature, and it is OpenPGP or SSH. No signature or an X.509 signature is denied with `tag_not_signed`
5. The signing key is in the file and its label is in `signers` of every matching rule. For an OpenPGP subkey its primary key counts. Otherwise the tag is denied with `tag_signer_not_allowed`
6. The signature is valid. Otherwise the tag is denied with `tag_signature_invalid`:
   - OpenPGP: the key and the subkey are neither expired nor revoked at the time of the push, the signature time is no later than the push plus 5 minutes, the hash is not SHA-1, MD5 or RIPEMD-160
   - SSH: the signature namespace is `git`, the hash is `sha256` or `sha512`, RSA only `rsa-sha2-256` or `rsa-sha2-512`, not `ssh-rsa` (SHA-1). A signature with an sk key must carry the user presence flag

The SSH signature format has no signature time and no key expiry, so an SSH key is valid while it is in the file. Its lifetime and a separate key for signing (not the one used to push) are the responsibility of whoever adds the key to TAGPOLICY.

TAGPOLICY does not govern tag deletion: GitLab protected tags decide who may create, move and delete tags. For projects with PATHPOLICY `settings-check` checks that they are in place.

Tags created before TAGPOLICY appeared, tags outside the rules and the signature of the commit the tag points to are not checked.

## How to sign a tag

OpenPGP:

```sh
git tag -s v1.4.0-prod -m "Release 1.4.0"
git push origin v1.4.0-prod
```

SSH:

```sh
git -c gpg.format=ssh -c user.signingkey=~/.ssh/id_ed25519.pub tag -s v1.4.0-staging -m "Release 1.4.0"
git push origin v1.4.0-staging
```

GitLab cannot sign the tags it creates. A tag created in the UI or through the Releases API under a rule is denied with `tag_not_signed`. Push a signed tag first, then create a release for the existing tag.

## Acceptance of a new version

In addition to the common checks ([pathpolicy.en.md](pathpolicy.en.md#acceptance-of-a-new-version)):

| Situation | Result |
| --- | --- |
| a private key in the file | denied, `invalid_policy_update`, with a plain message: treat this key as exposed |
| more than one key in an entry, an unknown SSH key type, an OpenPGP key that cannot sign or is revoked | denied, `invalid_policy_update` |
| a label from `signers` is missing in `keys` | denied, `invalid_policy_update` |
| the same key under two labels | denied, `invalid_policy_update` |
| an expired OpenPGP key in an added or changed entry | denied, `invalid_policy_update` |
| an expired OpenPGP key in an unchanged entry | a warning: somebody else's expired key does not block an edit of the file |
| a key that no rule uses | a warning |

In the default branch an expired key does not make TAGPOLICY invalid: it is rejected only when a signature is checked.

Rotating and revoking a key is a change of TAGPOLICY in the default branch. It applies from the first push after the merge. A compromised private key can sign a release until that merge happens.

## Denials

TAGPOLICY fails closed under uncertainty, but it does not use the API, so an API failure or an expired token do not affect it.

| Code | Title in the output | When |
| --- | --- | --- |
| `tag_not_signed` | tag is not signed | a lightweight tag, a tag without a signature, an X.509 signature, a tag from the UI or the Releases API |
| `tag_object_invalid` | tag object does not match ref | the name inside the object differs from the tag name, the tag does not point to a commit |
| `tag_signer_not_allowed` | tag signer not allowed | the key is not in the file or its label is not in `signers` |
| `tag_signature_invalid` | tag signature is invalid | the key is expired or revoked, a weak hash, a wrong SSH namespace |
| `invalid_policy` | policy is invalid | TAGPOLICY in the default branch is invalid: creating and moving any tag is denied, branches are not affected |
| `invalid_policy_update` | new policy version is invalid | a new version of the file failed acceptance |

```text
remote: TAGPOLICY [prod] refs/tags/v3.156.0-prod: tag signer not allowed
remote:   Prod releases are signed by release-lead or sre-lead.
remote:   signed by: dev-lead (4528A421CE40AA6A717FF2112DA7684307491280)
remote:   allowed:   release-lead, sre-lead
```

The audit event `decision` of the `tagpolicy` module holds the tag, the rules, the label and the fingerprint of the key that signed the tag.

`git push --mirror` with old unsigned tags under a rule is denied. Add TAGPOLICY after the import or use break-glass ([operations.en.md](operations.en.md)).

## Checking without a push

```sh
gitlab-repopolicy validate -f TAGPOLICY
gitlab-repopolicy test --repo . --old 0000000000000000000000000000000000000000 \
  --new "$(git rev-parse v1.4.0-prod)" --ref refs/tags/v1.4.0-prod --user release --config release-verify.yaml
```

`test` makes the same decision as the hook, on a local repository. `release-verify.yaml` is a copy of [config/release-verify.yaml](../config/release-verify.yaml): only TAGPOLICY is enabled in it, and no API is needed. This is how the project checks its own release tags ([release.en.md](release.en.md)).
