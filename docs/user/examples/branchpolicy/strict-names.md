[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](strict-names.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](strict-names.en.md)

# Заборона всіх неописаних імен

У проєкті дозволені лише release-гілки й робочі гілки `<prefix>/<name>`. Будь-яке інше нове ім'я має отримати відмову, а не пройти без збігу.

## Файл проєкту

Збережіть як `BRANCHPOLICY` у корені репозиторію. Перевірте `gitlab-repopolicy validate -f BRANCHPOLICY` перед merge в основну гілку. Для таблиці `main` є основною гілкою, GitLab захищає `main` і `release/*`; глобальний режим - `enforce`.

```yaml
# examples/strict-branches/BRANCHPOLICY
# Strict mode: only release and work branches may be created. The last rule
# has match: all and deny, so a name of no other class is refused instead of
# passing unchecked.
version: 1

rules:
  release:
    match: ['/^release\/v\d+\.\d+\.\d+$/']
    priority: 10
    protected: true
    message: Release branches look like release/v1.4.0.
  work:
    match: ['/^[a-z0-9.]+\/[a-z0-9._-]+$/']
    priority: 20
    message: Work branches are <prefix>/<name>, for example kpn/disable_auth.
  other:
    match: all
    priority: 1000
    deny: true
    message: Use release/vX.Y.Z or <prefix>/<name>.
```

<!-- check: branch-push examples/strict-branches/BRANCHPOLICY main,release/* -->
| Нова гілка | Хто створює | Правило | Результат | Чому |
| --- | --- | --- | --- | --- |
| `release/v1.4.0` | bob | `release` | дозволено | ім'я за стандартом, `release/*` захищено |
| `bob/disable_auth` | bob | `work` | дозволено | ім'я `<prefix>/<name>` |
| `fix-login` | bob | `other` | відмова | жодне інше правило не збіглося |
| `master` | bob | `other` | відмова | так само |

Правило з `match: all` має бути останнім: `validate` відхиляє файл, де після нього є ще правила.

Останнє правило `other` має найбільше число `priority` й `match: all`. Воно перехоплює лише те, що не прийняли попередні правила. Робоче правило цього прикладу перевіряє форму префікса, але не його існування як username: `user_prefix` не задано.

Запустіть `validate --branch fix-login` для діагностики, потім контрольний push нової `fix-login` у тестовому проєкті. У глобальному `enforce` очікується `branch_name_violation`. `validate --branch` сам не є перевіркою дозволу для CI.

Не розміщуйте правила після `match: all`: валідатор відхилить недосяжні правила. І не додавайте цю політику до масового імпорту старих імен до перевірки: одна неправильна нова гілка відхиляє весь push.

[Довідник BRANCHPOLICY](../../policies/branchpolicy.md)
