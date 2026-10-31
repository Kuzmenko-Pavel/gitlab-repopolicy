[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](inspect-tag.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](inspect-tag.en.md)

# Перевірка вимог до імені тегу

Перед випуском потрібно побачити, які правила охоплюють `v1.4.0-prod` і який ключ має його підписати. Для цього достатньо імені й policy-файлу; сам тег ще не потрібний.

## Відтворюваний запит

Збережіть цей серверний фрагмент як `presets.yaml` і наступний файл як `TAGPOLICY` у поточному каталозі. Це тестові публічні ключі; запит не потребує приватного ключа, Git-репозиторію, API чи токена.

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

```text
$ gitlab-repopolicy validate --config presets.yaml --tag v1.4.0-prod
TAGPOLICY: valid
  extends: org-tags
  rule prod: preset:org-tags
    match: preset:org-tags
    message: preset:org-tags
    protected: preset:org-tags
    signers: preset:org-tags
  rule staging: preset:org-tags
    match: preset:org-tags
    message: preset:org-tags
    signers: preset:org-tags
tag v1.4.0-prod: rules prod (from preset:org-tags)
  signers: release-lead (SSH)
  requires: the project protects the tag in Settings > Repository > Protected tags
  the tag must be signed by one of the signers; validate does not check signatures
```

`extends` показує підключений пресет; `from` - походження правил. `signers` є переліком допустимих ключів після перетину всіх збігів. `requires` називає умову protected tag, але не перевіряє її виконання в GitLab.

## Що результат не підтверджує

Код 0 означає валідний файл і успішно виконаний інформаційний запит. Він не підтверджує підпис, наявність приватного ключа або право користувача створити тег у GitLab. Навіть відповідь без спільних підписантів може мати код 0.

Повторіть запит для `build-42`: відсутність збігу означає, що TAGPOLICY не перевіряє підпис такого імені. Глобальна досяжність коміту й права GitLab залишаються. Для остаточного рішення створіть тег і використайте `test` на коректній копії репозиторію або контрольний push; [CLI](../../reference/cli.md#test) пояснює потрібні об'єкти й доступи.

[Довідник TAGPOLICY](../../policies/tagpolicy.md)
