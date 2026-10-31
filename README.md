[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](README.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](README.en.md)

[![CI](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/ci.yml?query=branch%3Amain)
[![e2e](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/e2e.yml/badge.svg?branch=main)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/actions/workflows/e2e.yml?query=branch%3Amain)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/Kuzmenko-Pavel/gitlab-repopolicy/badge)](https://scorecard.dev/viewer/?uri=github.com/Kuzmenko-Pavel/gitlab-repopolicy)
[![Coverage](https://codecov.io/gh/Kuzmenko-Pavel/gitlab-repopolicy/graph/badge.svg?branch=main)](https://codecov.io/gh/Kuzmenko-Pavel/gitlab-repopolicy)
[![Release](https://img.shields.io/github/v/release/Kuzmenko-Pavel/gitlab-repopolicy?sort=semver&include_prereleases)](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases)
[![License](https://img.shields.io/github/license/Kuzmenko-Pavel/gitlab-repopolicy)](./LICENSE)
[![Docs](https://img.shields.io/badge/docs-latest-blue)](docs/README.md)

# gitlab-repopolicy

**gitlab-repopolicy** — це застосунок для GitLab Self-Managed, який реалізує серверний Git-хук і дозволяє впроваджувати політики керування репозиторіями безпосередньо в проєктах GitLab.

За допомогою політик можна визначати, хто має право змінювати певні файли, встановлювати правила іменування гілок, вимоги до повідомлень комітів та умови створення тегів.

Правила описуються у YAML-файлах, які зберігаються безпосередньо в репозиторії разом із кодом проєкту.

Під час надсилання змін до GitLab застосунок перевіряє їх на відповідність встановленим політикам. Якщо зміни порушують правила, GitLab може відхилити їх із поясненням причини та рекомендаціями щодо виправлення.

## З чого почати

Застосунок перевіряє репозиторії групових проєктів. Адміністратор підключає hook один раз на вузлах Gitaly, а власники проєктів зберігають потрібні політики разом із кодом.

| Хочу | Наступний крок |
| --- | --- |
| Зрозуміти можливості й межі захисту | Огляд [політик](#політики) та [модель загроз](docs/user/administration/threat-model.md) |
| Установити застосунок | [Перший запуск](#перший-запуск) і [повна інструкція](docs/user/administration/install.md) |
| Налаштувати політику | Обрати файл у таблиці [політик](#політики), почати з його короткого прикладу |
| Подивитися готові рішення | Каталоги [файлів](docs/user/policies/pathpolicy.md#приклади), [комітів](docs/user/policies/commitpolicy.md#приклади), [тегів](docs/user/policies/tagpolicy.md#приклади) і [гілок](docs/user/policies/branchpolicy.md#приклади) |
| Перевірити конфігурацію до push | [Локальна перевірка policy](docs/user/reference/validate.md) або [серверний selftest](docs/user/reference/cli.md#selftest) |
| Розібратися у відмові | Розділ "Відмови" policy з повідомлення; для серверних збоїв - [діагностика](docs/user/administration/operations.md#типові-проблеми) |
| Узяти участь у розробці | [Документація розробника](docs/development/README.md) і [CONTRIBUTING](CONTRIBUTING.md) |

## Політики

Кожна політика - окремий YAML-файл у корені репозиторію.

| Файл | Що вирішує |
| --- | --- |
| [PATHPOLICY](docs/user/policies/pathpolicy.md) | хто може змінювати певні файли: CI-конфігурацію, deploy-логіку, prod-значення |
| [COMMITPOLICY](docs/user/policies/commitpolicy.md) | яким має бути формат commit messages |
| [TAGPOLICY](docs/user/policies/tagpolicy.md) | хто може випускати релізи: тег приймається лише з підписом дозволеного ключа OpenPGP або SSH |
| [BRANCHPOLICY](docs/user/policies/branchpolicy.md) | як називаються нові гілки і які з них мають бути захищені в GitLab |

## Навіщо

Політика файлів потрібна, коли доступ до гілки ширший, ніж доступ до її CI-конфігурації або deployment-скриптів. Наприклад, розробники можуть змінювати код у своїх гілках, але CI має залишатися під контролем команди платформи. Серверна перевірка застосовує цю вимогу незалежно від налаштувань локальних Git-хуків.

gitlab-repopolicy перевіряє зміну на сервері, тому байдуже, як її внесли: push по HTTPS чи SSH, merge кнопкою, auto-merge, Web IDE, API, push із CI. Правила версіонуються і проходять review разом з кодом. Hook може лише відмовити, права GitLab він не розширює.

Коли hook нічого не перевіряє:
- push у проєкт особистого namespace користувача (`alice/my-project`). Перевіряються лише проєкти груп
- push у репозиторії wiki, design і snippets
- у конфігурації сервера `mode: off`

З `mode: audit` hook виконує ті самі перевірки й записує рішення в журнал, але не блокує push через порушення політик. Помилки запуску можуть призвести до відмови; докладніше про це й інші режими - у [конфігурації](docs/user/administration/configuration.md#mode).

Приклад від файлу до виправлення. Команда платформи (alice і sre-lead) хоче, щоб CI-конфігурацію змінювала лише вона, у будь-якій гілці:

```yaml
# examples/ci-config/PATHPOLICY
# The platform team, alice and sre-lead, owns the CI configuration and the
# deploy scripts in every branch, where only new content counts, and the
# production values in main and release branches, where every commit that
# enters the branch counts. Everything else is authorized by GitLab as usual.
version: 1

admins: [alice, sre-lead]

rules:
  ci-config:
    match: all
    check_history: new-content
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "scripts/deploy/**"
    allow: [alice, sre-lead]
    message: >-
      The platform team changes the CI/CD configuration and the deploy scripts.
      Ask alice or sre-lead to push this change.
    help:
      contact: "#platform"

  prod-values:
    match: [main, '/^release\//']
    check_history: incoming
    paths: ["helm/**/values-prod.yaml"]
    allow: [alice, release-manager]
    message: >-
      Production values are changed in main and release branches by alice or
      release-manager. Propose the change in your branch and ask them to merge it.
```

bob, якого немає в `allow`, змінює `.gitlab-ci.yml` у своїй гілці `feature` і пушить. GitLab відхиляє push:

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: PATHPOLICY rule "ci-config", branch feature: you may not change these files
remote:   may change them: alice, sre-lead
remote:   files:
remote:     .gitlab-ci.yml
remote:   The platform team changes the CI/CD configuration and the deploy scripts. Ask alice or sre-lead to push this change.
remote:   the content of .gitlab-ci.yml in commit 11e97b832 differs both from this branch and from main; new content is pushed by one of them
remote:   who can help with this rule: #platform
remote:
remote: rules: PATHPOLICY in main (f50c5a0)
remote: checked as: bob (SSH)
remote: GL-HOOK-ERR: gitlab-repopolicy: you may not change .gitlab-ci.yml (PATHPOLICY rule ci-config; may change: alice, sre-lead)
```

Що робить bob: просить alice внести зміну або прибирає її зі своїх комітів і пушить ще раз:

```sh
git restore --source=origin/main -- .gitlab-ci.yml
git commit --amend --no-edit
git push --force-with-lease origin feature
```

[Розбір відмови PATHPOLICY](docs/user/policies/pathpolicy.md#відмови) пояснює кожний рядок повідомлення та способи виправлення.

## Передумови

Перед встановленням мають бути:
- GitLab Self-Managed, CE або EE, з версіями з таблиці нижче
- права root на кожній ноді, де працює Gitaly
- instance service account з токеном (`read_api`, `self_rotate`), Maintainer у кожній top-level групі ([install.md](docs/user/administration/install.md#3-service-account-і-токен))
- `custom_hooks_dir` у конфігурації Gitaly, наприклад `/var/opt/gitlab/gitaly/custom_hooks` у `gitlab.rb` і `gitlab-ctl reconfigure`. Без нього Gitaly не запускає глобальні hooks
- підготовлений пакет `.deb` або `.rpm`. Для локальної перевірки його збирає `make release-snapshot`; порядок перевірки підписаних артефактів наведено у [встановленні](docs/user/administration/install.md#1-підготувати-й-перевірити-артефакти)

## Перший запуск

Нижче наведено приклад встановлення та першого запуску gitlab-repopolicy для GitLab Self-Managed, встановленого через Linux package (Omnibus) на одному сервері.

Перед початком необхідно створити instance service account у GitLab, отримати токен доступу та налаштувати `custom_hooks_dir` у Gitaly. Пакет має бути підготовлений за [інструкцією встановлення](docs/user/administration/install.md#1-підготувати-й-перевірити-артефакти). `1.0.0` нижче позначає версію прикладу; замініть ім'я пакета на фактичне ім'я зібраного артефакту.

Для першого запуску на робочому сервері рекомендується використовувати режим `audit`. У цьому режимі застосунок перевіряє зміни та записує результати в журнал, але не блокує push через порушення політик. Після встановлення пакета, але перед підключенням hook, відкрийте конфігурацію й установіть `mode: audit`.

Встановлення та запуск можуть виглядати так:

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb
sudoedit /etc/gitlab-repopolicy/config.yaml
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

У редакторі змініть `mode` до виконання наступних команд. Також перевірте `default_branch: [main, master]` у поставці: це обмеження імені основної гілки для всіх групових проєктів, навіть без policy. Для початкового спостереження без цього обмеження задайте `default_branch: []`.

Після виконання команд:

- Пакет встановить застосунок, створить необхідні каталоги та конфігураційні файли
- `sudoedit` дасть змогу змінити режим до підключення hook
- Команда `cat` збереже токен service account у `/etc/gitlab-repopolicy/token`. Після введення токена натисніть Enter, а потім Ctrl+D. Для нового файлу `umask` задає права 0440
- `install-hook` створить wrapper у `pre-receive.d/`, надасть користувачу `git` доступ до конфігурації, токена й журналу. З цього моменту Gitaly запускатиме перевірку змін
- `selftest` перевірить встановлення та налаштування. Виконайте перевірку від `root` і `git`, оскільки Gitaly запускає hook від `git`. Будь-який рядок `FAIL` означає код завершення 1

Успішна перевірка підтверджує доступ до API й локальних файлів, але не виклик wrapper самим Gitaly і не охоплення всіх груп. Усуньте помилки `selftest`, перевірте групи командою `coverage` і виконайте контрольний push у тестовий проєкт. У режимі `audit` очікувану відмову шукайте в `/var/log/gitlab-repopolicy/audit.log` з `"enforced": false`. Помилки запуску, зокрема нечитабельна конфігурація, можуть блокувати push навіть у `audit`.

Це приклад базового встановлення на одному сервері. Докладні інструкції, включно з перевіркою релізу, налаштуванням service account, встановленням у Docker та конфігурацією окремих вузлів Gitaly, наведено в [документації зі встановлення](docs/user/administration/install.md). Перехід від спостереження до блокування описано в [порядку інтеграції](docs/user/administration/integration-guide.md).

## Підтримувані версії GitLab

| GitLab CE | Результат e2e |
| --- | --- |
| 19.4 (19.4.2) | підтримується |
| 19.3 (19.3.5) | підтримується |
| 19.2 (19.2.8) | підтримується |

Матриця e2e проходить на поточній і двох попередніх minor-версіях GitLab CE, nightly - на `latest`. EE і будь-який tier підтримуються тим самим кодом: використовуються лише функції Free. GitLab.com і GitLab Dedicated не підтримуються.

## Продуктивність

Hook працює всередині push, тому push з ним довший. Наскільки довший, залежить від того, чи потрібен hook-у API GitLab.

Як міряли. GitLab CE 19.4.2 у Docker і git-клієнт на одній машині, push по HTTPS і SSH на `localhost`. Кожен сценарій пушився без hook (після `uninstall-hook`) і з hook (`install-hook`, `mode: enforce`). Режими чергувалися блоками: без, з, без, з, без, з. У кожному блоці на кожен сценарій і протокол 3 push на прогрів і 10 замірів, разом 30 замірів на клітинку. Час - повний `git push` на клієнті, у таблиці медіана в мілісекундах.

| Сценарій | HTTPS: (без hook / з hook), мс | SSH: (без hook / з hook), мс |
| --- | --- | --- |
| без policy-файлів, один коміт | 274 / 329 (+55) | 372 / 400 (+28) |
| PATHPOLICY, коміт не змінює файли правила | 297 / 315 (+18) | 379 / 405 (+25) |
| PATHPOLICY, коміт змінює файл правила, користувач push є в `allow` | 297 / 448 (+151) | 376 / 600 (+223) |
| COMMITPOLICY, один коміт | 278 / 328 (+51) | 385 / 414 (+29) |
| COMMITPOLICY, нова гілка з 50 комітами | 309 / 342 (+33) | 385 / 441 (+55) |
| BRANCHPOLICY, нова гілка за `match` | 285 / 302 (+16) | 375 / 388 (+14) |
| BRANCHPOLICY, нова гілка з `user_prefix` | 281 / 299 (+17) | 388 / 465 (+77) |
| BRANCHPOLICY, нова гілка з `protected: true` | 290 / 398 (+108) | 384 / 477 (+93) |
| TAGPOLICY, підписаний тег | 293 / 320 (+27) | 375 / 409 (+35) |
| TAGPOLICY, підписаний тег з `protected: true` | 312 / 410 (+98) | 387 / 502 (+115) |
| усі чотири політики, коміт у новій гілці `alice/...` | 313 / 342 (+29) | 394 / 503 (+110) |

Що з цього видно:
- коли hook-у не потрібен API GitLab, push довший на 14-55 мс. Сам hook, за полем `duration_ms` в audit, працює 12-46 мс (медіана)
- помітно довше там, де hook питає GitLab. Коли коміт змінює файл правила PATHPOLICY, hook запитує учасників проєкту, щоб перевірити `allow`, це +151-223 мс. Для `protected: true` у BRANCHPOLICY і TAGPOLICY hook запитує захищені гілки або теги проєкту, це +93-115 мс
- по SSH hook ще перевіряє, що ключ особистий, а не deploy key. Тому SSH дорожчий у сценаріях з `user_prefix` і з усіма політиками

Паралельне навантаження. 10 клієнтів одночасно, кожен робить 30 push у свої гілки одного проєкту з усіма чотирма політиками, кожен десятий push - з підписаним тегом.

| Протокол | Без hook: (p50 / p90), мс | З hook: (p50 / p90), мс | Загальний час: (без hook / з hook), с |
| --- | --- | --- | --- |
| HTTPS | 324 / 357 | 347 / 372 | 10,2 / 10,7 |
| SSH | 400 / 428 | 445 / 550 | 12,3 / 14,2 |

Відмов немає. Hook під цим навантаженням працює так само, як і під час послідовних push: `duration_ms` медіана 18 мс, p90 24 мс. Усі 300 push одного заміру йдуть від одного користувача в один проєкт, близько 1500 на хвилину. Це більше за ліміт GitLab `gitlab_shell_operation_limit` (за замовчуванням 600 операцій на хвилину для користувача і проєкту), тому на час заміру ліміт знятий. З лімітом GitLab сам відхиляє зайві push з `GitLab: rate limited` ще до hook.

Обмеження заміру:
- клієнт і GitLab на одній машині, мережі немає. Якщо Gitaly на окремих нодах, кожен запит hook до API додає ще й затримку мережі між Gitaly і GitLab Rails
- один користувач, маленькі репозиторії, у кожному push один або кілька змінених файлів
- одна машина. На іншому залізі абсолютний час буде іншим, порівнювати варто різницю без hook і з hook

Машина: Intel Core i9-14900HX (32 потоки, governor `performance`), 62 GiB RAM, NVMe WD PC SN560 1 ТБ, Linux 6.17, Docker 29.9.2 без лімітів CPU і пам'яті для контейнера GitLab, git 2.43.0 на клієнті, gitlab-repopolicy з коміту `ffcda2e`. Замір від 28.10.2026.

## Редактор

Policy-файли не мають розширення, тому редактор треба навчити, що це YAML зі схемою. Схеми лежать у каталозі `schemas/` репозиторію. Посилання нижче ведуть на гілку `main`. Щоб редактор перевіряв файли за схемою конкретного релізу, замініть у посиланні `main` на тег релізу, наприклад `v1.0.0`.

**VS Code** з розширенням YAML (`yaml-language-server`), у `settings.json`:

```json
{
  "files.associations": { "PATHPOLICY": "yaml", "COMMITPOLICY": "yaml", "TAGPOLICY": "yaml", "BRANCHPOLICY": "yaml" },
  "yaml.schemas": {
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/pathpolicy.schema.json": "PATHPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/commitpolicy.schema.json": "COMMITPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/tagpolicy.schema.json": "TAGPOLICY",
    "https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/branchpolicy.schema.json": "BRANCHPOLICY"
  }
}
```

**PyCharm, GoLand, IntelliJ IDEA** та інші IDE JetBrains перевіряють YAML за схемою без плагінів. Схеми написані за JSON Schema draft 2020-12.
1. Settings > Editor > File Types > YAML, у "File name patterns" додайте `PATHPOLICY`, `COMMITPOLICY`, `TAGPOLICY`, `BRANCHPOLICY`. Це налаштування самої IDE, його робить кожен у себе один раз
2. Settings > Languages & Frameworks > Schemas and DTDs > JSON Schema Mappings, кнопка `+` для кожного файлу:
   - Name: `PATHPOLICY`
   - Schema file or URL: `https://raw.githubusercontent.com/Kuzmenko-Pavel/gitlab-repopolicy/main/schemas/pathpolicy.schema.json`
   - Schema version: JSON Schema 2020-12
   - кнопкою `+` під списком додайте File path pattern `PATHPOLICY`

   Так само для `COMMITPOLICY`, `TAGPOLICY` і `BRANCHPOLICY`. Ці прив'язки IDE зберігає в проєкті, у `.idea/jsonSchemas.xml`: закомітьте файл, і в команди схеми підключаться самі

Після цього IDE підсвічує невідомі ключі й неправильні значення і показує підказки з описів полів.

Перевірити файл локально тим самим кодом, що й hook, можна командою `gitlab-repopolicy validate` ([validate.md](docs/user/reference/validate.md)), повідомлення комітів - `gitlab-repopolicy lint-commit`. Редактор знає лише схему, а `validate` перевіряє ще й злиття з пресетами, регулярні вирази, порядок правил BRANCHPOLICY і ключі TAGPOLICY.

## Участь

Перед внеском прочитайте [правила розробки й review](CONTRIBUTING.md) та [кодекс поведінки](CODE_OF_CONDUCT.md). Про вразливості повідомляйте приватно за [процедурою SECURITY](SECURITY.md).

## Ліцензія

[Apache-2.0](LICENSE)
