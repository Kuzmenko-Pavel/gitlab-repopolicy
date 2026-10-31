[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](header-parsing.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](header-parsing.en.md)

# Conventional Commits parsing errors

A header can fail parsing before length, type or case checks. Then the checks of the header parts, such as `type-enum`, do not run, and `header-format` reports the error: the first line needs valid syntax first.

## Minimal rule

Save this as `COMMITPOLICY` in the current directory and run `gitlab-repopolicy validate -f COMMITPOLICY`:

```yaml
version: 1
rules:
  r:
    match: all
    parser: conventional
    checks:
      header-format:
        conventional: {level: error}
    message: Use a Conventional Commits header.
```

`fix: round refunds` and `fix(billing): round refunds` are valid headers. The parenthesized scope is optional. Exactly one space must follow the colon.

<!-- check: lint-commit parser=conventional -->
| Header | Error of `lint-commit` |
| --- | --- |
| `fix round refunds` | `header-format conventional: expected "type(scope): subject"` |
| `fix:round refunds` | `header-format conventional: expected "type(scope): subject", a space must follow ":"` |
| `fix:  round refunds` | `header-format conventional: expected "type(scope): subject", exactly one space must follow ":"` |
| `fix(billing: round refunds` | `header-format conventional: expected "type(scope): subject", the scope parenthesis is not closed` |
| `fix(): round refunds` | `header-format conventional: the scope in parentheses is empty` |
| `fix:` | `header-format conventional: expected "type(scope): subject", the subject is empty` |
| `fix (billing): round refunds` | `header-format conventional: expected "type(scope): subject"` |
| `[fix] round refunds` | `header-format conventional: expected "type(scope): subject"` |

The hint after the comma appears when the hook can tell what exactly is wrong. Without a hint the header does not look like `type(scope): subject` at all: a space before the parenthesis, square brackets, an issue key instead of a type.

The header is always the first line. `fix: round\nbody right after` is parsed without an error: the header is `fix: round`, the body is `body right after`. Whether an empty line is needed between them is set by a separate check `body-leading-blank-lines` ([message results](message-results.en.md)), not by the parser.

To check a row, save it with actual line breaks in `message.txt`, then run `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. The table contains expected diagnostic fragments; each row exits 1. After correcting it to `fix: round refunds`, the command should exit 0 without findings.

`parser: free` permits another header format, but choosing it changes the project's standard rather than repairing a particular Conventional Commits message. A rule with `parser: conventional` and without `header-format` does not reject a wrong header: the parser only takes the header apart for the other checks, and the denial comes from a check. `header-format` with `level: warn` makes a wrong header a warning, while the other checks of the rule stay errors.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
