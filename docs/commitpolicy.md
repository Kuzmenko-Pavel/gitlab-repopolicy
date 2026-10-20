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

Тут кожне повідомлення в default branch має бути у форматі `type(scope): subject`. Інших вимог немає.

## Швидкий старт

1. Почніть з `mode: warn`: порушення лише показуються у виводі push і пишуться в audit, push проходить. Так видно, скільки комітів команди не відповідає правилам, ще до того, як хтось отримає відмову
2. Перевірте файл командою `gitlab-repopolicy validate` і повідомлення командою `gitlab-repopolicy lint-commit` ([нижче](#перевірка-без-push))
3. Злийте файл у default branch. Hook читає правила лише звідти
4. Коли попереджень майже не лишилося, приберіть `mode: warn`, і порушення рівня `error` почнуть давати відмову

Хто може змінювати сам COMMITPOLICY, задає правило PATHPOLICY ([pathpolicy.md](pathpolicy.md#6-хто-змінює-інші-файли-політик)). Без такого правила файл змінює будь-хто з правом push.

## З чого складається повідомлення коміту

```text
fix(billing): round refunds to cents

Refunds were computed in floating point and lost a cent on some amounts.
Amounts are now kept in integer cents.

Refs: #665
Signed-off-by: Olena Ivanenko <olena@example.com>
```

Три частини, між ними порожній рядок:

| Частина | У прикладі | Що це |
| --- | --- | --- |
| header | `fix(billing): round refunds to cents` | перший рядок. Його показують `git log --oneline`, список комітів у GitLab і заголовок merge request |
| body | два рядки про refunds | пояснення: що змінилося і чому |
| trailer-блок | `Refs: #665` і `Signed-off-by: ...` | останній абзац, де кожен рядок має вигляд `Key: value` або `Key #value`: посилання на issue, автор за DCO |

Header у форматі [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

| Частина | У прикладі | Що це |
| --- | --- | --- |
| type | `fix` | вид зміни: `feat` - нова можливість, `fix` - виправлення, `docs` - документація |
| scope | `billing` | необов'язково, у дужках: частина системи, якої стосується зміна |
| `!` | немає | необов'язково, перед двокрапкою: зміна ламає сумісність |
| `: ` | `: ` | двокрапка і рівно один пробіл |
| subject | `round refunds to cents` | короткий опис зміни, непорожній |

Як написати таке повідомлення. Кожен `-m` - окремий абзац:

```sh
git commit -s \
  -m "fix(billing): round refunds to cents" \
  -m "Refunds were computed in floating point and lost a cent on some amounts." \
  -m "Refs: #665"
```

`-s` (`--signoff`) дописує `Signed-off-by: Ім'я <email>` з `user.name` і `user.email`. `-S` (велика літера) - інша річ, криптографічний підпис коміту. COMMITPOLICY перевіряє лише текст повідомлення, підпис коміту вона не перевіряє. Підпис тегів перевіряє TAGPOLICY ([tagpolicy.md](tagpolicy.md)).

Як виправити коміт після відмови. Hook відхиляє весь push, тож відхилені коміти є лише у вас локально:
- останній коміт: `git commit --amend` (з `-s`, якщо бракує `Signed-off-by`)
- кілька комітів: `git rebase -i origin/main`, біля потрібних комітів замініть `pick` на `reword`

Якщо `rebase -i` переписав коміти, які вже були на сервері, push потрібен з `--force-with-lease`.

## Довідник полів

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `version` | так | `1` |
| `mode` | ні | `enforce` (за замовчуванням) або `warn`. `warn` робить з усіх помилок попередження |
| `extends` | ні | ім'я пресета серверного конфігу або список імен ([configuration.md](configuration.md#presets)) |
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
- `\r\n` замінюється на `\n` (UI GitLab пише `\r\n`), кінцеві порожні рядки відкидаються
- trailer-блок - останній абзац, у якому всі рядки мають вигляд `Key: value` або `Key #value`, як у `git interpret-trailers`. Якщо хоч один рядок абзацу не trailer, весь абзац - частина body

### Каталог перевірок (`checks`)

Кожна перевірка записується як `{ level, value, types }`:
- `level` - `error` (відмова) або `warn` (лише попередження). Вимкнути перевірку з пресета можна лише через `null`
- `value` - значення перевірки. У `body-required` його немає
- `types` - необов'язковий список type: перевірка діє лише для комітів цих type. Лише з `parser: conventional`

| Перевірка | `value` | `parser` | Що перевіряє | Не пройде |
| --- | --- | --- | --- | --- |
| `type-enum` | список | conventional | type зі списку | `bugfix: round refunds` при `[feat, fix]` |
| `scope` | `optional`, `required`, `forbidden` | conventional | чи є scope | `fix: round refunds` при `required` |
| `scope-enum` | список | conventional | scope зі списку. Scope `a, b` або `a/b` ділиться на частини | `fix(ui): round refunds` при `[api, billing]` |
| `breaking-bang` | `allow`, `forbid` | conventional | чи можна `!` | `feat!: drop v1` при `forbid` |
| `header-max-length` | число | будь-який | довжина header у символах | header зі 101 символу при `100` |
| `subject-case` | `lower-first`, `sentence`, `any` | будь-який | перша літера subject | `fix: Round refunds` при `lower-first` |
| `subject-full-stop` | `forbid`, `require` | будь-який | крапка в кінці subject | `fix: round refunds.` при `forbid` |
| `body-required` | немає | будь-який | body є | повідомлення з одного header |
| `body-max-line-length` | число | будь-який | довжина кожного рядка body. Рядки з URL і код не враховуються | рядок body зі 101 символу при `100` |
| `trailer-required` | список ключів | будь-який | у trailer-блоці є ці ключі | коміт без `-s` при `[Signed-off-by]` |
| `pattern` | `location`, `value`, `keys` | будь-який | регулярний вираз у частині повідомлення | [нижче](#pattern) |

Числа в `value` - від 1 до 10000. Нові перевірки з'являються в нових версіях без зміни `version`. Невідома перевірка в новій версії файлу - помилка, а в default branch (наприклад, після відкату бінарника) вона пропускається з попередженням.

### `pattern`

`pattern` шукає регулярний вираз у вказаній частині повідомлення:

| Поле | Значення |
| --- | --- |
| `location` | `header`, `body`, `trailer` або `message` (усе повідомлення) |
| `value` | `/вираз/` або `/вираз/i` - має знайтися, `!/вираз/` або `!/вираз/i` - не має знайтися |
| `keys` | лише з `location: trailer`: ключі trailer, у значеннях яких шукати, без урахування регістру. Без `keys` - у всіх trailers |

Вираз у синтаксисі RE2, як у GitLab CI і в [match](match.md), і шукається будь-де в частині, як `=~`. Щоб прив'язатися до початку чи кінця, пишіть `^` і `$`. Для `body` і `message` `^` і `$` означають початок і кінець усієї частини, для рядків пишіть `(?m)`. З `location: trailer` вираз перевіряється на значенні кожного trailer окремо, і достатньо одного збігу.

В одному правилі один `pattern`. Кілька вимог - кілька правил з тим самим `match` ([приклад 6](#6-кілька-вимог-до-повідомлення)).

### Винятки (`except`)

| Ключ | Що пропускається |
| --- | --- |
| `merge_commit: true` | сам merge-коміт |
| `merged_commits: true` | коміти, які merge-коміт приносить через другого батька. Перевіряється лише ланцюжок first-parent. Fast-forward, rebase і squash перевіряються як зазвичай |
| `git_reverts: true` | повідомлення `git revert` і кнопки revert у GitLab: header `Revert "..."` і рядок `This reverts commit <sha>` |
| `users: [...]` | push цих користувачів, наприклад бота релізних комітів |

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

### 1. Conventional Commits у main і release-гілках

Задача: у `main` і `release/*` повідомлення у форматі Conventional Commits, з `Signed-off-by` і body для `feat` і `fix`. У release-гілках лише `fix`, `docs` і `chore`. Інші гілки не перевіряються, там пишуть як завгодно.

```yaml
# examples/conventional-commits/COMMITPOLICY
# Conventional Commits in main and release branches; a stricter type list in
# release branches. Other branches are not checked.
version: 1
mode: enforce

rules:
  conventional:
    match: [main, '/^release\//']
    parser: conventional
    except:
      merge_commit: true
      git_reverts: true
      users: [release-bot]
    checks:
      type-enum:         { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test] }
      scope:             { level: error, value: optional }
      breaking-bang:     { level: error, value: allow }
      header-max-length: { level: warn,  value: 100 }
      subject-case:      { level: error, value: lower-first }
      subject-full-stop: { level: error, value: forbid }
      body-required:     { level: error, types: [feat, fix] }
      pattern:           { level: warn,  location: message, value: '/[A-Za-z0-9_.\/-]*#[0-9]+/', types: [feat, fix] }
      trailer-required:  { level: error, value: [Signed-off-by] }
    message: >-
      Commits in main and release branches follow Conventional Commits.
      Reword them with git rebase -i or squash the merge request.

  release:
    match: ['/^release\//']
    parser: conventional
    checks:
      type-enum: { level: error, value: [fix, docs, chore] }
    message: Release branches take only fix, docs and chore commits.
```

У `release/1` діють обидва правила: повідомлення має пройти `conventional` і `release`.

### 2. Які повідомлення проходять

Для правила `conventional` з прикладу 1 у `main`. `\n` у таблиці - перенесення рядка.

<!-- check: lint-commit examples/conventional-commits/COMMITPOLICY conventional -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `fix(billing): round refunds to cents\n\nRefunds lost a cent.\n\nRefs: #665\nSigned-off-by: A <a@example.com>` | проходить | усе на місці |
| `docs: fix a typo\n\nSigned-off-by: A <a@example.com>` | проходить | `body-required` і `pattern` діють лише для `feat` і `fix` |
| `feat(api)!: drop v1\n\nV1 is gone.\n\nRefs: #2\nSigned-off-by: A <a@example.com>` | проходить | `!` дозволено (`breaking-bang: allow`) |
| `bugfix: round refunds\n\nSigned-off-by: A <a@example.com>` | відмова | `type-enum`: `bugfix` немає в списку |
| `fix: Round refunds\n\nBody.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | відмова | `subject-case`: subject з великої літери |
| `fix: round refunds.\n\nBody.\n\nRefs: #1\nSigned-off-by: A <a@example.com>` | відмова | `subject-full-stop`: крапка в кінці |
| `fix: round refunds\n\nSigned-off-by: A <a@example.com>` | відмова | `body-required`: у `fix` немає body |
| `fix: round refunds\n\nBody without a reference.\n\nSigned-off-by: A <a@example.com>` | попередження | `pattern` рівня `warn`: немає посилання на issue |
| `docs: fix a typo` | відмова | `trailer-required`: немає `Signed-off-by` |
| `Revert "feat: x"\n\nThis reverts commit 0123456789abcdef0123456789abcdef01234567.` | проходить | `except.git_reverts` |

Відмова у виводі push:

```text
remote: COMMITPOLICY rule "conventional", branch main: 1 commit message does not follow the format
remote:   required: type(scope): subject, for example feat(api): add refunds
remote:   types: feat, fix, build, chore, ci, docs, perf, refactor, revert, style, test
remote:   commits:
remote:     60a6b73c3 "Fix deploy."
remote:       - header: expected "type(scope): subject"
remote:       - trailer-required: missing Signed-off-by in the trailer block
remote:   Commits in main and release branches follow Conventional Commits. Reword them with git rebase -i or squash the merge request.
remote:   how to fix: reword the commit (git commit --amend for the last one, git rebase -i for an older one), then push again
```

### 3. Header, який `parser: conventional` не розбирає

Такі помилки є завжди, з будь-якими `checks` чи без них: правило з `parser: conventional` вимагає header у цьому форматі.

<!-- check: lint-commit parser=conventional -->
| Header | Помилка `lint-commit` |
| --- | --- |
| `fix round refunds` | `header: expected "type(scope): subject"` |
| `fix:round refunds` | `header: expected "type(scope): subject", a space must follow ":"` |
| `fix:  round refunds` | `header: expected "type(scope): subject", exactly one space must follow ":"` |
| `fix(billing: round refunds` | `header: expected "type(scope): subject", the scope parenthesis is not closed` |
| `fix(): round refunds` | `header: the scope in parentheses is empty` |
| `fix:` | `header: expected "type(scope): subject", the subject is empty` |
| `fix (billing): round refunds` | `header: expected "type(scope): subject"` |
| `[fix] round refunds` | `header: expected "type(scope): subject"` |
| `fix: round\nbody right after` | `the body must be separated from the header by an empty line` |

Підказка після коми з'являється, коли hook розуміє, що саме не так. Без підказки header зовсім не схожий на `type(scope): subject`: пробіл перед дужкою, квадратні дужки, ключ задачі замість type.

### 4. Вільний header з ключем задачі

Задача: команда пише header як `PAY-123 Round refunds to cents`. Формат Conventional Commits не потрібен, але ключ задачі на початку обов'язковий.

```yaml
# examples/free-format/COMMITPOLICY
# A free-form header that starts with an issue key, on the default branch only.
version: 1

rules:
  issue-key:
    match: default
    parser: free
    except:
      merge_commit: true
    checks:
      header-max-length: { level: error, value: 72 }
      subject-full-stop: { level: error, value: forbid }
      pattern:           { level: error, location: header, value: '/^[A-Z][A-Z0-9]+-[0-9]+ \S/' }
    message: Start the header with the issue key, for example PAY-123.
```

<!-- check: lint-commit examples/free-format/COMMITPOLICY -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `PAY-123 Round refunds to cents` | проходить | ключ задачі на початку |
| `Round refunds to cents` | відмова | `pattern`: немає ключа на початку header |
| `pay-123 round refunds` | відмова | `pattern`: ключ задачі великими літерами |
| `PAY-123 Round refunds to cents.` | відмова | `subject-full-stop`: з `parser: free` subject - це весь header |

### 5. `pattern` у кожній частині повідомлення

Кожен рядок - окреме правило з `parser: free` і одним `pattern`.

<!-- check: pattern -->
| `location` | `value` | `keys` | Повідомлення | Результат | Чому |
| --- | --- | --- | --- | --- | --- |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | | `PAY-12 Round refunds` | проходить | ключ задачі на початку header |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | | `Round refunds PAY-12` | відмова | ключ є, але не на початку, а вираз має `^` |
| `header` | `!/^(WIP\|fixup!)/i` | | `wip: try` | відмова | `!` вимагає, щоб вираз не знайшовся, а `i` не зважає на регістр |
| `header` | `!/^(WIP\|fixup!)/i` | | `Fix login` | проходить | виразу немає |
| `body` | `/(?m)^- /` | | `Fix login\n\n- check the token` | проходить | `(?m)` робить `^` початком кожного рядка body |
| `body` | `/(?m)^- /` | | `Fix login\n\nChecks the token.` | відмова | у body немає рядка-списку |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nRefs: payments/api#12` | проходить | значення trailer `Refs` збігається |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nCloses: #12` | відмова | trailer є, але з іншим ключем |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nSee #12 in the tracker` | відмова | останній абзац - звичайний текст, а не trailer |
| `message` | `/#[0-9]+/` | | `Fix login (#12)` | проходить | `message` - усе повідомлення, header теж |
| `message` | `!/password/i` | | `Fix login\n\nThe Password is hunter2.` | відмова | вираз знайшовся в body |

### 6. Кілька вимог до повідомлення

Задача: у default branch немає WIP-комітів, і в кожному коміті є посилання на issue в trailer. Це два `pattern`, тож два правила з тим самим `match`. Діють обидва.

```yaml
# examples/commit-patterns/COMMITPOLICY
# Two requirements for the commits of the default branch, one rule each: no
# work-in-progress headers, and a reference to an issue in a trailer.
version: 1

rules:
  no-wip:
    match: default
    parser: free
    checks:
      pattern: { level: error, location: header, value: '!/^(WIP|fixup!|squash!)/i' }
    message: Squash or reword work-in-progress commits before the merge.
  issue-ref:
    match: default
    parser: free
    except:
      merge_commit: true
    checks:
      pattern: { level: error, location: trailer, keys: [Refs, Closes], value: '/^[A-Za-z0-9_.\/-]*#[0-9]+$/' }
    message: 'End the message with a trailer such as "Refs: payments/api#123".'
```

<!-- check: lint-commit examples/commit-patterns/COMMITPOLICY -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `Fix login\n\nRefs: payments/api#12` | проходить | обидва правила виконані |
| `Fix login\n\nCloses: #12` | проходить | `Closes` теж є в `keys` |
| `Fix login\n\nRefs: 12` | відмова | `issue-ref`: значення без `#` |
| `fixup! Fix login\n\nRefs: #12` | відмова | `no-wip`: header починається з `fixup!` |

### 7. Схема commitizen

Задача: проєкт перевіряв повідомлення схемою commitizen: type зі списку, header до 100 символів, trailer `ref` з issue GitLab. У COMMITPOLICY схема розкладається на три частини: `parser: conventional` розбирає header, `checks` перевіряють список і довжину, `pattern` перевіряє trailer.

```yaml
# examples/commitizen/COMMITPOLICY
# The commitizen schema of a project: the type from a list, a header of up to
# 100 characters and a trailer ref, refs or issue with a GitLab issue. The
# parser takes the header apart, the checks cover the list and the length,
# the pattern covers the trailer.
version: 1

rules:
  commitizen:
    match: all
    parser: conventional
    except:
      merge_commit: true
    checks:
      type-enum:         { level: error, value: [build, ci, docs, feat, fix, perf, refactor, style, test, chore, revert, bump] }
      header-max-length: { level: error, value: 100 }
      pattern:           { level: error, location: trailer, keys: [ref, refs, issue], value: '/^[a-zA-Z0-9_.\/-]*#\d+$/' }
    message: 'Commits follow the commitizen schema: type: subject, then a trailer such as "ref: payments/api#123".'
```

<!-- check: lint-commit examples/commitizen/COMMITPOLICY -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `feat: add x\n\nref: group/project#1` | проходить | type зі списку, trailer `ref` з issue |
| `fix: x\n\nref: group/my.project#1\nSigned-off-by: A <a@example.com>` | проходить | інші trailers не заважають |
| `feat(: add x\n\nref: group/project#1` | відмова | header не розбирається |
| `feat: add x\nbody right after the header\nref: group/project#1` | відмова | немає порожнього рядка після header |
| `feat: add x` | відмова | немає trailer `ref` |

### 8. Пресет організації

Задача: правила комітів спільні для всіх проєктів і лежать у серверному конфігу. Проєкт лише попереджає про довгі header і дозволяє крапку в кінці subject.

Пресет (уривок `examples/presets.yaml`, ключ `presets.commitpolicy`):

```yaml
presets:
  commitpolicy:
    org-commits:
      rules:
        conventional:
          match: default
          parser: conventional
          except:
            merge_commit: true
            git_reverts: true
          checks:
            type-enum: { level: error, value: [feat, fix, build, chore, ci, docs, perf, refactor, test] }
            header-max-length: { level: error, value: 100 }
            subject-full-stop: { level: error, value: forbid }
          message: Commits in the default branch follow Conventional Commits.
          help:
            docs_url: https://www.conventionalcommits.org/en/v1.0.0/
```

Файл проєкту:

```yaml
# examples/org-commits/COMMITPOLICY
# The commit rules of the organization come from the preset org-commits.
# This project only warns about long headers and allows a full stop at the
# end of the subject.
version: 1
extends: org-commits

rules:
  conventional:
    checks:
      header-max-length: { level: warn }
      subject-full-stop: null
```

- `header-max-length: { level: warn }` змінює лише рівень, `value: 100` залишається з пресета: mapping зливається поле за полем
- `subject-full-stop: null` видаляє перевірку пресета

```text
$ gitlab-repopolicy validate --config presets.yaml
COMMITPOLICY: valid
  extends: org-commits
  rule conventional: preset:org-commits, file
    checks: preset:org-commits, file
    checks.header-max-length: preset:org-commits, file
    checks.type-enum: preset:org-commits
    except: preset:org-commits
    except.git_reverts: preset:org-commits
    except.merge_commit: preset:org-commits
    help: preset:org-commits
    help.docs_url: preset:org-commits
    match: preset:org-commits
    message: preset:org-commits
    parser: preset:org-commits
```

Якщо пресета з `extends` немає в серверному конфігу або він зламаний, COMMITPOLICY пропускається з попередженням: перевірка формату комітів не варта того, щоб блокувати роботу всіх проєктів.

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

Для `origin/<target>` у job потрібна історія цільової гілки: `GIT_DEPTH: 0` або `git fetch origin "$CI_MERGE_REQUEST_TARGET_BRANCH_NAME"`. Код виходу `lint-commit` - 0, якщо порушень рівня `error` немає, 1, якщо вони є, і 2 за неправильних аргументів. Детальніше про `validate` - [validate.md](validate.md), усі прапорці - [cli.md](cli.md).
