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

The package is not installed into the GitLab Docker image, see [Docker](#docker).

**Without the package** (archive, other distributions): unpack `gitlab-repopolicy` into `/usr/bin/` and create the same directories and files with the same permissions. A sample `config.yaml` is [config/config.yaml](../config/config.yaml).

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
- sets the group of `config.yaml` and the token and the owner of the audit log directory for `--user`. If the mode of the log directory is wider than 0750 or that of the audit file wider than 0640, narrows it
- records the path of `custom_hooks_dir` in `/var/lib/gitlab-repopolicy/hooks`, so that removing the package removes the wrapper
- running it again changes nothing and says so: `unchanged`, `owner unchanged (0:998)`, `mode unchanged (0750)`. It does not touch other files in `pre-receive.d/`

Gitaly runs the files of `pre-receive.d/` in name order and stops at the first one that fails. Gitaly does not run a separate `custom_hooks_dir/pre-receive` file next to `pre-receive.d/`: put your hooks into `pre-receive.d/`. Gitaly skips files whose names end with `~`.

## 6. Check

```sh
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

`selftest` checks the config, the modules, the audit log, the break-glass directory, the token file, the service account (`bot`, `external`, not an administrator), the scopes and expiry of the token, API access and the number of groups where the service account is a Maintainer. Any `FAIL` means exit code 1. If the service account is a member of no group, the `groups` line is `FAIL`: the hook cannot check any group project. Which groups are missing, `coverage` shows.

Run `selftest` both as root and as the git user: the hook runs as git, and only this shows that it can read the config and the token. For example, a `/etc/gitlab-repopolicy` directory with mode 0750 root goes unnoticed by every check run as root, while the hook denies every push with "internal error: the hook configuration on the server is invalid". An unreadable token shows only at the first API call: pushes that need no API pass, protected changes get "this project is not connected to gitlab-repopolicy correctly".

```text
$ sudo -u git gitlab-repopolicy selftest
info version     gitlab-repopolicy 1.0.0
ok   config      /etc/gitlab-repopolicy/config.yaml, mode enforce
ok   modules     enabled: pathpolicy, commitpolicy, tagpolicy
ok   audit       /var/log/gitlab-repopolicy/audit.log does not exist yet; the directory belongs to 998:998
ok   breakglass  /etc/gitlab-repopolicy/breakglass, no active grants
ok   api         http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket reachable as gitlab-repopolicy (id 2)
ok   account     gitlab-repopolicy is a service account
ok   groups      Maintainer of 3 groups; gitlab-repopolicy coverage checks every top-level group
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
| `gitlab-repopolicy-settings-check.timer` | `settings-check`: compares project settings and checks that the hook can write audit ([operations.en.md](operations.en.md#settings-check)) | daily |

```sh
sudo systemctl enable --now gitlab-repopolicy-token-rotate.timer gitlab-repopolicy-settings-check.timer
```

Both timers run once a day with a random delay of up to an hour. A different schedule is a drop-in file ([operations.en.md](operations.en.md#settings-check)).

Both services run as root in a restricted systemd environment: write access only to their own directories, no new privileges, two capabilities - `CAP_CHOWN` (the owner of the token and of a new audit log) and `CAP_DAC_OVERRIDE` (the log directory belongs to the hook user).

## Docker

**Do not install the package into the GitLab container.** The package lives in the container layer, while the hook wrapper lies in `custom_hooks_dir`, that is in a persistent volume. After `docker compose up --force-recreate`, which is what any GitLab upgrade in Docker looks like, the binary is gone and the wrapper stays. GitLab then rejects every push of the instance with `exec: /usr/bin/gitlab-repopolicy: not found`. systemd timers do not work in the container either: the GitLab image has no systemd.

So everything that has to survive a re-created container lives on the host:

| On the host | In the container | Why |
| --- | --- | --- |
| a directory with the binary, for example `/opt/gitlab-repopolicy/bin/` | `/opt/gitlab-repopolicy/bin`, read-only | the directory is mounted, not the file: a new binary put there on the host is seen in the container at once |
| `/srv/gitlab-repopolicy/etc/` with `config.yaml` and `token` | `/etc/gitlab-repopolicy` | `token rotate` writes the new token next to the old one and renames it over the old one, which does not work for a file mounted on its own |
| a volume or a directory | `/var/log/gitlab-repopolicy` | the audit log |
| the GitLab data volume | `/var/opt/gitlab` | `custom_hooks_dir` is already there |

A `compose.yaml` fragment:

```yaml
services:
  gitlab:
    image: gitlab/gitlab-ce:19.4.2-ce.0
    environment:
      GITLAB_OMNIBUS_CONFIG: |
        gitaly['configuration'] = {
          hooks: { custom_hooks_dir: '/var/opt/gitlab/gitaly/custom_hooks' }
        }
    volumes:
      - gitlab-config:/etc/gitlab
      - gitlab-logs:/var/log/gitlab
      - gitlab-data:/var/opt/gitlab
      - /opt/gitlab-repopolicy/bin:/opt/gitlab-repopolicy/bin:ro
      - /srv/gitlab-repopolicy/etc:/etc/gitlab-repopolicy
      - repopolicy-log:/var/log/gitlab-repopolicy

volumes:
  gitlab-config:
  gitlab-logs:
  gitlab-data:
  repopolicy-log:
```

Installation. The binary from the release archive goes to `/opt/gitlab-repopolicy/bin/`, `config.yaml` from the sample [config/config.yaml](../config/config.yaml) and the token go to `/srv/gitlab-repopolicy/etc/`. Then in the container:

```sh
docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy install-hook \
  --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git \
  --binary /opt/gitlab-repopolicy/bin/gitlab-repopolicy
docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy selftest
docker exec -u git gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy selftest
```

`docker exec` runs as root of the container, `docker exec -u git` as the user Gitaly runs hooks as. The GitLab image has no `sudo`, so commands of this documentation like `sudo -u git gitlab-repopolicy ...` become `docker exec -u git gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy ...` in Docker.

**Timers run on the host.** A systemd timer of the host calls the command in the container:

```ini
# /etc/systemd/system/gitlab-repopolicy-token-rotate.service
[Unit]
Description=Rotate the token of the gitlab-repopolicy service account

[Service]
Type=oneshot
ExecStart=/usr/bin/docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy token rotate

# /etc/systemd/system/gitlab-repopolicy-token-rotate.timer
[Timer]
OnCalendar=daily
RandomizedDelaySec=1h
Persistent=true

[Install]
WantedBy=timers.target
```

The same for `settings-check`: a service with `ExecStart=... gitlab-repopolicy settings-check` and the same timer. The same through cron on the host:

```text
# /etc/cron.d/gitlab-repopolicy
17 3 * * * root docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy token rotate
47 3 * * * root docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy settings-check
```

Without rotation the token stops working at its expiry, and every protected change is denied with `authorization_backend_failure`.

**Upgrading gitlab-repopolicy.** The new binary is put next to the old one under a temporary name and renamed over it. Pushes in progress finish on the old binary, the next ones take the new one:

```sh
install -m 0755 gitlab-repopolicy /opt/gitlab-repopolicy/bin/.gitlab-repopolicy.new
mv /opt/gitlab-repopolicy/bin/.gitlab-repopolicy.new /opt/gitlab-repopolicy/bin/gitlab-repopolicy
```

Then `selftest` as root and as git.

**Upgrading GitLab.** Re-creating the container does not touch the binary, the config, the token or `custom_hooks_dir`. After the upgrade, `selftest` as root and as git.

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

To turn checks off quickly without removing anything, set `mode: off` in the config ([operations.en.md](operations.en.md)).

Removing the package turns the checks off on purpose, and the package cleans up after itself:

```sh
sudo apt remove gitlab-repopolicy        # or dnf remove
```

```text
gitlab-repopolicy: the hook is removed from /var/opt/gitlab/gitaly/custom_hooks: pushes are no longer checked
```

| Action | What happens |
| --- | --- |
| package upgrade | nothing changes: the hook, the timers, the config and the token stay |
| `apt remove`, `dnf remove` | the timers are disabled, the wrapper is removed from every `custom_hooks_dir` where `install-hook` put it. `config.yaml`, the token and break-glass stay |
| `apt purge` | also removes `/etc/gitlab-repopolicy` (config, token, break-glass) and `/var/lib/gitlab-repopolicy`. rpm has no such step, remove these directories by hand |
| any | the audit log `/var/log/gitlab-repopolicy` is never removed: it is the record of the decisions of the hook |

If the package could not remove the wrapper, it prints `remove <dir>/pre-receive.d/50-gitlab-repopolicy by hand`. Then delete the file by hand or run `gitlab-repopolicy uninstall-hook --hooks-dir <dir>` before removing the package. A wrapper without the binary rejects every push of the instance. Without the package (Docker, archive) the wrapper is always removed by hand with `uninstall-hook`.

After removing it from every node, revoke the token of the service account so that it does not keep working until its expiry. With an administrator token:

```sh
# token id
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" "$GITLAB/api/v4/personal_access_tokens?user_id=$SA_ID&state=active"
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" -X DELETE "$GITLAB/api/v4/personal_access_tokens/<token-id>"
```

Or delete the service account itself in Admin area → Service accounts: its tokens and group memberships go with it.

## FAQ

**Can the package be installed into the GitLab container?** No: after the container is re-created the binary is gone, the wrapper stays, and GitLab starts rejecting every push. How to install in Docker is in [Docker](#docker).

**How do I tell from audit how a push ended?** All events of one run of the hook share one `receive_id`. The push was rejected if one of them is `"decision":"deny"` with `"enforced":true`: git rejects the whole push even if only one ref was denied. Details are in [operations.en.md](operations.en.md#audit).

**How do I notice that the token stopped working?** Every denial caused by an API failure writes an `authorization_backend_failure` event, also when GitLab answers 401 to the token. An alert on a series of such events catches an expired or revoked token ([operations.en.md](operations.en.md#events-and-alerts)).

**`selftest` says `ok`, but the projects of a group are not checked.** The service account was not added to the top-level group. `selftest` shows the number of groups in the `groups` line, and `coverage` shows which ones are missing (step 3).

**The commit-msg hook rejects `feat:`, though main allows it.** Without `--ruleset`, `lint-commit` applies every ruleset of the file, because locally it is not known which branch the commit goes to. Name the ruleset you need: `lint-commit --ruleset protected` ([commitpolicy.en.md](commitpolicy.en.md#checking-locally)).

**The `settings_check` expectations are the same for the whole instance, but infra projects have no tags.** `protected_tags` is checked only in projects with TAGPOLICY on the default branch, so projects without release tags do not drift. Keep the `ci_config_path` check also for projects without `.gitlab-ci.yml` ([configuration.en.md](configuration.en.md#settings_check)).

**`gitlab-repopolicy test` found nothing in a new branch.** For a new branch the hook checks only new commits, the ones not yet in the repository. In a working clone the commit is already on another local branch, so `test` prints `allowed: no rule was touched` with a hint. Run `test` in a copy that has only the branches of the server ([operations.en.md](operations.en.md#check-a-push-without-pushing)).

**Where do I get the project ID for break-glass?** `breakglass grant` also takes the path, `--project group/project`. If the API is not available, `--project-id` is needed: the ID is shown on the project's main page under its name.

**A merge through the API answers only "Branch cannot be merged".** GitLab writes the reason of the denial into the `merge_error` field of the merge request (`GET /projects/:id/merge_requests/:iid`); in the UI it is shown on the MR page.

**The `/var/log/gitlab-repopolicy` directory was created in advance with mode 0755.** `install-hook` narrows it to 0750 and the audit file to 0640, and says what it changed. `selftest` warns if anyone can read audit.
