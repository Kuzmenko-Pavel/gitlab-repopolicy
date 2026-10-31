[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](operations.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](operations.en.md)

# Експлуатація

Якщо push відхиляється, почніть із [таблиці симптомів](#типові-проблеми). Для конкретної зміни доступу використайте `explain`, для відтворення цілого рішення - [test](#перевірити-push-без-push). Перед recovery чи break-glass спершу визначте, яка політика або серверна перевірка відмовила.

## Audit

Hook пише кожне рішення, яке зачепило правило, у `audit.file` (за замовчуванням `/var/log/gitlab-repopolicy/audit.log`): один JSON-об'єкт на рядок, до 4 KiB на подію. Звичайний push без зачепленого правила не створює `decision`; глобальні відмови, приймання policy та аварійні процедури можуть створювати події незалежно від збігу path-правил.

```json
{"ts":"2026-10-14T09:14:02.418Z","event":"decision","receive_id":"Q4ZJ7OXKMV3HB2LDSY6TNRWAEC","module":"pathpolicy","decision":"deny","enforced":true,"code":"protected_path_violation","project_id":812,"project_path":"example/payments/deployment","user_id":314,"username":"alice","gl_id":"user-314","protocol":"web","ref":"refs/heads/main","oldrev":"9f1c...","newrev":"b77a...","anchor_rev":"9f1c...","policy_blob":"3e5d...","violations":[{"rule":"ci-config","code":"protected_path_violation","paths":[".gitlab-ci.yml"],"reason":"not in allow: alice"}],"api_calls":2,"duration_ms":143,"version":"1.0.0","git":"2.55.0","node":"gitaly-01"}
```

| Поле | Значення |
| --- | --- |
| `ts` | час UTC |
| `receive_id` | однаковий у всіх подіях одного запуску hook, тобто одного push |
| `event`, `module`, `decision`, `code`, `action` | що сталося, див. таблицю нижче |
| `enforced` | `false` у `mode: audit`: рішення записане, але push пройшов |
| `project_id`, `project_path` | проєкт |
| `user_id`, `username`, `gl_id`, `protocol` | хто вносив зміну і як: `http`, `ssh` або `web` (merge, Web IDE, API, операції UI) |
| `ref`, `oldrev`, `newrev` | гілка або тег і значення до і після |
| `anchor_rev`, `policy_blob` | коміт якоря і blob policy-файлу: за ними можна відновити точну policy, за якою прийнято рішення |
| `violations` | правило, код, шляхи або коміти, `reason` |
| `rules`, `signer`, `fingerprint` | для TAGPOLICY: правила, мітка і fingerprint ключа підпису |
| `reason` | причина з результатами API. У повідомлення користувачу не потрапляє |
| `truncated` | `true`, якщо `violations` або `reason` обрізано, щоб подія вмістилася в 4 KiB |
| `api_calls`, `duration_ms` | накопичені виклики API й час від початку перевірки до формування цієї події; не повний клієнтський час push |
| `presets` | імена й SHA-256 пресетів, за якими перевірено policy, якщо вони використовуються |
| `version`, `git`, `node` | версія gitlab-repopolicy, git, який бачив hook, і нода |

Для одного push у модуля може бути кілька подій: по одній на кожну гілку чи тег із порушеннями.

**Як знайти відмову цього hook.** Окремої підсумкової події немає. Відмова в pre-receive відхиляє всі refs цього push. Шукайте серед подій з одним `receive_id` `"decision":"deny"` з `"enforced":true`:

```sh
jq -c 'select(.receive_id == "Q4ZJ7OXKMV3HB2LDSY6TNRWAEC") | {event, module, decision, enforced, code, ref}' /var/log/gitlab-repopolicy/audit.log
```

Події `policy_change`, `policy_bootstrap` і `policy_recovery` пишуться, якщо цей hook допускає push, зокрема в `mode: audit`. Це відбувається до оновлення refs. Інший hook або конфлікт оновлення refs ще може відхилити операцію, тому такі події не є підтвердженням, що policy справді збережено в GitLab. Для підтвердження перевірте результат GitLab і ref репозиторію.

Якщо hook не може записати подію в audit, push це не змінює, а користувач бачить рядок `the audit log is not writable, tell your GitLab administrators`. `settings-check` щодня перевіряє, що користувач hook може писати в `audit.file`. Якщо не може, він друкує `audit: <причина>: the hook loses its events`, пише подію `internal_error` і завершується з кодом 1.

### Події й алерти

| `event` | Коли | Алерт |
| --- | --- | --- |
| `decision` | allow, deny або warn, коли зачеплено правило | ні |
| `policy_change` | створення, зміна або видалення policy-файлу | ні |
| `policy_bootstrap` | перша PATHPOLICY у проєкті | ні |
| `policy_recovery` | Owner змінив PATHPOLICY через recovery | **так, кожна** |
| `invalid_policy` | PATHPOLICY або TAGPOLICY у default branch невалідна | **так** |
| `invalid_commit_policy` | COMMITPOLICY у default branch невалідна (перевірку пропущено) | ні |
| `invalid_branch_policy` | BRANCHPOLICY у default branch невалідна (імена нових гілок не перевіряються, push проходить з попередженням) | ні |
| `authorization_backend_failure` | API недоступний на захищеній операції, токен не діє (зокрема 401 під час перевірки правила), service account не бачить проєкт (`reason`: `project_not_visible`, `role_below_maintainer`) | так, якщо їх кілька за 5 хвилин: одну подію дає короткий збій API, серію - прострочений токен, група без service account або недоступний API. Поріг під своє навантаження вибирає інсталяція |
| `unknown_principal` | deploy key або нерозбірний `GL_ID` на захищеному шляху | ні |
| `breakglass` | дозвіл break-glass виданий (`action: grant`), відкликаний (`action: revoke`) або використаний hook | **так** |
| `settings_drift` | `settings-check` знайшов розбіжність | **так** |
| `internal_error` | внутрішня помилка бінарника, збій `token rotate` (`action: token_rotate`) | **так** |

**Як називається подія відмови.** Зазвичай відмова - подія `decision` з кодом відмови в `code`. Але якщо для тієї самої гілки чи тегу ще й не відповів API GitLab або сталася внутрішня помилка, подія називається за цим збоєм: `authorization_backend_failure` або `internal_error`. Сам код відмови тоді лишається в `violations[].code`. Наприклад, тег з чужим підписом, для якого API не відповів, чи захищений тег, - це подія `authorization_backend_failure` з `"decision": "deny"` і `tag_signer_not_allowed` у `violations`. Тому алерт на серію `authorization_backend_failure` може спрацювати і на звичайні відмови, а шукати всі відмови варто за `"decision": "deny"`, а не лише за `"event": "decision"`:

```sh
jq -c 'select(.decision == "deny") | {event, code, codes: [.violations[]?.code], project_path, ref}' /var/log/gitlab-repopolicy/audit.log
```

Окремо варто стежити за `decision` з `code: invalid_policy_update` у `mode: audit`: таку версію policy hook у `enforce` не пропустив би, а після перемикання вона заблокує проєкт.

### Доставка і ротація логу

Куди відправляти лог (Fluent Bit, Vector, journald, SIEM), вирішує інсталяція. Hook відкриває файл на кожен запис з `O_APPEND`, тому logrotate не потребує `copytruncate`.

Пакет ставить `/etc/logrotate.d/gitlab-repopolicy`: щоденна ротація, 30 файлів, стиснення. Це conffile, ваші правки переживають оновлення пакета. Шлях у файлі - `audit.file` за замовчуванням: якщо ви змінили `audit.file` у конфігу, змініть шлях і тут. Якщо лог забирає зовнішній агент зі своєю ротацією, видаліть цей файл або замініть своїм. Без пакета - приклад у [install.md](install.md#2-встановити-пакет).

Наступний `audit.log` після ротації створить hook від користувача git, а команди від root створюють відсутній файл з власником каталогу логу, тож hook не втратить права на запис. Тому в конфігу logrotate стоїть `nocreate`.

## Recovery

Recovery вмикається, коли в PATHPOLICY не залишилося жодного effective admin: усіх видалили з проєкту, заблокували або понизили нижче Maintainer. Тоді Owner проєкту (зокрема Owner батьківської групи) може змінити PATHPOLICY. [Сценарій відновлення доступу](../examples/pathpolicy/recovery.md) показує повний файл і межі цього винятку.

```sh
sudo gitlab-repopolicy admins --project group/project
```

показує admins із файлу, effective admins і чи ввімкнено recovery. Збій API - помилка, а не "0 admins". `admins` і `explain` читають конфігурацію сервера і токен (`--config`, за замовчуванням `/etc/gitlab-repopolicy/config.yaml`), тому запускаються на ноді Gitaly від root або від користувача git.

```text
$ sudo gitlab-repopolicy admins --project acme/app
  alice                          missing: access level 30, Maintainer (40) is required
acme/app: configured 1, effective 0, missing 1
recovery active: an Owner of the project may change PATHPOLICY
```

Кожен recovery - подія `policy_recovery`. Owner'и namespace - не завжди ті, хто відповідає за policy, тому на цю подію потрібен алерт.

## Break-glass

Аварійний обхід для одного проєкту, коли policy заблокувала роботу, а виправити її штатно не можна (наприклад, невалідна PATHPOLICY після імпорту). Потрібен root на ноді Gitaly, через GitLab його не ввімкнути.

```sh
sudo gitlab-repopolicy breakglass grant --project acme/app --ttl 30m --reason "INC-123 policy lockout"
sudo gitlab-repopolicy breakglass list
sudo gitlab-repopolicy breakglass revoke --project acme/app
```

- проєкт задається шляхом `--project` або ID `--project-id`, рівно одним із них. Шлях команда `breakglass` переводить в ID через API. Якщо API недоступний, потрібен `--project-id`: ID показано на головній сторінці проєкту під назвою. З `--project` у виводі й події видно і ID, і шлях, з `--project-id` - лише ID

- `--ttl` обов'язковий, не більше 24 годин. Після строку дозвіл ігнорується
- поки дозвіл діє, усі модулі пропускають push у проєкт, у виводі push є попередження, кожен такий push - подія `breakglass`
- дозвіл - файл `<breakglass_dir>/<project-id>` з часом, причиною й оператором (`SUDO_USER`). Hook довіряє лише файлу і каталогу root, у які не можуть писати група й інші
- на кількох нодах Gitaly дозвіл видається на кожній, де може виконатися hook

## Ротація токена

Токен service account має строк. Прострочений токен - відмова `authorization_backend_failure` на кожній захищеній зміні.

```sh
sudo gitlab-repopolicy token rotate --before 30d
```

- якщо до кінця строку більше `--before`, команда нічого не робить
- інакше ротує токен через API з явним `expires_at` = сьогодні + `token_lifetime` (без нього GitLab видав би токен на 7 днів)
- старий токен перестає діяти одразу, тому новий записується атомарно: тимчасовий файл у тому ж каталозі, fsync, rename. Hook, який отримав 401 під час ротації, один раз перечитує файл і повторює запит
- `400` від GitLab (наприклад, строк більший за максимум інстансу) - поточний токен лишається робочим, подія `internal_error` з `action: token_rotate`

Таймер `gitlab-repopolicy-token-rotate.timer` запускає ротацію щодня ([install.md](install.md#7-таймери)). На кількох нодах Gitaly у кожної свій токен або ротацію робить одна нода, а інші отримують файл через вашу систему доставки: інакше ротація на одній ноді зламає токен на інших.

## `settings-check`

Hook бачить лише зміни гілок і тегів. Налаштування проєкту змінюються в Settings, і Maintainer може обійти PATHPOLICY через них: перенести шлях CI-конфігурації в незахищений файл, зробити default гілку без policy, увімкнути push через `CI_JOB_TOKEN`. `settings-check` знаходить такі зміни.

Команда обходить проєкти з файлами політик, які бачить service account. Архівні проєкти й проєкти в особистих namespace пропускаються. Проєкт без PATHPOLICY отримує тільки перевірки `default_branch`, `access`, `policy` і `protected_branches` за правилами BRANCHPOLICY: решта стосується PATHPOLICY:

| Перевірка (`violations[].rule`) | Що вважається розбіжністю |
| --- | --- |
| `default_branch` | default branch не входить у список `default_branch` |
| `protected_branches` | default branch не захищена (у звіті як `<branch> (the default branch)`, лише для проєктів з PATHPOLICY) або GitLab не захищає наявну гілку, чиє правило BRANCHPOLICY має `protected: true` (у звіті як `release/v1.4.0 (BRANCHPOLICY)`). Гілки проєкту вивантажуються через API |
| `protected_tags` | шаблони тегів не захищені. Перевіряється лише в проєктах з TAGPOLICY у default branch і з увімкненим модулем |
| `job_token_push` | дозвіл push через `CI_JOB_TOKEN` не такий, як очікується |
| `ci_config_path` | CI-конфігурація в іншому проєкті, за URL або не покрита path-правилом PATHPOLICY |
| `principals` | username з policy-файлів більше не активні користувачі GitLab |
| `access` | роль service account нижча за Maintainer: налаштування не видно |
| `policy` | файл політики в default branch не розбирається, або пресета з його `extends` немає в серверному конфігу, або пресет зламаний |
| `unavailable` | API не відповів для цього проєкту |

Розбіжність - подія `settings_drift` на проєкт і код виходу 1. Проєкт з PATHPOLICY шукається і на поточній default branch, і на очікуваних: перемикання default на гілку без PATHPOLICY - розбіжність, а не "проєкт без policy".

`ci_config_path` перевіряється й у проєктах без `.gitlab-ci.yml`: Maintainer може ввімкнути CI пізніше або вказати шлях до CI-конфігурації в іншому проєкті, і path-правило на `.gitlab-ci.yml` закриває цей обхід заздалегідь. Очікування задаються в `config.yaml` одні на весь інстанс ([configuration.md](configuration.md#settings_check)).

```text
$ sudo -u git gitlab-repopolicy settings-check
acme/app: protected_tags: not protected: v*
```

`settings-check` бачить стан налаштувань на момент запуску й не запобігає їх зміні. Зміну, яку повернули між двома запусками, він не побачить. Щоб зменшити цей ризик, обмежте роль Maintainer користувачами, яким довіряєте керування захищеними шляхами.

### На ноді за таймером

```sh
sudo systemctl enable --now gitlab-repopolicy-settings-check.timer
journalctl -u gitlab-repopolicy-settings-check.service
```

Запуск раз на добу з випадковою затримкою до години, той самий токен, що в hook, події в audit-лог. Алерт будується з audit (`settings_drift`).

Інший розклад - drop-in файл, пакет його не перезапише:

```sh
sudo systemctl edit gitlab-repopolicy-settings-check.timer
```

```ini
[Timer]
OnCalendar=
OnCalendar=*:0/15
```

Порожній `OnCalendar=` скидає розклад пакета, інакше systemd запускатиме за обома. Перевірка обходить проєкти послідовно, за замовчуванням `--timeout 1h`. На великому інстансі розклад частіше, ніж триває один прохід, не має сенсу.

### У запланованому pipeline GitLab CI

Якщо зручніше бачити результат як job у GitLab, `settings-check` можна запускати за розкладом у CI. Код не змінюється, але потрібні:
- **окремий** personal access token того самого service account зі scope `read_api`, у masked і protected CI/CD variable. Токен hook для цього не підходить: `token rotate` ротує його на ноді, і змінна в CI застаріє
- захищений проєкт, де job і змінну бачать лише адміністратори: токен читає налаштування всіх груп
- конфігурація з `audit.stdout: true` і порожнім `audit.file`: події потраплять у лог job

```yaml
# .gitlab-ci.yml
settings-check:
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule"
  script:
    - printf '%s' "$REPOPOLICY_TOKEN" > "$CI_PROJECT_DIR/token"
    - |
      cat > config.yaml <<EOF
      gitlab_url: $CI_SERVER_URL
      token_file: $CI_PROJECT_DIR/token
      breakglass_dir: /nonexistent
      audit: { file: "", stdout: true }
      default_branch: [main]
      settings_check:
        protected_tags: ["v*"]
        job_token_push: false
      EOF
    - gitlab-repopolicy settings-check --config config.yaml
```

Бінарник у job - з перевіреного релізу ([install.md](install.md#1-підготувати-й-перевірити-артефакти)), наприклад в образі runner'а. Job падає за будь-якої розбіжності.

Рекомендований варіант - таймер на ноді плюс алерт з audit: він не потребує ще одного токена з доступом до всіх груп.

## `coverage`

Service account сам не бачить приватних груп, де він не є учасником, і не може про них повідомити. Їх знаходить `coverage` з токеном адміністратора (scope `read_api`):

```sh
sudo gitlab-repopolicy coverage --token-file /root/admin-token
```

Друкує top-level групи без service account (Maintainer) і проєкти з policy-файлами в них. Код виходу 1, якщо такі є. Токен адміністратора лише читається і ніде не зберігається. Запускайте після створення нових груп або за розкладом.

## Оновлення і відкат

- Оновлення - пакетним менеджером. Бінарник замінюється атомарно, запущені hook завершують роботу на старій версії
- на кількох нодах Gitaly версія має бути однаковою; її видно в кожній audit-події (`version`)
- відкат - встановлення попередньої версії. Схеми PATHPOLICY і TAGPOLICY `version: 1` заморожені, тому відкат не робить їх невалідними. COMMITPOLICY з правилом, якого немає в старій версії, втрачає лише це правило (попередження)
- якщо причина збою незрозуміла, спершу `mode: audit` ([configuration.md](configuration.md#mode)): push проходять, рішення пишуться в лог

За наявності тегу попередньої minor-версії `make test-integration` перевіряє її бінарником валідні PATHPOLICY і TAGPOLICY корпусу та `examples/`. Якщо такого тегу немає, перевірка виводить повідомлення про пропуск; це не результат перевірки сумісності двох релізів.

## Типові проблеми

| Симптом | Причина | Що робити |
| --- | --- | --- |
| у кожному push: `internal error: the hook configuration on the server is invalid` | невалідний `config.yaml`, порожній `audit.file` (hook пише лише у файл, `audit.stdout` для нього не діє) або hook не може прочитати конфігурацію (права, каталог 0750 root) | `sudo -u git gitlab-repopolicy selftest` |
| `the default branch of this project is not allowed` у кожному push у проєкт | default branch проєкту не зі списку `default_branch` (код `default_branch_not_allowed`) | Maintainer повертає default branch у Settings > Repository > Branch defaults; якщо нове ім'я правильне, додайте його в `default_branch` |
| `internal error: git is not available to the hook` | git, який Gitaly передає hook, не запускається | версія GitLab і Gitaly, `gitlab-ctl status gitaly` |
| `could not check permissions: the GitLab API did not answer` на захищених змінах | API недоступний або токен прострочений | `selftest`; у audit `reason` |
| те саме, у audit `reason` з `HTTP 429` | спрацював ліміт API GitLab на service account | підняти ліміти ([install.md](install.md#ліміти-api-gitlab)) |
| `this project is not connected to gitlab-repopolicy correctly` | токен відхилено або hook не може його прочитати (токен читається лише при першому зверненні до API, тому push без API проходять), service account не учасник групи або роль нижча за Maintainer | `selftest`; у audit `reason` (`project_not_visible`, `role_below_maintainer`); `coverage` |
| те саме в особистому проєкті | власник проєкту заблокований або забанений: токен service account таких користувачів не бачить, і проєкт вважається груповим | очікувана поведінка, fail closed |
| `the check did not finish within the server limits` | push більший за ліміти: `max_api_calls`, `timeouts` або понад 50 000 комітів, чиї шляхи перевіряє PATHPOLICY | розбити push на частини; якщо push малий, ліміт занизький |
| `internal error: the push was not checked and is rejected` з id | внутрішня помилка бінарника | знайти подію `internal_error` з цим `receive_id` в audit |
| `the audit log is not writable, tell your GitLab administrators` | користувач hook не може писати в `audit.file` | `sudo -u git gitlab-repopolicy selftest`, права каталогу логу |
| у MR або Web IDE лише "pre-receive hook declined" без тексту | відмову дав інший hook у `pre-receive.d/` без рядка `GL-HOOK-ERR` | перевірте сусідні файли в `pre-receive.d/` |
| merge через API: лише "Branch cannot be merged" | API не повертає текст hook у відповіді на merge | поле `merge_error` merge request (`GET /projects/:id/merge_requests/:iid`) |
| hook не викликається | `custom_hooks_dir` не задано або не той, wrapper не виконуваний, ім'я закінчується на `~`, файл лежить як `custom_hooks_dir/pre-receive` | `grep -A2 '^\[hooks\]' /var/opt/gitlab/gitaly/config.toml`, `ls -l <custom_hooks_dir>/pre-receive.d/` |
| усі зміни проєкту відхиляються: `PATHPOLICY in main is broken, so no changes are accepted` | невалідна PATHPOLICY у default branch | Owner виправляє PATHPOLICY (recovery) або break-glass |
| теги відхиляються: `TAGPOLICY in main is broken, so no tag can be created or moved` | невалідна TAGPOLICY у default branch | виправити TAGPOLICY через merge |
| новий тег відхиляється: `the tag must be protected, but the project does not protect it` | правило TAGPOLICY з `protected: true`, а тег не захищено в проєкті (`tag_not_protected`) | Maintainer додає шаблон тегу в Settings > Repository > Protected tags і пушить тег знову |
| нова гілка відхиляється: `the name does not follow the branch standard` або `the branch must be protected, but the project does not protect it` | правило BRANCHPOLICY (`branch_name_violation`, `branch_not_protected`) | перейменувати гілку за `message` правила (`git branch -m`) або, для захисту, Maintainer захищає гілку в Settings > Repository > Protected branches |
| тег відхиляється: `the tag points to a commit that is in no branch` | тег запушили без гілки, на якій лежить його коміт (`tag_not_on_branch`), у будь-якому груповому проєкті | `git push origin <гілка> <тег>` або `git push --follow-tags` |
| попередження `COMMITPOLICY cannot be used` або `BRANCHPOLICY in main cannot be used` у багатьох проєктах | пресета з `extends` немає в конфігу ноди або він зламаний | `selftest` показує зламані пресети; доставте правильний конфіг на всі ноди |
| відмови `PATHPOLICY in main is broken` або `TAGPOLICY in main is broken` після зміни конфігу | зник або зламався пресет, який бере файл | повернути пресет у конфіг: PATHPOLICY і TAGPOLICY без пресета вважаються зламаними |
| merge через UI повільний або `could not check permissions` під піковим навантаженням | web-операції тримають Puma-worker, а hook звертається назад до Rails | `api_calls`, `duration_ms` в audit; `timeouts.http` |

Якщо в конфігурації задано `help.contact`, у відмовах, де користувач нічого не може виправити сам, замість `your GitLab administrators` друкується цей контакт ([configuration.md](configuration.md#help)).

## Перевірити push без push

`gitlab-repopolicy test` запускає перевірку локальних Git-об'єктів без push. Щоб відтворити відмову, підготуйте mirror-копію з refs сервера до push і об'єктами нового коміту або тегу. Далі задайте старе й нове значення ref та користувача:

```sh
git clone --mirror https://gitlab.example.com/acme/app.git app.git
gitlab-repopolicy test --repo app.git --old <old> --new <new> --ref refs/heads/main --user alice --project acme/app --project-id 812
```

`--project` і `--project-id` - шлях і ID справжнього проєкту. З ними `test` питає API про ролі й protected branches саме цього проєкту. Без них береться `local/test/repository` з ID 1, і відповіді API стосуються проєкту 1.

Що потрібно для `test`:
- конфігурація сервера (`--config`, за замовчуванням `/etc/gitlab-repopolicy/config.yaml`) і токен з неї, коли правилам потрібен API. Тому `test` запускають на ноді Gitaly від користувача, який може їх прочитати, наприклад `sudo -u git`
- хто пушить. `--user alice` задає `GL_USERNAME`, а `GL_ID` (`user-<id>`) `test` шукає через API за username. Без API задайте його явно, `--gl-id user-314`, інакше `test` завершиться помилкою `--gl-id is not given and user alice cannot be looked up`
- `--protocol` (`ssh` за замовчуванням, `http` або `web`) - як ніби push прийшов цим способом. Від нього залежать рядок `checked as:` у виводі і `protocol` в audit

Код виходу 0 - ядро допускає зміну в заданому режимі (у `audit` навіть за порушень), і `test` друкує підсумок: `allowed: no rule was touched, 3 new commits` або `allowed: the rules allow the change, 3 new commits`. 1 - відмова з тим самим текстом, що побачив би користувач, або помилка виконання. 2 - помилка у використанні, наприклад не задано обов'язковий прапорець.

Запускайте `test` на mirror-копії, де є лише гілки й теги сервера до перевірюваного оновлення; нові об'єкти додайте без зміни цих refs. Для звичайної робочої гілки набір нових комітів виключає історію, уже досяжну з інших гілок і тегів. У робочому клоні локальна гілка з новими комітами може спорожнити цей набір; тоді `test` друкує підказку про `git clone --mirror`. Для основної, захищених і явно вибраних `match` гілок діють інші межі історії, описані в [PATHPOLICY](../policies/pathpolicy.md) і [COMMITPOLICY](../policies/commitpolicy.md#які-коміти-перевіряються). Повний перелік параметрів містить [довідник test](../reference/cli.md#test).
