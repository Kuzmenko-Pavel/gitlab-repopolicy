[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](organization-preset.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](organization-preset.en.md)

# Налаштування стандарту комітів із пресета

Організація централізовано задає формат комітів. Проєкт залишає типи й parser, але замінює відмову за довгий header на попередження та дозволяє крапку наприкінці subject.

## Два джерела правил

Адміністратор розміщує цей фрагмент у серверному `config.yaml` на всіх вузлах Gitaly. Збережіть його також як `presets.yaml` для локальної перевірки.

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
            type-enum: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, test] }
            header-max-length: { level: error, value: 100 }
            subject-full-stop: { level: error, value: forbid }
          message: Commits in the default branch follow Conventional Commits.
          help:
            docs_url: https://www.conventionalcommits.org/en/v1.0.0/
```

Проєкт додає в корінь `COMMITPOLICY`:

```yaml
# examples/org-commits/COMMITPOLICY
# The commit rules of the organization come from the preset org-commits.
# This project only warns about long headers and allows a full stop at the
# end of the subject.
version: 1
extends: org-commits

rules:
  conventional:
    checks:
      header-max-length: { level: warn }
      subject-full-stop: null
```

`header-max-length` успадковує `value: 100`, змінюючи лише `level`. `subject-full-stop: null` видаляє перевірку повністю. Це відрізняється від `mode: warn`: решта помилок політики й надалі можуть блокувати push.

```text
$ gitlab-repopolicy validate --config presets.yaml
COMMITPOLICY: valid
  extends: org-commits
  rule conventional: preset:org-commits, file
    checks: preset:org-commits, file
    checks.header-max-length: preset:org-commits, file
    checks.type-enum: preset:org-commits
    except: preset:org-commits
    except.git_reverts: preset:org-commits
    except.merge_commit: preset:org-commits
    help: preset:org-commits
    help.docs_url: preset:org-commits
    match: preset:org-commits
    message: preset:org-commits
    parser: preset:org-commits
```

Запустіть `gitlab-repopolicy validate -f COMMITPOLICY --config presets.yaml`, потім `gitlab-repopolicy lint-commit -f COMMITPOLICY --config presets.yaml -m "docs: update the guide."`. Останнє повідомлення проходить, бо крапку дозволено. Для перевірки межі довжини використайте header понад 100 символів: очікується попередження й код 0, якщо інших помилок немає.

Без `--config` локально пресет не знайдеться. Якщо пресет зникне на сервері, чинна COMMITPOLICY пропускається з попередженням, але нова версія файлу з таким `extends` не пройде приймання. Відновіть узгоджену копію пресета; не видаляйте `extends` лише для приховування збою.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
