[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](pathpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](pathpolicy.en.md)

# PATHPOLICY

PATHPOLICY відповідає на одне питання: чи може людина, яка вносить зміни в репозиторій, змінювати ці файли. Файл лежить у корені репозиторію з ім'ям `PATHPOLICY` (без розширення, регістр важливий), вміст - YAML.

PATHPOLICY - корінь довіри проєкту. Його `admins` вирішують, хто змінює сам PATHPOLICY, а звичайні path-правила вирішують, хто змінює інші policy-файли (`COMMITPOLICY`, `TAGPOLICY`).

## Як читається policy

- Hook читає всі policy-файли з **якоря**. Якір - коміт, на який вказує default branch проєкту до цього push. Push не може послабити правила для самого себе: зміни в push завжди оцінюють за правилами, які вже були в default branch
- Один якір на всі гілки й теги одного push. Гілка зі своєю, слабшою версією PATHPOLICY не авторизує сама себе
- Нову версію policy-файлу перевіряють у момент push у **будь-яку** гілку, зокрема у feature-гілку. Автор бачить помилку одразу, а не під час merge. Докладно - [приймання нової версії](#приймання-нової-версії)
- Права GitLab нікуди не зникають. Hook запускається після штатної перевірки GitLab і може лише відмовити: PATHPOLICY звужує права і ніколи не видає нових

## Приклад

```yaml
# examples/ci-config/PATHPOLICY
# The platform team owns the CI configuration, the deploy logic and the
# production values; everything else is authorized by GitLab as usual.
version: 1

admins: [platform-lead, sre-lead]

rules:
  - name: ci-config
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "Makefile"
      - "scripts/deploy/**"
    allow: [platform-lead, sre-lead, ci-maintainer]
    message: >-
      The platform team changes the CI/CD configuration and the deploy logic.
      Ask platform-lead, sre-lead or ci-maintainer to make this change.

  - name: prod-values
    paths: ["helm/**/values-prod.yaml"]
    refs: protected
    allow: [platform-lead, release-manager]
```

Правило `ci-config` діє на всіх гілках (`refs` не задано, тобто `all`), бо pipeline будь-якої гілки виконує CI-конфігурацію саме цієї гілки, з runner'ами і змінними проєкту. `refs: protected` дозволив би Developer запушити гілку зі зміненим `.gitlab-ci.yml`, і її pipeline виконав би цю зміну ще до review. Тому `message` радить попросити власників шляху, а не відкривати merge request.

Делегування інших policy-файлів їхнім власникам:

```yaml
# examples/delegation/PATHPOLICY
# PATHPOLICY stays with the platform team; the other policy files and the
# GitLab agent configuration are delegated to their owners by path rules.
version: 1

admins: [platform-lead]

rules:
  - name: commit-policy
    paths: ["COMMITPOLICY"]
    allow: [dev-lead, platform-lead]

  - name: tag-policy
    paths: ["TAGPOLICY"]
    allow: [release-lead, platform-lead]

  - name: kubernetes-agents
    paths: [".gitlab/agents/**"]
    allow: [cluster-owner]
```

## Поля

| Поле | Обов'язкове | Значення |
| --- | --- | --- |
| `version` | так | лише `1`. Схема `version: 1` заморожена: нові поля з'являться тільки у `version: 2` |
| `admins` | так | 1-5 username. Лише вони змінюють сам PATHPOLICY |
| `rules` | ні | до 100 правил |
| `rules[].name` | так | `[a-z0-9-]{1,63}`, унікальне у файлі. Видно в повідомленнях і audit |
| `rules[].paths` | так | 1-50 doublestar-патернів |
| `rules[].refs` | ні | `all` (за замовчуванням), `protected`, `default` або список гілок і тегів |
| `rules[].allow` | так | 1-50 username. Порожній список заборонений, щоб шлях не можна було випадково заморозити |
| `rules[].message` | ні | текст для того, кому відмовили, до 1000 символів. Без нього повідомлення перелічує `allow` |

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB.

## Хто вносить зміну

Того, хто вносить зміну, далі називаємо principal. У `admins` і `allow` пишуть лише username конкретних людей або ботів. Груп і ролей немає навмисно: членство в групі змінюється поза review policy-файлу, і людина, яку додали до групи заради іншої задачі, мовчки отримала б право.

Username порівнюють без урахування регістру, як у GitLab. Формат - як у username GitLab.

Кого hook вважає автором зміни:

| Як зроблено push | Хто principal |
| --- | --- |
| HTTPS, Web IDE, merge та інші операції в UI і API | користувач, від імені якого йде операція (`GL_USERNAME`). Без запитів до API |
| SSH особистим ключем | власник ключа. Hook один раз перевіряє через API, що ключ особистий |
| SSH deploy key | ніхто. Deploy key - не людина, навіть якщо його створив admin: на захищеному шляху відмова `unknown_principal` |
| project або group access token, service account | бот, якщо його явно вказано за username |
| merge кнопкою, auto-merge | той, хто натиснув merge або ввімкнув auto-merge. Автор комітів не враховується |
| push із CI з `CI_JOB_TOKEN` | користувач, який запустив pipeline |

Push із CI job hook бачить як push користувача, який запустив pipeline: інших ознак job у середовищі hook немає. Тому у відмові такого push `checked as:` показує цього користувача, а job відхиляється, якщо цього користувача немає в `allow`. Для автоматизації, якій потрібен захищений шлях, підходить project access token, бот якого вказано в правилі.

Commit author, committer і підписи комітів для авторизації не використовуються: їх легко підробити.

## Зіставлення шляхів

- Патерн - doublestar-glob, прив'язаний до кореня репозиторію. `*` - у межах одного сегмента, `**` - нуль і більше сегментів, `?`, `[abc]`
- `terraform/prod/**` не збігається з `x/terraform/prod/a.tf`. Немає заперечення (`!`), фігурних дужок `{a,b}` і правила gitignore "ім'я без `/` збігається в будь-якому каталозі"
- Збіг побайтовий і з урахуванням регістру, так, як шлях бачить git
- Патерн відносний: без початкового `/`, без `\`, без порожніх сегментів, `.` і `..`, в UTF-8, до 256 байт і не більше чотирьох `**`
- Rename і move - це видалення старого шляху і додавання нового: перевіряються обидва. Зміна режиму файлу, symlink і gitlink (submodule) - теж зміна шляху

**Мають виконуватися всі правила, що збіглися (AND).** Якщо шлях потрапив у два правила, автор зміни має бути в `allow` обох. Нове правило може лише звузити коло авторів. Вузьке правило не розширює широке: якщо `**` дозволено лише platform-lead, а `docs/**` - технічному письменнику, то `docs/a.md` може змінювати тільки той, хто є в обох списках. Перевірити, які правила збігаються, можна командою `explain` (нижче).

Шлях без правил, що збіглися, авторизує GitLab як зазвичай. PATHPOLICY - не allowlist репозиторію.

## Де діє правило (`refs`)

| `refs` | Де | Що вважається зміною |
| --- | --- | --- |
| `all` (за замовчуванням) | усі гілки й теги | у default і protected гілках - різниця вмісту гілки до і після push, в інших - нові коміти push |
| `protected` | default branch, protected branches і protected tags проєкту | різниця вмісту до і після push |
| `default` | лише default branch | різниця вмісту до і після push |
| список, наприклад `["refs/heads/release/*"]` | гілки й теги зі списку, навіть якщо в GitLab вони не захищені | різниця вмісту до і після push |

У списку `*` збігається в межах одного сегмента: `refs/heads/*` збігається з `refs/heads/main`, але не з `refs/heads/feature/x`.

Чому по-різному. У default і protected гілках рішення залежить від того, хто вносить зміну в саму гілку: merge у `release/1` коміту з чужої гілки потребує права в того, хто натиснув merge, навіть якщо коміт уже лежить в іншій гілці. Відкат файлу через force push теж є зміною. В інших гілках перевіряються лише нові коміти push: rebase feature-гілки на свіжий `main`, де platform-команда змінила CI, не перетворюється на чужу зміну, а гілка від уже перевіреного коміту нічого не змінює.

Наслідок, про який варто пам'ятати: merge у незахищену гілку не приносить нових комітів, і hook його не перевіряє. Захищайте гілки, у які йдуть merge, або задавайте їх списком у `refs`.

Ціна `all` - Developer без права не може запушити навіть гілку-пропозицію із захищеним шляхом. Робочі варіанти:
- гілку зі зміною створює власник шляху
- для шляхів, де пропозиції гілкою важливіші за суворість, - `refs: protected`. Тоді правило перевіряє merge у default і protected гілки
- merge request із fork в особистому namespace. Особисті проєкти hook не перевіряє, а merge в upstream перевіряється як зазвичай

## Admins, перша policy і recovery

**Admin** - користувач з `admins`, який активний і має в проєкті роль не нижче Maintainer (з урахуванням успадкування від груп). Admin, якого понизили до Developer, admin'ом не вважається.

**Зміну самого PATHPOLICY** (створення, правку, видалення, rename) перевіряють у всіх гілках, незалежно від правил файлу.

| Хто змінює PATHPOLICY | Effective admins за policy якоря | Результат |
| --- | --- | --- |
| admin | будь-які | дозволено |
| Owner проєкту (не admin) | є хоча б один | відмова `policy_admin_violation` |
| Owner проєкту (не admin) | жодного, і API це підтвердив | дозволено: **recovery** |
| Owner проєкту (не admin) | API не відповів | відмова `authorization_backend_failure` |
| deploy key або нерозбірний `GL_ID` | будь-які | відмова `unknown_principal`: змінити PATHPOLICY може лише людина |
| усі інші | будь-які | відмова `policy_admin_violation` |

**Перша PATHPOLICY (bootstrap).** Якщо в default branch PATHPOLICY немає, додати її туди може користувач з роллю Maintainer або вище. Зловмисний bootstrap - незручність, а не ескалація: PATHPOLICY лише звужує права, і Owner може зняти членство того, хто захопив policy, після чого спрацює recovery.

**Нова версія повинна мати admin.** Якщо жоден admin нової версії не активний або не має ролі Maintainer+, відмова `lockout_prevented`. Так ловиться помилка в імені.

**Recovery** вмикається лише тоді, коли API підтвердив, що effective admins немає: усіх видалили з проєкту, заблокували або понизили. Право отримує effective Owner проєкту, зокрема Owner батьківської групи на будь-якій глибині. Збій API recovery ніколи не вмикає. Instance administrator не є учасником проєкту і recovery не отримує: його шлях - стати Owner'ом (це видно в аудиті GitLab) або break-glass ([operations.md](operations.md)).

Recovery-push змінює тільки PATHPOLICY. Захищені шляхи в тому ж push перевіряються за старими правилами, і відмова по них підказує спершу довести нову PATHPOLICY до default branch. Owner бачить у виводі push `PATHPOLICY is changed by Owner <username> because it has no working admins. This is logged.` Кожен прийнятий recovery пише audit-подію `policy_recovery`, на неї варто поставити алерт. Відмова `policy_admin_violation` recovery не згадує: її бачать усі, хто не admin, а recovery доступний лише Owner'у і лише коли admins немає.

**Невалідна PATHPOLICY у default branch** (так буває лише в обхід hook, наприклад через break-glass або `mode: audit`) блокує будь-яку зміну вмісту гілок і тегів проєкту, бо правила невідомі. Проходять видалення гілок і тегів, break-glass і Owner, який виправляє або видаляє лише PATHPOLICY.

## Приймання нової версії

Нову версію будь-якого policy-файлу перевіряють у будь-якій гілці. Помилка дає відмову `invalid_policy_update`, якщо не сказано інше.

| Перевірка | PATHPOLICY | COMMITPOLICY | TAGPOLICY |
| --- | --- | --- | --- |
| звичайний файл (не каталог, не symlink, не submodule), розмір у межах ліміту | так | так | так |
| `version`, схема, невідомі ключі, ліміти | так | так | так |
| патерни й regex компілюються, приклади `pattern` проходять | так | так | `tags` |
| додані в цій версії username існують і активні (один GraphQL-запит) | так | `except.users` | ні |
| ключі підпису | ні | ні | так |
| є хоча б один effective admin | `lockout_prevented` | ні | ні |
| API недоступний під час перевірок | `authorization_backend_failure` | `authorization_backend_failure` | ні |
| делегований файл не покритий правилом PATHPOLICY | ні | попередження | попередження |

Username, які вже були у версії на якорі, повторно не перевіряються. Зниклих principals знаходить `settings-check` ([operations.md](operations.md)).

Видалення policy-файлу - не нова версія, а повернення до стану без policy:

| Що видаляють | Хто може | Після merge у default branch |
| --- | --- | --- |
| PATHPOLICY | admin, під час recovery Owner | правил немає; наступну PATHPOLICY додає Maintainer+ |
| COMMITPOLICY, TAGPOLICY, BRANCHPOLICY | кому дозволяє правило PATHPOLICY якоря; без правила - будь-хто з правом push | перевірок формату комітів, підпису тегів або імен гілок немає |

Якщо видалити PATHPOLICY і залишити COMMITPOLICY, TAGPOLICY або BRANCHPOLICY, їх зможе змінювати будь-хто з правом push. Hook попереджає про це під час приймання. Каталог `PATHPOLICY/` на місці файлу - теж зміна policy (видалення і невалідна нова версія).

## Відмови

| Код | Заголовок у виводі | Коли |
| --- | --- | --- |
| `protected_path_violation` | you may not change these files | шлях із правила змінює той, кого немає в `allow` |
| `policy_admin_violation` | only PATHPOLICY admins may change PATHPOLICY | PATHPOLICY змінює не admin |
| `lockout_prevented` | the changed PATHPOLICY would have no working admin | у новій версії немає effective admin |
| `invalid_policy` | PATHPOLICY in main is broken, so no changes are accepted | PATHPOLICY у default branch невалідна |
| `invalid_policy_update` | the changed PATHPOLICY is invalid | нова версія policy-файлу не пройшла приймання |
| `authorization_backend_failure` | could not check permissions: the GitLab API did not answer; this project is not connected to gitlab-repopolicy correctly | API GitLab не відповів або токен прострочений; service account не бачить проєкт або має роль нижчу за Maintainer |
| `unknown_principal` | only a person may change these files | deploy key або нерозбірний `GL_ID` на захищеному шляху або в PATHPOLICY |
| `tag_object_invalid` | the tag does not point to a commit, so its files cannot be checked | тег під правилом PATHPOLICY вказує не на коміт, а на дерево чи blob |
| `limit_exceeded` | the check did not finish within the server limits | ліміт часу, викликів API або комітів |
| `internal_error` | internal error: the push was not checked and is rejected | внутрішня помилка, у виводі id для пошуку в audit |

За невизначеності PATHPOLICY закривається (fail closed): якщо відповідь отримати не можна, push, що зачепив захищені шляхи, відхиляється. Push, який не зачепив жодного правила, від API не залежить.

Так виглядає відмова в терміналі:

```text
remote: gitlab-repopolicy: push rejected by the rules of this repository (1 problem)
remote:
remote: PATHPOLICY rule "ci-config", branch feature: you may not change these files
remote:   may change them: platform-lead, sre-lead, ci-maintainer
remote:   files:
remote:     .gitlab-ci.yml
remote:   The platform team changes the CI/CD configuration and the deploy logic. Ask platform-lead, sre-lead or ci-maintainer to make this change.
remote:   this rule applies to every branch: ask one of them to push the change
remote:
remote: rules: PATHPOLICY in main (9f1c2ab)
remote: checked as: bob (HTTPS)
remote: GL-HOOK-ERR: gitlab-repopolicy: you may not change .gitlab-ci.yml (PATHPOLICY rule ci-config; may change: platform-lead, sre-lead, ci-maintainer)
```

Одна проблема - один блок: якщо те саме порушення є в кількох гілках і тегах одного push, у заголовку блоку перелічено їх усі. `rules:` показує, з якої версії policy взято правила, `checked as:` - кого і яким способом перевіряв hook.

У merge request, Web IDE і відповідях API GitLab показує лише рядок `GL-HOOK-ERR`, тому в ньому перша проблема описана повністю, а решта перелічена заголовками. Під час merge через API GitLab відповідає лише "Branch cannot be merged", а текст відмови записує в поле `merge_error` merge request. Результати API (роль, "заблокований") у повідомлення не потрапляють ніколи, тільки в audit.

## Перевірка без push

| Команда | Що робить |
| --- | --- |
| `gitlab-repopolicy validate -f PATHPOLICY` | ті самі перевірки, що й під час приймання, без GitLab |
| `gitlab-repopolicy validate -f PATHPOLICY --online --config FILE --project P` | ще існування username і роль admins у проєкті через API. Про кожного admin, який не є активним користувачем з роллю Maintainer або вище, - попередження і підсумок `N of M admins work`. Файл при цьому валідний, код виходу 0 |
| `gitlab-repopolicy explain --project P --user U --path X [--ref R]` | які правила збіглися зі шляхом і чи виконує їх користувач. Код виходу 0 - дозволено або жодне правило не збіглося, 1 - заборонено або помилка (текст помилки в stderr), 2 - не задано `--project`, `--user` чи `--path` |
| `gitlab-repopolicy admins --project P` | admins із файлу, effective admins і чи ввімкнено recovery |

`explain` і `admins` читають PATHPOLICY з default branch проєкту через API, тобто ту версію, за якою буде оцінено наступний push.

Приклад помилки `validate`:

```text
$ gitlab-repopolicy validate -f PATHPOLICY
PATHPOLICY: invalid
  rule ci: paths: "/ci/**": patterns are relative to the repository root, remove the leading /
```

## Чого PATHPOLICY не захищає

Hook бачить лише зміни гілок і тегів. Налаштування проєкту (шлях CI-конфігурації, protected branches, default branch, job-token push) змінюють у Settings, і hook їх не бачить. Їх звіряє `settings-check` ([operations.md](operations.md)), повний перелік обмежень - у [threat-model.md](threat-model.md).

Проєкти в особистому namespace hook не перевіряє: вони не деплоять і не зберігають командний код, а зміни з них потрапляють у групові проєкти через merge, який перевіряється.
