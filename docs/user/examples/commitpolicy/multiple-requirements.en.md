[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](multiple-requirements.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](multiple-requirements.en.md)

# Multiple independent commit requirements

The default branch must contain no WIP commits, and each ordinary commit must reference an issue in a trailer. Each rule holds one `pattern`, so this needs two rules.

## COMMITPOLICY file

Add this file to the repository root, run `gitlab-repopolicy validate`, and activate it by merging into the default branch.

```yaml
# examples/commit-patterns/COMMITPOLICY
# Two requirements for the commits of the default branch, one rule each: no
# work-in-progress headers, and a reference to an issue in a trailer.
version: 1

rules:
  no-wip:
    match: default
    parser: free
    checks:
      pattern: { level: error, location: header, value: '!/^(WIP|fixup!|squash!)/i' }
    message: Squash or reword work-in-progress commits before the merge.
  issue-ref:
    match: default
    parser: free
    except:
      merge_commit: true
    checks:
      pattern: { level: error, location: trailer, keys: [Refs, Closes], value: '/^[A-Za-z0-9_.\/-]*#[0-9]+$/' }
    message: 'End the message with a trailer such as "Refs: payments/api#123".'
```

Both rules use `match: default` and apply together. `no-wip` checks forbidden prefixes case-insensitively. `issue-ref` requires one reference in `Refs` or `Closes`; merge commits skip this rule, but `no-wip` remains.

<!-- check: lint-commit examples/commit-patterns/COMMITPOLICY -->
| Message | Result | Why |
| --- | --- | --- |
| `Fix login\n\nRefs: payments/api#12` | passes | both rules are met |
| `Fix login\n\nCloses: #12` | passes | `Closes` is in `keys` too |
| `Fix login\n\nRefs: 12` | denied | `issue-ref`: the value has no `#` |
| `fixup! Fix login\n\nRefs: #12` | denied | `no-wip`: the header starts with `fixup!` |

Save the message with actual line breaks in `message.txt` and run `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. Checking all rules is appropriate here because they describe the same branch. The `fixup!` row has a valid reference but still fails: both requirements must hold.

Do not replace the two rules with one regex of alternatives: that would require only one match instead of both. After removing the WIP prefix and adding a valid trailer, expect exit 0. Policy `warn` and global `audit` change enforcement compared with the table's `enforce` results.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
