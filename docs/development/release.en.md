[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](release.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](release.en.md)

# Releasing

To publish a release, a maintainer creates a signed `vX.Y.Z` tag on main. The workflow checks its commit and builds artifacts with signed checksums. Consumers verify the bundle following the [installation guide](../user/administration/install.en.md#1-prepare-and-verify-artifacts).

## Versions

- A tag is `vX.Y.Z` or `vX.Y.Z-rc.N`, there are no other formats. Go modules need the `v`
- In the binary, archive names, packages and audit the version has no `v`: `1.0.0`. In deb and rpm metadata a pre-release is written with a tilde (`1.0.0~rc.1`), so that the package manager orders it before `1.0.0`
- SemVer. `-rc.N` is published as a pre-release on GitHub
- `version: 1` in policy files is not tied to the version of the binary
- The release text is the tag message without the signature block. There is no separate `CHANGELOG.md`

## Who may sign a release

The keys allowed to sign release tags are in the project's own [TAGPOLICY](../../TAGPOLICY). Before publishing, the workflow checks the tag signature against this file with the same code as the hook on GitLab. A new signer is added by a pull request with their OpenPGP public key: the history of release rights is the history of this file.

Only the repository owner may create, move and delete `v*` tags on GitHub (a repository rule).

## How to release

1. Make sure main is green: `ci` and `e2e` on the last commit
2. On an up-to-date main without uncommitted changes:

   ```sh
   make release RELEASE_VERSION=1.0.0
   ```

   The target checks the branch, a clean tree and a match with `origin/main`, collects a changelog from Conventional Commits since the previous release (for `-rc` since the previous tag, for a final version since the previous non-rc tag), groups it into sections (Breaking changes, Features, Fixes, Performance, Dependencies, Other changes) and creates a signed annotated tag, opening an editor to review the text. Without `RELEASE_VERSION` it asks for the version. If no matching previous tag exists, the changelog covers the full history.

3. Push the tag:

   ```sh
   git push origin v1.0.0
   ```

The `release.yml` workflow does the rest.

## What `release.yml` does

1. **Tag check** (`make code-build release-verify-tag`): the tag is annotated, the version format is allowed, the tag commit is in main, the project `TAGPOLICY` accepts the tag signature. Otherwise the release stops
2. **The same checks as for a pull request**, and the full e2e matrix on the tag commit. When a final tag for a previous minor version exists, integration also checks that its binary accepts the valid PATHPOLICY and TAGPOLICY corpus. Without such a tag, the step is skipped with an explicit message
3. **Publishing** (`make release-publish`): GoReleaser builds `linux/amd64` and `linux/arm64`, archives, deb and rpm, SBOM, `checksums.txt` and a cosign keyless signature, and creates a GitHub Release with the tag text. Then the target downloads the published artifacts and verifies them the way a consumer does
4. **Attestation**: SLSA provenance for the release files

If a release fails, the fix goes out with a new tag. A published tag is never moved.

## Tag signatures and artifact provenance

TAGPOLICY verifies the Git tag's allowed signing key. The cosign keyless signature verifies that the checksum file came from the release workflow. These are separate checks: a valid Git signature alone does not verify a downloaded `.deb`, and a checksum without its signature does not establish file origin.

## Reproducibility

The build is configured for reproducibility. For a published tag, verify it by comparing a local build with the artifacts:

```sh
make release-reproduce
```

The target builds the binaries from the tag commit and compares their sha256 with the binaries from the release archives.

## Checking without publishing

```sh
make release-snapshot
```

Full packaging without signing and publishing, into `dist/`. CI runs it on every pull request, so that a broken packaging config shows up before a tag.
