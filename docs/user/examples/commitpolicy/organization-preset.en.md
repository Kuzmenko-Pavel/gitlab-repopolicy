[![Українська](https://img.shields.io/badge/lang-Українська-lightgrey.svg)](organization-preset.md)
[![English](https://img.shields.io/badge/lang-English-blue.svg)](organization-preset.en.md)

# Customizing a shared commit standard

The organization defines commit formatting centrally. A project keeps the type list and parser, but turns long-header errors into warnings and allows a full stop at the end of the subject.

## Two rule sources

An administrator places this fragment in the server `config.yaml` on every Gitaly node. Save it as `presets.yaml` too for local checks.

```yaml
presets:
  commitpolicy:
    org-commits:
      rules:
        conventional:
          match: default
          parser: conventional
          except:
            merge_commit: true
            git_reverts: true
          checks:
            header-format:
              conventional: { level: error }
            header-max-length:
              limit: { level: error, value: 100 }
            type-enum:
              allowed: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, test] }
            subject-trailing-dot:
              no-dot: { level: error, value: forbidden }
          message: Commits in the default branch follow Conventional Commits.
          help:
            docs_url: https://www.conventionalcommits.org/en/v1.0.0/
```

The project adds `COMMITPOLICY` in its root:

```yaml
# examples/org-commits/COMMITPOLICY
# The commit rules of the organization come from the preset org-commits.
# This project only warns about long headers and allows a full stop at the
# end of the subject: value optional lifts the requirement of the preset.
version: 1
extends: org-commits

rules:
  conventional:
    checks:
      header-max-length:
        limit: { level: warn }
      subject-trailing-dot:
        no-dot: { value: optional }
```

The file refers to the checks of the preset by type and name. `header-max-length.limit` inherits `value: 100`, changing only `level`. `subject-trailing-dot.no-dot` gets `value: optional`: the check stays in the rule, but the full stop may now be there or not. The requirement of any check of a feature is lifted the same way; `null` is needed only when the check must leave the rule completely. This differs from `mode: warn`: other policy errors can still block pushes.

```text
$ gitlab-repopolicy validate --config presets.yaml
COMMITPOLICY: valid
  extends: org-commits
  rule conventional: preset:org-commits, file
    checks: preset:org-commits, file
    checks.header-format: preset:org-commits
    checks.header-format.conventional: preset:org-commits
    checks.header-max-length: preset:org-commits, file
    checks.header-max-length.limit: preset:org-commits, file
    checks.subject-trailing-dot: preset:org-commits, file
    checks.subject-trailing-dot.no-dot: preset:org-commits, file
    checks.type-enum: preset:org-commits
    checks.type-enum.allowed: preset:org-commits
    except: preset:org-commits
    except.git_reverts: preset:org-commits
    except.merge_commit: preset:org-commits
    help: preset:org-commits
    help.docs_url: preset:org-commits
    match: preset:org-commits
    message: preset:org-commits
    parser: preset:org-commits
```

Run `gitlab-repopolicy validate -f COMMITPOLICY --config presets.yaml`, then `gitlab-repopolicy lint-commit -f COMMITPOLICY --config presets.yaml -m "docs: update the guide."`. The latter passes because the full stop is permitted. To check length, use a header longer than 100 characters: expect a warning and exit 0 if there are no other errors.

Without `--config`, local checking cannot find the preset. If the preset disappears from the server, the active COMMITPOLICY is skipped with a warning, but a new file version with that `extends` fails acceptance. Restore a consistent preset copy; do not remove `extends` merely to hide the failure.

[COMMITPOLICY reference](../../policies/commitpolicy.en.md)
