[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](architecture.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](architecture.en.md)

# Архітектура

## Огляд

gitlab-repopolicy - Go-бінарник для глобального server hook `pre-receive` у Gitaly. На кожен push він застосовує правила, які лежать у самому репозиторії:

| Файл | Що перевіряє |
| --- | --- |
| `PATHPOLICY` | хто може змінювати певні файли: CI-конфігурацію, deploy- та infra-конфігурацію, інші чутливі шляхи |
| `COMMITPOLICY` | формат commit messages у гілках |
| `TAGPOLICY` | хто може випускати релізи: тег під правилом приймається лише з підписом дозволеного ключа OpenPGP або SSH |

У GitLab Self-Managed немає механізму, який обмежує зміну конкретних файлів конкретними людьми на будь-якому шляху зміни (push, merge, Web IDE, API), і немає структурної серверної перевірки формату комітів. Protected branches працюють на рівні гілки, Code Owners - лише в merge request і обходиться прямим push, push rules є лише в Premium і не знають, хто вносить зміну. Клієнтські інструменти (commitlint, pre-commit) обходяться `--no-verify` і не бачать merge і Web IDE.

Принципи:
- **жорстка перевірка на сервері**: правило не обійти локальним налаштуванням, іншим клієнтом чи кнопкою merge
- **правила в репозиторії**: policy-файли версіонуються і проходять review, як код
- **GitLab AND gitlab-repopolicy**: hook може лише відмовити, нових прав він не дає
- **різні власники різних правил**: PATHPOLICY делегує інші policy-файли їхнім власникам
- **швидкий шлях без мережі**: push без policy або без зачеплених правил не звертається до API
- **без вічного блокування**: втрату всіх admins відновлює явний recovery з audit
- **зрозумілі повідомлення**: з тексту відмови видно, що порушено і як виправити

## Системний контекст

```text
 git push, merge, Web IDE, API
              |
              v
     GitLab Rails / Workhorse ----- /internal/allowed: штатні права GitLab
              |
              v
           Gitaly --- custom_hooks_dir/pre-receive.d/50-gitlab-repopolicy
              |                      |
              |                      v
              |            gitlab-repopolicy hook pre-receive
              |              |            |              |
              |      git (Gitaly)   REST/GraphQL API   audit.log
              |              |      (лише коли треба)
              v              v
       репозиторій і quarantine-об'єкти push
```

Gitaly запускає hook лише після штатної перевірки прав GitLab. Hook отримує зміни гілок і тегів на stdin, ідентичність того, хто вносить зміну, - у змінних `GL_*`, і читає репозиторій тим самим git, що й Gitaly. Назовні він звертається лише до API GitLab і лише тоді, коли рішення залежить від ролі користувача, типу SSH-ключа або того, чи захищена гілка.

## Відповідальність

Входить:
- рішення allow або deny для кожного push за policy-файлами з default branch
- приймання нових версій policy-файлів у будь-якій гілці
- одне агреговане повідомлення на push і audit-подія на кожне рішення
- допоміжні команди: встановлення hook, самоперевірка, ротація токена, звірка налаштувань проєктів, аварійний обхід

Не входить:
- налаштування проєктів (шлях CI-конфігурації, protected branches, default branch): hook їх не бачить, `settings-check` лише виявляє зміни
- якість змін: проєкт перевіряє, хто і в якому форматі, а не що
- історія, яка була до появи policy
- проєкти в особистому namespace
- GitLab.com і GitLab Dedicated

## Основні компоненти

```text
cmd/gitlab-repopolicy     main: сигнали, код виходу
internal/cli              підкоманди, складання конфігурації, клієнтів і модулів
internal/core             якір, читання policy, приймання, порядок модулів, бюджети, агрегація
internal/module           контракт модуля: Module, Policy, Input, Result
internal/modules/path     PATHPOLICY
internal/modules/commit   COMMITPOLICY
internal/modules/tag      TAGPOLICY
internal/changes          які шляхи й коміти змінює push
internal/gitx             дочірні процеси git в ізольованому оточенні
internal/refs             default, protected чи інша гілка
internal/identity         хто вносить зміну, тип SSH-ключа, тип namespace
internal/gitlab           клієнт REST і GraphQL, бюджет викликів
internal/report           повідомлення користувачу, GL-HOOK-ERR
internal/audit            JSON-події
internal/install, settings, coverage, token, breakglass   допоміжні команди
```

Ядро не знає, що перевіряє модуль, модуль не знає, як отримано зміни й ідентичність. Модуль оголошує свій файл, режим при невалідній policy (закритися чи пропустити) і потрібні дані; ядро обчислює лише їх. Правила залежностей між пакетами перевіряє лінтер (`depguard`, `forbidigo`) у `make code-lint`.

Новий тип правил - це новий модуль і новий файл; ядро, модель довіри й експлуатація не змінюються. Файл модуля автоматично стає захищеним шляхом, а власника задає path-правило PATHPOLICY.

## Зовнішні залежності

| Залежність | Для чого | Коли |
| --- | --- | --- |
| git, який Gitaly передає в `PATH` hook | читання policy з якоря, шляхів, комітів, об'єктів тегів | на кожен push у проєкт |
| GitLab REST API v4 | роль користувача в проєкті, особистий чи deploy SSH-ключ, protected branches і tags, видимість проєкту для service account | лише коли зачеплено правило, під час приймання policy і bootstrap |
| GitLab GraphQL | чи існують і активні username, додані в policy | приймання нової версії, підрахунок admins, `settings-check` |

API доступний через unix socket Workhorse на тій самій ноді або через HTTPS, якщо Gitaly на окремій ноді. Один запит - до 3 s, на push - до 30 викликів. Кешу між викликами немає: відкликання прав діє одразу.

Go-залежності бінарника - `go.yaml.in/yaml/v3`, `github.com/bmatcuk/doublestar/v4`, `github.com/dlclark/regexp2/v2` (лише `pattern` у COMMITPOLICY), `github.com/ProtonMail/go-crypto` і `golang.org/x/crypto` (лише підписи тегів). Повний список версій - `go.mod` і SBOM релізу. Збірка статична, `CGO_ENABLED=0`.

## Потік даних

Один push:

1. **Вхід.** Сховище не проєкт (wiki, design, snippet) - дозволено без логу. `mode: off` - дозволено
2. **Якір.** Default branch до push. Усі policy-файли читаються лише звідси: push не може послабити правила для самого себе, а гілка зі слабшою policy не авторизує себе
3. **Break-glass.** Чинний дозвіл для проєкту - дозволено з подією `breakglass`
4. **Без policy.** Якщо в якорі немає policy-файлів, дозволено; push, який додає першу PATHPOLICY в default branch, проходить bootstrap-перевірку (Maintainer+)
5. **Зміни.** Лише для гілок і тегів, які зачіпають правила: шляхи, нові й вхідні коміти. У default і protected гілках - різниця вмісту до і після push, в інших - нові коміти (докладно - [pathpolicy.md](pathpolicy.md#де-діє-правило-refs))
6. **Модулі.** PATHPOLICY першим, далі COMMITPOLICY і TAGPOLICY. API - лише якщо рішення від нього залежить
7. **Приймання.** Кожен змінений policy-файл у будь-якій гілці перевіряється як нова версія
8. **Рішення.** AND за модулями; одне повідомлення на push у stderr; audit-подія на кожне рішення

Типовий час - близько 10 ms для проєкту без policy і 15-50 ms, коли policy є, але правила не зачеплено: здебільшого це запуск процесів git. Звернення до API додає 50-300 ms і буває рідко.

Ліміти, що захищають від DoS:

| Ресурс | Ліміт | Що буде |
| --- | --- | --- |
| розмір policy-файлу | 64 KiB, TAGPOLICY - 256 KiB | policy невалідна |
| path-правил / патернів у правилі | 100 / 50 | policy невалідна |
| час одного виклику hook | 10 s | `limit_exceeded` |
| викликів API на push | 30 | `limit_exceeded` |
| нових комітів (PATHPOLICY) | 50 000 | `limit_exceeded` |
| вхідних комітів (COMMITPOLICY) | 10 000 | `limit_exceeded` за `mode` файлу |
| `pattern` на повідомлення / на push | 100 ms / 2 s | `commit_check_timeout` |
| вивід користувачу | 8 KiB | обрізається |

## Модель розгортання

- бінарник і конфігурація на кожній ноді Gitaly; hook підключається командою `install-hook` у `custom_hooks_dir`
- пакет deb або rpm кладе файли лише у свої каталоги і GitLab не змінює
- окремий процес на кожен push, без daemon і без стану між викликами
- обліковий запис hook - instance service account з роллю Maintainer у кожній top-level групі і токеном `read_api` + `self_rotate`

Покроково - [install.md](install.md), експлуатація - [operations.md](operations.md).

## Безпека

- **Модель довіри.** Policy читається лише з default branch до push. PATHPOLICY захищає сама себе (`admins`) і через path-правила - інші policy-файли
- **Ідентичність.** Лише з `GL_ID` і `GL_USERNAME`, які задає GitLab. Author, committer і підписи комітів для авторизації не використовуються. Deploy key - не людина. Push options клієнта на рішення не впливають
- **Відмова за невизначеності.** PATHPOLICY і TAGPOLICY закриваються (fail closed): збій API, невалідна policy, внутрішня помилка - відмова на захищеній зміні. COMMITPOLICY за збою пропускає перевірку з попередженням: це гігієна, а не доступ. Збій API ніколи не вмикає recovery
- **Ізоляція git.** Дочірні git запускаються без replace refs і без системної та глобальної конфігурації: `refs/replace/*` не може сховати зміну від перевірки
- **Недовірений вхід.** Policy-файли, stdin, повідомлення комітів і об'єкти тегів розбираються строго і з лімітами; парсери покрито fuzz-тестами
- **Конфіденційність повідомлень.** Користувач бачить правила з policy-файлу (вони й так у репозиторії), але не результати API: ролі, стан облікового запису, деталі помилок. Ці дані - лише в audit
- **Мінімальні права.** Service account може лише читати і ротувати власний токен, прав адміністратора не має

Акторів, обходи і те, що поза моделлю, описано в [threat-model.md](threat-model.md).

## Відомі обмеження

- Hook бачить лише зміни гілок і тегів. Зміни налаштувань проєкту (шлях CI-конфігурації, default branch, protected branches, push через `CI_JOB_TOKEN`) Maintainer робить в обхід; `settings-check` лише виявляє їх за хвилини
- Merge у незахищену гілку не приносить нових комітів і не перевіряється. Перевіряється merge у default і protected гілки
- Нова гілка або тег на старому коміті, який уже лежить у захищеній гілці, PATHPOLICY і COMMITPOLICY не перевіряються: вміст пройшов перевірку раніше
- Username може звільнитися після rename або видалення користувача і бути зайнятий іншою людиною. `settings-check` повідомляє про principals, яких більше немає
- Токен service account не бачить заблокованого чи забаненого користувача. Особистий проєкт такого користувача обробляється як груповий (fail closed)
- Web-операції (merge кнопкою) тримають Puma-worker, поки hook звертається назад до Rails. Під піковим навантаженням на одній ноді це може дати `authorization_backend_failure` через таймаут
- Helm chart (Kubernetes) не підтримується
