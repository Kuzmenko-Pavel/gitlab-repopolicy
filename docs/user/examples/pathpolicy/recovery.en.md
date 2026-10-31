[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](recovery.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](recovery.en.md)

# Recovering PATHPOLICY ownership

alice was the policy's sole admin but lost access or became a Developer. An Owner needs to appoint a new owner without permitting arbitrary protected CI changes in the same push.

## Initial state

The following `PATHPOLICY` is already stored in the default branch root:

```yaml
version: 1
admins: [alice]
rules:
  ci:
    match: all
    paths: [".gitlab-ci.yml"]
    allow: [alice]
    message: CI belongs to alice.
```

Recovery requires GitLab to confirm that none of `admins` is an active project Maintainer or Owner, including inherited roles. An admin being on leave or not replying does not satisfy this condition.

| Who changes PATHPOLICY | Working admins by the PATHPOLICY of the default branch | Result |
| --- | --- | --- |
| an admin | any | allowed |
| an Owner of the project, not an admin | at least one | denied with `policy_admin_violation` |
| an Owner of the project, not an admin | none, and the API confirmed it | allowed, recovery |
| an Owner of the project, not an admin | the API did not answer | denied with `authorization_backend_failure` |
| a deploy key | any | denied with `unknown_principal` |
| everybody else | any | denied with `policy_admin_violation` |

A failure of the API never turns recovery on. An instance administrator is not a member of the project and does not get recovery: the way for them is to become an Owner (it shows in the audit of GitLab) or break-glass ([operations.en.md](../../administration/operations.en.md#break-glass)).

A recovery push changes only PATHPOLICY: protected files in the same push are checked by the old rules. The Owner sees `PATHPOLICY is changed by Owner <username> because it has no working admins. This is logged.` in the output of the push, and the event `policy_recovery` is written to audit. It deserves an alert.

## Recovery procedure

On a Gitaly node, an operator with access to the server configuration and token runs `gitlab-repopolicy admins --project group/project`. An API failure is a failed check, not confirmation that no admins remain.

The Owner prepares PATHPOLICY with new `admins` and the appropriate `allow`, checks it with `validate --online --config /etc/gitlab-repopolicy/config.yaml --project group/project -f PATHPOLICY`, and pushes only the policy repair. The new admin must exist and have Maintainer or Owner access. The recovery push still checks CI against the old rule, so push CI changes separately after the new policy is active.

The hook reports `PATHPOLICY is changed by Owner <username> because it has no working admins. This is logged.` and creates `policy_recovery` if it permits the push. In global `audit`, this does not establish that recovery was necessary for permission, because denials are not enforced.

If the API is unavailable or the Owner cannot repair the policy, root can use [break-glass](../../administration/operations.en.md#break-glass). An instance administrator does not automatically qualify for recovery solely by being an administrator.

After recovery, run `admins` again, confirm the new file is active on the default branch and check that the new admin can make a control change. Keep several working admins so one person's ordinary absence does not require an emergency procedure.

[PATHPOLICY reference](../../policies/pathpolicy.en.md)
