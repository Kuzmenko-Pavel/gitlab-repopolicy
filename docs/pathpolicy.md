[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](pathpolicy.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](pathpolicy.en.md)

# PATHPOLICY

PATHPOLICY визначає, хто може змінювати певні файли репозиторію: CI-конфігурацію, deploy-скрипти, production-значення. Файл лежить у корені репозиторію з ім'ям `PATHPOLICY` (без розширення, регістр важливий), вміст - YAML. Якщо зміну захищеного файлу вносить людина, якої немає в правилі, GitLab відхиляє push.

Спочатку розберемо PATHPOLICY на прикладі і подивимося, як читати відмову. Далі - довідник полів і правил.

## Приклад

Команда платформи хоче, щоб CI-конфігурацію і deploy-логіку змінювала лише вона, а production-значення Helm - лише вона і release-manager:

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

Що тут написано:
- `admins` - люди, які можуть змінювати сам файл PATHPOLICY. Без цього списку будь-хто, кому PATHPOLICY заважає, просто прибрав би з неї своє обмеження
- `rules` - правила. Кожне правило каже, які файли (`paths`) хто може змінювати (`allow`) і в яких гілках (`refs`)
- `message` - текст, який побачить той, кому відмовили

Як це працює для різних людей. bob - Developer проєкту, його немає в жодному `allow`.

| Хто | Що змінює | Де | Результат |
| --- | --- | --- | --- |
| bob | `README.md` | будь-яка гілка | дозволено. Жодне правило не стосується цього файлу, рішення приймає GitLab, як зазвичай |
| bob | `.gitlab-ci.yml` | будь-яка гілка, навіть своя feature-гілка | відмова за правилом `ci-config` |
| ci-maintainer | `.gitlab-ci.yml` | будь-яка гілка | дозволено |
| bob | `helm/app/values-prod.yaml` | своя feature-гілка | дозволено. Правило `prod-values` діє лише в захищених гілках |
| bob | merge цієї feature-гілки в `main` | `main` | відмова за правилом `prod-values`, бо зміну в `main` вносить bob |
| release-manager | merge тієї самої гілки в `main` | `main` | дозволено |
| bob | сам файл `PATHPOLICY` | будь-яка гілка | відмова. Змінювати PATHPOLICY можуть лише `admins` |

Чому `ci-config` діє в усіх гілках, а не лише в захищених. Pipeline будь-якої гілки виконує CI-конфігурацію саме цієї гілки, з runner'ами і змінними проєкту. Якби правило діяло лише в захищених гілках, Developer запушив би гілку зі зміненим `.gitlab-ci.yml`, і її pipeline виконав би цю зміну ще до review. Тому `message` радить попросити власників файлів внести зміну, а не відкривати merge request.

Інші policy-файли (`COMMITPOLICY`, `TAGPOLICY`, `BRANCHPOLICY`) PATHPOLICY захищає так само, правилом із їхнім ім'ям. Так кожен файл отримує свого власника:

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

Без такого правила policy-файл може змінити будь-хто з правом push. Hook приймає таку версію, але попереджає про це.

## Як читати відмову

Так виглядає відмова, коли bob змінив `.gitlab-ci.yml` у гілці `feature`:

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

`remote:` git додає до кожного рядка, який надіслав сервер. Далі рядок за рядком:

| Рядок | Що означає |
| --- | --- |
| `push rejected by the rules of this repository (1 problem)` | push не прийнято цілком, жодна гілка і жоден тег з нього не змінилися. У дужках - скільки проблем знайдено |
| `PATHPOLICY rule "ci-config", branch feature: ...` | заголовок проблеми: який файл правил, яке правило, яка гілка і що не так |
| `may change them:` | хто може внести цю зміну |
| `files:` | які саме файли з вашого push захищені цим правилом |
| текст англійською | `message` правила, його написали власники файлів |
| `this rule applies to every branch: ...` | правило діє в усіх гілках, тож обійти його окремою гілкою не вийде |
| `rules: PATHPOLICY in main (9f1c2ab)` | правила взято з гілки `main`, з коміту `9f1c2ab`. Саме ця версія вирішує, а не та, що у вашій гілці |
| `checked as: bob (HTTPS)` | кого перевіряв hook і як прийшов push (HTTPS, SSH, UI або API GitLab). Якщо тут не ви, push зроблено від чужого імені, наприклад з CI job, який запустив інший користувач |
| `who can help: ...` | є лише у відмовах, де ви нічого не можете виправити самі: збій API GitLab, неправильне налаштування сервера, зламаний файл правил у `main` |
| `GL-HOOK-ERR: ...` | короткий підсумок одним рядком. Merge request, Web IDE і API GitLab показують лише його |

Що робити далі:
- зміна справді потрібна - попросіть когось із `may change them` внести її або створити гілку з нею. Merge request від вас не допоможе, якщо правило діє в усіх гілках, бо ви не зможете навіть запушити гілку
- правило здається хибним - зверніться до `admins` PATHPOLICY. Перелік показує `gitlab-repopolicy admins` (нижче) або сам файл `PATHPOLICY` у `main`
- зачепили захищений файл випадково - приберіть його зміну з комітів (`git restore --source=origin/main -- .gitlab-ci.yml`, потім `git commit --amend` або новий коміт) і зробіть push ще раз

Одна проблема - один блок: якщо те саме порушення є в кількох гілках і тегах одного push, у заголовку блоку перелічено їх усі. Під час merge через API GitLab відповідає лише "Branch cannot be merged", а текст відмови записує в поле `merge_error` merge request. Результати API (роль, "заблокований") у повідомлення не потрапляють ніколи, тільки в audit.

Цей вивід - для гілки, яка не є default. Щоб дізнатися, чи захищена гілка, hook питає API GitLab. Тому `gitlab-repopolicy test` (нижче) відтворить його лише з робочим токеном. Для `main` рішення приймається без API.

## Як hook читає правила

- Hook бере всі policy-файли з **якоря**. Якір - це коміт, на який вказувала default branch проєкту (зазвичай `main`) до цього push. Тому push не може послабити правила для самого себе: зміни в push завжди оцінюють за правилами, які вже були в `main`
- Один якір на всі гілки й теги одного push. Гілка зі своєю, слабшою версією PATHPOLICY не дозволяє зміни сама собі
- Нову версію policy-файлу перевіряють у момент push у **будь-яку** гілку, зокрема у feature-гілку. Автор бачить помилку одразу, а не під час merge. Докладно - [приймання нової версії](#приймання-нової-версії)
- Права GitLab нікуди не зникають. Hook запускається після штатної перевірки GitLab і може лише відмовити: PATHPOLICY звужує права і ніколи не видає нових

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

Невідомі ключі, YAML anchors і aliases - помилка. Розмір файлу - до 64 KiB. Push, що приносить понад 50 000 нових комітів, hook не перевіряє і відхиляє з `limit_exceeded`.

## Хто вносить зміну

Того, хто вносить зміну, далі називаємо principal: людина або бот, від імені якого GitLab прийняв push. У `admins` і `allow` пишуть лише username конкретних людей або ботів. Груп і ролей немає навмисно: членство в групі змінюється поза review policy-файлу, і людина, яку додали до групи заради іншої задачі, мовчки отримала б право.

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

Шлях у `paths` - це шаблон (glob) відносно кореня репозиторію. Спеціальні символи:
- `*` - будь-які символи в межах однієї частини шляху, тобто не перетинає `/`
- `**` - будь-яка кількість каталогів, зокрема жодного
- `?` - один символ, `[abc]` - один символ зі списку

| Шаблон | Охоплює | Не охоплює |
| --- | --- | --- |
| `Makefile` | `Makefile` | `src/Makefile`, `makefile` |
| `*.tf` | `main.tf` | `infra/main.tf` |
| `ci/**` | `ci/a.yml`, `ci/x/y.yml` | `x/ci/a.yml` |
| `helm/**/values-prod.yaml` | `helm/values-prod.yaml`, `helm/app/values-prod.yaml`, `helm/a/b/values-prod.yaml` | `helm/app/values-stage.yaml` |

Обмеження:
- шаблон завжди від кореня, `terraform/prod/**` не охоплює `x/terraform/prod/a.tf`. Правила gitignore "ім'я без `/` збігається в будь-якому каталозі" тут немає
- немає заперечення (`!`) і фігурних дужок `{a,b}`
- збіг побайтовий і з урахуванням регістру, так, як шлях бачить git
- без початкового `/`, без `\`, без порожніх частин, `.` і `..`, в UTF-8, до 256 байт і не більше чотирьох `**`
- rename і move - це видалення старого шляху і додавання нового: перевіряються обидва. Зміна режиму файлу, symlink і gitlink (submodule) - теж зміна шляху

**Мають виконуватися всі правила, що збіглися (AND).** Якщо шлях потрапив у два правила, автор зміни має бути в `allow` обох. Нове правило може лише звузити коло авторів. Вузьке правило не розширює широке: якщо `**` дозволено лише platform-lead, а `docs/**` - технічному письменнику, то `docs/a.md` може змінювати тільки той, хто є в обох списках. Перевірити, які правила збігаються, можна командою `explain` (нижче).

Шлях без правил, що збіглися, авторизує GitLab як зазвичай. PATHPOLICY - не allowlist репозиторію.

## Де діє правило (`refs`)

| `refs` | Де | Що вважається зміною |
| --- | --- | --- |
| `all` (за замовчуванням) | усі гілки й теги | у default і protected гілках - різниця вмісту гілки до і після push, в інших - нові коміти push |
| `protected` | default branch, protected branches і protected tags проєкту | різниця вмісту до і після push |
| `default` | лише default branch | різниця вмісту до і після push |
| список, наприклад `["refs/heads/release/*"]` | гілки й теги зі списку, навіть якщо в GitLab вони не захищені | різниця вмісту до і після push |

У списку гілки й теги пишуть повним ім'ям git: гілка `main` - це `refs/heads/main`, гілка `release/1.2` - `refs/heads/release/1.2`, тег `v1.0.0` - `refs/tags/v1.0.0`. Коротке `main` у списку - помилка. `*` заміняє будь-які символи, крім `/`. Тому `refs/heads/*` охоплює `refs/heads/main`, але не `refs/heads/feature/x`.

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

| Перевірка | PATHPOLICY | COMMITPOLICY | TAGPOLICY | BRANCHPOLICY |
| --- | --- | --- | --- | --- |
| звичайний файл (не каталог, не symlink, не submodule), розмір у межах ліміту | так | так | так | так |
| `version`, схема, невідомі ключі, ліміти | так | так | так | так |
| патерни й regex компілюються, приклади `pattern` проходять | так | так | `tags` | так, і порядок правил ([branchpolicy.md](branchpolicy.md)) |
| додані в цій версії username існують і активні (один GraphQL-запит) | так | `except.users` | ні | ні |
| ключі підпису | ні | ні | так | ні |
| є хоча б один effective admin | `lockout_prevented` | ні | ні | ні |
| API недоступний під час перевірок | `authorization_backend_failure` | `authorization_backend_failure` | ні | ні |
| делегований файл не покритий правилом PATHPOLICY | ні | попередження | попередження | попередження |

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

PATHPOLICY стежить за доступом. Тому, коли hook не може отримати потрібну відповідь (API GitLab не відповів, файл у `main` зламаний, внутрішня помилка), push, що зачепив захищені файли, відхиляється, а не пропускається. Це називають fail closed. Push, який не зачепив жодного правила, від API не залежить і проходить.

Як читати текст відмови - [вище](#як-читати-відмову).

## Перевірка без push

| Команда | Що робить |
| --- | --- |
| `gitlab-repopolicy validate -f PATHPOLICY` | ті самі перевірки, що й під час приймання, без GitLab |
| `gitlab-repopolicy validate -f PATHPOLICY --online --config FILE --project P` | ще існування username і роль admins у проєкті через API. Про кожного admin, який не є активним користувачем з роллю Maintainer або вище, - попередження і підсумок `N of M admins work`. Файл при цьому валідний, код виходу 0 |
| `gitlab-repopolicy explain --project P --user U --path X [--ref R]` | які правила збіглися зі шляхом і чи виконує їх користувач. Код виходу 0 - дозволено або жодне правило не збіглося, 1 - заборонено або помилка (текст помилки в stderr), 2 - не задано `--project`, `--user` чи `--path` |
| `gitlab-repopolicy admins --project P` | admins із файлу, effective admins і чи ввімкнено recovery |

`validate` без `--online` працює будь-де, де є бінарник і файл: локально, у CI merge request. `explain`, `admins` і `validate --online` звертаються до API GitLab з токеном hook. Їх запускають на ноді Gitaly, від користувача, який може прочитати конфігурацію сервера (`--config`, за замовчуванням `/etc/gitlab-repopolicy/config.yaml`) і файл токена з неї. `explain` і `admins` читають PATHPOLICY з default branch проєкту, тобто ту версію, за якою буде оцінено наступний push.

Відтворити відмову для конкретного push можна командою `gitlab-repopolicy test` ([operations.md](operations.md#перевірити-push-без-push)). Їй потрібні:
- `--user` і `--gl-id user-<id>` того, хто пушив. Без `--gl-id` команда шукає ID через API, а без робочого токена завершується помилкою `--gl-id is not given and user bob cannot be looked up`
- `--protocol` - `ssh` (за замовчуванням), `http` або `web`. Від нього залежить рядок `checked as:`
- API GitLab, якщо гілка не default, бо hook питає, чи вона захищена

Приклад помилки `validate`. Hook друкує рядок файлу, де знайдено помилку:

```text
$ gitlab-repopolicy validate -f PATHPOLICY
PATHPOLICY: invalid
  line 6: rule ci: paths: "/ci/**": patterns are relative to the repository root, remove the leading /
```

## Чого PATHPOLICY не захищає

Hook бачить лише зміни гілок і тегів. Налаштування проєкту (шлях CI-конфігурації, protected branches, default branch, job-token push) змінюють у Settings, і hook їх не бачить. Їх звіряє `settings-check` ([operations.md](operations.md)), повний перелік обмежень - у [threat-model.md](threat-model.md).

Проєкти в особистому namespace hook не перевіряє: вони не деплоять і не зберігають командний код, а зміни з них потрапляють у групові проєкти через merge, який перевіряється.
