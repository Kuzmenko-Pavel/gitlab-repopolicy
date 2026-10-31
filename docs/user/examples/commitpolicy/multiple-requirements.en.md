[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](multiple-requirements.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](multiple-requirements.en.md)

# Multiple independent commit requirements

The default branch must contain no WIP commits, and each ordinary commit must reference an issue in a trailer. Both requirements are about the same commits, so this is one rule with two `pattern` checks: each has its own name, while `match` and `except` are shared.

## COMMITPOLICY file

Add this file to the repository root, run `gitlab-repopolicy validate`, and activate it by merging into the default branch.

```yaml
# examples/commit-patterns/COMMITPOLICY
# Two requirements for the commits of the default branch in one rule, two
# named pattern checks: no work-in-progress headers, and a reference to an
# issue in a trailer.
version: 1

rules:
  default-branch:
    match: default
    except:
      merge_commit: true
    checks:
      trailer-required:
        issue-ref: { level: error, value: required }
      pattern:
        no-wip: { level: error, location: header, value: '!/^(WIP|fixup!|squash!)/i' }
        issue-ref: { level: error, location: trailer, value: '/(?m)^(Refs|Closes): [A-Za-z0-9_.\/-]*#[0-9]+$/' }
    message: >-
      Squash or reword work-in-progress commits before the merge, and end the
      message with a trailer such as "Refs: payments/api#123".
```

`no-wip` checks forbidden prefixes case-insensitively. `trailer-required` requires a trailer, the last part of the message, and `pattern issue-ref` requires a `Refs` or `Closes` line with a reference in it. The name `issue-ref` in two types is two separate checks, a denial shows each together with its type. The rule skips a merge commit entirely (`except.merge_commit`): GitLab writes its header, it never has a WIP prefix, and it has no issue reference. Each check has its own `level`, so one of them can become a warning without touching the other.

<!-- check: lint-commit examples/commit-patterns/COMMITPOLICY -->
| Message | Result | Why |
| --- | --- | --- |
| `Fix login\n\nRefs: payments/api#12` | passes | both checks are met |
| `Fix login\n\nCloses: #12` | passes | `Closes` is in the regex too |
| `Fix login\n\nRefs: 12` | denied | `pattern issue-ref`: the value has no `#` |
| `fixup! Fix login\n\nRefs: #12` | denied | `pattern no-wip`: the header starts with `fixup!` |

Save the message with actual line breaks in `message.txt` and run `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. The `fixup!` row has a valid reference but still fails: both requirements must hold.

Do not replace the two checks with one regex of alternatives: that would require only one match instead of both. A project overrides a preset with these checks by type and name: `pattern: {issue-ref: {level: warn}}` together with `trailer-required: {issue-ref: {level: warn}}` make only the requirement of the issue reference a warning. After removing the WIP prefix and adding a valid trailer, expect exit 0. Policy `warn` and global `audit` change enforcement compared with the table's `enforce` results.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
