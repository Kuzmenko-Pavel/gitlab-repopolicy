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
    checks:
      header-format:
        conventional: {level: error}
    message: Commits in the default branch follow Conventional Commits.
```

Тут повідомлення в основній гілці (`default branch`) має містити тип і короткий опис, наприклад `feat: add request validation`. Scope у дужках необов'язковий. Формат header перевіряє `header-format`: parser лише розбирає header на частини, а відмову дає перевірка. Файл не задає перевірок довжини чи списку типів.

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

Повідомлення має header і може містити body та trailer-блок. Header - завжди перший рядок, body починається з другого. Порожній рядок між ними - оформлення, яке перевіряє `body-leading-blank-lines`, а не умова розбору. Body та trailers необов'язкові, доки policy не вимагає їх через `checks`:

| Частина | У прикладі | Що це |
| --- | --- | --- |
| header | `feat(api): add request validation` | перший рядок. Його показують `git log --oneline`, список комітів у GitLab і заголовок merge request |
| body | `Validate incoming requests before processing.` | пояснення: що змінилося і чому |
| trailer-блок | `Refs: #123` | останній абзац після порожнього рядка, де кожен рядок має вигляд `Key: value` або `Key #value`: структуровані метадані, наприклад посилання на issue; `Signed-off-by` може бути додатковим trailer |

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
| `rules.<ім'я>.check_history` | ні | які коміти гілки перевіряти: `new-content` (за замовчуванням) або `incoming` ([нижче](#які-коміти-перевіряються)) |
| `rules.<ім'я>.parser` | так | `conventional` або `free` ([нижче](#parser)) |
| `rules.<ім'я>.except` | ні | які коміти пропустити ([нижче](#винятки-except)) |
| `rules.<ім'я>.checks` | ні | перевірки з каталогу: тип → ім'я перевірки → її поля ([нижче](#каталог-перевірок-checks)) |
| `rules.<ім'я>.message` | так | текст для автора коміту з порушенням, до 1000 символів |
| `rules.<ім'я>.help` | ні | `contact` і `docs_url`, як у [PATHPOLICY](pathpolicy.md#довідник-полів) |

"Обов'язкове" означає обов'язкове після злиття з пресетами. Будь-яке поле правила у файлі може бути `null`: так видаляються правило, тип перевірки, окрема перевірка чи поле пресета.

Діють усі правила, чий `match` збігся з гілкою. Помилка будь-якого з них - відмова. Гілка, з якою не збіглося жодне правило, не перевіряється.

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB, до 50 username в `except.users` одного правила.

### `parser`

`parser` задає, як hook розбирає header.

- `conventional` - header Conventional Commits, `type(scope)!: subject`. Scope і `!` необов'язкові. Type - латинські літери, цифри, `_` і `-`
- `free` - вільний header, весь перший рядок вважається subject. Перевірки type і scope для такого правила недоступні

Parser лише розбирає повідомлення. Без перевірки дають порушення тільки повідомлення, яке неможливо розібрати взагалі:
- порожній header
- NUL-байт у повідомленні або коміт більший за 1 MiB

Header, який не відповідає схемі `conventional`, сам відмови не дає: його показує перевірка `header-format` з її `level`. Якщо header не розібрався, його частини невідомі, тому перевірки частин header (`type-enum`, `scope`, `scope-enum`, `breaking-bang`, `subject-starts-lowercase`, `subject-trailing-dot`) для такого повідомлення не виконуються: header уже неправильний, і `header-format` про це повідомляє.

Як hook читає повідомлення:
- `\r\n` і `\r` замінюються на `\n` (UI GitLab пише `\r\n`)
- header - перший рядок, body - усе від другого рядка до trailer-блоку, без порожніх рядків по краях
- чи є порожній рядок одразу після header, `body-leading-blank-lines` дивиться на повідомлення так, як його збережено в git, ще до того, як hook відкине кінцеві порожні рядки
- trailer-блок - останній абзац перед яким стоїть порожній рядок, і всі рядки якого мають вигляд `Key: value` або `Key #value`, як у `git interpret-trailers`. Рядки з початковим пробілом або tab можуть продовжувати попередній trailer. Інший звичайний рядок робить увесь останній абзац частиною body. Рядок `Signed-off-by: ...` одразу після header без порожнього рядка - body, а не trailer

### Каталог перевірок (`checks`)

`checks` - словник словників. Ключ першого рівня - тип перевірки з каталогу, ключ другого рівня - ім'я перевірки, яке обираєте ви (`[a-z0-9-]{1,63}`). Тому в одному правилі може бути кілька перевірок одного типу, наприклад два `pattern`, і кожна має свій рівень і параметри, а спільні `match`, `parser` і `except` не повторюються. Ім'я видно у відмові поруч із типом: `pattern no-wip: ...`.

```yaml
checks:
  header-max-length:
    limit: {level: warn, value: 100}
  pattern:
    no-wip: {level: error, location: header, value: '!/^WIP/i'}
    issue-ref: {level: error, location: trailer, keys: [Refs], value: '/#[0-9]+/'}
```

Поля перевірки:

- `level` - обов'язковий після злиття з пресетами: `error` позначає порушення як помилку перевірки, `warn` - як попередження
- `value` - параметр перевірки: список, рядок або ціле число з таблиці нижче. Обов'язковий після злиття для всіх типів, крім `header-format`, у якого параметра немає, і `body-min-line-length`, де без `value` діє `1`
- `types` - необов'язковий непорожній список type, доступний лише з `parser: conventional`. Обмежує перевірку комітами цих type; без нього в підсумковому правилі такого обмеження немає. У `header-format` поля `types` немає: поки header не розібрано, type невідомий

Перевірка з пресета перевизначається за типом і ім'ям: файл задає лише поля, які змінює, решта береться з пресета. Наприклад, `header-max-length: {limit: {level: warn}}` лишає `value` пресета і робить перевірку попередженням.

Перевірки наявності ознаки - `scope`, `breaking-bang`, `subject-starts-lowercase`, `subject-trailing-dot` і `body-leading-blank-lines` - мають однакове `value`:

| `value` | Що означає |
| --- | --- |
| `optional` | ознака може бути або не бути |
| `required` | ознака обов'язкова |
| `forbidden` | ознаки не має бути |

Щоб зняти вимогу пресета, задайте тій самій перевірці `value: optional`. `null` видаляє перевірку повністю.

| Перевірка | Значення поля `value` | `parser` | Що перевіряє | Не пройде |
| --- | --- | --- | --- | --- |
| `header-format` | Поле `value` не задається | conventional | header у форматі `type(scope)!: subject`, scope у дужках непорожній | `Fix timeout`, `feat(): add login` |
| `header-max-length` | Ціле число від 1 до 10000 | будь-який | довжина header у символах | header зі 101 символу при `100` |
| `type-enum` | Непорожній список type, наприклад `[feat, fix]` | conventional | type зі списку | `bugfix: round refunds` при `[feat, fix]` |
| `scope` | `optional`, `required`, `forbidden` | conventional | scope у дужках після type | `fix: round refunds` при `required` |
| `scope-enum` | Непорожній список scope, наприклад `[api, billing]` | conventional | scope зі списку. Scope `a, b` або `a/b` ділиться на частини | `fix(ui): round refunds` при `[api, billing]` |
| `breaking-bang` | `optional`, `required`, `forbidden` | conventional | `!` після type або scope | `feat!: drop v1` при `forbidden` |
| `subject-starts-lowercase` | `optional`, `required`, `forbidden` | будь-який | subject починається з малої літери. Цифра, знак або символ без регістру - не мала літера | `fix: Round refunds` і `fix: 2fa for login` при `required`; `fix: round refunds` при `forbidden` |
| `subject-trailing-dot` | `optional`, `required`, `forbidden` | будь-який | крапка в кінці subject | `fix: round refunds.` при `forbidden` |
| `body-leading-blank-lines` | `optional`, `required`, `forbidden` | будь-який | порожній рядок одразу після header, також коли тексту body немає | `fix: round refunds` одним рядком або body одразу з другого рядка при `required` |
| `body-min-line-length` | Ціле число від 0 до 10000, за замовчуванням `1` | будь-який | довжина тексту body в символах, без trailer-блоку і порожніх рядків по краях. `0` - body необов'язковий, `1` - обов'язковий | повідомлення лише з header і `Signed-off-by` при `1` |
| `body-max-line-length` | Ціле число від 1 до 10000 | будь-який | довжина кожного рядка body. Рядки з URL і код не враховуються | рядок body зі 101 символу при `100` |
| `trailer-required` | Непорожній список ключів, наприклад `[Signed-off-by]` | будь-який | у trailer-блоці є ці ключі | коміт без `-s` при `[Signed-off-by]` |
| `pattern` | `/вираз/`, `/вираз/i`, `!/вираз/` або `!/вираз/i` | будь-який | регулярний вираз у частині повідомлення; додатково потрібне поле `location` | [нижче](#pattern) |

`body-leading-blank-lines` дивиться на повідомлення так, як його збережено в git. `git commit` за звичайного очищення відкидає кінцеві порожні рядки, тому коміт з одного header зберігається як `header\n`, навіть якщо в редакторі після нього був порожній рядок. З `required` такий коміт не пройде: додайте body або trailer, наприклад через `git commit -s`. `body-min-line-length: 0` дозволяє не писати body, але не вимикає `body-leading-blank-lines`.

Нові типи перевірок з'являються в нових версіях без зміни `version`. Невідомий тип у новій версії файлу - помилка, а в default branch (наприклад, після відкату бінарника) він пропускається з попередженням.

### `pattern`

`pattern` шукає регулярний вираз у вказаній частині повідомлення:

| Поле | Значення |
| --- | --- |
| `location` | `header`, `body`, `trailer` або `message` (усе повідомлення) |
| `value` | `/вираз/` або `/вираз/i` - має знайтися, `!/вираз/` або `!/вираз/i` - не має знайтися |
| `keys` | лише з `location: trailer`: ключі trailer, у значеннях яких шукати, без урахування регістру. Без `keys` - у всіх trailers |

Вираз у синтаксисі RE2, як у GitLab CI і в [match](match.md), і шукається будь-де в частині, як `=~`. Щоб прив'язатися до початку чи кінця, пишіть `^` і `$`. Для `body` і `message` `^` і `$` означають початок і кінець усієї частини, для рядків пишіть `(?m)`. З `location: trailer` вираз перевіряється на значенні кожного trailer окремо, і достатньо одного збігу.

Кілька вимог до тих самих комітів - кілька перевірок `pattern` з різними іменами в одному правилі ([приклад](../examples/commitpolicy/multiple-requirements.md)).

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

`match` лише вибирає гілки. Які коміти гілки перевірити, задає `check_history` правила:

| `check_history` | Які коміти |
| --- | --- |
| `new-content` (за замовчуванням) | лише нові коміти, яких ще немає в жодній гілці чи тегу |
| `incoming` | усі коміти, які push додає в гілку. Для нової гілки - коміти, яких немає в default branch |

Захист гілки в GitLab і вид `match` на цей вибір не впливають: `match: all` і список `['/./']`, що вибирає ті самі гілки, перевіряють їх однаково. Ті самі дії з обома значеннями порівнює [окремий приклад](../examples/commitpolicy/check-history.md).

- з `new-content` merge `main` у feature-гілку або rebase на свіжий `main` уже наявні коміти не перевіряє, лише сам merge-коміт і власні нові коміти. Merge у default branch гілки, яку вже запушено, теж не перевіряє її коміти: вони не нові для репозиторію
- з `incoming` merge request без squash приносить у гілку всі коміти гілки і merge-коміт, і всі вони перевіряються. Squash merge приносить один коміт, його повідомлення задає squash commit template проєкту
- повідомлення тегів не перевіряються
- історія до появи COMMITPOLICY не перевіряється. З `incoming` виняток - merge гілки зі старими комітами: вони входять у гілку разом із merge
- понад 10 000 комітів, які входять у гілку одним push, hook не перевіряє: `limit_exceeded`, в `enforce` відмова, у `warn` попередження

## Приклади

| Приклад | Що пояснює |
| --- | --- |
| [Conventional Commits](../examples/commitpolicy/conventional-commits.md) | Загальні вимоги в `main` і суворіший список типів у release-гілках |
| [Результати повідомлень](../examples/commitpolicy/message-results.md) | Коректні повідомлення, відмови й попередження на одній конфігурації |
| [Розбір header](../examples/commitpolicy/header-parsing.md) | Пробіли, scope і відокремлення body |
| [Посилання на issue, MR і Jira](../examples/commitpolicy/issue-references.md) | Хоча б одне посилання GitLab чи Jira будь-де в повідомленні Conventional Commits |
| [Ключ задачі](../examples/commitpolicy/issue-key.md) | Вільний формат замість Conventional Commits |
| [Пошук за шаблоном](../examples/commitpolicy/patterns.md) | Вибір `header`, `body`, `trailer` або `message` |
| [`new-content` і `incoming`](../examples/commitpolicy/check-history.md) | Merge і fast-forward робочої гілки в `main` з обома значеннями `check_history` |
| [Кілька вимог](../examples/commitpolicy/multiple-requirements.md) | Дві перевірки `pattern` з різними іменами в одному правилі |
| [Вимоги commitizen](../examples/commitpolicy/commitizen.md) | Перенесення конкретних вимог у серверну перевірку |
| [Пресет організації](../examples/commitpolicy/organization-preset.md) | Зміна рівня перевірки й зняття успадкованої вимоги через `value: optional` |

## Відмови

| Ситуація | Результат |
| --- | --- |
| порушення перевірки рівня `error` | відмова `commit_format_violation` |
| порушення перевірки рівня `warn` або будь-яке порушення з `mode: warn` | push проходить, порушення у виводі push і в audit |
| понад 10 000 комітів входять у гілку | `limit_exceeded`: в `enforce` відмова, у `warn` попередження |
| COMMITPOLICY у default branch невалідна, пресет зник або зламаний | перевірку пропущено, попередження |
| невідомий тип перевірки в default branch | пропущено лише цей тип, попередження |
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
remote:       - pattern issue-ref: no match of /[A-Za-z0-9_.\/-]*#[0-9]+/ in the message
remote:   Commits in main and release branches follow Conventional Commits. Reword them with git rebase -i or squash the merge request.
remote:   next time: follow the format; commits that are pushed stay as they are
```

Попередження успішного push видно в терміналі як рядки `remote:`. Для merge кнопкою, Web IDE та інших дій в UI GitLab успішний вивід hook не показує: там попередження видно лише в audit.

## Часті помилки

| Симптом | Причина | Що зробити |
| --- | --- | --- |
| `parser: required` у `validate` | `parser` обов'язковий | вкажіть `parser: conventional` або `parser: free` |
| `checks.type-enum: needs parser: conventional` | перевірка type з `parser: free` | змініть `parser` або приберіть перевірку |
| `checks.type-enum.level: expected a mapping, got a string` | перевірку записано без імені: `type-enum: {level: error, ...}` | додайте ім'я: `type-enum: {allowed: {level: error, ...}}` |
| `Fix timeout` проходить у правилі з `parser: conventional` | у правилі немає `header-format` | додайте `header-format: {conventional: {level: error}}` |
| `pattern` з `location: trailer` не бачить посилання | посилання стоїть у body, а не в останньому абзаці, або між trailers є звичайний рядок | винесіть посилання окремим останнім абзацом `Refs: #12` |
| `pattern` для `body` не знаходить рядок, що починається з `- ` | `^` означає початок усього body | пишіть `/(?m)^- /` |
| відмова для `feat:` у release-гілці, хоча в `main` проходить | у release-гілці діють обидва правила прикладу 1 | так задумано. Для локальної перевірки беріть `lint-commit --rule` |
| merge-коміт GitLab відхиляється | header `Merge branch ...` не у форматі Conventional Commits | `except.merge_commit: true` |
| `checks.header-max-length.limit.level: "off" is not one of error, warn` | значення `off` немає | щоб зняти вимогу пресета, задайте перевірці `value: optional` або видаліть її через `null`; для `body-min-line-length` - `value: 0` |

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
message: [conventional] error: header-format conventional: expected "type(scope): subject"
message: [conventional] error: body-leading-blank-lines blank-line: an empty line must follow the header
message: [conventional] error: trailer-required dco: missing Signed-off-by in the trailer block
```

Правило `release` прикладу нічого не повідомляє: у нього немає `header-format`, а `type-enum` для header, що не розібрався, не виконується.

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
