[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](commitpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](commitpolicy.en.md)

# COMMITPOLICY

## Коротко

COMMITPOLICY перевіряє, як написані повідомлення комітів, які push додає в гілку. Якщо повідомлення не відповідає правилам, GitLab відхиляє push, і коміт треба переписати. Файл лежить у корені репозиторію з ім'ям `COMMITPOLICY`, вміст - YAML. Найменший робочий файл:

```yaml
version: 1
rules:
  conventional:
    match: default
    parser: conventional
    message: Commits in the default branch follow Conventional Commits.
```

Тут повідомлення в основній гілці (`default branch`) має містити тип і короткий опис, наприклад `feat: add request validation`. Scope у дужках необов'язковий. Файл не задає додаткових перевірок довжини чи списку типів, але parser усе одно перевіряє синтаксис.

## Швидкий старт

1. Почніть з `mode: warn`: порушення лише показуються у виводі push і пишуться в audit, push проходить. Так видно, скільки комітів команди не відповідає правилам, ще до того, як хтось отримає відмову
2. Перевірте файл командою `gitlab-repopolicy validate` і повідомлення командою `gitlab-repopolicy lint-commit` ([нижче](#перевірка-без-push))
3. Злийте файл у default branch. Hook читає правила лише звідти
4. Коли попереджень майже не лишилося, приберіть `mode: warn`, і порушення рівня `error` почнуть давати відмову

Хто може змінювати сам COMMITPOLICY, задає правило PATHPOLICY ([pathpolicy.md](../examples/pathpolicy/delegation.md)). Без такого правила файл змінює будь-хто з правом push.

## З чого складається повідомлення коміту

```text
feat(api): add request validation

Validate incoming requests before processing.

Refs: #123
```

Повідомлення має header і може містити body та trailer-блок. Порожні рядки відокремлюють ці частини. Body та trailers необов'язкові, доки policy не вимагає їх через `checks`:

| Частина | У прикладі | Що це |
| --- | --- | --- |
| header | `feat(api): add request validation` | перший рядок. Його показують `git log --oneline`, список комітів у GitLab і заголовок merge request |
| body | `Validate incoming requests before processing.` | пояснення: що змінилося і чому |
| trailer-блок | `Refs: #123` | останній абзац, де кожен рядок має вигляд `Key: value` або `Key #value`: структуровані метадані, наприклад посилання на issue; `Signed-off-by` може бути додатковим trailer |

Header у форматі [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

| Частина | У прикладі | Що це |
| --- | --- | --- |
| type | `feat` | вид зміни: `feat` - нова можливість, `fix` - виправлення, `docs` - документація |
| scope | `api` | необов'язково, у дужках: частина системи, якої стосується зміна |
| `!` | У цьому повідомленні не використано | необов'язково, перед двокрапкою: зміна ламає сумісність |
| `: ` | `: ` | двокрапка і рівно один пробіл |
| subject | `add request validation` | короткий опис зміни, непорожній |

Для іншого стандарту, наприклад `PAY-123 Add request validation`, використайте `parser: free` та потрібні `pattern`. Conventional Commits задає структуру першого рядка, а список типів, довжину й обов'язкові метадані визначає policy. Коректне `feat: add validation` може бути відхилено додатковою вимогою body чи trailer. [Порівняння повідомлень](../examples/commitpolicy/message-results.md) містить перевірені коректні й некоректні варіанти.

Повідомлення з body, посиланням і DCO можна створити так. Кожен `-m` додає окремий абзац:

```sh
git commit -s \
  -m "fix(billing): round refunds to cents" \
  -m "Refunds were computed in floating point and lost a cent on some amounts." \
  -m "Refs: #665"
```

`-s` (`--signoff`) дописує `Signed-off-by: Ім'я <email>` з `user.name` і `user.email`. `-S` (велика літера) - інша річ, криптографічний підпис коміту. COMMITPOLICY перевіряє лише текст повідомлення, підпис коміту вона не перевіряє. Підпис тегів перевіряє TAGPOLICY ([tagpolicy.md](tagpolicy.md)).

Як виправити коміт після відмови. Hook не оновлює refs цього push. Виправляйте відхилені коміти у своєму клоні; вони могли вже бути доступні через інші гілки або push:
- останній коміт: `git commit --amend` (з `-s`, якщо бракує `Signed-off-by`)
- кілька комітів: `git rebase -i origin/main`, біля потрібних комітів замініть `pick` на `reword`

Якщо `rebase -i` переписав коміти, які вже були на сервері, push потрібен з `--force-with-lease`.

## Довідник полів

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `version` | так | `1` |
| `mode` | ні | `enforce` (за замовчуванням) або `warn`. `warn` робить з усіх помилок попередження |
| `extends` | ні | ім'я пресета серверного конфігу або список імен ([configuration.md](../administration/configuration.md#presets)) |
| `rules` | ні | правила, до 50: ім'я правила → правило. Ім'я - `[a-z0-9-]{1,63}` |
| `rules.<ім'я>.match` | так | у яких гілках діє правило: `all`, `default` або список гілок ([match.md](match.md)) |
| `rules.<ім'я>.parser` | так | `conventional` або `free` ([нижче](#parser)) |
| `rules.<ім'я>.except` | ні | які коміти пропустити ([нижче](#винятки-except)) |
| `rules.<ім'я>.checks` | ні | перевірки з каталогу ([нижче](#каталог-перевірок-checks)) |
| `rules.<ім'я>.message` | так | текст для автора коміту з порушенням, до 1000 символів |
| `rules.<ім'я>.help` | ні | `contact` і `docs_url`, як у [PATHPOLICY](pathpolicy.md#довідник-полів) |

"Обов'язкове" означає обов'язкове після злиття з пресетами. Будь-яке поле правила у файлі може бути `null`: так видаляються правило, перевірка чи поле пресета.

Діють усі правила, чий `match` збігся з гілкою. Помилка будь-якого з них - відмова. Гілка, з якою не збіглося жодне правило, не перевіряється.

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB, до 50 username в `except.users` одного правила.

### `parser`

`parser` задає, як hook розбирає header.

- `conventional` - header Conventional Commits, `type(scope)!: subject`. Scope і `!` необов'язкові. Type - латинські літери, цифри, `_` і `-`
- `free` - вільний header, весь перший рядок вважається subject. Перевірки type і scope для такого правила недоступні

Ці вимоги діють завжди, окремої перевірки для них немає:
- `conventional`: header розбирається за схемою вище, а між header і body порожній рядок
- `free`: header непорожній, а перед body порожній рядок
- повідомлення з NUL-байтом або коміт більший за 1 MiB - порушення

Як hook читає повідомлення:
- `\r\n` і `\r` замінюються на `\n` (UI GitLab пише `\r\n`), кінцеві порожні рядки відкидаються
- trailer-блок - останній абзац, у якому всі рядки мають вигляд `Key: value` або `Key #value`, як у `git interpret-trailers`. Рядки з початковим пробілом або tab можуть продовжувати попередній trailer. Інший звичайний рядок робить увесь останній абзац частиною body

### Каталог перевірок (`checks`)

Кожна перевірка - окремий запис у `checks` із такими полями:

- `level` - обов'язковий після злиття з пресетами: `error` позначає порушення як помилку перевірки, `warn` - як попередження
- `value` - параметр перевірки: список, рядок або ціле число з таблиці нижче. Обов'язковий після злиття для всіх перевірок, крім `body-required`: у неї поле `value` не підтримується
- `types` - необов'язковий непорожній список type, доступний лише з `parser: conventional`. Обмежує перевірку комітами цих type; без нього в підсумковому правилі такого обмеження немає

Наприклад, у цьому фрагменті правила `header-max-length` має числовий параметр, а `body-required` достатньо рівня:

```yaml
checks:
  header-max-length: {level: warn, value: 100}
  body-required: {level: error}
```

`body-required` вимагає непорожній body. Це не булевий перемикач: `body-required: false` і `body-required: {level: error, value: false}` невалідні. Якщо перевірка не потрібна, не додавайте її; успадковану з пресета видаліть через `checks.body-required: null`.

| Перевірка | Значення поля `value` | `parser` | Що перевіряє | Не пройде |
| --- | --- | --- | --- | --- |
| `type-enum` | Непорожній список type, наприклад `[feat, fix]` | conventional | type зі списку | `bugfix: round refunds` при `[feat, fix]` |
| `scope` | `optional`, `required`, `forbidden` | conventional | чи є scope | `fix: round refunds` при `required` |
| `scope-enum` | Непорожній список scope, наприклад `[api, billing]` | conventional | scope зі списку. Scope `a, b` або `a/b` ділиться на частини | `fix(ui): round refunds` при `[api, billing]` |
| `breaking-bang` | `allow`, `forbid` | conventional | чи можна `!` | `feat!: drop v1` при `forbid` |
| `header-max-length` | Ціле число від 1 до 10000 | будь-який | довжина header у символах | header зі 101 символу при `100` |
| `subject-case` | `lower-first`, `sentence`, `any` | будь-який | перша літера subject | `fix: Round refunds` при `lower-first` |
| `subject-full-stop` | `forbid`, `require` | будь-який | крапка в кінці subject | `fix: round refunds.` при `forbid` |
| `body-required` | Поле `value` не задається | будь-який | body непорожній | повідомлення з одного header |
| `body-max-line-length` | Ціле число від 1 до 10000 | будь-який | довжина кожного рядка body. Рядки з URL і код не враховуються | рядок body зі 101 символу при `100` |
| `trailer-required` | Непорожній список ключів, наприклад `[Signed-off-by]` | будь-який | у trailer-блоці є ці ключі | коміт без `-s` при `[Signed-off-by]` |
| `pattern` | `/вираз/`, `/вираз/i`, `!/вираз/` або `!/вираз/i` | будь-який | регулярний вираз у частині повідомлення; додатково потрібне поле `location` | [нижче](#pattern) |

Нові перевірки з'являються в нових версіях без зміни `version`. Невідома перевірка в новій версії файлу - помилка, а в default branch (наприклад, після відкату бінарника) вона пропускається з попередженням.

### `pattern`

`pattern` шукає регулярний вираз у вказаній частині повідомлення:

| Поле | Значення |
| --- | --- |
| `location` | `header`, `body`, `trailer` або `message` (усе повідомлення) |
| `value` | `/вираз/` або `/вираз/i` - має знайтися, `!/вираз/` або `!/вираз/i` - не має знайтися |
| `keys` | лише з `location: trailer`: ключі trailer, у значеннях яких шукати, без урахування регістру. Без `keys` - у всіх trailers |

Вираз у синтаксисі RE2, як у GitLab CI і в [match](match.md), і шукається будь-де в частині, як `=~`. Щоб прив'язатися до початку чи кінця, пишіть `^` і `$`. Для `body` і `message` `^` і `$` означають початок і кінець усієї частини, для рядків пишіть `(?m)`. З `location: trailer` вираз перевіряється на значенні кожного trailer окремо, і достатньо одного збігу.

В одному правилі один `pattern`. Кілька вимог - кілька правил з тим самим `match` ([приклад 6](../examples/commitpolicy/multiple-requirements.md)).

### Винятки (`except`)

| Ключ | Значення | Що пропускається |
| --- | --- | --- |
| `merge_commit` | `true` або `false` | за `true` - сам merge-коміт |
| `merged_commits` | `true` або `false` | за `true` - коміти, які merge-коміт приносить через другого батька. Перевіряється лише ланцюжок first-parent. Fast-forward, rebase і squash перевіряються як зазвичай |
| `git_reverts` | `true` або `false` | за `true` - повідомлення `git revert` і кнопки revert у GitLab: header `Revert "..."` і рядок `This reverts commit <sha>` |
| `users` | Список username, наприклад `[release-bot]` | push цих користувачів, наприклад бота релізних комітів |

Якщо після злиття з пресетами булевий виняток не задано, він дорівнює `false`; список `users` за замовчуванням порожній (`[]`). Пропущене поле у файлі проєкту успадковується з пресета. Щоб скасувати успадкований булевий виняток, явно задайте `false`; щоб очистити успадкований список користувачів - `users: []`.

`except.users` діє лише для людини або бота, яких hook розпізнав. Push deploy key під виняток не потрапляє.

### Які коміти перевіряються

| `match` правила | Гілка | Які коміти |
| --- | --- | --- |
| `default`, `all` | default branch | усі коміти, які push додає в гілку |
| список | гілка зі списку | коміти, яких ще немає в default branch і в інших гілках зі списку |
| `all` | інші гілки | лише нові коміти, яких ще немає в жодній гілці чи тегу |

- merge `main` у feature-гілку або rebase на свіжий `main` уже наявні коміти не перевіряє, лише сам merge-коміт і власні нові коміти
- merge request без squash у default branch приносить усі коміти гілки і merge-коміт. Squash merge приносить один коміт, його повідомлення задає squash commit template проєкту
- повідомлення тегів не перевіряються
- історія до появи COMMITPOLICY не перевіряється. Виняток - merge у default branch гілки зі старими комітами: вони входять у default branch разом із merge
- понад 10 000 комітів, які входять у гілку одним push, hook не перевіряє: `limit_exceeded`, в `enforce` відмова, у `warn` попередження

## Приклади

| Приклад | Що пояснює |
| --- | --- |
| [Conventional Commits](../examples/commitpolicy/conventional-commits.md) | Загальні вимоги в `main` і суворіший список типів у release-гілках |
| [Результати повідомлень](../examples/commitpolicy/message-results.md) | Коректні повідомлення, відмови й попередження на одній конфігурації |
| [Розбір header](../examples/commitpolicy/header-parsing.md) | Пробіли, scope і відокремлення body |
| [Ключ задачі](../examples/commitpolicy/issue-key.md) | Вільний формат замість Conventional Commits |
| [Пошук за шаблоном](../examples/commitpolicy/patterns.md) | Вибір `header`, `body`, `trailer` або `message` |
| [Кілька вимог](../examples/commitpolicy/multiple-requirements.md) | Два правила `pattern`, що діють одночасно |
| [Вимоги commitizen](../examples/commitpolicy/commitizen.md) | Перенесення конкретних вимог у серверну перевірку |
| [Пресет організації](../examples/commitpolicy/organization-preset.md) | Зміна рівня перевірки й видалення успадкованої вимоги |

## Відмови

| Ситуація | Результат |
| --- | --- |
| порушення перевірки рівня `error` | відмова `commit_format_violation` |
| порушення перевірки рівня `warn` або будь-яке порушення з `mode: warn` | push проходить, порушення у виводі push і в audit |
| понад 10 000 комітів входять у гілку | `limit_exceeded`: в `enforce` відмова, у `warn` попередження |
| COMMITPOLICY у default branch невалідна, пресет зник або зламаний | перевірку пропущено, попередження |
| невідома перевірка в default branch | пропущено лише цю перевірку, попередження |
| API не відповів, коли треба знати, хто пушить (для `except.users`) | правило пропущено, попередження |
| push змінює COMMITPOLICY на невалідну версію | відмова `invalid_policy_update` |

COMMITPOLICY стежить за порядком в історії, а не за доступом. Тому, коли перевірку неможливо виконати, push проходить з попередженням, а не блокується.

Попередження у виводі push:

```text
remote: gitlab-repopolicy: push accepted with 1 warning
remote:
remote: COMMITPOLICY rule "conventional", branch main: 1 commit message does not follow the format; this is a warning, it does not block the push
remote:   required: type(scope): subject, for example feat(api): add refunds
remote:   types: feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test
remote:   commits:
remote:     7680d8aca "fix: round refunds"
remote:       - pattern: no match of /[A-Za-z0-9_.\/-]*#[0-9]+/ in the message
remote:   Commits in main and release branches follow Conventional Commits. Reword them with git rebase -i or squash the merge request.
remote:   next time: follow the format; commits that are pushed stay as they are
```

Попередження успішного push видно в терміналі як рядки `remote:`. Для merge кнопкою, Web IDE та інших дій в UI GitLab успішний вивід hook не показує: там попередження видно лише в audit.

## Часті помилки

| Симптом | Причина | Що зробити |
| --- | --- | --- |
| `parser: required` у `validate` | `parser` обов'язковий | вкажіть `parser: conventional` або `parser: free` |
| `checks.type-enum: needs parser: conventional` | перевірка type з `parser: free` | змініть `parser` або приберіть перевірку |
| `pattern` з `location: trailer` не бачить посилання | посилання стоїть у body, а не в останньому абзаці, або між trailers є звичайний рядок | винесіть посилання окремим останнім абзацом `Refs: #12` |
| `pattern` для `body` не знаходить рядок, що починається з `- ` | `^` означає початок усього body | пишіть `/(?m)^- /` |
| відмова для `feat:` у release-гілці, хоча в `main` проходить | у release-гілці діють обидва правила прикладу 1 | так задумано. Для локальної перевірки беріть `lint-commit --rule` |
| merge-коміт GitLab відхиляється | header `Merge branch ...` не у форматі Conventional Commits | `except.merge_commit: true` |
| `checks.body-required.level: "off" is not one of error, warn` | значення `off` немає | щоб вимкнути перевірку пресета, напишіть `checks.body-required: null` |

## Перевірка без push

`gitlab-repopolicy lint-commit` перевіряє повідомлення тією самою реалізацією, що й hook.

| Команда | Що перевіряє |
| --- | --- |
| `gitlab-repopolicy lint-commit -m "feat: add x"` | одне повідомлення за `COMMITPOLICY` поточного каталогу |
| `gitlab-repopolicy lint-commit --range origin/main..HEAD` | коміти діапазону. Одна ревізія - уся її історія. Не більше 100 000 комітів |
| `-f FILE` | інший файл COMMITPOLICY |
| `--rule NAME` | лише це правило, можна повторити. Невідоме ім'я - помилка зі списком правил файлу |
| `--skip-author EMAIL` | з `--range`: пропустити коміти цього автора, можна повторити |
| `--config FILE` | серверний конфіг з пресетами, якщо файл використовує `extends`. Інші ключі не читаються |

Без `--rule` `lint-commit` застосовує всі правила файлу і не дивиться на `match`: локально невідомо, у яку гілку піде коміт. `except.users` і `except.merge_commit` для `-m` не діють.

```text
$ gitlab-repopolicy lint-commit -f examples/conventional-commits/COMMITPOLICY -m "Fix deploy."
message: [conventional] error: header: expected "type(scope): subject"
message: [conventional] error: trailer-required: missing Signed-off-by in the trailer block
message: [release] error: header: expected "type(scope): subject"
```

Як `commit-msg` hook git:

```sh
#!/bin/sh
# .git/hooks/commit-msg
exec gitlab-repopolicy lint-commit --rule conventional -m "$(git stripspace --strip-comments < "$1")"
```

`git stripspace` прибирає рядки-коментарі редактора. `--rule conventional` потрібен, коли правила для різних гілок суперечать одне одному: правило `release` з прикладу 1 дозволяє лише `fix`, `docs` і `chore`, і без `--rule` hook відхилив би `feat:`.

У CI merge request ті самі перевірки йдуть до merge:

```yaml
# .gitlab-ci.yml
repopolicy:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
  script:
    - gitlab-repopolicy validate
    - gitlab-repopolicy lint-commit --rule conventional --range "origin/$CI_MERGE_REQUEST_TARGET_BRANCH_NAME..HEAD"
```

Для `origin/<target>` у job потрібна історія цільової гілки: `GIT_DEPTH: 0` або `git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"`. Код виходу `lint-commit` - 0, якщо порушень рівня `error` немає, 1, якщо вони є, і 2 за неправильних аргументів. Сам файл політики перевіряйте за [інструкцією validate](../reference/validate.md). Параметри лінтера містить [довідник CLI](../reference/cli.md#lint-commit).
