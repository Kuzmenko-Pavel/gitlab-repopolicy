[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](exception-vs-deny.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](exception-vs-deny.en.md)

# A match exception is not a prohibition

You need to forbid `tmp/...`. An exception in a work rule's `match` looks convenient, but only removes the name from that class. Without another match, the branch passes unchecked.

## Why this file does not forbid tmp/x

Save this as `BRANCHPOLICY` in the current directory:

```yaml
version: 1
rules:
  work:
    match: ['/^[a-z0-9.]+\/[a-z0-9._-]+$/', '!/^tmp\//']
    priority: 20
    message: Work branches are <prefix>/<name>.
```

The file is valid. `gitlab-repopolicy validate --branch tmp/x` reports `branch tmp/x: no rule matches, the name is not checked`. This is not a parser error: the rule fails to match exactly as configured.

## Explicit denial

Replace the file with this complete configuration:

```yaml
version: 1
rules:
  no-tmp:
    match: ['/^tmp\//']
    priority: 10
    deny: true
    message: Temporary branches are not pushed; keep them local.
  work:
    match: ['/^[a-z0-9.]+\/[a-z0-9._-]+$/']
    priority: 20
    message: Work branches are <prefix>/<name>.
```

`no-tmp`, with a lower `priority`, catches `tmp/x` before `work` and produces `branch_name_violation` under global and policy `enforce`. `bob/fix` matches `work`; `fix-login` remains unmatched and unchecked.

Run `gitlab-repopolicy validate -f BRANCHPOLICY`, inspect `validate --branch tmp/x`, merge the file into the default branch, and reproduce creation of `tmp/x` in a test project. Do not expect an existing branch to be deleted or stop accepting commits: BRANCHPOLICY checks creation only.

Exit 0 from `validate --branch` means a completed query, even when its text says `result: denied`. Use `test` or a real push with the required GitLab permissions to check the final decision.

[BRANCHPOLICY reference](../../policies/branchpolicy.en.md)
