[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](header-parsing.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](header-parsing.en.md)

# Conventional Commits parsing errors

A header can fail parsing before length, type or case checks. Adding `type-enum` will not fix this: the first line needs valid syntax first.

## Minimal rule

Save this as `COMMITPOLICY` in the current directory and run `gitlab-repopolicy validate -f COMMITPOLICY`:

```yaml
version: 1
rules:
  r:
    match: all
    parser: conventional
    message: Use a Conventional Commits header.
```

`fix: round refunds` and `fix(billing): round refunds` are valid headers. The parenthesized scope is optional. Exactly one space must follow the colon, and an empty line must precede the body.

<!-- check: lint-commit parser=conventional -->
| Header | Error of `lint-commit` |
| --- | --- |
| `fix round refunds` | `header: expected "type(scope): subject"` |
| `fix:round refunds` | `header: expected "type(scope): subject", a space must follow ":"` |
| `fix:  round refunds` | `header: expected "type(scope): subject", exactly one space must follow ":"` |
| `fix(billing: round refunds` | `header: expected "type(scope): subject", the scope parenthesis is not closed` |
| `fix(): round refunds` | `header: the scope in parentheses is empty` |
| `fix:` | `header: expected "type(scope): subject", the subject is empty` |
| `fix (billing): round refunds` | `header: expected "type(scope): subject"` |
| `[fix] round refunds` | `header: expected "type(scope): subject"` |
| `fix: round\nbody right after` | `the body must be separated from the header by an empty line` |

The hint after the comma appears when the hook can tell what exactly is wrong. Without a hint the header does not look like `type(scope): subject` at all: a space before the parenthesis, square brackets, an issue key instead of a type.

To check a row, save it with actual line breaks in `message.txt`, then run `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. The table contains expected diagnostic fragments; each row exits 1. After correcting it to `fix: round refunds`, the command should exit 0 without findings.

`parser: free` permits another header format, but choosing it changes the project's standard rather than repairing a particular Conventional Commits message. A rule without `checks` still enforces its parser syntax.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
