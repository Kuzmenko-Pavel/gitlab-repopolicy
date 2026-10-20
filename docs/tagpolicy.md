[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](tagpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](tagpolicy.en.md)

# TAGPOLICY

TAGPOLICY перевіряє, хто підписав тег. Для кожного виду тегів, наприклад релізів у prod, файл задає ключі, якими такий тег можна підписати. Тег без підпису або з чужим підписом GitLab не приймає. Файл лежить у корені репозиторію з ім'ям `TAGPOLICY`, вміст - YAML.

Спочатку розберемося, що таке тег і підписаний тег і як їх створити. Далі - як TAGPOLICY це перевіряє.

## Що таке тег

Тег - це постійне ім'я для одного коміту, наприклад `v1.4.0-prod`. Гілка рухається з кожним новим комітом, а тег лишається на тому коміті, де його поставили. Тому теги використовують для релізів. Pipeline тегу `v1.4.0-prod` деплоїть у prod саме той код, на який указує тег.

У git є три види тегів:

| Вид | Як створити | Що всередині |
| --- | --- | --- |
| lightweight | `git tag v1.4.0-prod` | лише ім'я, що вказує на коміт. Ні автора, ні дати, ні тексту |
| annotated | `git tag -a v1.4.0-prod -m "Release 1.4.0"` | окремий об'єкт з ім'ям, комітом, автором, датою і текстом |
| підписаний | `git tag -s v1.4.0-prod -m "Release 1.4.0"` | annotated-тег плюс криптографічний підпис ключем OpenPGP (GPG) або SSH |

TAGPOLICY приймає лише підписані теги. `-m` у `git tag` - це текст самого тегу, а не повідомлення коміту, COMMITPOLICY його не перевіряє.

Ось як виглядає підписаний тег зсередини (`git cat-file -p v1.4.0-staging`):

```text
object ae36b6beebcaec2e0099026818110c852ba2a6e3
type commit
tag v1.4.0-staging
tagger Olena Ivanenko <olena@example.com> 1792250148 +0300

Release 1.4.0
-----BEGIN SSH SIGNATURE-----
U1NIU0lHAAAAAQAAADMAAAALc3NoLWVkMjU1MTkAAAAg74/0eQwXM2XR1UKytRridOUhTu
...
-----END SSH SIGNATURE-----
```

| Рядок | Що це |
| --- | --- |
| `object`, `type commit` | коміт, на який указує тег |
| `tag` | ім'я тегу, записане в підписаний об'єкт |
| `tagger` | хто створив тег і коли |
| `Release 1.4.0` | текст з `-m` |
| `BEGIN SSH SIGNATURE` | підпис. Для GPG-ключа тут `BEGIN PGP SIGNATURE` |

### Як підписати і запушити тег

Ключем GPG (OpenPGP), якщо ключ уже налаштовано в `user.signingkey`:

```sh
git tag -s v1.4.0-prod -m "Release 1.4.0"
git push origin v1.4.0-prod
```

Ключем SSH, вказавши публічну частину ключа:

```sh
git -c gpg.format=ssh -c user.signingkey=~/.ssh/id_ed25519.pub tag -s v1.4.0-staging -m "Release 1.4.0"
git push origin v1.4.0-staging
```

`git push` без імені тегу теги не відправляє, тому тег пушать окремо, за ім'ям.

Перевірити підпис локально можна `git tag -v v1.4.0-prod`. Для GPG потрібен публічний ключ у вашому keyring, для SSH - файл дозволених ключів у `gpg.ssh.allowedSignersFile`, інакше git відповість `gpg.ssh.allowedSignersFile needs to be configured`. Сервер так не перевіряє, hook бере ключі лише з TAGPOLICY.

### Чим `git tag -s` відрізняється від `git commit -s` і `-S`

Три схожі прапорці роблять різне:
- `git tag -s` - підписаний тег. Саме його перевіряє TAGPOLICY
- `git commit -s` - рядок `Signed-off-by` у повідомленні коміту, лише текст без криптографії ([commitpolicy.md](commitpolicy.md#як-написати-таке-повідомлення))
- `git commit -S` - криптографічний підпис коміту. TAGPOLICY його не перевіряє, навіть для коміту, на який указує тег

### Як виправити тег після відмови

Hook відхиляє push, тому тега на сервері немає, він є лише у вас. Видаліть його локально, створіть заново з правильним підписом і запуште:

```sh
git tag -d v1.4.0-prod
git tag -s v1.4.0-prod -m "Release 1.4.0"
git push origin v1.4.0-prod
```

Цю ж команду hook пише у відмові в рядку `how to fix`. Якщо ваш ключ не дозволений для цього тегу, підписати його має власник дозволеного ключа, див. [Відмови](#відмови).

### Теги з UI GitLab і GitLab Release

GitLab не вміє підписувати теги, які створює сам. Тег, створений кнопкою New tag в UI або через Releases API, під правилом отримує відмову `tag_not_signed`. Тому спершу створіть підписаний тег через `git tag -s` і `git push`, а потім реліз у GitLab (Deploy > Releases > New release) для вже наявного тегу. Реліз до тегу додає опис і файли, сам тег він не змінює.

## Як TAGPOLICY це перевіряє

Файл складається з правил. Правило пов'язує regex імені тегу з ключами, якими такий тег можна підписати. Різні оточення зазвичай випускають різні люди. Staging - кілька розробників, prod - лише release-менеджер.

Перевірка підпису спирається лише на сам тег і ключі з файлу. API GitLab, `gpg` і `ssh-keygen` на сервері не потрібні. API потрібен лише правилу з `protected: true` і лише тоді, коли тег створюють.

Хто може змінювати сам TAGPOLICY, задає правило PATHPOLICY ([pathpolicy.md](pathpolicy.md)). Історія того, хто має право на реліз, - це історія цього файлу.

## Приклад

```yaml
# examples/signed-releases/TAGPOLICY
# Release tags: staging is signed by release-lead or dev-lead, prod only by
# release-lead, and a prod tag must be protected in GitLab. Tags that match no
# rule, such as build-42, are not checked.
version: 1

rules:
  staging:
    match: ['/^v\d+\.\d+\.\d+-staging$/']
    signers: [release-lead, dev-lead]
    message: Staging releases are signed by release-lead or dev-lead.
  prod:
    match: ['/^v\d+\.\d+\.\d+-prod$/']
    signers: [release-lead]
    protected: true
    message: >-
      Prod releases are signed by release-lead.
      Protect v*-prod in Settings > Repository > Protected tags.

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

Як читається:
- `keys` - публічні ключі під мітками. `release-lead` - ключ GPG, `dev-lead` - ключ SSH
- `v1.4.0-staging` збігається з правилом `staging`, його може підписати `release-lead` або `dev-lead`
- `v1.4.0-prod` збігається з правилом `prod`, його може підписати лише `release-lead`. Крім того, проєкт має захищати цей тег у GitLab
- `build-42` не збігається з жодним правилом і проходить без підпису

## Поля

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `version` | так | `1`. Схема `version: 1` заморожена |
| `rules` | ні | до 50 правил |
| `rules[].name` | так | `[a-z0-9-]{1,63}`, унікальне у файлі |
| `rules[].tags` | так | regex (синтаксис RE2), до 256 байт. Має збігтися з усім ім'ям тегу, наприклад `v1.4.0-prod`. `^` і `$` додавати не треба |
| `rules[].signers` | так | 1-32 мітки ключів із `keys` |
| `rules[].protected` | ні | `true` - новий тег під цим правилом проєкт має захищати в GitLab (Settings > Repository > Protected tags), див. [нижче](#захищені-теги-protected). За замовчуванням `false` |
| `rules[].message` | ні | текст для того, кому відмовили, до 1000 символів |
| `keys` | так | 1-32 записи `мітка: ключ` |

Ключ - це рівно один публічний ключ на запис, приватні ключі у файл не кладуть:
- OpenPGP - armored-блок `-----BEGIN PGP PUBLIC KEY BLOCK-----`
- SSH - рядок публічного ключа OpenSSH одного з типів `ssh-ed25519`, `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`, `ssh-rsa`, `sk-ssh-ed25519@openssh.com`, `sk-ecdsa-sha2-nistp256@openssh.com`. Без options і сертифікатів

Мітка - ім'я ключа всередині файлу (`[A-Za-z0-9_.-]`, до 255 символів), а не користувач GitLab. Hook не звіряє її з GitLab і не пов'язує того, хто підписав, з тим, хто робить push. Зручно брати username власника ключа, так легше читати audit.

Розмір файлу - до 256 KiB, бо armored-ключі великі.

## Як зіставляються теги

- Тег не збігся з жодним правилом - модуль його не перевіряє. У прикладі вище `build-42` пройде без підпису
- Тег збігся з кількома правилами - мають виконуватися всі. Ключ підпису має бути в `signers` кожного з них. Підпис у тегу один, тому кілька правил для одного тегу звужують список дозволених ключів. Пишіть правила, що не перетинаються
- Regex, а не шаблон з `*`, бо `v*` не відрізняє `v1.2.3` від `v1.2.3-prod`. Regex з правил CI (`$CI_COMMIT_TAG =~ /.../`) зазвичай переноситься як є, але синтаксис тут RE2, у ньому немає lookahead, lookbehind і зворотних посилань (`\1`). Перевірте файл командою `validate`
- Правило має покривати все, що деплоїть CI цього оточення. Тоді розбіжність дає зайву перевірку, а не деплой неперевіреного тегу

## Що перевіряється

Для кожного тегу, який push створює або переміщує на інший об'єкт і ім'я якого збіглося з правилом:

1. Тег annotated. Lightweight-тег - відмова `tag_not_signed`
2. Ім'я в об'єкті тегу (рядок `tag`) збігається з ім'ям, під яким його пушать. Інакше підписаний staging-тег можна було б запушити під ім'ям prod-тегу. Підпис об'єкта при цьому лишається правильним, і `git verify-tag` його приймає. Розбіжність - відмова `tag_object_invalid`
3. Тег указує безпосередньо на коміт. Тег на тег, дерево чи blob - відмова `tag_object_invalid`
4. Підпис є і він OpenPGP або SSH. Немає підпису або підпис X.509 - відмова `tag_not_signed`
5. Ключ підпису є у файлі, і його мітка є в `signers` кожного правила, що збіглося. Якщо підписано підключем OpenPGP, враховується основний ключ, до якого він належить. Інакше відмова `tag_signer_not_allowed`
6. Підпис дійсний. Інакше відмова `tag_signature_invalid`:
   - OpenPGP: ключ і підключ не прострочені й не відкликані на момент push, час підпису не пізніше за момент push плюс 5 хвилин, хеш не SHA-1, не MD5 і не RIPEMD-160
   - SSH: namespace підпису `git`, хеш `sha256` або `sha512`, RSA лише `rsa-sha2-256` або `rsa-sha2-512`, не `ssh-rsa` (SHA-1). Підпис sk-ключем повинен мати прапорець присутності користувача

У форматі SSH-підпису немає часу підпису і строку дії ключа, тому SSH-ключ діє, поки лежить у файлі. Строк його життя і окремий ключ для підпису (а не той, яким пушать) - відповідальність того, хто додає ключ у TAGPOLICY.

### Захищені теги (`protected`)

Окремо від підпису є захист тегу в GitLab. Проєкт захищає теги за шаблоном у Settings > Repository > Protected tags, наприклад `v*-prod`. Від цього залежить деплой. Protected CI/CD variables (паролі, токени деплою) і protected runners доступні лише pipeline захищеного тегу. Без захисту тег створиться, pipeline запуститься, але нічого не задеплоїть, і причину шукатимуть довго.

Тому для правила з `protected: true` hook під час створення тегу питає GitLab, чи захищає проєкт цей тег. Не захищає - відмова `tag_not_protected` з підказкою, що налаштувати. Це перевірка налаштувань, а не доступу. Якщо API не відповів, тег приймається з попередженням. Якщо в тегу проблема і з підписом, і з захистом, у виводі обидві. Переміщення наявного тегу на захист не перевіряється.

Видалення тегу TAGPOLICY не регулює. Хто може створювати, переміщувати й видаляти теги, вирішують protected tags GitLab. Для проєктів з PATHPOLICY їхню наявність звіряє `settings-check`.

Не перевіряються теги, створені до появи TAGPOLICY, теги поза правилами і підпис коміту, на який указує тег.

## Приймання нової версії

Крім загальних перевірок ([pathpolicy.md](pathpolicy.md#приймання-нової-версії)):

| Ситуація | Результат |
| --- | --- |
| приватний ключ у файлі | відмова `invalid_policy_update` з прямим текстом, що ключ треба вважати скомпрометованим |
| у записі не один ключ, невідомий тип SSH-ключа, OpenPGP-ключ без права підпису або відкликаний | відмова `invalid_policy_update` |
| мітка з `signers` відсутня в `keys` | відмова `invalid_policy_update` |
| той самий ключ під двома мітками | відмова `invalid_policy_update` |
| прострочений OpenPGP-ключ у доданому або зміненому записі | відмова `invalid_policy_update` |
| прострочений OpenPGP-ключ у незміненому записі | попередження. Чужий прострочений ключ не блокує правку файлу |
| ключ, який не використовує жодне правило | попередження |

Якщо в default branch проєкту немає PATHPOLICY, TAGPOLICY може змінити будь-хто з правом push, тобто й список ключів, якими підписують релізи. Hook приймає таку версію з попередженням `the project has no PATHPOLICY on the default branch: anyone who can push may change TAGPOLICY`. Захистіть файл правилом PATHPOLICY ([pathpolicy.md](pathpolicy.md#приклад)).

У default branch прострочений ключ не робить TAGPOLICY невалідною, його відхиляють лише під час перевірки підпису.

Заміна і відкликання ключа - це зміна TAGPOLICY у default branch. Вона діє з першого push після merge. Скомпрометованим приватним ключем можна підписати реліз, доки цей merge не відбувся.

## Відмови

TAGPOLICY захищає релізи, тому за будь-якої невизначеності вона відмовляє. Сумнівний підпис, зламаний файл TAGPOLICY і внутрішня помилка hook дають відмову, а не пропуск. Виняток - перевірка `protected`. Вона про налаштування, тому збій API чи прострочений токен дають попередження, а не відмову.

| Код | Заголовок у виводі | Коли |
| --- | --- | --- |
| `tag_not_signed` | the tag must be signed | lightweight-тег, тег без підпису, підпис X.509, тег з UI чи Releases API |
| `tag_object_invalid` | the tag was signed as "v2.0.0-staging" but is pushed as "v2.0.0-prod" | ім'я в об'єкті тегу не збігається з ім'ям, під яким його пушать |
| `tag_object_invalid` | the tag points to a tree, not a commit | тег указує не на коміт, а на дерево, blob чи інший тег |
| `tag_signer_not_allowed` | this key may not sign these tags | ключа немає у файлі або його мітки немає в `signers` |
| `tag_signature_invalid` | the signature cannot be accepted | ключ прострочений чи відкликаний, слабкий хеш, неправильний namespace SSH |
| `tag_not_protected` | the tag must be protected, but the project does not protect it | новий тег під правилом з `protected: true`, а проєкт його не захищає |
| `authorization_backend_failure`, `limit_exceeded` | could not check that the tag is protected; the tag is accepted | лише попередження: API не відповів під час перевірки `protected` |
| `invalid_policy` | TAGPOLICY in main is broken, so no tag can be created or moved | TAGPOLICY у default branch невалідна. Відмова на створення і переміщення будь-якого тегу, гілки не зачеплені |
| `invalid_policy_update` | the changed TAGPOLICY is invalid | нова версія файлу не пройшла приймання |

Так виглядає відмова, коли `dev-lead` підписав prod-тег з прикладу вище, а для prod дозволено лише `release-lead`:

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: TAGPOLICY rule "prod", tag v3.156.0-prod: this key may not sign these tags
remote:   signed with: key "dev-lead" from TAGPOLICY (SSH, SHA256:pttIxH9SsvfRFuR5nv5vaNqIlF5N15hu0MZMwzg6AyE)
remote:   allowed keys: release-lead (OpenPGP)
remote:   Prod releases are signed by release-lead. Protect v*-prod in Settings > Repository > Protected tags.
remote:   ask the owner of an allowed key to sign the tag, or ask the people who may change TAGPOLICY to add your key
remote:
remote: rules: PATHPOLICY, TAGPOLICY in main (9f1c2ab)
remote: checked as: dev-lead (SSH)
remote: GL-HOOK-ERR: gitlab-repopolicy: this key may not sign these tags (TAGPOLICY rule "prod", tag v3.156.0-prod)
```

Як це читати:
- `signed with` - яким ключем з файлу підписано тег, його мітка і відбиток
- `allowed keys` - мітки ключів, яким це правило дозволяє підписувати
- далі текст `message` правила і що робити
- `rules` - які файли політик діяли і з якого коміту default branch hook їх прочитав
- `checked as` - хто робив push. Тут це username у GitLab, з міткою ключа він не пов'язаний
- `GL-HOOK-ERR` - короткий рядок для UI GitLab

Тег, створений в UI GitLab або через Releases API, не підписаний. Для тегів під правилом відмова так і каже (`GitLab cannot sign a tag created in the UI or API`) і підказує створити тег через `git tag -s` і `git push`, а реліз потім створити для вже наявного тегу.

Подія audit `decision` модуля `tagpolicy` містить тег, правила, мітку і fingerprint ключа, яким підписано тег.

`git push --mirror` перевіряє всі теги, яких ще немає на сервері. Під час імпорту старого репозиторію в новий проєкт це всі теги, тож старі непідписані теги під правилом дають відмову всьому push. Додавайте TAGPOLICY після імпорту або використовуйте break-glass ([operations.md](operations.md)).

## Перевірка без push

```sh
gitlab-repopolicy validate -f TAGPOLICY
gitlab-repopolicy test --repo . --old 0000000000000000000000000000000000000000 \
  --new "$(git rev-parse v1.4.0-prod)" --ref refs/tags/v1.4.0-prod --user release --config release-verify.yaml
```

`test` приймає те саме рішення, що й hook, на локальному репозиторії. `--old` з нулів означає, що тег створюється. `release-verify.yaml` - копія [config/release-verify.yaml](../config/release-verify.yaml), у ній увімкнено тільки TAGPOLICY і немає доступу до API. Для правила з `protected: true` `test` тоді додає попередження `could not check that the tag is protected`, а підпис перевіряє повністю.
