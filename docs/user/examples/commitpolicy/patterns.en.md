[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](patterns.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](patterns.en.md)

# Matching the right part of a message

An issue key in the header, a list in the body and a reference in a trailer are different requirements. Searching the entire message can allow an unwanted format because another part happens to match.

## One `pattern` check

For the first table row, create this complete `COMMITPOLICY` in the current directory:

```yaml
version: 1
rules:
  issue-key:
    match: all
    checks:
      trailer-required:
        trailer: {level: error, value: optional}
      pattern:
        issue-key: {level: error, location: header, value: '/^[A-Z][A-Z0-9]+-[0-9]+ /'}
    message: Start the header with an issue key.
```

Run `gitlab-repopolicy validate -f COMMITPOLICY`, then `lint-commit -m "PAY-12 Round refunds"`. For each following row, replace the fields `location` and `value` of `checks.pattern.issue-key` and `value` of `checks.trailer-required.trailer` with that row's values. Keep the rest of the file unchanged. Each row is a separate experiment.

`trailer-required` in the file sets how the hook finds the trailer ([message parsing](../../policies/commitpolicy.en.md#how-the-hook-parses-a-message)). With `optional` the paragraph right after the header is the body, and a trailer exists only after one more empty line. With `required` there is always a trailer when the header is followed by lines.

<!-- check: pattern -->
| `location` | `value` | `trailer-required` | Message | Result | Why |
| --- | --- | --- | --- | --- | --- |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | `optional` | `PAY-12 Round refunds` | passes | the issue key is at the start of the header |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | `optional` | `Round refunds PAY-12` | denied | the key is there, but not at the start, and the regex has `^` |
| `header` | `!/^(WIP\|fixup!)/i` | `optional` | `wip: try` | denied | `!` requires that the regex is not found, and `i` ignores case |
| `header` | `!/^(WIP\|fixup!)/i` | `optional` | `Fix login` | passes | the regex is not found |
| `body` | `/(?m)^- /` | `optional` | `Fix login\n\n- check the token` | passes | `(?m)` makes `^` the start of every line of the body |
| `body` | `/(?m)^- /` | `optional` | `Fix login\n\nChecks the token.` | denied | the body has no list line |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `required` | `Fix login\n\nRefs: payments/api#12` | passes | the `Refs` line of the trailer matches |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `required` | `Fix login\n\nWhy it changed.\n\nRefs: #12\nSigned-off-by: A <a@example.com>` | passes | `(?m)` finds the `Refs` line among the other lines of the trailer |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `required` | `Fix login\n\nCloses: #12` | denied | there is a trailer, but it has no `Refs` line |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `required` | `Fix login\n\nSee #12 in the tracker` | denied | the trailer is the last paragraph, but it is plain text without `Refs:` |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `optional` | `Fix login\n\nRefs: payments/api#12` | denied | the paragraph right after the header is the body, there is no trailer |
| `message` | `/#[0-9]+/` | `optional` | `Fix login (#12)` | passes | `message` is the whole message, the header too |
| `message` | `!/password/i` | `optional` | `Fix login\n\nThe Password is hunter2.` | denied | the regex is found in the body |

In table messages, `\n` represents an actual line break. Save the message in `message.txt` and pass `-m "$(cat message.txt)"`. Rows marked "passes" exit 0; "denied" rows exit 1.

`!` before a regex forbids a match, `i` ignores case, and `(?m)` changes line boundaries for `^` and `$`. For the trailer the regex is searched in its whole text, so one regex checks both the key and the value of a line, as `Refs: ...` above. The `password` example checks a text pattern, not every possible secret.

A common mistake is expecting a trailer where the parsing sees a body. A reference right after the header becomes the trailer only with `trailer-required: required`. To require two `pattern` conditions simultaneously, add two `pattern` checks with different names to the same rule ([multiple requirements](multiple-requirements.en.md)).

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
