[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](install.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](install.en.md)

# Installation

gitlab-repopolicy is installed on every node that runs Gitaly and connects to GitLab as a global server hook. The package puts files only into its own directories and does not change GitLab: the administrator turns the hook on with a separate command.

## Requirements

- GitLab Self-Managed, CE or EE, any tier. Supported versions are in the [README](../README.en.md) table. GitLab.com and GitLab Dedicated are not supported: server hooks are not available there
- Linux `amd64` or `arm64`, root on the Gitaly nodes
- an instance service account (available in Free since GitLab 18.11)

| Installation type | Support |
| --- | --- |
| Linux package (Omnibus), one node | yes |
| GitLab Docker image | yes |
| Self-compiled | yes |
| Gitaly on separate nodes, Gitaly Cluster (Praefect) | yes, the binary and config on every Gitaly node |
| Helm chart (Kubernetes) | no |
| Geo secondary | not needed: Geo does not replicate server hooks, pushes go to the primary |

e2e covers only the Linux package in the Docker image. The other types follow the GitLab documentation and are not part of the test matrix.

## In short

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'     # service account token, Ctrl-D
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

Before that you need a service account in GitLab and `custom_hooks_dir` in the Gitaly configuration. Step by step below.

## 1. Download and verify the release

Release artifacts are in [GitHub Releases](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases). The examples use version `1.0.0`, tag `v1.0.0`.

| File | What it is |
| --- | --- |
| `gitlab-repopolicy_<version>_linux_<arch>.tar.gz` | static binary, `LICENSE`, README |
| `gitlab-repopolicy_<version>_linux_<arch>.deb`, `.rpm` | package |
| `*.spdx.json` | SBOM of every archive |
| `checksums.txt` | SHA-256 of all files |
| `checksums.txt.sigstore.json` | keyless signature of `checksums.txt` (cosign) |

Verify the signature and the checksums before installing:

```sh
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --check --ignore-missing checksums.txt
```

Optional - the link between an artifact, its commit and the build workflow (SLSA provenance):

```sh
gh attestation verify gitlab-repopolicy_1.0.0_linux_amd64.tar.gz --repo Kuzmenko-Pavel/gitlab-repopolicy
```

The build is reproducible: the same tag gives a byte-identical binary.

## 2. Install the package

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb      # Debian, Ubuntu
sudo dnf install ./gitlab-repopolicy_1.0.0_linux_amd64.rpm      # RHEL, Rocky, Alma
```

The package metadata writes a pre-release version with a tilde (`1.0.0~rc.1`), so that the package manager orders it after the previous release and before `1.0.0`.

What the package installs:

```text
/usr/bin/gitlab-repopolicy                         binary
/etc/gitlab-repopolicy/config.yaml                 config, root:<git-user> 0640, conffile
/etc/gitlab-repopolicy/breakglass/                 root 0755
/var/log/gitlab-repopolicy/                        <git-user> 0750
/usr/lib/systemd/system/gitlab-repopolicy-*.{service,timer}   disabled timers
```

`<git-user>` is the user Gitaly runs hooks as: `git` in the Linux package. On the first installation the package takes it from the variable `GITLAB_REPOPOLICY_USER` (default `git`). If the user does not exist yet (GitLab is not installed), the package only warns, and `install-hook --user` sets the owners later.

An upgrade does not change the permissions an administrator set and does not overwrite a changed `config.yaml`: dpkg asks which version to keep (with `--force-confold` it keeps yours and puts the new one next to it as `.dpkg-dist`), rpm keeps yours and puts the new one as `.rpmnew`.

**Without the package** (archive, Docker, other distributions): unpack `gitlab-repopolicy` into `/usr/bin/` and create the same directories and files with the same permissions. A sample `config.yaml` is [config/config.yaml](../config/config.yaml).

## 3. Service account and token

The hook reads user roles, protected branches and SSH keys from the GitLab API. This needs a separate account with minimal rights:

| What | Requirement |
| --- | --- |
| account | an instance service account (Admin area → Service accounts). Not a person: it does not sign in to the UI and does not depend on employees |
| membership | **Maintainer** in every top-level group. The hook does not check personal namespaces |
| token | a personal access token with scopes exactly `read_api` and `self_rotate` |
| expiry | no longer than the instance maximum. `token rotate` rotates it on a timer |

Other account types, roles and scopes are not supported: `selftest` rejects them. The service account has no administrator rights.

Through the API, with an administrator token:

```sh
GITLAB=https://gitlab.example.com
ADMIN_TOKEN=...      # an administrator token with scope api, only for these steps

# service account
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" -X POST "$GITLAB/api/v4/service_accounts" \
  --data name=gitlab-repopolicy --data username=gitlab-repopolicy
# id from the response
SA_ID=...

# token: scopes read_api and self_rotate, expiry no longer than the instance maximum
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" -X POST "$GITLAB/api/v4/users/$SA_ID/personal_access_tokens" \
  --data name=gitlab-repopolicy --data 'scopes[]=read_api' --data 'scopes[]=self_rotate' \
  --data expires_at="$(date -d '+364 days' +%F)"

# Maintainer in every top-level group
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" -X POST "$GITLAB/api/v4/groups/<group-id>/members" \
  --data user_id="$SA_ID" --data access_level=40
```

Write the token on every Gitaly node:

```sh
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'     # paste the token, Enter, Ctrl-D
```

The file gets mode 0440, and `install-hook --user` sets the group of the git user on it in step 5: together this is `root:git 0440`. The hook only reads the token, only `token rotate` run as root changes it.

For a new top-level group, remember to add the service account to it. Without that, protected changes in its projects are denied with `authorization_backend_failure`. `coverage` with an administrator token finds groups without the service account (the token is not stored on the node):

```sh
sudo gitlab-repopolicy coverage --token-file /root/admin-token
```

`coverage` prints the top-level groups without the service account and the projects with policy files in them; the exit code is 1 if there are any.

## 4. Configuration

Most single-node installations do not need to change `/etc/gitlab-repopolicy/config.yaml`: by default the hook reaches the API through the Workhorse unix socket of the same node. If Gitaly runs on a separate node, set the HTTPS address of GitLab and, if needed, the CA:

```yaml
gitlab_url: https://gitlab.example.com
ca_file: /etc/gitlab-repopolicy/ca.crt
```

All settings - [configuration.en.md](configuration.en.md).

For the first run on a production instance, set `mode: audit`: the hook denies nobody and writes decisions to audit with `"enforced": false`. How to move to `enforce` - [integration-guide.en.md](integration-guide.en.md).

## 5. Connect the hook

Gitaly runs global hooks from the `custom_hooks_dir` directory. Set it in the GitLab configuration first:

| Installation | Where `custom_hooks_dir` is set |
| --- | --- |
| Linux package | `/etc/gitlab/gitlab.rb`, then `sudo gitlab-ctl reconfigure` |
| Docker | `GITLAB_OMNIBUS_CONFIG` or `gitlab.rb` in the volume; the directory - in a persistent volume |
| Self-compiled | `[hooks] custom_hooks_dir` in `gitaly/config.toml` |
| separate Gitaly nodes, Gitaly Cluster | on every Gitaly node; in Gitaly Cluster hooks run only on the primary |

Linux package:

```ruby
# /etc/gitlab/gitlab.rb
gitaly['configuration'] = {
  hooks: {
    custom_hooks_dir: '/var/opt/gitlab/gitaly/custom_hooks',
  },
}
```

If `gitlab.rb` already has `gitaly['configuration']`, add the `hooks` key to the existing hash instead of a new one.

```sh
sudo gitlab-ctl reconfigure
```

`reconfigure` writes the directory into the Gitaly configuration but does not create the directory. `install-hook` creates it if the parent directory exists (so a typo in the path does not create a stray directory):

```sh
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
```

`install-hook`:
- creates `custom_hooks_dir/pre-receive.d/50-gitlab-repopolicy`, a wrapper that runs `/usr/bin/gitlab-repopolicy hook pre-receive --config /etc/gitlab-repopolicy/config.yaml`. Owner `--user`, mode 0755
- sets the group of `config.yaml` and the token and the owner of the audit log directory for `--user`
- running it again changes nothing (`unchanged`). It does not touch other files in `pre-receive.d/`

Gitaly runs the files of `pre-receive.d/` in name order and stops at the first one that fails. Gitaly does not run a separate `custom_hooks_dir/pre-receive` file next to `pre-receive.d/`: put your hooks into `pre-receive.d/`. Gitaly skips files whose names end with `~`.

## 6. Check

```sh
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

`selftest` checks the config, the modules, the audit log, the break-glass directory, the token file, the service account (`bot`, `external`, not an administrator), the scopes and expiry of the token and API access. Any `FAIL` means exit code 1.

Run `selftest` both as root and as the git user: the hook runs as git, and only this shows that it can read the config and the token. For example, a `/etc/gitlab-repopolicy` directory with mode 0750 root goes unnoticed by every check run as root, while the hook denies every push to group projects with "push could not be verified".

```text
$ sudo -u git gitlab-repopolicy selftest
info version     gitlab-repopolicy 1.0.0
ok   config      /etc/gitlab-repopolicy/config.yaml, mode enforce
ok   modules     enabled: pathpolicy, commitpolicy, tagpolicy
ok   audit       /var/log/gitlab-repopolicy/audit.log does not exist yet; the directory belongs to 998:998
ok   breakglass  /etc/gitlab-repopolicy/breakglass, no active grants
ok   api         http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket reachable as gitlab-repopolicy (id 2)
ok   account     gitlab-repopolicy is a service account
ok   token       scopes read_api, self_rotate, expires on 2027-10-13
info git         git version 2.55.0-rc1.g02bb39c in PATH of selftest; the hook uses the git of Gitaly
```

The `git` line is informational: the hook uses the git that Gitaly passes to it, and writes that version to audit.

Then the real check: add PATHPOLICY to a test project and make a push that breaks it ([pathpolicy.en.md](pathpolicy.en.md)). The decision is visible in the push output and in `/var/log/gitlab-repopolicy/audit.log`.

## 7. Timers

The package installs two systemd timers, disabled:

| Timer | What it does | How often |
| --- | --- | --- |
| `gitlab-repopolicy-token-rotate.timer` | `token rotate`: rotates the token 30 days before it expires | daily |
| `gitlab-repopolicy-settings-check.timer` | `settings-check`: compares project settings ([operations.en.md](operations.en.md)) | every 15 minutes |

```sh
sudo systemctl enable --now gitlab-repopolicy-token-rotate.timer gitlab-repopolicy-settings-check.timer
```

Both services run as root in a restricted systemd environment (write access only to their own directories, no new privileges).

## Docker

- mount the binary into the GitLab container read-only, for example as `/usr/bin/gitlab-repopolicy`
- put `config.yaml` and the token into the container as files that root of the container can change: `install-hook` changes their group, and `token rotate` replaces the token
- keep `custom_hooks_dir` and `/var/log/gitlab-repopolicy` in a persistent volume so that they survive a re-created container
- run `install-hook` and `selftest` in the container: `docker exec -it gitlab gitlab-repopolicy ...`

## Separate Gitaly nodes and Gitaly Cluster

- the binary, `config.yaml`, the token and `install-hook` on every Gitaly node, the same version everywhere. It shows in every audit event
- `gitlab_url` is the HTTPS address of Rails, `ca_file` if the certificate is not in the system store
- break-glass applies on the node where it was granted: grant it on every node where the hook can run

## For developers

To check policy files and commit messages locally (`validate`, `lint-commit`), building the binary is enough:

```sh
go install github.com/Kuzmenko-Pavel/gitlab-repopolicy/cmd/gitlab-repopolicy@v1.0.0
```

Such a build is not signed. Install only verified release artifacts on servers.

## Turn off and remove

```sh
sudo gitlab-repopolicy uninstall-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks
sudo apt remove gitlab-repopolicy        # or dnf remove
```

`uninstall-hook` removes only its own wrapper. Removing the package disables the timers but does not touch the hook: if you forget `uninstall-hook`, the wrapper points to a missing binary, and GitLab rejects every push with "Prevented by server hooks". `config.yaml` and the token stay in place after `apt remove`; delete them by hand if they are no longer needed. To turn checks off quickly without removing anything, set `mode: off` in the config ([operations.en.md](operations.en.md)).
