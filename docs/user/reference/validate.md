[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](validate.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](validate.en.md)

# Локальна перевірка policy-файлів

## Швидка перевірка policy-файлів

Відкрийте каталог проєкту, у якому лежать policy-файли, і виконайте:

```sh
gitlab-repopolicy validate
```

Команда шукає `PATHPOLICY`, `COMMITPOLICY`, `TAGPOLICY` і `BRANCHPOLICY` лише в поточному каталозі. Вона перевіряє YAML, схему, регулярні вирази, правила `match`, порядок BRANCHPOLICY та публічні ключі TAGPOLICY. GitLab, токен і навіть Git-репозиторій для офлайн-перевірки не потрібні.

Для одного файлу:

```sh
gitlab-repopolicy validate -f PATHPOLICY
```

Наприклад, у каталозі з одним PATHPOLICY з [прикладу CI](../examples/pathpolicy/ci-configuration.md) вивід такий:

```console
$ gitlab-repopolicy validate
PATHPOLICY: valid
$ gitlab-repopolicy validate -f PATHPOLICY
PATHPOLICY: valid
```

`valid` означає, що виконані перевірки файла не знайшли помилок. За помилки команда друкує `invalid`, причину й, коли вона відома, позицію в YAML; код завершення - 1. Порожній каталог дає `no policy files in the current directory` і код 0, тому успішний job без явного `-f` не доводить наявності політики.

Попередження й рядки `not checked` не роблять файл невалідним. Перегляньте їх перед merge. Файл із `extends` потребує копії пресетів, як описано нижче.

## Один файл, чернетка й кілька файлів

`-f` приймає один шлях. Для кількох стандартно названих файлів у корені проєкту виконайте `validate` без `-f`. Для різних каталогів запускайте команду окремо для кожного потрібного шляху; повторення `-f` не є списком файлів.

Модуль визначається за ім'ям файлу. Для чернетки з іншим ім'ям задайте його явно:

```sh
gitlab-repopolicy validate -f drafts/pathpolicy.yaml --module pathpolicy
```

Доступні `pathpolicy`, `commitpolicy`, `tagpolicy`, `branchpolicy`. Неправильні аргументи дають код 2. Повний перелік прапорців містить [довідник CLI](cli.md#validate).

## Пресети

`extends` посилається на іменований набір правил із серверної конфігурації. Для локальної перевірки потрібна та сама версія пресетів; токен із сервера передавати розробнику не потрібно.

```sh
gitlab-repopolicy validate --config presets.yaml
```

`presets.yaml` може містити лише ключ `presets`, як [файл прикладів](../../../examples/presets.yaml). Команда зливає пресети й файл, показує походження правил та повідомляє про зламані пресети конфігурації, навіть якщо поточний файл їх не використовує. Без потрібного пресета `extends` дає помилку.

Перевірити готовий приклад із клону цього проєкту можна без підготовки демонстраційного репозиторію:

```sh
gitlab-repopolicy validate -f examples/org-ci/PATHPOLICY --config examples/presets.yaml
```

## Перевірка match для гілки чи тегу

Щоб перевірити, який клас отримає нове ім'я гілки, використайте `--branch`; для переліку правил і ключів тегу - `--tag`:

```sh
gitlab-repopolicy validate -f BRANCHPOLICY --branch release/v1.4.0
gitlab-repopolicy validate -f TAGPOLICY --tag v1.4.0-prod
```

Це інформаційні запити після перевірки валідності файлу. Відповідь `result: denied`, відсутність збігу або спільного підписанта не перетворює код 0 на 1. `--tag` не перевіряє підпис реального тегу. Вимога `protected` лише показується, навіть з `--online`.

Сценарії [вибору імені гілки](../examples/branchpolicy/organization-standard.md) і [перевірки імені тегу](../examples/tagpolicy/inspect-tag.md) містять повні конфігурації. Спільний синтаксис зіставлення описано в [match](../policies/match.md).

## Онлайн-перевірка користувачів і admins

На вузлі Gitaly користувач із правом читання серверного конфігу й токена може додати `--online`. Команда звертається до GitLab API; `--config` обов'язковий і читається повністю.

```sh
sudo -u git gitlab-repopolicy validate -f PATHPOLICY   --online --config /etc/gitlab-repopolicy/config.yaml   --project group/project
```

Без `--project` перевіряється існування й активність username, але не ролі admins. З `--project` перевіряються ролі admins PATHPOLICY: потрібен хоча б один активний Maintainer або Owner. Непрацездатний admin поряд із робочим дає попередження; жоден робочий admin - помилка. `--online --branch` також може перевірити існування username з префікса.

Недоступність перевірки може дати `not checked` і код 0; неможливість прочитати явно заданий проєкт дає код 1. Тому в CI не можна трактувати лише код 0 як підтвердження всіх прав. `explain` відповідає на інше питання: чи дозволяють path-правила конкретному користувачу змінити шлях.

## validate у CI merge request

Для job потрібен бінарник з перевіреного релізу або образ із ним. Підпис і контрольні суми артефакту перевіряються за [інструкцією встановлення](../administration/install.md#1-підготувати-й-перевірити-артефакти) під час підготовки образу. Самої суми, завантаженої поруч із бінарником, недостатньо для підтвердження походження.

```yaml
# .gitlab-ci.yml; the runner image already contains the verified binary
validate-policies:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
      changes: [PATHPOLICY, COMMITPOLICY, TAGPOLICY, BRANCHPOLICY]
  script:
    - gitlab-repopolicy validate
```

Якщо використано `extends`, доставте ту саму копію `presets.yaml`, що й на сервері, і додайте `--config presets.yaml`. Для контролю обов'язкової наявності конкретного файлу запускайте окрему команду з `-f`. Серверний токен для офлайн-перевірки не потрібний.

## Чого validate не знає

| Питання | Інструмент або обмеження |
| --- | --- |
| Чи валідний файл | `validate`; без online не підтверджує користувачів і ролі |
| Чи відповідає повідомлення правилам | `lint-commit`; без `--rule` застосовує всі правила незалежно від match |
| Хто може змінити шлях | `explain`; читає чинну PATHPOLICY через API, не аналізує весь push |
| Чи дозволить hook задане оновлення ref | `test`; потребує правильних Git-об'єктів, refs, конфігурації та за потреби API |

Офлайн `validate` не бачить версії з якоря, фактичного користувача push, налаштувань GitLab і Git-типу файлу. Symlink може читатися локально, але не приймається як policy у Git. Нові username й робочі admins перевіряються при прийманні. Прострочений незмінений OpenPGP-ключ може бути попередженням під час push, хоча перевірка файла самого по собі дає помилку.

Для повного навчального проходу з YAML-помилками, пресетами, простроченими ключами й точним виводом відкрийте [демонстраційний каталог validate](../examples/validation-walkthrough.md). Це окремий практикум; для щоденної перевірки достатньо команд вище.
