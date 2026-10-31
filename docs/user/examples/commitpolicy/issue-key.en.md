[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](issue-key.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](issue-key.en.md)

# A free-form header with an issue key

The team writes `PAY-123 Round refunds to cents`. It does not use `feat` and `fix` types, but requires an issue key at the start of the first line.

## Project rule

Save this as `COMMITPOLICY` in the repository root and run `gitlab-repopolicy validate -f COMMITPOLICY` before merging it into the default branch.

```yaml
# examples/free-format/COMMITPOLICY
# A free-form header that starts with an issue key, on the default branch only.
version: 1

rules:
  issue-key:
    match: default
    parser: free
    except:
      merge_commit: true
    checks:
      header-max-length:
        limit: { level: error, value: 72 }
      subject-trailing-dot:
        no-dot: { level: error, value: forbidden }
      pattern:
        issue-key: { level: error, location: header, value: '/^[A-Z][A-Z0-9]+-[0-9]+ \S/' }
    message: Start the header with the issue key, for example PAY-123.
```

`parser: free` treats the entire header as the subject. The regex uses `^` to require the key at the start, rather than anywhere in the line. `match: default` leaves work branches unrestricted; an ordinary merge commit has its own exception.

<!-- check: lint-commit examples/free-format/COMMITPOLICY -->
| Message | Result | Why |
| --- | --- | --- |
| `PAY-123 Round refunds to cents` | passes | the issue key is at the start |
| `Round refunds to cents` | denied | `pattern issue-key`: no key at the start of the header |
| `pay-123 round refunds` | denied | `pattern issue-key`: the issue key is in capital letters |
| `PAY-123 Round refunds to cents.` | denied | `subject-trailing-dot`: with `parser: free` the subject is the whole header |

Check each message with `gitlab-repopolicy lint-commit -m "PAY-123 Round refunds to cents"`, substituting a table row. A valid row exits 0, a violation exits 1. Under global and policy `enforce`, these errors block the corresponding incoming commits on the default branch.

Do not add `type-enum` or `scope-enum` to `parser: free`: those checks require `conventional`. If the issue key belongs in a trailer, change `location`; `header` does not search the entire message.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
