[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](user-prefix.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](user-prefix.en.md)

# A username at the start of a work branch

The prefix helps identify the person associated with a branch. It checks username existence rather than ownership: creating a branch for a colleague is also allowed.

## Complete policy

Save this as `BRANCHPOLICY`, run `gitlab-repopolicy validate -f BRANCHPOLICY` and merge into the default branch. The regex also permits an optional issue number or Jira key between the username and name.

```yaml
version: 1
rules:
  work:
    match: ['/^[a-z0-9][a-z0-9._-]*\/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)\/)?[a-z0-9][a-z0-9._-]*$/']
    priority: 900
    user_prefix: true
    message: Work branches are <username>/[<issue>/]<name>, for example kpn/PAY-12/refunds; PAY is the Jira project of this team.
```

| Creator | Branch | Result with available API and enforce |
| --- | --- | --- |
| alice | `alice/disable_auth` | allowed, the username matches the recognized pusher |
| alice | `bob/fix` | allowed if bob is active |
| alice | `ghost/fix` | denied if ghost is missing or inactive |
| alice | `fix-login` | no match, name not checked |

The table concerns new non-default branches in a group project. A recognized user's own username needs no additional name lookup; SSH may still require checking the key type. Another username is checked through the API. An API failure warns rather than establishing that the user is missing.

```text
remote: BRANCHPOLICY rule "work", branch ghost/fix: the name does not follow the branch standard
remote:   ghost is not a GitLab user; the name starts with a GitLab username and /
remote:   Work branches are <username>/[<issue>/]<name>, for example kpn/PAY-12/refunds; PAY is the Jira project of this team.
remote:   how to fix: rename the branch locally (git branch -m) and push again
```

Offline `gitlab-repopolicy validate --branch bob/fix` only states the requirement. To look up the user, run `validate --online --config /etc/gitlab-repopolicy/config.yaml --branch bob/fix` on a node with server-token access. In either case, exit 0 from the query does not grant push permission.

Do not use `user_prefix` to enforce branch access. GitLab permissions govern that; rejecting names without a prefix needs a separate final `deny` for remaining names.

[BRANCHPOLICY reference](../../policies/branchpolicy.en.md)
