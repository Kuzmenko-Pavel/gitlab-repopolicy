[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](tagpolicy.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](tagpolicy.en.md)

# TAGPOLICY

TAGPOLICY checks who signed a tag. For every kind of tag, for example prod releases, the file sets the keys that may sign such a tag. GitLab does not accept a tag without a signature or with somebody else's signature. The file lives in the repository root as `TAGPOLICY`, its content is YAML.

First, what a tag and a signed tag are and how to create them. Then, how TAGPOLICY checks them.

## What a tag is

A tag is a permanent name for one commit, for example `v1.4.0-prod`. A branch moves with every new commit, while a tag stays on the commit where it was set. That is why tags are used for releases. The pipeline of the tag `v1.4.0-prod` deploys to prod exactly the code the tag points to.

Git has three kinds of tags:

| Kind | How to create | What is inside |
| --- | --- | --- |
| lightweight | `git tag v1.4.0-prod` | only a name that points to a commit. No author, no date, no text |
| annotated | `git tag -a v1.4.0-prod -m "Release 1.4.0"` | a separate object with the name, the commit, the author, the date and a text |
| signed | `git tag -s v1.4.0-prod -m "Release 1.4.0"` | an annotated tag plus a cryptographic signature with an OpenPGP (GPG) or SSH key |

TAGPOLICY accepts only signed tags. `-m` of `git tag` is the text of the tag itself, not a commit message, COMMITPOLICY does not check it.

This is how a signed tag looks inside (`git cat-file -p v1.4.0-staging`):

```text
object ae36b6beebcaec2e0099026818110c852ba2a6e3
type commit
tag v1.4.0-staging
tagger Olena Ivanenko <olena@example.com> 1792250148 +0300

Release 1.4.0
-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAg74/0eQwXM2XR1UKytRridOUhTu
...
-----END SSH SIGNATURE-----
```

| Line | What it is |
| --- | --- |
| `object`, `type commit` | the commit the tag points to |
| `tag` | the tag name written into the signed object |
| `tagger` | who created the tag and when |
| `Release 1.4.0` | the text from `-m` |
| `BEGIN SSH SIGNATURE` | the signature. For a GPG key it is `BEGIN PGP SIGNATURE` |

### How to sign and push a tag

With a GPG (OpenPGP) key, if the key is already set in `user.signingkey`:

```sh
git tag -s v1.4.0-prod -m "Release 1.4.0"
git push origin v1.4.0-prod
```

With an SSH key, giving the public part of the key:

```sh
git -c gpg.format=ssh -c user.signingkey=~/.ssh/id_ed25519.pub tag -s v1.4.0-staging -m "Release 1.4.0"
git push origin v1.4.0-staging
```

`git push` without a tag name does not send tags, so a tag is pushed separately, by its name.

You can check a signature locally with `git tag -v v1.4.0-prod`. For GPG the public key must be in your keyring, for SSH a file of allowed keys in `gpg.ssh.allowedSignersFile`, otherwise git answers `gpg.ssh.allowedSignersFile needs to be configured`. The server does not check this way, the hook takes keys only from TAGPOLICY.

### How `git tag -s` differs from `git commit -s` and `-S`

Three similar flags do different things:
- `git tag -s` - a signed tag. This is what TAGPOLICY checks
- `git commit -s` - the line `Signed-off-by` in the commit message, only text without cryptography ([commitpolicy.en.md](commitpolicy.en.md#how-to-write-such-a-message))
- `git commit -S` - a cryptographic signature of a commit. TAGPOLICY does not check it, not even for the commit the tag points to

### How to fix a tag after a denial

The hook rejects the push, so the tag is not on the server, it exists only on your machine. Delete it locally, create it again with the right signature and push:

```sh
git tag -d v1.4.0-prod
git tag -s v1.4.0-prod -m "Release 1.4.0"
git push origin v1.4.0-prod
```

The hook prints the same command in the denial, in the `how to fix` line. If your key is not allowed for this tag, the owner of an allowed key has to sign it, see [Denials](#denials).

### Tags from the GitLab UI and GitLab Release

GitLab cannot sign the tags it creates. A tag created with the New tag button in the UI or through the Releases API is denied with `tag_not_signed` under a rule. So first create a signed tag with `git tag -s` and `git push`, then a release in GitLab (Deploy > Releases > New release) for the existing tag. A release adds a description and files to the tag, it does not change the tag itself.

## How TAGPOLICY checks it

The file consists of rules. A rule ties a regex of tag names to the keys that may sign such a tag. Different environments are usually released by different people. Staging by several developers, prod only by the release manager.

The signature check relies only on the tag itself and the keys from the file. Neither the GitLab API nor `gpg` or `ssh-keygen` on the server is needed. The API is needed only by a rule with `protected: true`, and only when a tag is created.

Who may change TAGPOLICY itself is set by a PATHPOLICY rule ([pathpolicy.en.md](pathpolicy.en.md)). The history of who may release is the history of this file.

## Example

```yaml
# examples/signed-releases/TAGPOLICY
# Release tags: staging is signed by release-lead or dev-lead, prod only by
# release-lead, and a prod tag must be protected in GitLab. Tags that match no
# rule, such as build-42, are not checked.
version: 1

rules:
  - name: staging
    tags: 'v\d+\.\d+\.\d+-staging'
    signers: [release-lead, dev-lead]
  - name: prod
    tags: 'v\d+\.\d+\.\d+-prod'
    signers: [release-lead]
    protected: true
    message: >-
      Prod releases are signed by release-lead.
      Protect v*-prod in Settings > Repository > Protected tags.

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

How to read it:
- `keys` - public keys under labels. `release-lead` is a GPG key, `dev-lead` is an SSH key
- `v1.4.0-staging` matches the rule `staging`, `release-lead` or `dev-lead` may sign it
- `v1.4.0-prod` matches the rule `prod`, only `release-lead` may sign it. In addition, the project must protect this tag in GitLab
- `build-42` matches no rule and passes without a signature

## Fields

| Field | Required | Meaning |
| --- | --- | --- |
| `version` | yes | `1`. The `version: 1` schema is frozen |
| `rules` | no | up to 50 rules |
| `rules[].name` | yes | `[a-z0-9-]{1,63}`, unique in the file |
| `rules[].tags` | yes | a regex (RE2 syntax), up to 256 bytes. It must match the whole tag name, for example `v1.4.0-prod`. There is no need to add `^` and `$` |
| `rules[].signers` | yes | 1-32 key labels from `keys` |
| `rules[].protected` | no | `true` - the project must protect a new tag under this rule in GitLab (Settings > Repository > Protected tags), see [below](#protected-tags-protected). `false` by default |
| `rules[].message` | no | text for the person who is denied, up to 1000 characters |
| `keys` | yes | 1-32 entries `label: key` |

A key is exactly one public key per entry, private keys do not go into the file:
- OpenPGP - an armored block `-----BEGIN PGP PUBLIC KEY BLOCK-----`
- SSH - an OpenSSH public key line of one of the types `ssh-ed25519`, `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`, `ssh-rsa`, `sk-ssh-ed25519@openssh.com`, `sk-ecdsa-sha2-nistp256@openssh.com`. No options and no certificates

A label is the name of a key inside the file (`[A-Za-z0-9_.-]`, up to 255 characters), not a GitLab user. The hook does not check it against GitLab and does not connect whoever signed with whoever pushes. Using the username of the key owner keeps audit easy to read.

The file size is up to 256 KiB, because armored keys are large.

## How tags are matched

- A tag that matches no rule is not checked by the module. In the example above `build-42` passes without a signature
- A tag that matches several rules must satisfy all of them. The signing key must be in `signers` of each of them. A tag has one signature, so several rules for one tag narrow the list of allowed keys. Write rules that do not overlap
- A regex, not a pattern with `*`, because `v*` does not tell `v1.2.3` from `v1.2.3-prod`. A regex from the CI rules (`$CI_COMMIT_TAG =~ /.../`) usually carries over as is, but the syntax here is RE2, it has no lookahead, lookbehind and backreferences (`\1`). Check the file with `validate`
- A rule must cover everything the CI of this environment deploys. Then a mismatch gives an extra check, not a deployment of an unchecked tag

## What is checked

For every tag that the push creates or moves to another object and whose name matches a rule:

1. The tag is annotated. A lightweight tag is denied with `tag_not_signed`
2. The name inside the tag object (the `tag` line) equals the name it is pushed under. Otherwise a signed staging tag could be pushed under a prod tag name. The signature of the object stays correct then, and `git verify-tag` accepts it. A mismatch is denied with `tag_object_invalid`
3. The tag points directly to a commit. A tag of a tag, a tree or a blob is denied with `tag_object_invalid`
4. There is a signature, and it is OpenPGP or SSH. No signature or an X.509 signature is denied with `tag_not_signed`
5. The signing key is in the file, and its label is in `signers` of every matching rule. If a tag is signed with an OpenPGP subkey, the primary key it belongs to counts. Otherwise the tag is denied with `tag_signer_not_allowed`
6. The signature is valid. Otherwise the tag is denied with `tag_signature_invalid`:
   - OpenPGP: the key and the subkey are neither expired nor revoked at the time of the push, the signature time is no later than the push plus 5 minutes, the hash is not SHA-1, MD5 or RIPEMD-160
   - SSH: the signature namespace is `git`, the hash is `sha256` or `sha512`, RSA only `rsa-sha2-256` or `rsa-sha2-512`, not `ssh-rsa` (SHA-1). A signature with an sk key must carry the user presence flag

The SSH signature format has no signature time and no key expiry, so an SSH key is valid while it is in the file. Its lifetime and a separate key for signing (not the one used to push) are the responsibility of whoever adds the key to TAGPOLICY.

### Protected tags (`protected`)

Apart from the signature there is the protection of a tag in GitLab. A project protects tags by a pattern in Settings > Repository > Protected tags, for example `v*-prod`. The deployment depends on it. Protected CI/CD variables (passwords, deploy tokens) and protected runners are available only to the pipeline of a protected tag. Without protection the tag is created, the pipeline starts, but deploys nothing, and people search for the reason for a long time.

So for a rule with `protected: true` the hook asks GitLab, when a tag is created, whether the project protects this tag. If not, the tag is denied with `tag_not_protected` and a hint what to set up. This is a check of settings, not of access. If the API did not answer, the tag is accepted with a warning. If a tag has both a signature problem and a protection problem, the output shows both. Moving an existing tag is not checked for protection.

TAGPOLICY does not govern tag deletion. GitLab protected tags decide who may create, move and delete tags. For projects with PATHPOLICY `settings-check` checks that they are in place.

Tags created before TAGPOLICY appeared, tags outside the rules and the signature of the commit the tag points to are not checked.

## Acceptance of a new version

In addition to the common checks ([pathpolicy.en.md](pathpolicy.en.md#acceptance-of-a-new-version)):

| Situation | Result |
| --- | --- |
| a private key in the file | denied, `invalid_policy_update`, with a plain message that the key must be treated as exposed |
| more than one key in an entry, an unknown SSH key type, an OpenPGP key that cannot sign or is revoked | denied, `invalid_policy_update` |
| a label from `signers` is missing in `keys` | denied, `invalid_policy_update` |
| the same key under two labels | denied, `invalid_policy_update` |
| an expired OpenPGP key in an added or changed entry | denied, `invalid_policy_update` |
| an expired OpenPGP key in an unchanged entry | a warning. Somebody else's expired key does not block an edit of the file |
| a key that no rule uses | a warning |

If the project has no PATHPOLICY on the default branch, anyone who can push may change TAGPOLICY, that is also the list of keys that sign releases. The hook accepts such a version with the warning `the project has no PATHPOLICY on the default branch: anyone who can push may change TAGPOLICY`. Protect the file with a PATHPOLICY rule ([pathpolicy.en.md](pathpolicy.en.md#example)).

In the default branch an expired key does not make TAGPOLICY invalid, it is rejected only when a signature is checked.

Replacing and revoking a key is a change of TAGPOLICY in the default branch. It applies from the first push after the merge. A compromised private key can sign a release until that merge happens.

## Denials

TAGPOLICY guards releases, so it denies under any uncertainty. A doubtful signature, a broken TAGPOLICY file and an internal error of the hook give a denial, not a skip. The exception is the `protected` check. It is about settings, so an API failure or an expired token give a warning, not a denial.

| Code | Title in the output | When |
| --- | --- | --- |
| `tag_not_signed` | the tag must be signed | a lightweight tag, a tag without a signature, an X.509 signature, a tag from the UI or the Releases API |
| `tag_object_invalid` | the tag was signed as "v2.0.0-staging" but is pushed as "v2.0.0-prod" | the name inside the tag object differs from the name it is pushed under |
| `tag_object_invalid` | the tag points to a tree, not a commit | the tag points not to a commit but to a tree, a blob or another tag |
| `tag_signer_not_allowed` | this key may not sign these tags | the key is not in the file or its label is not in `signers` |
| `tag_signature_invalid` | the signature cannot be accepted | the key is expired or revoked, a weak hash, a wrong SSH namespace |
| `tag_not_protected` | the tag must be protected, but the project does not protect it | a new tag under a rule with `protected: true`, and the project does not protect it |
| `authorization_backend_failure`, `limit_exceeded` | could not check that the tag is protected; the tag is accepted | only a warning: the API did not answer during the `protected` check |
| `invalid_policy` | TAGPOLICY in main is broken, so no tag can be created or moved | TAGPOLICY in the default branch is invalid. Creating and moving any tag is denied, branches are not affected |
| `invalid_policy_update` | the changed TAGPOLICY is invalid | a new version of the file failed acceptance |

This is the denial when `dev-lead` signed a prod tag of the example above, and only `release-lead` is allowed for prod:

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: TAGPOLICY rule "prod", tag v3.156.0-prod: this key may not sign these tags
remote:   signed with: key "dev-lead" from TAGPOLICY (SSH, SHA256:pttIxH9SsvfRFuR5nv5vaNqIlF5N15hu0MZMwzg6AyE)
remote:   allowed keys: release-lead (OpenPGP)
remote:   Prod releases are signed by release-lead. Protect v*-prod in Settings > Repository > Protected tags.
remote:   ask the owner of an allowed key to sign the tag, or ask the people who may change TAGPOLICY to add your key
remote:
remote: rules: PATHPOLICY, TAGPOLICY in main (9f1c2ab)
remote: checked as: dev-lead (SSH)
remote: GL-HOOK-ERR: gitlab-repopolicy: this key may not sign these tags (TAGPOLICY rule "prod", tag v3.156.0-prod)
```

How to read it:
- `signed with` - which key from the file signed the tag, its label and fingerprint
- `allowed keys` - the labels of the keys this rule allows to sign
- then the `message` of the rule and what to do
- `rules` - which policy files applied and from which commit of the default branch the hook read them
- `checked as` - who pushed. Here it is the GitLab username, it is not connected to the key label
- `GL-HOOK-ERR` - a short line for the GitLab UI

A tag created in the GitLab UI or through the Releases API is not signed. For tags under a rule the denial says so (`GitLab cannot sign a tag created in the UI or API`) and suggests to create the tag with `git tag -s` and `git push` and then create the release for the existing tag.

The audit event `decision` of the `tagpolicy` module holds the tag, the rules, the label and the fingerprint of the key that signed the tag.

`git push --mirror` checks every tag that is not yet on the server. When an old repository is imported into a new project, that is every tag, so old unsigned tags under a rule deny the whole push. Add TAGPOLICY after the import or use break-glass ([operations.en.md](operations.en.md)).

## Checking without a push

```sh
gitlab-repopolicy validate -f TAGPOLICY
gitlab-repopolicy test --repo . --old 0000000000000000000000000000000000000000 \
  --new "$(git rev-parse v1.4.0-prod)" --ref refs/tags/v1.4.0-prod --user release --config release-verify.yaml
```

`test` makes the same decision as the hook, on a local repository. `--old` of zeros means that the tag is created. `release-verify.yaml` is a copy of [config/release-verify.yaml](../config/release-verify.yaml), only TAGPOLICY is enabled in it and there is no API access. For a rule with `protected: true` `test` then adds the warning `could not check that the tag is protected`, and checks the signature fully.
