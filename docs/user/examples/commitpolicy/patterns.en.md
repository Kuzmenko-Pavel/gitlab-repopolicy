[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](patterns.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](patterns.en.md)

# Matching the right part of a message

An issue key in the header, a list in the body and a reference in a trailer are different requirements. Searching the entire message can allow an unwanted format because another part happens to match.

## One requirement per rule

For the first table row, create this complete `COMMITPOLICY` in the current directory:

```yaml
version: 1
rules:
  issue-key:
    match: all
    parser: free
    checks:
      pattern: {level: error, location: header, value: '/^[A-Z][A-Z0-9]+-[0-9]+ /'}
    message: Start the header with an issue key.
```

Run `gitlab-repopolicy validate -f COMMITPOLICY`, then `lint-commit -m "PAY-12 Round refunds"`. For each following row, replace `checks.pattern` with that row's `location`, `value` and nonempty `keys`. Keep the rest of the file unchanged. Each row is a separate experiment.

An empty `keys` cell means the field is omitted. A `keys` list is supported only with `location: trailer`; without it, the regex is searched in the values of all trailers.

<!-- check: pattern -->
| `location` | `value` | `keys` | Message | Result | Why |
| --- | --- | --- | --- | --- | --- |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | | `PAY-12 Round refunds` | passes | the issue key is at the start of the header |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | | `Round refunds PAY-12` | denied | the key is there, but not at the start, and the regex has `^` |
| `header` | `!/^(WIP\|fixup!)/i` | | `wip: try` | denied | `!` requires that the regex is not found, and `i` ignores case |
| `header` | `!/^(WIP\|fixup!)/i` | | `Fix login` | passes | the regex is not found |
| `body` | `/(?m)^- /` | | `Fix login\n\n- check the token` | passes | `(?m)` makes `^` the start of every line of the body |
| `body` | `/(?m)^- /` | | `Fix login\n\nChecks the token.` | denied | the body has no list line |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nRefs: payments/api#12` | passes | the value of the trailer `Refs` matches |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nCloses: #12` | denied | there is a trailer, but with another key |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nSee #12 in the tracker` | denied | the last paragraph is plain text, not a trailer |
| `message` | `/#[0-9]+/` | | `Fix login (#12)` | passes | `message` is the whole message, the header too |
| `message` | `!/password/i` | | `Fix login\n\nThe Password is hunter2.` | denied | the regex is found in the body |

In table messages, `\n` represents an actual line break. Save the message in `message.txt` and pass `-m "$(cat message.txt)"`. Rows marked "passes" exit 0; "denied" rows exit 1.

`!` before a regex forbids a match, `i` ignores case, and `(?m)` changes line boundaries for `^` and `$`. Trailer matching examines the value without the key; `keys` selects which keys to consider. The `password` example checks a text pattern, not every possible secret.

A common mistake is writing a normal paragraph such as `See #12 in the tracker` and expecting a trailer. Every line of the last paragraph must be a trailer. To require two `pattern` conditions simultaneously, create two rules with the same `match`.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
