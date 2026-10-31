[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](commitizen.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](commitizen.en.md)

# Enforcing commitizen requirements on the server

The team uses a type list, a header of at most 100 characters and a `ref`, `refs` or `issue` trailer containing a GitLab issue reference. COMMITPOLICY can enforce these requirements on the server independently of a client-side message generator.

## Configuration

Save this as `COMMITPOLICY` in the repository root, run `gitlab-repopolicy validate -f COMMITPOLICY`, and merge it into the default branch.

```yaml
# examples/commitizen/COMMITPOLICY
# The commitizen schema of a project: the type from a list, a header of up to
# 100 characters and a trailer ref, refs or issue with a GitLab issue. The
# checks cover the format, the list, the length and the empty line after the
# header; the trailer is required and the pattern covers its lines.
version: 1

rules:
  commitizen:
    match: all
    except:
      merge_commit: true
    checks:
      header-format:
        conventional: { level: error, value: required }
      header-max-length:
        commitizen: { level: error, value: 100 }
      type-enum:
        commitizen: { level: error, value: [build, ci, docs, feat, fix, perf, refactor, style, test, chore, revert, bump] }
      body-leading-blank-lines:
        blank-line: { level: error, value: required }
      trailer-required:
        ref: { level: error, value: required }
      pattern:
        ref: { level: error, location: trailer, value: '/(?m)^(ref|refs|issue): [a-zA-Z0-9_.\/-]*#\d+$/' }
    message: 'Commits follow the commitizen schema: type: subject, then a trailer such as "ref: payments/api#123".'
```

`header-format` with `value: required` rejects a header that is not in the Conventional Commits format, `type-enum` defines allowed types, `header-max-length` limits length, `body-leading-blank-lines` requires an empty line after the header. `trailer-required` requires a trailer, the last part of the message, and `pattern` checks that it has a `ref`, `refs` or `issue` line with an issue number. `(?m)` makes `^` and `$` the bounds of every line of the trailer, so other lines, such as `Signed-off-by`, may stand next to it. `match: all` extends requirements to new work-branch commits, rather than only merges into the default branch.

<!-- check: lint-commit examples/commitizen/COMMITPOLICY -->
| Message | Result | Why |
| --- | --- | --- |
| `feat: add x\n\nref: group/project#1` | passes | a type from the list, a trailer `ref` with an issue |
| `fix: x\n\nref: group/my.project#1\nSigned-off-by: A <a@example.com>` | passes | `Signed-off-by` in the same paragraph does not get in the way |
| `feat(: add x\n\nref: group/project#1` | denied | `header-format`: the header cannot be read |
| `feat: add x\nbody right after the header\nref: group/project#1` | denied | `body-leading-blank-lines`: no empty line after the header |
| `feat: add x\n\nSee group/project#1 in the tracker` | denied | `pattern ref`: there is a trailer, but without a `ref:` line |
| `feat: add x` | denied | `body-leading-blank-lines`, `trailer-required` and `pattern ref`: nothing follows the header |

Save a table message with actual line breaks in `message.txt` and run `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. Valid rows exit 0, violations exit 1. This checks the specific requirements shown, rather than all commitizen features or plugins.

Do not add a body directly after the header without an empty line. The reference must stand in the last paragraph as a line `ref: ...`: `pattern` looks for it only in the trailer. `Signed-off-by` in the same paragraph does not interfere but cannot replace `ref`.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
