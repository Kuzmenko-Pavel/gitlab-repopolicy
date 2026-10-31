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
# parser takes the header apart, the checks cover the list and the length,
# the pattern covers the trailer.
version: 1

rules:
  commitizen:
    match: all
    parser: conventional
    except:
      merge_commit: true
    checks:
      type-enum:         { level: error, value: [build, ci, docs, feat, fix, perf, refactor, style, test, chore, revert, bump] }
      header-max-length: { level: error, value: 100 }
      pattern:           { level: error, location: trailer, keys: [ref, refs, issue], value: '/^[a-zA-Z0-9_.\/-]*#\d+$/' }
    message: 'Commits follow the commitizen schema: type: subject, then a trailer such as "ref: payments/api#123".'
```

The parser reads a Conventional Commits header; `type-enum` defines allowed types, `header-max-length` limits length, and `pattern` checks the trailer value. `match: all` extends requirements to new work-branch commits, rather than only merges into the default branch.

<!-- check: lint-commit examples/commitizen/COMMITPOLICY -->
| Message | Result | Why |
| --- | --- | --- |
| `feat: add x\n\nref: group/project#1` | passes | a type from the list, a trailer `ref` with an issue |
| `fix: x\n\nref: group/my.project#1\nSigned-off-by: A <a@example.com>` | passes | other trailers do not get in the way |
| `feat(: add x\n\nref: group/project#1` | denied | the header cannot be read |
| `feat: add x\nbody right after the header\nref: group/project#1` | denied | no empty line after the header |
| `feat: add x` | denied | no trailer `ref` |

Save a table message with actual line breaks in `message.txt` and run `gitlab-repopolicy lint-commit -m "$(cat message.txt)"`. Valid rows exit 0, violations exit 1. This checks the specific requirements shown, rather than all commitizen features or plugins.

Do not add a body directly after the header without an empty line. Do not place the reference in a normal final paragraph: `pattern` checks a trailer with a selected key. Other trailers, including `Signed-off-by`, do not interfere but cannot replace `ref`.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
