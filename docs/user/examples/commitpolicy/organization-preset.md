[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](organization-preset.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](organization-preset.en.md)

# Налаштування стандарту комітів із пресета

Організація централізовано задає формат комітів. Проєкт залишає типи й формат header, але замінює відмову за довгий header на попередження та дозволяє крапку наприкінці subject.

## Два джерела правил

Адміністратор розміщує цей фрагмент у серверному `config.yaml` на всіх вузлах Gitaly. Збережіть його також як `presets.yaml` для локальної перевірки.

```yaml
presets:
  commitpolicy:
    org-commits:
      rules:
        conventional:
          match: default
          except:
            merge_commit: true
            git_reverts: true
          checks:
            header-format:
              conventional: { level: error, value: required }
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

Проєкт додає в корінь `COMMITPOLICY`:

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

Файл звертається до перевірок пресета за типом і ім'ям. `header-max-length.limit` успадковує `value: 100`, змінюючи лише `level`. `subject-trailing-dot.no-dot` отримує `value: optional`: перевірка лишається в правилі, але крапка тепер може бути або не бути. Так само знімається вимога будь-якої перевірки наявності ознаки; `null` потрібен, лише коли перевірку треба прибрати з правила зовсім. Це відрізняється від `mode: warn`: решта помилок політики й надалі можуть блокувати push.

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
```

Запустіть `gitlab-repopolicy validate -f COMMITPOLICY --config presets.yaml`, потім `gitlab-repopolicy lint-commit -f COMMITPOLICY --config presets.yaml -m "docs: update the guide."`. Останнє повідомлення проходить, бо крапку дозволено. Для перевірки межі довжини використайте header понад 100 символів: очікується попередження й код 0, якщо інших помилок немає.

Без `--config` локально пресет не знайдеться. Якщо пресет зникне на сервері, чинна COMMITPOLICY пропускається з попередженням, але нова версія файлу з таким `extends` не пройде приймання. Відновіть узгоджену копію пресета; не видаляйте `extends` лише для приховування збою.

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
