[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](tagpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](tagpolicy.en.md)

# TAGPOLICY

TAGPOLICY відповідає на одне питання: чи підписано тег ключем, якому дозволено випускати теги з таким ім'ям. Файл лежить у корені репозиторію з ім'ям `TAGPOLICY`, вміст - YAML.

Реліз - це підписаний тег, і різні оточення випускають різні люди. Правило пов'язує regex імені тегу з набором публічних ключів OpenPGP або SSH. Перевірка спирається лише на об'єкт тегу і ключі з самого файлу: API GitLab, `gpg` і `ssh-keygen` на сервері не потрібні.

Хто змінює сам TAGPOLICY, задає path-правило PATHPOLICY ([pathpolicy.md](pathpolicy.md)). Історія прав на реліз - це історія цього файлу.

## Приклад

```yaml
# examples/signed-releases/TAGPOLICY
# Release tags: staging is signed by release-lead or dev-lead, prod only by
# release-lead. Tags that match no rule, such as build-42, are not checked.
version: 1

rules:
  - name: staging
    tags: 'v\d+\.\d+\.\d+-staging'
    signers: [release-lead, dev-lead]
  - name: prod
    tags: 'v\d+\.\d+\.\d+-prod'
    signers: [release-lead]
    message: Prod releases are signed by release-lead.

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
  dev-lead: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG6MuGxttDci5himWr8891GvUm9MjQljladoClrTOUiE ed25519
```

## Поля

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `version` | так | `1`. Схема `version: 1` заморожена |
| `rules` | ні | до 50 правил |
| `rules[].name` | так | `[a-z0-9-]{1,63}`, унікальне у файлі |
| `rules[].tags` | так | RE2 regex, до 256 байт. Має збігтися з усім ім'ям тегу без `refs/tags/`, `^` і `$` додавати не треба |
| `rules[].signers` | так | 1-32 мітки ключів із `keys` |
| `rules[].message` | ні | текст для того, кому відмовили, до 1000 символів |
| `keys` | так | 1-32 записи `мітка: ключ` |

Ключ - це рівно один публічний ключ на запис:
- OpenPGP - armored-блок `-----BEGIN PGP PUBLIC KEY BLOCK-----`
- SSH - рядок публічного ключа OpenSSH: `ssh-ed25519`, `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`, `ssh-rsa`, `sk-ssh-ed25519@openssh.com`, `sk-ecdsa-sha2-nistp256@openssh.com`. Без options і сертифікатів

Мітка - ім'я ключа всередині файлу (`[A-Za-z0-9_.-]`, до 255 символів), а не користувач GitLab. Hook не звіряє її з GitLab і не пов'язує того, хто підписав, з тим, хто робить push. Зручно брати username власника ключа: так легше читати audit.

Розмір файлу - до 256 KiB, бо armored-ключі великі.

## Як зіставляються теги

- Тег не збігся з жодним правилом - модуль його не перевіряє. У прикладі вище `build-42` пройде без підпису
- Тег збігся з кількома правилами - мають виконуватися всі (AND): ключ підпису має бути в `signers` кожного. Підпис у тегу один, тому перетин правил дає перетин наборів. Пишіть правила без перетину
- Regex, а не glob, бо glob не відрізняє `v1.2.3` від `v1.2.3-prod`. Regex можна перенести з правил CI (`$CI_COMMIT_TAG =~ ...`) як є
- Правило має покривати все, що деплоїть CI цього оточення. Тоді розбіжність дає зайву перевірку, а не деплой неперевіреного тегу

## Що перевіряється

Для кожного нового або переміщеного тегу, ім'я якого збіглося з правилом:

1. Тег annotated. Lightweight-тег - відмова `tag_not_signed`
2. Ім'я в об'єкті тегу збігається з ім'ям, під яким його пушать. Інакше підписаний staging-тег можна було б запушити під ім'ям prod-тегу: підпис об'єкта лишається правильним, і `git verify-tag` його приймає. Розбіжність - відмова `tag_object_invalid`
3. Тег указує безпосередньо на коміт. Тег на тег, дерево чи blob - відмова `tag_object_invalid`
4. Підпис є і він OpenPGP або SSH. Немає підпису або підпис X.509 - відмова `tag_not_signed`
5. Ключ підпису є у файлі і його мітка є в `signers` кожного правила, що збіглося. Для підключа OpenPGP враховується його primary-ключ. Інакше відмова `tag_signer_not_allowed`
6. Підпис дійсний. Інакше відмова `tag_signature_invalid`:
   - OpenPGP: ключ і підключ не прострочені й не відкликані на момент push, час підпису не пізніше за момент push плюс 5 хвилин, хеш не SHA-1, не MD5 і не RIPEMD-160
   - SSH: namespace підпису `git`, хеш `sha256` або `sha512`, RSA лише `rsa-sha2-256` або `rsa-sha2-512`, не `ssh-rsa` (SHA-1). Підпис sk-ключем повинен мати прапорець присутності користувача

У форматі SSH-підпису немає часу підпису і строку дії ключа, тому SSH-ключ діє, поки лежить у файлі. Строк його життя і окремий ключ для підпису (а не той, яким пушать) - відповідальність того, хто додає ключ у TAGPOLICY.

Видалення тегу TAGPOLICY не регулює: хто може створювати, переміщувати й видаляти теги, вирішують protected tags GitLab. Для проєктів з PATHPOLICY їхню наявність звіряє `settings-check`.

Не перевіряються теги, створені до появи TAGPOLICY, теги поза правилами і підпис коміту, на який указує тег.

## Як підписати тег

OpenPGP:

```sh
git tag -s v1.4.0-prod -m "Release 1.4.0"
git push origin v1.4.0-prod
```

SSH:

```sh
git -c gpg.format=ssh -c user.signingkey=~/.ssh/id_ed25519.pub tag -s v1.4.0-staging -m "Release 1.4.0"
git push origin v1.4.0-staging
```

GitLab не вміє підписувати теги, які створює сам. Тег, створений в UI або через Releases API, під правилом отримує відмову `tag_not_signed`. Спершу запуште підписаний тег, потім створюйте реліз для наявного тегу.

## Приймання нової версії

Крім загальних перевірок ([pathpolicy.md](pathpolicy.md#приймання-нової-версії)):

| Ситуація | Результат |
| --- | --- |
| приватний ключ у файлі | відмова `invalid_policy_update` з прямим текстом: вважайте цей ключ скомпрометованим |
| у записі не один ключ, невідомий тип SSH-ключа, OpenPGP-ключ без права підпису або відкликаний | відмова `invalid_policy_update` |
| мітка з `signers` відсутня в `keys` | відмова `invalid_policy_update` |
| той самий ключ під двома мітками | відмова `invalid_policy_update` |
| прострочений OpenPGP-ключ у доданому або зміненому записі | відмова `invalid_policy_update` |
| прострочений OpenPGP-ключ у незміненому записі | попередження: чужий прострочений ключ не блокує правку файлу |
| ключ, який не використовує жодне правило | попередження |

У default branch прострочений ключ не робить TAGPOLICY невалідною: його відхиляють лише під час перевірки підпису.

Ротація і відкликання ключа - це зміна TAGPOLICY у default branch. Вона діє з першого push після merge. Скомпрометованим приватним ключем можна підписати реліз, доки цей merge не відбувся.

## Відмови

TAGPOLICY закривається за невизначеності (fail closed), але API не використовує, тому збій API чи прострочений токен на неї не впливають.

| Код | Заголовок у виводі | Коли |
| --- | --- | --- |
| `tag_not_signed` | tag is not signed | lightweight-тег, тег без підпису, підпис X.509, тег з UI чи Releases API |
| `tag_object_invalid` | tag object does not match ref | ім'я в об'єкті не збігається з ім'ям тегу, тег не на коміт |
| `tag_signer_not_allowed` | tag signer not allowed | ключа немає у файлі або його мітки немає в `signers` |
| `tag_signature_invalid` | tag signature is invalid | ключ прострочений чи відкликаний, слабкий хеш, неправильний namespace SSH |
| `invalid_policy` | policy is invalid | TAGPOLICY у default branch невалідна: відмова на створення і переміщення будь-якого тегу, гілки не зачеплені |
| `invalid_policy_update` | new policy version is invalid | нова версія файлу не пройшла приймання |

```text
remote: TAGPOLICY [prod] refs/tags/v3.156.0-prod: tag signer not allowed
remote:   Prod releases are signed by release-lead or sre-lead.
remote:   signed by: dev-lead (4528A421CE40AA6A717FF2112DA7684307491280)
remote:   allowed:   release-lead, sre-lead
```

Подія audit `decision` модуля `tagpolicy` містить тег, правила, мітку і fingerprint ключа, яким підписано тег.

`git push --mirror` зі старими непідписаними тегами під правилом отримає відмову. Додавайте TAGPOLICY після імпорту або використовуйте break-glass ([operations.md](operations.md)).

## Перевірка без push

```sh
gitlab-repopolicy validate -f TAGPOLICY
gitlab-repopolicy test --repo . --old 0000000000000000000000000000000000000000 \
  --new "$(git rev-parse v1.4.0-prod)" --ref refs/tags/v1.4.0-prod --user release --config release-verify.yaml
```

`test` приймає те саме рішення, що й hook, на локальному репозиторії. `release-verify.yaml` - копія [config/release-verify.yaml](../config/release-verify.yaml): у ній увімкнено тільки TAGPOLICY, і API не потрібен. Так сам проєкт перевіряє свої релізні теги ([release.md](release.md)).
