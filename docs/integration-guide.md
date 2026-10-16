[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](integration-guide.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](integration-guide.en.md)

# Порядок інтеграції

Як увімкнути gitlab-repopolicy на робочому інстансі так, щоб нічого не зламати. Порядок перевірено на першому робочому розгортанні; конкретні групи, люди, ключі й правила кожна інсталяція вирішує сама.

Головна ідея - вмикати від найменш ризикованого до найбільш: спершу лише спостерігати, потім гігієна комітів, потім доступ до файлів, потім релізи.

## 1. Репозиторій розгортання

Тримайте все, що стосується вашої інсталяції, в окремому репозиторії:
- зафіксована версія gitlab-repopolicy
- `config.yaml` і пресети COMMITPOLICY
- CI, який перевіряє підпис і суми артефактів ([install.md](install.md#1-завантажити-й-перевірити-реліз)) до будь-якого встановлення, зберігає перевірені пакети у вашому registry і доставляє їх на ноди
- відкат - той самий job з попередньою версією

Доставку і відкат запускайте вручну (manual job, консоль хмари), а не push'ем: так зламаний hook не заблокує власне виправлення, навіть якщо доставка живе в тому ж GitLab.

## 2. Service account до ввімкнення hook

1. Створіть instance service account і токен `read_api` + `self_rotate` ([install.md](install.md#3-service-account-і-токен)), перевірте `selftest`
2. Запустіть `coverage` з токеном адміністратора і додайте service account Maintainer'ом в усі top-level групи з виводу
3. Додайте крок "додати service account Maintainer'ом" у ваш регламент створення top-level групи

Проєкт з policy в групі без service account отримує відмову `authorization_backend_failure` (`project_not_visible` у audit). Це штатна поведінка: захист не пропускається мовчки.

## 3. Глобально `mode: audit`

Встановіть hook на всі ноди Gitaly з `mode: audit` і залиште на 1-2 тижні. Hook нікому не відмовляє, а рішення пише в audit з `"enforced": false`.

Що дивитися в audit:
- події `decision` з `"decision": "deny"` - хто і що зламав би `enforce`
- `authorization_backend_failure` - групи без service account, проблеми з токеном
- `duration_ms` і `api_calls` - вартість hook на вашому навантаженні
- `invalid_policy` і `decision` з `code: invalid_policy_update` - policy, які в `enforce` заблокують проєкт

## 4. COMMITPOLICY у кількох проєктах

COMMITPOLICY - найбезпечніший старт: він не загрожує блокуванням доступу, а збій API лише пропускає перевірку.

1. Опишіть стандарт команди як пресет у `commitpolicy_presets` ([commitpolicy.md](commitpolicy.md#пресети-extends)). Виводьте його з реальних комітів команди, а не з колишніх клієнтських схем
2. У 2-3 проєктах додайте COMMITPOLICY з `extends:` і `mode: warn`
3. Налаштуйте squash commit template проєкту так, щоб squash-коміт відповідав стандарту. Посилання на issue найпростіше отримати змінною `%{issues}`: GitLab підставляє `Closes #N` для issue з опису MR
4. Коли попереджень майже немає, переведіть файл у `mode: enforce`

Обов'язкове посилання на issue вмикайте поступово: у пресеті `reference` з `level: warn`, проєкт, готовий до вимоги, піднімає його до `error` у своєму файлі (правила файлу перекривають пресет), далі `error` переходить у пресет. Готовність видно з audit: частка попереджень `reference` за проєктом.

Під час merge через UI автор попереджень не бачить: GitLab не показує успішний вивід hook для web-операцій. Етап `warn` дає статистику, а не навчання.

## 5. PATHPOLICY в одному-двох проєктах

Почніть з CI-логіки deployment-проєктів ([examples/ci-config/PATHPOLICY](../examples/ci-config/PATHPOLICY)):
- захищайте не лише `.gitlab-ci.yml`, а все, що визначає, що виконає deploy-job: includes, `Makefile`, скрипти. Захист самого `.gitlab-ci.yml` без `Makefile` декоративний, якщо job викликає `make`
- шлях CI-конфігурації проєкту, якщо він не `.gitlab-ci.yml`, теж має бути в правилі. Його звіряє `settings-check`
- компоненти з CI/CD Catalog фіксуйте на тегах: їхня логіка живе в проєкті компонента і захищається там
- делегуйте COMMITPOLICY і TAGPOLICY їхнім власникам path-правилами ([examples/delegation/PATHPOLICY](../examples/delegation/PATHPOLICY))

Перевіряйте policy до merge: `gitlab-repopolicy validate --online` і `explain` ([pathpolicy.md](pathpolicy.md#перевірка-без-push)).

## 6. TAGPOLICY для релізів

1. Перенесіть ключі тих, хто підписує релізи, у TAGPOLICY, а regex правил - з правил CI, які деплоять теги ([tagpolicy.md](tagpolicy.md)). Прострочені ключі продовжте або не переносьте: приймання відхилить новий запис з простроченим ключем
2. Розділіть підписантів за оточеннями: окреме правило на staging і prod
3. Поки працюють і стара перевірка в CI, і TAGPOLICY, нічого не видаляйте. Після перших релізів через TAGPOLICY стару перевірку підпису з deploy-jobs можна прибрати
4. Перевірки, які не про авторизацію (вікно релізу, розклад), лишаються в CI

Перед додаванням TAGPOLICY переконайтеся, що імпорт чи `git push --mirror` не принесуть старих непідписаних тегів під правилами.

## 7. Глобально `mode: enforce`

Перед перемиканням:
- в audit за період `mode: audit` немає подій `invalid_policy` і `invalid_policy_update`: невалідна PATHPOLICY у default branch заблокує проєкт повністю, невалідна TAGPOLICY - теги
- налаштовано алерти ([operations.md](operations.md#події-й-алерти)): обов'язково `policy_recovery`, `breakglass`, `internal_error`, `invalid_policy`, `settings_drift`, а `authorization_backend_failure` - за порогом
- увімкнено таймери `token rotate` і `settings-check`
- у черговому runbook є break-glass і швидкий відкат через `mode: audit`

## 8. Решта проєктів

Policy-файли додаються merge request'ами в міру потреби, кожен репозиторій - окремо. Правило `refs: all` за замовчуванням не дає Developer'у запушити навіть гілку-пропозицію із захищеним шляхом; для шляхів, де це заважає, використовуйте `refs: protected` ([pathpolicy.md](pathpolicy.md#де-діє-правило-refs)).

Повністю обходи через налаштування проєкту закриває лише організаційний захід: роль Maintainer тільки у власників захищених шляхів. Там, де це неможливо, `settings-check` обов'язковий ([threat-model.md](threat-model.md#обходи-через-налаштування-проєкту)).
