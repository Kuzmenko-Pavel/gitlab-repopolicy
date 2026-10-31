[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](pathpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](pathpolicy.en.md)

# PATHPOLICY

## Коротко

PATHPOLICY визначає, хто може змінювати певні файли репозиторію: CI-конфігурацію, deploy-скрипти, production-значення. Якщо захищений файл змінює людина, якої немає в правилі, GitLab відхиляє push. Права GitLab при цьому нікуди не зникають: PATHPOLICY лише звужує їх і ніколи не дає нових.

Файл лежить у корені репозиторію з ім'ям `PATHPOLICY` (без розширення, регістр важливий), вміст - YAML. Найменший робочий файл:

```yaml
version: 1
admins: [alice]
rules:
  ci:
    match: all
    paths: [".gitlab-ci.yml"]
    allow: [alice]
    message: CI belongs to alice.
```

## Швидкий старт

1. Створіть `PATHPOLICY` у гілці, перевірте його командою `gitlab-repopolicy validate` ([validate.md](../reference/validate.md)) і злийте в default branch. Першу PATHPOLICY додає Maintainer або Owner проєкту
2. З цього моменту hook читає правила з default branch. `.gitlab-ci.yml` у будь-якій гілці може змінити лише alice. Інші файли GitLab авторизує як зазвичай
3. Сам `PATHPOLICY` тепер змінюють лише `admins`. Інші файли політик (`COMMITPOLICY`, `TAGPOLICY`, `BRANCHPOLICY`) захистіть правилом PATHPOLICY, інакше їх змінить будь-хто з правом push ([приклад 6](../examples/pathpolicy/delegation.md))

Що побачить bob, якого немає в `allow`, коли запушить гілку зі зміненим `.gitlab-ci.yml`, - у розділі [Відмови](#відмови).

## Довідник полів

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `version` | так | лише `1` |
| `extends` | ні | ім'я пресета серверного конфігу або список імен ([configuration.md](../administration/configuration.md#presets)) |
| `admins` | так | 1-5 username. Лише вони змінюють сам PATHPOLICY. У пресетах `admins` немає, він завжди у файлі проєкту |
| `rules` | ні | правила, до 100: ім'я правила → правило. Ім'я - `[a-z0-9-]{1,63}`, його видно у відмовах і audit |
| `rules.<ім'я>.match` | так | у яких гілках діє правило: `all`, `default` або список гілок ([match.md](match.md)) |
| `rules.<ім'я>.paths` | так | 1-50 шаблонів шляхів ([нижче](#шаблони-шляхів)) |
| `rules.<ім'я>.allow` | так | 1-50 username тих, хто може змінювати ці шляхи |
| `rules.<ім'я>.message` | так | текст для того, кому відмовили, до 1000 символів |
| `rules.<ім'я>.help` | ні | `contact` (до 200 символів) і `docs_url` (адреса `http(s)://`). Відмова за правилом друкує `who can help with this rule:` і `docs of this rule:` |

"Обов'язкове" означає обов'язкове після злиття з пресетами: у файлі правило може лише змінювати окремі поля правила з пресета ([приклад 7](../examples/pathpolicy/organization-preset.md)). Будь-яке поле правила у файлі може бути `null`, тоді воно видаляється з результату злиття.

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB.

### Шаблони шляхів

Шлях у `paths` - це шаблон відносно кореня репозиторію:
- `*` - будь-які символи в межах однієї частини шляху, `/` не перетинає
- `**` - будь-яка кількість каталогів, зокрема жодного
- `?` - один символ, `[abc]` - один символ зі списку

| Шаблон | Охоплює | Не охоплює |
| --- | --- | --- |
| `Makefile` | `Makefile` | `src/Makefile`, `makefile` |
| `*.tf` | `main.tf` | `infra/main.tf` |
| `ci/**` | `ci/a.yml`, `ci/x/y.yml` | `x/ci/a.yml` |
| `helm/**/values-prod.yaml` | `helm/values-prod.yaml`, `helm/app/values-prod.yaml` | `helm/app/values-stage.yaml` |

- шаблон завжди від кореня: `terraform/prod/**` не охоплює `x/terraform/prod/a.tf`
- заперечення (`!`) і фігурних дужок `{a,b}` немає
- збіг з урахуванням регістру, так, як шлях бачить git
- без початкового `/`, без `\`, `.`, `..` і порожніх частин, до 256 байт і не більше чотирьох `**`
- перейменування - це видалення старого шляху і додавання нового, перевіряються обидва. Зміна режиму файлу, symlink і submodule - теж зміна шляху

**Діють усі правила, що збіглися.** Якщо шлях потрапив у два правила, зміну дозволено лише тому, хто є в `allow` обох. Нове правило може тільки звузити коло людей. Шлях, з яким не збіглося жодне правило, GitLab авторизує як зазвичай: PATHPOLICY - не список дозволених файлів.

### Хто вносить зміну

У `admins` і `allow` пишуть лише username людей або ботів. Груп і ролей немає навмисно: членство в групі змінюється поза review файлу політики, і людина, яку додали в групу заради іншої задачі, мовчки отримала б право. Username порівнюються без урахування регістру.

| Як зроблено push | Хто вносить зміну |
| --- | --- |
| HTTPS, Web IDE, merge та інші дії в UI і API | користувач, від імені якого йде дія |
| SSH особистим ключем | власник ключа |
| SSH deploy key | ніхто: deploy key - не людина, на захищеному шляху відмова `unknown_principal` |
| project або group access token, service account | бот, якщо його username є в правилі |
| кнопка merge, auto-merge | той, хто натиснув merge або ввімкнув auto-merge. Автор комітів не враховується |
| push із CI з `CI_JOB_TOKEN` | користувач, який запустив pipeline |

Автор і committer є метаданими коміту й не підтверджують користувача push. Підписи комітів також не використовуються для цієї авторизації: рішення стосується того, хто надсилає зміну в гілку. Для автоматизації, якій потрібен захищений шлях, підходить project access token, username бота якого вказано в правилі.

### Що вважається зміною

| Де | Що вважається зміною захищеного файлу |
| --- | --- |
| default branch, гілки, захищені в GitLab, гілки зі списку `match` правила | кожен коміт, який потрапляє в гілку цим push, і різниця вмісту гілки до і після push. Байдуже, хто зробив коміт і чи лежить він уже в іншій гілці: зміну в гілку вносить той, хто пушить або мержить |
| інші гілки (під `match: all`) | новий вміст файлу. Коміт змінює файл, лише якщо після нього вміст (разом із режимом файлу) відрізняється від усіх версій файлу в цій гілці до push і від default branch |
| теги | не перевіряються: тег можна поставити лише на коміт, який уже лежить у гілці ([tagpolicy.md](../examples/tagpolicy/tag-on-branch.md)), а цей коміт перевірено разом із гілкою |

Звідси два наслідки, які розібрано в прикладах:
- у `main` зміна, яку відкотили в тому ж push, усе одно зміна ([приклад 5](../examples/pathpolicy/change-and-revert.md))
- у звичайній гілці rebase, squash і cherry-pick комітів alice не є зміною, поки вміст файлу той самий ([приклад 4](../examples/pathpolicy/shared-branch.md))

Сам файл `PATHPOLICY` під друге правило не потрапляє: його змінюють лише `admins` у будь-якій гілці.

Якщо в гілку одним push потрапляє понад 50 000 комітів, hook їх не перевіряє і відхиляє push з `limit_exceeded`. Великий імпорт або переписування історії перевіряйте окремо й за потреби розбивайте на частини.

### Де файл має ефект

Правильне `match` залежить від того, звідки GitLab читає файл:

| Файли | Де GitLab їх читає | `match` |
| --- | --- | --- |
| `.gitlab-ci.yml`, `.gitlab/ci/**`, скрипти, які викликає CI | pipeline будь-якої гілки виконує CI-конфігурацію саме цієї гілки, з runner'ами і змінними проєкту | `all` |
| конфіг агентів `.gitlab/agents/**`, `CODEOWNERS`, файли політик | лише з default branch | `default` |
| deploy-значення (`values-prod.yaml`) | з гілок, з яких іде деплой | список гілок, наприклад `[main, '/^release\//']` |

## Приклади

| Приклад | Що пояснює |
| --- | --- |
| [Захист CI/CD](../examples/pathpolicy/ci-configuration.md) | CI та deploy-скрипти змінюють alice і sre-lead у будь-якій гілці |
| [Конфігурація GitLab Agent](../examples/pathpolicy/agent-configuration.md) | Будь-хто пропонує зміну; лише alice приймає її в основну гілку |
| [Production-конфігурація](../examples/pathpolicy/production-values.md) | Правила для `main` і release-гілок |
| [Спільна гілка](../examples/pathpolicy/shared-branch.md) | Rebase, squash, cherry-pick і межа винятку для відомого вмісту |
| [Зміна та відкат](../examples/pathpolicy/change-and-revert.md) | Чому порожній кінцевий diff не скасовує перевірку комітів |
| [Делегування політик](../examples/pathpolicy/delegation.md) | Окремі власники COMMITPOLICY і TAGPOLICY |
| [Пресет організації](../examples/pathpolicy/organization-preset.md) | Злиття правил, заміна списків і видалення через `null` |
| [Відновлення доступу](../examples/pathpolicy/recovery.md) | Owner призначає admins, коли API підтвердив відсутність робочих |

## Перша PATHPOLICY і нові версії

**Першу PATHPOLICY** в default branch додає користувач з роллю Maintainer або вище. Зловмисний перший файл - незручність, а не ескалація: PATHPOLICY лише звужує права, а Owner може зняти членство того, хто захопив файл, і спрацює recovery.

**Нову версію будь-якого файлу політики** hook перевіряє під час push у будь-яку гілку, не лише в `main`. Автор бачить помилку одразу, а не під час merge. Помилка дає відмову `invalid_policy_update`.

| Перевірка | PATHPOLICY | COMMITPOLICY | TAGPOLICY | BRANCHPOLICY |
| --- | --- | --- | --- | --- |
| звичайний файл (не каталог, не symlink, не submodule), розмір у межах ліміту | так | так | так | так |
| `version`, схема, невідомі ключі, ліміти, `match`, злиття з пресетами | так | так | так | так |
| додані в цій версії username існують і активні | так | `except.users` | ні | ні |
| ключі підпису | ні | ні | так | ні |
| є хоча б один робочий admin, інакше `lockout_prevented` | так | ні | ні | ні |
| файл не захищено правилом PATHPOLICY | ні | попередження | попередження | попередження |

Username, які вже були у версії з default branch, повторно не перевіряються. Тих, хто зник пізніше, знаходить `settings-check` ([operations.md](../administration/operations.md#settings-check)).

**Невалідна PATHPOLICY у default branch** (так буває лише в обхід hook, через break-glass або `mode: audit`) блокує будь-яку зміну вмісту гілок і тегів проєкту, бо правила невідомі. Проходять видалення гілок і тегів, break-glass і Owner, який виправляє або видаляє лише PATHPOLICY.

**Видалення файлу політики** - повернення до стану без нього. PATHPOLICY видаляє admin (під час recovery Owner), інші файли - той, кому дозволяє правило PATHPOLICY. Якщо видалити PATHPOLICY і залишити інші файли, їх зможе змінювати будь-хто з правом push.

## Відмови

Так виглядає відмова, коли bob змінив `.gitlab-ci.yml` у гілці `feature` ([приклад 1](../examples/pathpolicy/ci-configuration.md)):

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

`remote:` git додає до кожного рядка від сервера.

| Рядок | Що означає |
| --- | --- |
| `push rejected by the rules of this repository (1 problem)` | push не прийнято цілком, жодна гілка і жоден тег з нього не змінилися |
| `PATHPOLICY rule "ci-config", branch feature: ...` | файл правил, правило, гілка і що не так |
| `may change them:` | хто може внести цю зміну |
| `files:` | які файли з push захищені цим правилом |
| текст англійською | `message` правила |
| `the content of ... differs ...` | у звичайній гілці: який коміт приніс новий вміст. У `main` і гілках зі списку тут порада, кого попросити |
| `who can help with this rule:`, `docs of this rule:` | `help` правила |
| `rules: PATHPOLICY in main (f50c5a0)` | правила взято з `main`, з коміту `f50c5a0`. Вирішує ця версія, а не та, що у вашій гілці |
| `checked as: bob (SSH)` | кого перевіряв hook і як прийшов push. Якщо тут не ви, push зроблено від чужого імені, наприклад з CI job, який запустив інший користувач |
| `who can help:` | є лише у відмовах, які ви не виправите самі: збій API GitLab, налаштування сервера, зламаний файл у `main` |
| `GL-HOOK-ERR: ...` | підсумок одним рядком. Merge request, Web IDE і API GitLab показують лише його |

Що робити далі:
- зміна справді потрібна - попросіть когось із `may change them` внести її
- правило здається хибним - зверніться до `admins` PATHPOLICY (`gitlab-repopolicy admins` або сам файл у `main`)
- зачепили захищений файл випадково - поверніть його з `main` (`git restore --source=origin/main -- .gitlab-ci.yml`, потім `git commit --amend`) і запуште ще раз

Зміну самого PATHPOLICY не admin'ом:

```text
remote: PATHPOLICY, branch feature: only PATHPOLICY admins may change PATHPOLICY
remote:   admins: alice, sre-lead
remote:   files:
remote:     PATHPOLICY
remote:   ask one of them to make this change; this applies to every branch, including yours
```

| Код | Заголовок у виводі | Коли |
| --- | --- | --- |
| `protected_path_violation` | you may not change these files | шлях із правила змінює той, кого немає в `allow` |
| `policy_admin_violation` | only PATHPOLICY admins may change PATHPOLICY | PATHPOLICY змінює не admin |
| `lockout_prevented` | the changed PATHPOLICY would have no working admin | у новій версії немає робочого admin |
| `invalid_policy` | PATHPOLICY in main is broken, so no changes are accepted | PATHPOLICY у default branch невалідна |
| `invalid_policy_update` | the changed PATHPOLICY is invalid | нова версія файлу політики не пройшла перевірку |
| `authorization_backend_failure` | could not check permissions: the GitLab API did not answer | API GitLab не відповів, токен прострочений або service account не бачить проєкт |
| `unknown_principal` | only a person may change these files | deploy key на захищеному шляху або в PATHPOLICY |
| `limit_exceeded` | the check did not finish within the server limits | ліміт часу, викликів API або понад 50 000 комітів |
| `internal_error` | internal error: the push was not checked and is rejected | внутрішня помилка, у виводі id для пошуку в audit |

PATHPOLICY стежить за доступом. Коли hook не може отримати потрібну відповідь (API не відповів, файл у `main` зламаний, внутрішня помилка), push, що зачепив захищені файли, відхиляється, а не пропускається. Push, який не зачепив жодного правила, від API не залежить і проходить.

## Часті помилки

| Симптом | Причина | Що зробити |
| --- | --- | --- |
| bob не може запушити навіть гілку з `.gitlab-ci.yml` | правило з `match: all` діє в усіх гілках | так задумано для CI. Гілку зі зміною пушить власник файлу |
| після rebase гілки відмова, хоча `ci.yml` не чіпали | гілка є в списку `match`, або вона захищена в GitLab, і там рахується кожен коміт | для CI-файлів беріть `match: all`, а не список |
| bob змінив файл у гілці, і hook пропустив | правило з `match: default` або списком діє не в цій гілці | так задумано. Зміна буде перевірена під час merge у `main` |
| відмова за зміну, яку тут же відкотили | у `main` рахується кожен коміт | приберіть обидва коміти з історії (`git rebase -i`) |
| нова PATHPOLICY відхилена з `lockout_prevented` | помилка в імені admin, або admin не Maintainer | виправте ім'я, перевірте командою `validate --online --project` |
| у відмові `checked as:` чужий username | push із CI job від імені того, хто запустив pipeline | дайте автоматизації project access token і впишіть бота в `allow` |
| `allow: [kpn]` у файлі з `extends` прибрав інших людей пресета | список у файлі замінює список пресета | перелічіть у файлі всіх, хто має право |

## Перевірка без push

| Команда | Що робить |
| --- | --- |
| `gitlab-repopolicy validate -f PATHPOLICY` | ті самі перевірки, що й під час push, без GitLab. З `--config` - злиття з пресетами і походження правил |
| `gitlab-repopolicy validate -f PATHPOLICY --online --config FILE --project P` | ще існування username і роль admins у проєкті через API |
| `gitlab-repopolicy explain --project P --user U --path X [--ref R]` | які правила збіглися зі шляхом, звідки вони і чи дозволяють зміну |
| `gitlab-repopolicy admins --project P` | admins із файлу, робочі admins і чи ввімкнено recovery |
| `gitlab-repopolicy test` | рішення hook для конкретного push без самого push ([operations.md](../administration/operations.md#перевірити-push-без-push)) |

[Посібник validate](../reference/validate.md) показує перевірку файла та пояснює її результат. Для інших задач і параметрів команд використайте [довідник CLI](../reference/cli.md).

Приклад помилки `validate`, з рядком файлу:

```text
$ gitlab-repopolicy validate -f PATHPOLICY
PATHPOLICY: invalid
  line 6: rule ci: paths: "/ci/**": patterns are relative to the repository root, remove the leading /
```

## Чого PATHPOLICY не захищає

Hook бачить лише зміни гілок і тегів. Налаштування проєкту (шлях CI-конфігурації, protected branches, default branch, job-token push) змінюють у Settings, і hook їх не бачить. Їх звіряє `settings-check` ([operations.md](../administration/operations.md#settings-check)), повний перелік обмежень - у [threat-model.md](../administration/threat-model.md).

Проєкти в особистому namespace не входять до сфери перевірки hook. Merge їхніх змін у груповий проєкт перевіряється в цьому груповому проєкті. Якщо особистий проєкт запускає deployment, цей hook не є його захистом.
