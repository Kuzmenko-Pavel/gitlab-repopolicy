[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](overlapping-rules.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](overlapping-rules.en.md)

# Один тег під кількома правилами

Загальні релізні теги дозволено підписувати release-lead і dev-lead. Для production діє додаткова вимога release-lead або sec-lead. Потрібно зрозуміти, чому prod-тег може підписати лише release-lead.

## Політика з перетином

Збережіть повний файл як `TAGPOLICY`, перевірте його через `validate -f TAGPOLICY` та активуйте в основній гілці. Ключі є тестовими публічними зразками; для реальних релізів замініть їх своїми публічними ключами.

```yaml
# examples/release-overlap/TAGPOLICY
# Every release tag is signed by release-lead or dev-lead; a prod release
# also matches the prod rule, signed by release-lead or sec-lead. A tag that
# matches both rules is signed only by a key that both rules list.
version: 1

rules:
  releases:
    match: ['/^v\d+\.\d+\.\d+/']
    signers: [release-lead, dev-lead]
    message: Release tags are signed by release-lead or dev-lead.
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [release-lead, sec-lead]
    message: Prod releases are also signed by release-lead or sec-lead.

keys:
  release-lead: |
    -----BEGIN PGP PUBLIC KEY BLOCK-----
    
    mDMEas8yyBYJKwYBBAHaRw8BAQdARgQtR6FZCckYy8pf9+9snMoIb3gcbH9QVotM
    2CEt2za0H1JlbGVhc2UgTGVhZCA8bGVhZEBleGFtcGxlLmNvbT6IkwQTFgoAOxYh
    BGx5V+qHOlRnp48dB1l3WaOCcpNZBQJqzzLIAhsDBQsJCAcCAiICBhUKCQgLAgQW
    AgMBAh4HAheAAAoJEFl3WaOCcpNZf7IBAO02jJtCe9KhJlLAOTz/XX+kpgyY8mth
    6Cku2bukuF6bAP40EeqZQ1ISoo/Agcdncm5Z+KysHKFd7y49xzE0q6ObDA==
    =3XJY
    -----END PGP PUBLIC KEY BLOCK-----
  dev-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE dev-lead
  sec-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl sec-lead
```

<!-- check: tag-rules examples/release-overlap/TAGPOLICY -->
| Тег | Правила | Хто може підписати |
| --- | --- | --- |
| `v1.4.0` | `releases` | release-lead, dev-lead |
| `v1.4.0-prod` | `releases`, `prod` | release-lead |
| `build-42` | - | не перевіряється |

Ні dev-lead, ні sec-lead не можуть підписати `v1.4.0-prod`: кожен із них є лише в одному списку. Підпис у тегу один, тож кілька правил для одного тегу лише звужують коло підписантів. Якщо в правил немає спільного підписанта, такий тег неможливо створити, і `validate --tag` пише `signers: none, the matching rules have no signer in common`.

`releases` навмисно не має кінцевого `$`, тому збігається і з суфіксом `-prod`. Для одного тегу потрібен один ключ із перетину всіх списків. Два окремі підписи двох різних людей ця політика не вимагає й не реалізує.

## Перевірка очікувань

Виконайте `gitlab-repopolicy validate -f TAGPOLICY --tag v1.4.0-prod`: у відповіді мають бути правила `prod`, `releases` і спільна мітка release-lead. Порядок імен у відповіді задає реалізація; сенс полягає в перетині. Код 0 підтверджує валідність файлу й виконання запиту, не справжній підпис тегу.

Для перевірки підпису потрібен створений тег і `test` або контрольний push у глобальному `enforce`. dev-lead і sec-lead окремо отримають `tag_signer_not_allowed`. Якщо прибрати release-lead з одного списку, спільних ключів не буде і жоден підписант не зможе створити такий тег.

[Довідник TAGPOLICY](../../policies/tagpolicy.md)
