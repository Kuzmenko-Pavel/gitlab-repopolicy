[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](commitpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](commitpolicy.en.md)

# COMMITPOLICY

COMMITPOLICY перевіряє формат кожного commit message, що потрапляє цим push у гілку. Файл лежить у корені репозиторію з ім'ям `COMMITPOLICY`, вміст - YAML.

У команд різні стандарти: Conventional Commits зі scope і без, обов'язковий issue або ні, проза чи списки в body, вільний формат. Тому модуль - не одна схема, а каталог незалежних правил. Кожне правило має рівень, значення і необов'язковий фільтр за типом. Правило, яке не вказане, не перевіряється: вбудованих правил за замовчуванням немає.

Хто змінює сам COMMITPOLICY, задає path-правило PATHPOLICY ([pathpolicy.md](pathpolicy.md)). Без такого правила файл змінює будь-хто з правом push.

## Приклади

Conventional Commits у захищених гілках:

```yaml
# examples/conventional-commits/COMMITPOLICY
# Conventional Commits in protected branches; a stricter type list in
# release branches. Feature branches are not checked.
version: 1
mode: enforce

rulesets:
  - name: protected
    refs: protected
    format: conventional
    except:
      merge_commits: true
      git_reverts: true
      users: [release-bot]
    rules:
      type-enum:         { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
      scope:             { level: error, value: optional }
      breaking-bang:     { level: error, value: allow }
      header-max-length: { level: warn,  value: 100 }
      subject-case:      { level: error, value: lower-first }
      subject-full-stop: { level: error, value: forbid }
      body-required:     { level: error, types: [feat, fix] }
      reference:         { level: warn,  location: any, pattern: "[A-Za-z0-9_./-]*#[0-9]+", types: [feat, fix] }
      trailer-required:  { level: error, value: [Signed-off-by] }
    message: >-
      Commits in protected branches follow Conventional Commits.
      Reword them with git rebase -i or squash the merge request.

  - name: release
    refs: ["refs/heads/release/*"]
    format: conventional
    rules:
      type-enum: { level: error, value: [fix, docs, chore] }
```

Вільний формат із ключем issue на початку заголовка:

```yaml
# examples/free-format/COMMITPOLICY
# A free-form header that starts with an issue key, on the default branch only.
version: 1

rulesets:
  - name: issue-key
    refs: default
    format: free
    except:
      merge_commits: true
    rules:
      header-max-length: { level: error, value: 72 }
      subject-full-stop: { level: error, value: forbid }
      pattern:
        level: error
        value: '[A-Z][A-Z0-9]+-[0-9]+ \S'
        examples:
          valid: ["PAY-123 Round refunds to cents"]
          invalid: ["Round refunds to cents", "pay-123 Round refunds"]
    message: Start the header with the issue key, for example PAY-123.
```

## Структура файлу

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `version` | так | `1` |
| `mode` | ні | `enforce` (за замовчуванням) або `warn`. `warn` знижує всі `error` до попереджень: так вмикається весь rollout одним рядком |
| `extends` | ні | ім'я пресету з `commitpolicy_presets` конфігурації сервера ([configuration.md](configuration.md)) |
| `rulesets` | так | набори правил |
| `rulesets[].name` | так | `[a-z0-9-]{1,63}`, унікальне у файлі |
| `rulesets[].refs` | ні | `all` (за замовчуванням), `default`, `protected` або список гілок і тегів |
| `rulesets[].format` | ні | `conventional` (за замовчуванням) або `free` |
| `rulesets[].except` | ні | винятки, див. нижче |
| `rulesets[].rules` | ні | правила з каталогу |
| `rulesets[].message` | ні | текст, який побачить автор коміту з порушенням |

Застосовуються всі rulesets, що збіглися з гілкою (AND). У списку `refs` символ `*` збігається в межах одного сегмента: `refs/heads/*` збігається з `main`, але не з `feature/x`.

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB.

## Формати

**`conventional`** - header `type(scope)!: subject`: тип, необов'язковий scope у дужках, необов'язковий `!`, двокрапка, один пробіл і непорожній subject.

**`free`** - header цілком є subject. Правила про type і scope для такого ruleset недоступні.

Граматику формату перевіряють завжди, окремого правила для цього немає:
- для `conventional` - header за схемою вище, порожній рядок між header і body, коректний trailer-блок
- для `free` - непорожній header і порожній рядок перед body
- повідомлення з NUL-байтом або об'єкт коміту більший за 1 MiB - порушення

Як читається повідомлення:
- `\r\n` замінюється на `\n` (UI GitLab пише `\r\n`), кінцеві порожні рядки відкидаються (git завжди додає `\n`)
- trailer-блок - останній абзац, усі рядки якого мають вигляд `Key: value` або `Key #value`, як у `git interpret-trailers`. `BREAKING CHANGE:` теж trailer. Порядок trailers не важливий
- body за формою не обмежується: списки, абзаци, код, довгі описи допустимі, поки не задано `body-max-line-length`

## Каталог правил

Кожне правило - `{ level, value, types }`:
- `level` - `error`, `warn` або `off`
- `value` - значення правила (у `body-required` його немає)
- `types` - необов'язковий список типів: правило перевіряється лише для комітів цих типів

| Правило | `value` | Формат | Що перевіряє |
| --- | --- | --- | --- |
| `type-enum` | список | conventional | тип зі списку |
| `scope` | `optional`, `required`, `forbidden` | conventional | наявність scope |
| `scope-enum` | список | conventional | scope зі списку. Scope `a, b` або `a/b` ділиться на частини, кожна має бути у списку |
| `breaking-bang` | `allow`, `forbid` | conventional | `type!:` |
| `header-max-length` | число | будь-який | довжина першого рядка в символах Unicode |
| `subject-case` | `lower-first`, `sentence`, `any` | будь-який | регістр першої літери subject |
| `subject-full-stop` | `forbid`, `require` | будь-який | крапка в кінці subject |
| `body-required` | немає | будь-який | body є |
| `body-max-line-length` | число | будь-який | довжина кожного рядка body. Рядки з URL і код (огороджений блок або рядки з відступом) не враховуються |
| `reference` | `location`, `pattern`, `keys` | будь-який | посилання на issue знайдено в указаному місці |
| `trailer-required` | список ключів | будь-який | є trailers з цими ключами |
| `pattern` | regex | будь-який | усе повідомлення відповідає regex |

`reference`:
- `location: any` - посилання будь-де в повідомленні, наприклад `(group/project#665)` усередині абзацу body
- `location: body` - лише в body
- `location: trailer` - лише в trailer-блоці; `keys` обмежує ключі, наприклад `[Refs, Issue]`. Ключі порівнюються без урахування регістру
- `pattern` - RE2 regex посилання

Числа в `value` - від 1 до 10000. Каталог правил розширюється в minor-версіях без зміни схеми.

## `pattern`

Для форматів, які структурні правила не описують. Regex компілюється бібліотекою `regexp2` з опцією `RE2`: є lookahead і lookbehind, а також групи `(?P<name>…)`. Збіг шукається з початку нормалізованого повідомлення, як `re.match` у Python.

Різниця з Python `re`: `$` у Python збігається ще й перед кінцевим `\n`. Повідомлення нормалізується (кінцевий `\n` відкидається), тому патерн, налагоджений у Python, тут працює так само.

`regexp2` - backtracking-рушій без гарантії лінійного часу. Тому на одне повідомлення відведено 100 ms, на весь push - 2 s (`timeouts.pattern`, `timeouts.pattern_budget` у конфігурації сервера). Перевищення - порушення `commit_check_timeout` з рівнем правила `pattern` для кожного неперевіреного коміту: автор контролює вхід і не повинен обходити перевірку повільними повідомленнями.

`examples` з `valid` і `invalid` перевіряються під час приймання нової версії і в `validate`. Приклад, що не пройшов, робить файл невалідним.

## Винятки (`except`)

| Ключ | Що пропускається |
| --- | --- |
| `merge_commits: true` | сам merge-коміт |
| `merged_commits: true` | коміти, які merge-коміт приносить через другого й наступних батьків. Перевіряється лише ланцюжок first-parent. Fast-forward, rebase і squash перевіряються як зазвичай |
| `git_reverts: true` | повідомлення `git revert` і кнопки revert у GitLab: header `Revert "..."` і рядок `This reverts commit <sha>` |
| `users: [...]` | push цих користувачів, наприклад бота релізних комітів. Username порівнюється без урахування регістру |

`except.users` діє лише для людини або бота, яких hook розпізнав. Push deploy key під виняток не потрапляє. Для SSH hook перевіряє через API, що ключ особистий; якщо API не відповів, ruleset пропускається з попередженням.

## Які коміти перевіряються

- Лише в гілках із `refs` ruleset. За `refs: protected` feature-гілки вільні, WIP-коміти не блокуються
- Перевіряються всі коміти, які цей push додає в гілку. Під час створення protected гілки - коміти, яких немає в default branch і інших protected гілках. Під час створення іншої гілки - нові коміти
- Merge request без squash приносить усі коміти гілки і merge-коміт. Squash merge приносить один коміт, його повідомлення задає squash commit template проєкту
- Повідомлення тегів не перевіряються. Підпис тегів перевіряє TAGPOLICY ([tagpolicy.md](tagpolicy.md))
- Історія до появи COMMITPOLICY не перевіряється

## Пресети (`extends`)

Пресет - іменований набір правил у конфігурації сервера, щоб не копіювати одне й те саме в кожен репозиторій:

```yaml
# /etc/gitlab-repopolicy/config.yaml
commitpolicy_presets:
  org-conventional:
    rules:
      type-enum: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
```

Правила пресету застосовуються до кожного ruleset файлу, правила ruleset перекривають їх за іменем. Правила для `conventional` не застосовуються до ruleset з `format: free`. Невідомий або невалідний пресет - помилка приймання і `validate`. Якщо пресет зник з конфігурації вже після приймання файлу, hook пропускає його з попередженням.

## Режими і відмови

COMMITPOLICY - про гігієну, а не про доступ, тому за збою він не блокує роботу (fail open). Те, що контролює автор push, - порушення, а не пропуск.

| Ситуація | Результат |
| --- | --- |
| `mode: warn`, порушення | push проходить, порушення у виводі push і в audit |
| `mode: enforce`, порушення рівня `error` | відмова `commit_format_violation` |
| правило з `level: warn` | лише попередження, і в `enforce` теж |
| таймаут або бюджет `pattern` | порушення `commit_check_timeout` за рівнем правила |
| понад 10 000 вхідних комітів | `limit_exceeded`: в `enforce` відмова, у `warn` попередження |
| COMMITPOLICY у default branch невалідна | перевірку пропущено, попередження, audit `invalid_commit_policy` |
| невідоме правило у default branch (наприклад, після відкату бінарника) | пропущено лише це правило, попередження |
| API не відповів, коли треба знати, чи захищена гілка | ruleset з `refs: protected` пропущено, попередження |
| push змінює COMMITPOLICY на невалідну версію | відмова `invalid_policy_update` |

Невідоме правило в новій версії файлу - помилка приймання: друкарська помилка не пройде мовчки.

Попередження успішного push видно в терміналі як рядки `remote:`. Для merge кнопкою, Web IDE та інших операцій UI GitLab успішний вивід hook не показує: там попередження видно лише в audit.

## Локальна перевірка

`gitlab-repopolicy lint-commit` використовує ту саму реалізацію, що й hook, тож локальна перевірка і сервер не розходяться. Python чи Node не потрібні. Бінарник для розробника можна встановити через `go install` ([install.md](install.md)).

| Команда | Що перевіряє |
| --- | --- |
| `gitlab-repopolicy lint-commit -m "feat: add x"` | одне повідомлення за `COMMITPOLICY` поточного каталогу |
| `gitlab-repopolicy lint-commit --range origin/main..HEAD` | коміти діапазону; одна ревізія - уся її історія |
| `-f FILE` | інший файл COMMITPOLICY |
| `--config FILE` | конфігурація сервера з пресетами, якщо файл використовує `extends` |

`lint-commit` застосовує всі rulesets файлу і не дивиться на `refs`: локально невідомо, у яку гілку піде коміт. `except.users` локально не діє.

```text
$ gitlab-repopolicy lint-commit -f COMMITPOLICY -m "Fix deploy."
message: [protected] error: header: expected "type(scope): subject"
message: [protected] error: trailer-required: missing Signed-off-by in the trailer block
message: [release] error: header: expected "type(scope): subject"
```

Як `commit-msg` hook git:

```sh
#!/bin/sh
# .git/hooks/commit-msg
exec gitlab-repopolicy lint-commit -m "$(git stripspace --strip-comments < "$1")"
```

`git stripspace` прибирає рядки-коментарі, які залишає редактор: git передає hook файл повідомлення ще з ними. Код виходу `lint-commit` - 0, якщо порушень рівня `error` немає (попередження не заважають), і 1, якщо вони є.
