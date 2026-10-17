[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](commitpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](commitpolicy.en.md)

# COMMITPOLICY

COMMITPOLICY перевіряє, як написані повідомлення комітів, які push додає в гілку. Файл лежить у корені репозиторію з ім'ям `COMMITPOLICY`, вміст - YAML. Якщо повідомлення не відповідає правилам, GitLab відхиляє push, і коміт треба переписати.

Спочатку розберемося, з чого складається повідомлення коміту і як його написати. Далі - як COMMITPOLICY це перевіряє.

## З чого складається повідомлення коміту

Ось повне повідомлення в стилі [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```text
fix(billing): round refunds to cents

Refunds were computed in floating point and lost a cent on some amounts.
Amounts are now kept in integer cents.

Refs: #665
Signed-off-by: Olena Ivanenko <olena@example.com>
```

Воно складається з трьох частин, між ними порожній рядок.

| Частина | У прикладі | Що це |
| --- | --- | --- |
| header | `fix(billing): round refunds to cents` | перший рядок. Його показують `git log --oneline`, список комітів у GitLab і заголовок merge request |
| body | два рядки про refunds | пояснення: що змінилося і чому. Необов'язковий, якщо правила проєкту його не вимагають |
| trailer-блок | `Refs: #665` і `Signed-off-by: ...` | останній абзац, де кожен рядок має вигляд `Key: value` або `Key #value`. Сюди пишуть службові дані: посилання на issue, автора за DCO |

Header у форматі Conventional Commits теж складається з частин.

| Частина | У прикладі | Що це |
| --- | --- | --- |
| type | `fix` | вид зміни: `feat` - нова можливість, `fix` - виправлення, `docs` - документація і так далі. Список дозволених типів задає проєкт |
| scope | `billing` | необов'язково, у дужках одразу після type. Частина системи, якої стосується зміна |
| `!` | немає | необов'язково, перед двокрапкою: `feat(api)!: drop the v1 endpoints`. Позначає зміну, що ламає сумісність |
| `: ` | `: ` | двокрапка і рівно один пробіл |
| subject | `round refunds to cents` | короткий опис зміни, непорожній |

Так header виглядає в різних варіантах. Помилки - це вивід `gitlab-repopolicy lint-commit`, те саме повідомлення пише hook під час push.

| Header | Результат |
| --- | --- |
| `fix: round refunds to cents` | правильно, без scope |
| `fix(billing): round refunds to cents` | правильно, зі scope |
| `feat(api)!: drop the v1 endpoints` | правильно, зміна ламає сумісність |
| `fix round refunds to cents` | `header: expected "type(scope): subject"` - немає двокрапки |
| `fix:round refunds to cents` | `header: expected "type(scope): subject", a space must follow ":"` |
| `fix:  round refunds to cents` | `header: expected "type(scope): subject", exactly one space must follow ":"` |
| `fix(billing: round refunds to cents` | `header: expected "type(scope): subject", the scope parenthesis is not closed` |
| `fix(): round refunds to cents` | `header: the scope in parentheses is empty` |
| `fix:` | `header: expected "type(scope): subject", the subject is empty` |

Якщо після header одразу, без порожнього рядка, іде текст, це помилка `the body must be separated from the header by an empty line`.

### Як написати таке повідомлення

Кожен `-m` - окремий абзац, git сам ставить між ними порожній рядок.

```sh
git commit -s \
  -m "fix(billing): round refunds to cents" \
  -m "Refunds were computed in floating point and lost a cent on some amounts. Amounts are now kept in integer cents." \
  -m "Refs: #665"
```

Або `git commit -s` без `-m`. Тоді git відкриє редактор, перший рядок - header, далі порожній рядок і body. Подивитися, що вийшло, можна командою `git log -1 --format=%B`.

`-s` (`--signoff`) дописує в trailer-блок рядок `Signed-off-by: Ім'я <email>` з `user.name` і `user.email` вашого git. Це текстова позначка, що автор погоджується з умовами внеску проєкту (DCO). Якщо проєкт вимагає `trailer-required: [Signed-off-by]`, без `-s` push не пройде.

`-S` (`--gpg-sign`, велика літера) - інша річ. Це криптографічний підпис коміту ключем GPG або SSH. COMMITPOLICY перевіряє лише текст повідомлення, підпис коміту вона не перевіряє і не вимагає. Підпис тегів перевіряє TAGPOLICY ([tagpolicy.md](tagpolicy.md)).

### Як виправити коміт після відмови

Hook відхиляє весь push, тому відхилені коміти є лише у вас локально. Виправте повідомлення і зробіть push ще раз:
- останній коміт: `git commit --amend` (з `-s`, якщо бракує `Signed-off-by`)
- кілька комітів: `git rebase -i origin/main`, у списку замініть `pick` на `reword` біля потрібних комітів і перепишіть повідомлення

Якщо `rebase -i` переписав і коміти, які вже були на сервері раніше, push потрібен з `--force-with-lease`.

Перевірити повідомлення ще до push можна локально, див. [Локальна перевірка](#локальна-перевірка).

## Як COMMITPOLICY це перевіряє

Файл складається з rulesets. Ruleset - це набір правил і гілки, для яких він діє. Наприклад, суворі правила для `main` і релізних гілок, а feature-гілки без перевірки. Push проходить, лише коли кожен ruleset, що діє для гілки, не знайшов помилок.

У команд різні стандарти: Conventional Commits зі scope і без, обов'язковий issue або ні, проза чи списки в body, вільний формат. Тому правила - це каталог незалежних перевірок, а не одна схема. Кожне правило має рівень, значення і необов'язковий фільтр за type. Правило, якого немає у файлі, не перевіряється, вбудованих правил за замовчуванням немає.

Хто може змінювати сам COMMITPOLICY, задає правило PATHPOLICY ([pathpolicy.md](pathpolicy.md)). Без такого правила файл змінює будь-хто з правом push.

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

Для гілки діють усі rulesets, чиї `refs` її охоплюють. Порушення будь-якого з них - відмова.

### Для яких гілок діє ruleset (`refs`)

- `all` - усі гілки
- `default` - лише default branch проєкту, зазвичай `main`
- `protected` - default branch і гілки, які проєкт захищає в GitLab (Settings > Repository > Protected branches)
- список - гілки і теги за повним іменем git

Повне ім'я - це те, як git називає гілку чи тег усередині репозиторію: гілка `main` - це `refs/heads/main`, гілка `release/1.2` - `refs/heads/release/1.2`, тег `v1.0.0` - `refs/tags/v1.0.0`. У списку потрібне саме повне ім'я. На коротке `main` команда `validate` відповідає помилкою `refs: main is not a ref pattern such as refs/heads/release/*`.

`*` у списку заміняє будь-які символи, крім `/`. `refs/heads/release/*` охоплює `refs/heads/release/1.2`, але не `refs/heads/release/1.2/hotfix`. `refs/heads/*` охоплює `refs/heads/main`, але не `refs/heads/feature/x`.

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB, до 50 rulesets і до 50 username в `except.users` одного ruleset.

## Формати

Формат задає, як hook розбирає header.

**`conventional`** (за замовчуванням) - header Conventional Commits, як у розділі [З чого складається повідомлення коміту](#з-чого-складається-повідомлення-коміту). Схема `type(scope)!: subject`, scope і `!` необов'язкові. Type - латинські літери, цифри, `_` і `-`.

**`free`** - вільний header, весь перший рядок вважається subject. Правила про type і scope для такого ruleset недоступні. Формат для команд, що пишуть, наприклад, `PAY-123 Round refunds to cents`.

Ці перевірки діють завжди, окремого правила для них немає:
- `conventional`: header за схемою вище і порожній рядок між header і body
- `free`: непорожній header і порожній рядок перед body
- повідомлення з NUL-байтом або об'єкт коміту, більший за 1 MiB, - порушення

Як hook читає повідомлення:
- `\r\n` заміняється на `\n` (UI GitLab пише `\r\n`), кінцеві порожні рядки відкидаються (git завжди додає `\n`)
- trailer-блок - останній абзац, у якому всі рядки мають вигляд `Key: value` або `Key #value`, як у `git interpret-trailers`. `BREAKING CHANGE:` теж trailer. Порядок trailers не важливий. Якщо хоч один рядок абзацу не trailer, увесь абзац - частина body
- body за формою не обмежується: списки, абзаци, код, довгі описи допустимі, поки не задано `body-max-line-length`

## Каталог правил

Кожне правило записується як `{ level, value, types }`:
- `level` - `error` (відмова), `warn` (лише попередження) або `off` (вимкнено)
- `value` - значення правила. У `body-required` його немає
- `types` - необов'язковий список type. Правило перевіряється лише для комітів цих type. Наприклад, `body-required` з `types: [feat, fix]` вимагає body лише в `feat` і `fix`

Приклади в останній колонці не пройдуть перевірку з указаним значенням.

| Правило | `value` | Формат | Що перевіряє | Не пройде |
| --- | --- | --- | --- | --- |
| `type-enum` | список | conventional | type зі списку | `bugfix: round refunds` при `[feat, fix]` |
| `scope` | `optional`, `required`, `forbidden` | conventional | чи є scope | `fix: round refunds` при `required` |
| `scope-enum` | список | conventional | scope зі списку. Scope `a, b` або `a/b` ділиться на частини, кожна має бути у списку | `fix(ui): round refunds` при `[api, billing]` |
| `breaking-bang` | `allow`, `forbid` | conventional | чи можна `!` | `feat!: drop v1` при `forbid` |
| `header-max-length` | число | будь-який | довжина header у символах Unicode | header зі 101 символу при `100` |
| `subject-case` | `lower-first`, `sentence`, `any` | будь-який | перша літера subject: мала, велика або будь-яка | `fix: Round refunds` при `lower-first` |
| `subject-full-stop` | `forbid`, `require` | будь-який | крапка в кінці subject | `fix: round refunds.` при `forbid` |
| `body-required` | немає | будь-який | body є | повідомлення з одного header |
| `body-max-line-length` | число | будь-який | довжина кожного рядка body. Рядки з URL і код (огороджений блок або рядки з відступом) не враховуються | рядок body зі 101 символу при `100` |
| `reference` | `location`, `pattern`, `keys` | будь-який | посилання на issue в указаному місці | повідомлення без `#665` |
| `trailer-required` | список ключів | будь-який | у trailer-блоці є рядки з цими ключами | коміт без `-s` при `[Signed-off-by]` |
| `pattern` | regex | будь-який | повідомлення з початку відповідає regex | див. [`pattern`](#pattern) |

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
- У default branch, а для ruleset з `refs: protected` або списком - і в цих гілках, перевіряються всі коміти, які push додає в гілку. Під час створення protected гілки - коміти, яких немає в default branch і інших protected гілках
- В інших гілках перевіряються лише нові коміти, яких ще немає в жодній гілці чи тегу репозиторію. Merge `main` у feature-гілку або rebase на свіжий `main` уже наявні коміти не перевіряє, лише сам merge-коміт (якщо не ввімкнено `except.merge_commits`) і власні нові коміти. Ruleset з `refs: all` перевіряє кожен коміт, коли той уперше з'являється в будь-якій гілці
- Merge request без squash у default branch приносить усі коміти гілки і merge-коміт. Squash merge приносить один коміт, його повідомлення задає squash commit template проєкту
- Повідомлення тегів не перевіряються. Підпис тегів перевіряє TAGPOLICY ([tagpolicy.md](tagpolicy.md))
- Історія до появи COMMITPOLICY не перевіряється. Виняток - merge у default branch гілки зі старими комітами: вони входять у default branch разом із merge і перевіряються

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

COMMITPOLICY стежить за порядком в історії, а не за доступом. Тому, коли перевірку неможливо виконати (файл у default branch зламаний, API GitLab не відповів), push проходить з попередженням, а не блокується. Це називають fail open. Те, що контролює автор push, наприклад повільне для regex повідомлення, - порушення, а не пропуск.

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

Якщо в default branch проєкту немає PATHPOLICY, COMMITPOLICY може змінити будь-хто з правом push, зокрема послабити правила. Hook приймає таку версію з попередженням `the project has no PATHPOLICY on the default branch: anyone who can push may change COMMITPOLICY`. Захистити файл можна правилом PATHPOLICY ([pathpolicy.md](pathpolicy.md#приклад)). Якщо модуль PATHPOLICY вимкнено в конфігурації, попередження немає.

Попередження успішного push видно в терміналі як рядки `remote:`. Для merge кнопкою, Web IDE та інших операцій UI GitLab успішний вивід hook не показує: там попередження видно лише в audit.

## Локальна перевірка

`gitlab-repopolicy lint-commit` використовує ту саму реалізацію, що й hook, тож локальна перевірка і сервер не розходяться. Python чи Node не потрібні. Бінарник для розробника можна встановити через `go install` ([install.md](install.md)).

| Команда | Що перевіряє |
| --- | --- |
| `gitlab-repopolicy lint-commit -m "feat: add x"` | одне повідомлення за `COMMITPOLICY` поточного каталогу |
| `gitlab-repopolicy lint-commit --range origin/main..HEAD` | коміти діапазону; одна ревізія - уся її історія. Не більше 100 000 комітів |
| `-f FILE` | інший файл COMMITPOLICY |
| `--skip-author EMAIL` | разом з `--range`: пропустити коміти цього автора; можна повторити. Пропущений коміт друкується рядком `skipped, author EMAIL` |
| `--ruleset NAME` | лише цей ruleset; можна повторити для кількох. Невідоме ім'я - помилка зі списком rulesets файлу |
| `--config FILE` | файл з `commitpolicy_presets`, якщо COMMITPOLICY використовує `extends`. Інші ключі конфігурації не потрібні ([configuration.md](configuration.md#commitpolicy_presets)) |

Без `--ruleset` `lint-commit` застосовує всі rulesets файлу і не дивиться на `refs`: локально невідомо, у яку гілку піде коміт. `except.users` локально не діє.

```text
$ gitlab-repopolicy lint-commit -f examples/conventional-commits/COMMITPOLICY -m "Fix deploy."
message: [protected] error: header: expected "type(scope): subject"
message: [protected] error: trailer-required: missing Signed-off-by in the trailer block
message: [release] error: header: expected "type(scope): subject"
```

Як `commit-msg` hook git:

```sh
#!/bin/sh
# .git/hooks/commit-msg
exec gitlab-repopolicy lint-commit --ruleset protected -m "$(git stripspace --strip-comments < "$1")"
```

`git stripspace` прибирає рядки-коментарі, які залишає редактор: git передає hook файл повідомлення ще з ними. `--ruleset protected` потрібен, коли rulesets для різних гілок суперечать один одному. У прикладі вище ruleset `release` дозволяє лише `fix`, `docs` і `chore`, і без `--ruleset` hook відхилив би `feat:`, хоча в main він дозволений. Візьміть ruleset тих гілок, куди зазвичай іде робота.

У CI merge request ті самі перевірки йдуть до merge, щоб автор побачив проблему в pipeline, а не під час merge:

```yaml
# .gitlab-ci.yml
repopolicy:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - gitlab-repopolicy validate
    - gitlab-repopolicy lint-commit --ruleset protected --range "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME..HEAD"
```

Бінарник у job - з перевіреного релізу, наприклад в образі runner'а. Для `origin/<target>` у job потрібна історія цільової гілки: `GIT_DEPTH: 0` або `git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"`. Код виходу `lint-commit` - 0, якщо порушень рівня `error` немає (попередження не заважають), 1, якщо вони є, і 2 за неправильних аргументів команди.
