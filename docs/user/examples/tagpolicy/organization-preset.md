[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](organization-preset.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](organization-preset.en.md)

# Пресет правил і ключі проєкту

Організація використовує однакові правила staging/prod, але кожен проєкт контролює власні ключі підпису. Серверний пресет називає мітки підписантів, а TAGPOLICY проєкту визначає, які публічні ключі за ними стоять.

## Обидві конфігурації

Адміністратор додає фрагмент до серверного `config.yaml`. Для локальної перевірки збережіть той самий фрагмент як `presets.yaml`.

```yaml
presets:
  tagpolicy:
    org-tags:
      rules:
        staging:
          match: ['/^v\d+\.\d+\.\d+-staging$/']
          signers: [release-lead, dev-lead]
          message: Staging releases are signed by release-lead or dev-lead.
        prod:
          match: ['/^v\d+\.\d+\.\d+-prod$/']
          signers: [release-lead]
          protected: true
          message: Prod releases are signed by release-lead. Protect v*-prod in Settings > Repository > Protected tags.
```

Проєкт зберігає в корені такий `TAGPOLICY`. Ключі прикладу є тестовими; для застосування потрібні власні публічні ключі й доступ до відповідних приватних ключів у підписантів.

```yaml
# examples/org-tags/TAGPOLICY
# The tag rules of the organization come from the preset org-tags: it names
# the signers by label. The keys behind the labels live in this file, so
# replacing or revoking a key is a reviewed change of the project.
version: 1
extends: org-tags

keys:
  release-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl release-lead
  dev-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE dev-lead
```

Виконайте `gitlab-repopolicy validate --config presets.yaml -f TAGPOLICY` перед merge в основну гілку. Для створення prod-тегів Maintainer також захищає `v*-prod` у GitLab.

## Чому ключі залишаються локальними

Пресет містить лише `rules`. Заміна або видалення ключа відбувається через review TAGPOLICY проєкту й діє для наступних запусків після merge. Це дає історію того, який ключ міг підписувати релізи саме цього проєкту. Додавати `keys` до пресета не можна.

Кожна мітка з успадкованих `signers` має бути в `keys`. Видалення dev-lead лише з `keys` зробить новий файл невалідним; для відкликання треба також змінити всі списки, які посилаються на цю мітку. Перевірте результат `validate --config presets.yaml --tag v1.4.0-staging` до merge.

У глобальному `enforce` prod-тег приймається за підписом release-lead, коли решта вимог виконується. Після відкликання ключа наступні push цього тегу не мають отримувати дозвіл за видаленим ключем; уже наявні теги автоматично не переоцінюються.

[Довідник TAGPOLICY](../../policies/tagpolicy.md)
