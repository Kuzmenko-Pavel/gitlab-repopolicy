[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](integration-guide.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](integration-guide.en.md)

# Порядок інтеграції

Почніть упровадження на робочому інстансі зі спостереження в `audit`. До застосування відмов перевірте доступи, політики та очікувані порушення на вашому навантаженні.

Головна ідея - вмикати від найменш ризикованого до найбільш. Спершу лише спостерігати, потім гігієна комітів і гілок, потім доступ до файлів, потім релізи.

## Коротко

| Крок | Що робимо | Коли переходити далі | Чим перевірити | Як відкотити |
| --- | --- | --- | --- | --- |
| [1. Репозиторій розгортання](#1-репозиторій-розгортання) | Готуємо окремий репозиторій для встановлення, конфігурації й оновлень | Зафіксовано версію, перевірку артефактів, доставку та відкат | Перевірити CI pipeline, manual deployment і повернення попередньої версії | Відкат не потрібен, hook ще не підключено |
| [2. Service account до ввімкнення hook](#2-service-account-до-ввімкнення-hook) | Service account у всіх top-level групах | `coverage` не знаходить груп без service account | `sudo gitlab-repopolicy selftest`, `coverage --token-file <admin-token>` | Нічого вмикати назад не треба, hook ще не встановлено |
| [3. Глобально `mode: audit`](#3-глобально-mode-audit) | Hook на всіх нодах з `mode: audit` | 1-2 тижні в `audit`, усі `deny` зрозумілі, немає `invalid_policy` і `decision` з `code: invalid_policy_update` | `jq` по audit-логу ([operations.md](operations.md#audit)) | `mode: off` або `uninstall-hook` |
| [4. COMMITPOLICY у кількох проєктах](#4-commitpolicy-у-кількох-проєктах) | COMMITPOLICY з `mode: warn` у 2-3 проєктах, потім `mode: enforce` | Попереджень майже немає | `gitlab-repopolicy lint-commit --range`, події `decision` модуля `commitpolicy` | `mode: warn` у файлі проєкту |
| [5. BRANCHPOLICY](#5-branchpolicy) | BRANCHPOLICY з `mode: warn`, потім `mode: enforce` | Попереджень майже немає | `gitlab-repopolicy validate`, події `decision` модуля `branchpolicy` | `mode: warn` у файлі проєкту |
| [6. PATHPOLICY в одному-двох проєктах](#6-pathpolicy-в-одному-двох-проєктах) | PATHPOLICY в одному-двох проєктах | У audit немає несподіваних `deny` за цими проєктами | `validate --online`, `explain` | Merge request з попередньою версією від admin або break-glass ([operations.md](operations.md#break-glass)) |
| [7. TAGPOLICY для релізів](#7-tagpolicy-для-релізів) | TAGPOLICY для релізів | Перші релізи пройшли через TAGPOLICY | `gitlab-repopolicy test` на тегу | Merge request з попередньою версією TAGPOLICY |
| [8. Глобально `mode: enforce`](#8-глобально-mode-enforce) | Глобально `mode: enforce` | Усі відмови пояснені, доступи перевірені, оператор готовий до відкату | Алерти з [operations.md](operations.md#події-й-алерти) | `mode: audit`, діє з наступного push |
| [9. Решта проєктів](#9-решта-проєктів) | Підключаємо узгоджені пресети в решті проєктів невеликими групами | Кожна група пройшла `validate`, контрольні push і перевірку audit | CI перевірки policy, `selftest` вузлів, `settings-check` | Повернути policy проєкту її власником; для масового інциденту тимчасово `audit` |

На кроках 4-7 глобальний режим залишається `audit`: локальний `mode: enforce` ще не блокує push. Тут перевіряють готовність за журналом і локальними інструментами. Увімкнення блокування для всього інстансу відбувається на кроці 8. Після нього нові проєкти потребують власної підготовки й контрольних перевірок до merge policy.

`settings-check` у цьому процесі лише виявляє зміни Settings проєктів за розкладом (раз на добу таймером), а не забороняє їх. Зміну, яку зробили і повернули між двома запусками, він не побачить ([operations.md](operations.md#settings-check)).

## 1. Репозиторій розгортання

Тримайте все, що стосується вашої інсталяції, в окремому репозиторії:
- зафіксована версія gitlab-repopolicy
- `config.yaml` з пресетами всіх модулів
- CI, який перевіряє підпис і суми артефактів ([install.md](install.md#1-підготувати-й-перевірити-артефакти)) до будь-якого встановлення, зберігає перевірені пакети у вашому registry і доставляє їх на ноди
- відкат - той самий job з попередньою версією

Доставку і відкат запускайте вручну (manual job, консоль хмари), а не push'ем: так зламаний hook не заблокує власне виправлення, навіть якщо доставка живе в тому ж GitLab.

## 2. Service account до ввімкнення hook

До `selftest` встановіть пакет, підготуйте конфігурацію в `audit` і файл токена за [кроками встановлення](install.md). Wrapper поки не підключайте. Для запуску від git також потрібні належні права читання файлів; остаточно їх налаштовує `install-hook` на наступному кроці.

1. Створіть instance service account і токен `read_api` + `self_rotate` ([install.md](install.md#3-service-account-і-токен)), перевірте `selftest`
2. Запустіть `coverage` з токеном адміністратора і додайте service account Maintainer'ом в усі top-level групи з виводу
3. Додайте крок "додати service account Maintainer'ом" у ваш регламент створення top-level групи

Проєкт з policy в групі без service account отримує відмову `authorization_backend_failure` (`project_not_visible` у audit). Це штатна поведінка: захист не пропускається мовчки.

## 3. Глобально `mode: audit`

Встановіть hook на всі ноди Gitaly з `mode: audit` і залиште на 1-2 тижні. Рішення ядра не блокують push і записуються з `"enforced": false`. Помилки запуску до ядра, зокрема нечитабельна конфігурація, усе ще можуть відхилити push; перевірте `selftest` від root і git.

Що дивитися в audit:
- усі події з `"decision": "deny"` - хто і що зламав би `enforce`
- `authorization_backend_failure` - групи без service account, проблеми з токеном
- `duration_ms` і `api_calls` - вартість hook на вашому навантаженні
- `invalid_policy` і `decision` з `code: invalid_policy_update` - policy, які в `enforce` заблокують проєкт
- `decision` з `code: default_branch_not_allowed`, якщо задано `default_branch` ([configuration.md](configuration.md#default_branch)) - проєкти, де default branch названа інакше; у `enforce` push у них відхилятимуться
- `decision` з `code: tag_not_on_branch` - хто ставить теги на коміти поза гілками. Ця перевірка діє в усіх групових проєктах, навіть без файлів політик ([tagpolicy.md](../examples/tagpolicy/tag-on-branch.md)). Перед `enforce` домовтеся з командами, що теги пушать разом з гілкою (`git push --follow-tags`)

## 4. COMMITPOLICY у кількох проєктах

COMMITPOLICY - найбезпечніший старт: він не загрожує блокуванням доступу, а збій API лише пропускає перевірку. Виняток - нова версія COMMITPOLICY з `except.users`: якщо API не відповів, її приймання відхиляється з `authorization_backend_failure`.

1. Опишіть стандарт команди як пресет у `presets.commitpolicy` ([configuration.md](configuration.md#presets)). Узгодьте вимоги з командою й перевірте їх на реальних повідомленнях
2. У 2-3 проєктах додайте COMMITPOLICY з `extends:` і `mode: warn`
3. Налаштуйте squash commit template проєкту так, щоб squash-коміт відповідав стандарту. Посилання на issue найпростіше отримати змінною `%{issues}`: GitLab підставляє `Closes #N` для issue з опису MR
4. Коли попереджень майже немає, переведіть файл у `mode: enforce`

Обов'язкове посилання на issue вмикайте поступово: у пресеті перевірка `pattern` з `level: warn`, проєкт, готовий до вимоги, піднімає її до `error` у своєму файлі (`checks.pattern: { level: error }`, файл зливається поверх пресета), далі `error` переходить у пресет. Готовність видно з audit: частка попереджень `pattern` за проєктом.

Під час merge через UI автор попереджень не бачить: GitLab не показує успішний вивід hook для web-операцій. Етап `warn` дає статистику, а не навчання.

## 5. BRANCHPOLICY

BRANCHPOLICY перевіряє лише створення гілок, тому її можна впроваджувати без перейменування наявних. Збій API пропускає перевірку з попередженням. Створіть пресет у `presets.branchpolicy`, додайте в проєкти BRANCHPOLICY з `extends` і `mode: warn`, перевірте попередження в audit протягом тижня-двох і лише потім задайте `mode: enforce`. [Короткий старт BRANCHPOLICY](../policies/branchpolicy.md#швидкий-старт) містить конфігурацію для цього порядку дій. До merge перевірте потрібні імена командою `validate --config presets.yaml --branch <ім'я>`.

`git push --mirror` старого репозиторію створює всі гілки разом і відхиляється цілком, якщо хоч одне ім'я не за стандартом. Додавайте BRANCHPOLICY після імпорту.

## 6. PATHPOLICY в одному-двох проєктах

Почніть з CI-логіки deployment-проєктів ([examples/ci-config/PATHPOLICY](../../../examples/ci-config/PATHPOLICY)):
- захищайте не лише `.gitlab-ci.yml`, а все, що визначає, що виконає deploy-job: includes, `Makefile`, скрипти. Захист самого `.gitlab-ci.yml` без `Makefile` декоративний, якщо job викликає `make`
- шлях CI-конфігурації проєкту, якщо він не `.gitlab-ci.yml`, теж має бути в правилі. Його звіряє `settings-check`
- компоненти з CI/CD Catalog фіксуйте на тегах: їхня логіка живе в проєкті компонента і захищається там
- делегуйте COMMITPOLICY, TAGPOLICY і BRANCHPOLICY їхнім власникам path-правилами ([examples/delegation/PATHPOLICY](../../../examples/delegation/PATHPOLICY))

Перевіряйте policy до merge: `gitlab-repopolicy validate --online --config /etc/gitlab-repopolicy/config.yaml --project <проєкт>` і `explain` ([pathpolicy.md](../policies/pathpolicy.md#перевірка-без-push)).

## 7. TAGPOLICY для релізів

1. Перенесіть ключі тих, хто підписує релізи, у TAGPOLICY, а regex правил - з правил CI, які деплоять теги ([tagpolicy.md](../policies/tagpolicy.md)). Прострочені ключі продовжте або не переносьте: приймання відхилить новий запис з простроченим ключем
2. Розділіть підписантів за оточеннями: окреме правило на staging і prod
3. Поки працюють і стара перевірка в CI, і TAGPOLICY, нічого не видаляйте. Після перших релізів через TAGPOLICY стару перевірку підпису з deploy-jobs можна прибрати
4. Перевірки, які не про авторизацію (вікно релізу, розклад), лишаються в CI

Перед додаванням TAGPOLICY переконайтеся, що імпорт чи `git push --mirror` не принесуть старих непідписаних тегів під правилами.

## 8. Глобально `mode: enforce`

Перед перемиканням:
- в audit за період `mode: audit` немає подій `invalid_policy` і `decision` з `code: invalid_policy_update`. Невалідна PATHPOLICY у default branch заблокує проєкт повністю, невалідна TAGPOLICY - теги
- налаштовано алерти ([operations.md](operations.md#події-й-алерти)): обов'язково `policy_recovery`, `breakglass`, `internal_error`, `invalid_policy`, `settings_drift`, а `authorization_backend_failure` - за порогом
- увімкнено таймери `token rotate` і `settings-check`
- у черговому runbook є break-glass і швидкий відкат через `mode: audit`

## 9. Решта проєктів

Коли проєктів сотні, правила не копіюють у кожен репозиторій. Порядок такий:
1. Стандарти організації - пресети в серверному конфігу ([configuration.md](configuration.md#presets)): `org-ci` для PATHPOLICY, `org-commits`, `org-branches`, `org-tags`. Пресети лежать у репозиторії розгортання і доставляються на всі ноди разом з конфігом
2. Файл проєкту - один рядок `extends` плюс те, що в проєкті своє: `admins` PATHPOLICY, `keys` TAGPOLICY, відхилення від стандарту ([приклад](../examples/pathpolicy/organization-preset.md))
3. Перед merge кожен файл перевіряє `validate --config presets.yaml` у CI merge request ([validate.md](../reference/validate.md#validate-у-ci-merge-request)). Копія пресетів для CI - той самий файл з репозиторію розгортання
4. Перші відмови після merge. `validate` не знає всього, що знає hook ([validate.md](../reference/validate.md#чого-validate-не-знає)): найчастіше це новий username, якого немає в GitLab, або admin без ролі Maintainer. Текст відмови називає правило і поле, виправлення - новий merge request з файлом. Відмови розробникам за захищені файли - очікувана поведінка: у тексті відмови сказано, кого попросити
5. Зміна пресета діє в усіх проєктах з першого push після доставки конфігу. Перевіряйте її так само: `validate --config` на кількох файлах проєктів до доставки, `selftest` після

Правило з `match: all` не дає Developer'у запушити навіть гілку-пропозицію зі зміненим захищеним файлом. Для файлів, які GitLab читає лише з default branch, достатньо `match: default` ([pathpolicy.md](../policies/pathpolicy.md#де-файл-має-ефект)).

Після впровадження: старі гілки, створені до політик, живуть як є. Їхній merge у `main` перевіряється за правилами, і право на внесення захищеної зміни потрібне користувачу, який виконує merge. Авторство старого коміту саме по собі не визначає відмову. Видаліть гілки, які ніхто не планує зливати, а потрібні перебазуйте на свіжий `main`: тоді в них лишаються лише власні зміни автора.

Зменшіть ризик обходів через налаштування проєкту: надавайте роль Maintainer тим, кому довіряєте керування захищеними шляхами, і запускайте `settings-check` для виявлення розбіжностей. Сам hook не авторизує зміни Settings; їхні наслідки пояснює [модель загроз](threat-model.md#обходи-через-налаштування-проєкту).
