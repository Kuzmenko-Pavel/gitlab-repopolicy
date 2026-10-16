[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](operations.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](operations.en.md)

# Експлуатація

## Audit

Hook пише кожне рішення, яке зачепило правило, у `audit.file` (за замовчуванням `/var/log/gitlab-repopolicy/audit.log`): один JSON-об'єкт на рядок, до 4 KiB на подію. Push, який не зачепив жодного правила, не логується, інакше був би рядок на кожен push інстансу.

```json
{"ts":"2026-10-14T09:14:02.418Z","event":"decision","module":"pathpolicy","decision":"deny","enforced":true,"code":"protected_path_violation","project_id":812,"project_path":"example/payments/deployment","user_id":314,"username":"alice","gl_id":"user-314","protocol":"web","ref":"refs/heads/main","oldrev":"9f1c...","newrev":"b77a...","anchor_rev":"9f1c...","policy_blob":"3e5d...","violations":[{"rule":"ci-config","code":"protected_path_violation","paths":[".gitlab-ci.yml"],"reason":"not in allow: alice"}],"api_calls":2,"duration_ms":143,"version":"1.0.0","git":"2.55.0","node":"gitaly-01"}
```

| Поле | Значення |
| --- | --- |
| `ts` | час UTC |
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
| `api_calls`, `duration_ms` | вартість рішення |
| `version`, `git`, `node` | версія gitlab-repopolicy, git, який бачив hook, і нода |

Для одного push у модуля може бути кілька подій: по одній на кожну гілку чи тег із порушеннями.

### Події й алерти

| `event` | Коли | Алерт |
| --- | --- | --- |
| `decision` | allow, deny або warn, коли зачеплено правило | ні |
| `policy_change` | створення, зміна або видалення policy-файлу | ні |
| `policy_bootstrap` | перша PATHPOLICY у проєкті | ні |
| `policy_recovery` | Owner змінив PATHPOLICY через recovery | **так, кожна** |
| `invalid_policy` | PATHPOLICY або TAGPOLICY у default branch невалідна | **так** |
| `invalid_commit_policy` | COMMITPOLICY у default branch невалідна (перевірку пропущено) | ні |
| `authorization_backend_failure` | API недоступний на захищеній операції, токен не діє, service account не бачить проєкт (`reason`: `project_not_visible`, `role_below_maintainer`) | так, якщо їх кілька за 5 хвилин: одну подію дає короткий збій API, серію - прострочений токен, група без service account або недоступний API. Поріг під своє навантаження вибирає інсталяція |
| `unknown_principal` | deploy key або нерозбірний `GL_ID` на захищеному шляху | ні |
| `breakglass` | дозвіл break-glass виданий (`action: grant`), відкликаний (`action: revoke`) або використаний hook | **так** |
| `settings_drift` | `settings-check` знайшов розбіжність | **так** |
| `internal_error` | внутрішня помилка бінарника, збій `token rotate` (`action: token_rotate`) | **так** |

Окремо варто стежити за `decision` з `code: invalid_policy_update` у `mode: audit`: таку версію policy hook у `enforce` не пропустив би, а після перемикання вона заблокує проєкт.

### Доставка і ротація логу

Куди відправляти лог (Fluent Bit, Vector, journald, SIEM), вирішує інсталяція. Hook відкриває файл на кожен запис з `O_APPEND`, тому logrotate не потребує `copytruncate`. Пакет конфігурацію logrotate не ставить, приклад:

```text
# /etc/logrotate.d/gitlab-repopolicy
/var/log/gitlab-repopolicy/audit.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
}
```

Новий файл створить hook від користувача git. Інші команди від root створюють відсутній файл з власником каталогу логу, тож hook не втратить права на запис.

## Recovery

Recovery вмикається, коли в PATHPOLICY не залишилося жодного effective admin: усіх видалили з проєкту, заблокували або понизили нижче Maintainer. Тоді Owner проєкту (зокрема Owner батьківської групи) може змінити PATHPOLICY. Правила - [pathpolicy.md](pathpolicy.md#admins-перша-policy-і-recovery).

```sh
sudo gitlab-repopolicy admins --project group/project
```

показує admins із файлу, effective admins і чи ввімкнено recovery. Збій API - помилка, а не "0 admins".

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
sudo gitlab-repopolicy breakglass grant --project-id 812 --ttl 30m --reason "INC-123 policy lockout"
sudo gitlab-repopolicy breakglass list
sudo gitlab-repopolicy breakglass revoke --project-id 812
```

- `--ttl` обов'язковий, не більше 24 годин. Після строку дозвіл ігнорується
- поки дозвіл діє, усі модули пропускають push у проєкт, у виводі push є попередження, кожен такий push - подія `breakglass`
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

Для кожного проєкту з PATHPOLICY, який бачить service account:

| Перевірка (`violations[].rule`) | Що вважається розбіжністю |
| --- | --- |
| `default_branch` | default branch не та, що в `settings_check.default_branch` |
| `protected_branches`, `protected_tags` | очікувані гілки або шаблони тегів не захищені |
| `job_token_push` | дозвіл push через `CI_JOB_TOKEN` не такий, як очікується |
| `ci_config_path` | CI-конфігурація в іншому проєкті, за URL або не покрита path-правилом PATHPOLICY |
| `principals` | username з policy-файлів більше не активні користувачі GitLab |
| `access` | роль service account нижча за Maintainer: налаштування не видно |
| `policy` | policy-файл не розбирається |
| `unavailable` | API не відповів для цього проєкту |

Розбіжність - подія `settings_drift` на проєкт і код виходу 1. Проєкт з PATHPOLICY шукається і на поточній default branch, і на очікуваній: перемикання default на гілку без PATHPOLICY - розбіжність, а не "проєкт без policy".

```text
$ sudo -u git gitlab-repopolicy settings-check
acme/app: protected_tags: not protected: v*
```

Це виявлення за хвилини, а не запобігання: зміну, яку повернули між двома запусками, `settings-check` не побачить. Повністю цю діру закриває лише організаційний захід - роль Maintainer тільки у власників захищених шляхів.

### На ноді за таймером

```sh
sudo systemctl enable --now gitlab-repopolicy-settings-check.timer
journalctl -u gitlab-repopolicy-settings-check.service
```

Запуск кожні 15 хвилин, той самий токен, що в hook, події в audit-лог. Алерт будується з audit (`settings_drift`).

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
      settings_check:
        default_branch: main
        protected_branches: [main]
        protected_tags: ["v*"]
        job_token_push: false
      EOF
    - gitlab-repopolicy settings-check --config config.yaml
```

Бінарник у job - з перевіреного релізу ([install.md](install.md#1-завантажити-й-перевірити-реліз)), наприклад в образі runner'а. Job падає за будь-якої розбіжності.

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

Кожен реліз перевіряє, що попередня minor-версія приймає всі валідні PATHPOLICY і TAGPOLICY з корпусу тестів і з `examples/`.

## Типові проблеми

| Симптом | Причина | Що робити |
| --- | --- | --- |
| усі push у групові проєкти: `push could not be verified` | hook не може прочитати конфігурацію або токен (права, каталог 0750 root), git недоступний | `sudo -u git gitlab-repopolicy selftest` |
| `the hook configuration on the server is invalid` | невалідний `config.yaml` або порожній `audit.file` (hook пише лише у файл, `audit.stdout` для нього не діє) | `sudo gitlab-repopolicy selftest` |
| `unable to verify authorization` на захищених змінах | API недоступний, токен прострочений, service account не учасник групи або роль нижча за Maintainer | `selftest`; у audit `reason` (`project_not_visible`, `role_below_maintainer`); `coverage` |
| те саме в особистому проєкті | власник проєкту заблокований або забанений: токен service account таких користувачів не бачить, і проєкт вважається груповим | очікувана поведінка, fail closed |
| у MR або Web IDE лише "pre-receive hook declined" без тексту | відмову дав інший hook у `pre-receive.d/` без рядка `GL-HOOK-ERR` | перевірте сусідні файли в `pre-receive.d/` |
| hook не викликається | `custom_hooks_dir` не задано або не той, wrapper не виконуваний, ім'я закінчується на `~`, файл лежить як `custom_hooks_dir/pre-receive` | `grep -A2 '^\[hooks\]' /var/opt/gitlab/gitaly/config.toml`, `ls -l <custom_hooks_dir>/pre-receive.d/` |
| усі зміни проєкту відхиляються з `policy is invalid` | невалідна PATHPOLICY у default branch | Owner виправляє PATHPOLICY (recovery) або break-glass |
| теги відхиляються з `policy is invalid` | невалідна TAGPOLICY у default branch | виправити TAGPOLICY через merge |
| merge через UI повільний або `unable to verify authorization` під піковим навантаженням | web-операції тримають Puma-worker, а hook звертається назад до Rails | `api_calls`, `duration_ms` в audit; `timeouts.http` |

`gitlab-repopolicy test` приймає рішення hook на локальному репозиторії без push - зручно, щоб відтворити відмову:

```sh
gitlab-repopolicy test --repo /path/to/clone --old <old> --new <new> --ref refs/heads/main --user alice
```
