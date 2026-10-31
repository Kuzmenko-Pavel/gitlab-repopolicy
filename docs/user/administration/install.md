[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](install.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](install.en.md)

# Встановлення

gitlab-repopolicy ставиться на кожну ноду, де працює Gitaly, і підключається до GitLab як глобальний server hook. Пакет кладе файли лише у свої каталоги і GitLab не змінює: hook вмикає адміністратор окремою командою.

## Вимоги

- GitLab Self-Managed, CE або EE, будь-який tier. Підтримувані версії - у таблиці [README](../../../README.md). GitLab.com і GitLab Dedicated не підтримуються: server hooks там недоступні
- Linux `amd64` або `arm64`, права root на нодах Gitaly
- instance service account із токеном і членством у групах за кроком 3; e2e перевіряє цей сценарій на CE 19.2-19.4

| Тип інсталяції | Підтримка |
| --- | --- |
| Linux package (Omnibus), одна нода | так |
| Docker-образ GitLab | так |
| Self-compiled | так |
| Gitaly на окремих нодах, Gitaly Cluster (Praefect) | так, бінарник і конфігурація на кожній ноді Gitaly |
| Helm chart (Kubernetes) | ні |
| Geo secondary | не потрібно: Geo не реплікує server hooks, push іде в primary |

Перевірено e2e лише на Linux package в Docker-образі. Інші типи описані за документацією GitLab і не входять у матрицю тестів.

## Порядок встановлення

Для Linux package виконайте кроки 1-7 послідовно. Перевірка артефактів передує встановленню; service account, токен і конфігурація мають бути готові до підключення wrapper. Саме `install-hook`, а не встановлення пакета, починає перевірку push.

Перед цим прочитайте [мінімальну конфігурацію](configuration.md#мінімальна-конфігурація). На робочому сервері задайте `mode: audit` до підключення hook: так порушення політик спочатку потрапляють лише до журналу. `selftest` після підключення запускається від root і git, а контрольний push перевіряє фактичний виклик Gitaly.

Для контейнера використайте [окремий спосіб Docker](#docker), щоб бінарник переживав перестворення контейнера. Для розподіленого сховища застосуйте ті самі кроки на [кожному вузлі Gitaly](#окремі-ноди-gitaly-і-gitaly-cluster).

## 1. Підготувати й перевірити артефакти

Для локальної перевірки встановлення зберіть артефакти з клону цього репозиторію:

```sh
make release-snapshot
```

Архіви й пакети будуть у `dist/`. Ця збірка не публікується й не має release-підпису. У командах нижче `1.0.0` і `v1.0.0` є прикладами версії й тегу; фактичне ім'я беріть із результату збірки.

Процедура [випуску релізу](../../development/release.md) передбачає публікацію підписаних артефактів у [GitHub Releases](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases). Таблиця й команди нижче описують перевірку такого комплекту, коли він опублікований. Не застосовуйте перевірку cosign до непідписаного snapshot.

| Файл | Що це |
| --- | --- |
| `gitlab-repopolicy_<версія>_linux_<arch>.tar.gz` | статичний бінарник, `LICENSE`, README |
| `gitlab-repopolicy_<версія>_linux_<arch>.deb`, `.rpm` | пакет |
| `*.spdx.json` | SBOM кожного архіву |
| `checksums.txt` | SHA-256 усіх файлів |
| `checksums.txt.sigstore.json` | keyless-підпис `checksums.txt` (cosign) |

Перевірте підпис і суми до встановлення:

```sh
cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --check --ignore-missing checksums.txt
```

Необов'язково - зв'язок артефакту з комітом і workflow збірки (SLSA provenance):

```sh
gh attestation verify gitlab-repopolicy_1.0.0_linux_amd64.tar.gz --repo Kuzmenko-Pavel/gitlab-repopolicy
```

Збірка відтворювана: той самий тег дає побайтово той самий бінарник.

## 2. Встановити пакет

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb      # Debian, Ubuntu
sudo dnf install ./gitlab-repopolicy_1.0.0_linux_amd64.rpm      # RHEL, Rocky, Alma
```

У метаданих пакета версія з тильдою для pre-release (`1.0.0~rc.1`), щоб пакетний менеджер вважав її старшою за попередній реліз і молодшою за `1.0.0`.

Що кладе пакет:

```text
/usr/bin/gitlab-repopolicy                         бінарник
/etc/gitlab-repopolicy/config.yaml                 конфігурація, root:<git-user> 0640, conffile
/etc/gitlab-repopolicy/breakglass/                 root 0755
/var/log/gitlab-repopolicy/                        <git-user> 0750
/etc/logrotate.d/gitlab-repopolicy                 ротація audit-логу, 0644, conffile
/usr/lib/systemd/system/gitlab-repopolicy-*.{service,timer}   вимкнені таймери
```

`<git-user>` - користувач, від якого Gitaly запускає hooks: у Linux package це `git`. Під час першого встановлення пакет бере його зі змінної `GITLAB_REPOPOLICY_USER` (за замовчуванням `git`). Якщо такого користувача ще немає (GitLab не встановлено), пакет лише попереджає, а власників потім призначає `install-hook --user`.

Оновлення не змінює права, які виставив адміністратор, і не перезаписує змінений `config.yaml`: dpkg запитає, яку версію залишити (з `--force-confold` залишить вашу, а нову покладе поруч як `.dpkg-dist`), rpm залишить вашу, а нову покладе як `.rpmnew`.

У Docker-образ GitLab пакет не ставиться, див. розділ [Docker](#docker).

**Без пакета** (архів, інші дистрибутиви, Docker): розпакуйте `gitlab-repopolicy` в `/usr/bin/` і створіть ті самі каталоги й файли з тими самими правами. Зразок `config.yaml` - [config/config.yaml](../../../config/config.yaml). Ротацію audit-логу налаштуйте самі, наприклад так само, як пакет:

```text
# /etc/logrotate.d/gitlab-repopolicy
/var/log/gitlab-repopolicy/audit.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    nocreate
}
```

`nocreate`: наступний `audit.log` створюють hook і команди від root з власником каталогу логу, тому logrotate його не створює. `su` не потрібен: каталог `0750` належить користувачу Gitaly. Шлях у файлі - `audit.file` за замовчуванням, якщо ви його змінили, змініть і тут. У Docker цей файл кладуть на хості, для каталогу, змонтованого в `/var/log/gitlab-repopolicy`.

## 3. Service account і токен

Hook читає з API GitLab ролі користувачів, захищені гілки і SSH-ключі. Для цього потрібен окремий обліковий запис з мінімальними правами:

| Що | Вимога |
| --- | --- |
| обліковий запис | instance service account (Admin area → Service accounts). Не людина: не входить в UI і не залежить від співробітників |
| членство | **Maintainer** у кожній top-level групі. Особисті namespace hook не перевіряє |
| токен | personal access token зі scopes рівно `read_api` і `self_rotate` |
| строк | не більше максимуму інстансу. Ротацію робить `token rotate` за таймером |

Інші типи облікових записів, ролі й scopes не підтримуються: `selftest` їх відхиляє. Прав адміністратора service account не має.

Через API, токеном адміністратора:

```sh
GITLAB=https://gitlab.example.com
ADMIN_TOKEN=...      # токен адміністратора зі scope api, лише для цих кроків

# service account
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" -X POST "$GITLAB/api/v4/service_accounts" \
  --data name=gitlab-repopolicy --data username=gitlab-repopolicy
# id з відповіді
SA_ID=...

# токен: scopes read_api і self_rotate, строк не більше максимуму інстансу
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" -X POST "$GITLAB/api/v4/users/$SA_ID/personal_access_tokens" \
  --data name=gitlab-repopolicy --data 'scopes[]=read_api' --data 'scopes[]=self_rotate' \
  --data expires_at="$(date -d '+364 days' +%F)"

# Maintainer у кожній top-level групі
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" -X POST "$GITLAB/api/v4/groups/<group-id>/members" \
  --data user_id="$SA_ID" --data access_level=40
```

Запишіть токен на кожну ноду Gitaly:

```sh
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'     # вставте токен, Enter, Ctrl-D
```

Файл отримає режим 0440, а групу користувача git йому призначить `install-hook --user` на кроці 5: разом виходить `root:git 0440`. Hook токен лише читає, змінює його тільки `token rotate` від root.

Нова top-level група - не забудьте додати до неї service account. Без цього захищені зміни в її проєктах отримають відмову `authorization_backend_failure`. Знайти групи без service account допоможе `coverage` з токеном адміністратора (токен на ноді не зберігається):

```sh
sudo gitlab-repopolicy coverage --token-file /root/admin-token
```

`coverage` друкує top-level групи без service account і проєкти з policy-файлами в них; код виходу 1, якщо такі є.

### Ліміти API GitLab

Hook ходить в API одним service account на весь інстанс. GitLab обмежує частину запитів API кількістю на одного користувача за хвилину, тому всі push інстансу ділять ці ліміти service account. До API hook звертається не в кожному push, а лише коли правилу потрібна відповідь GitLab: зміна файлів правила PATHPOLICY, `protected: true`, перевірка SSH-ключа. Скільки запитів зробив push, видно в полі `api_calls` подій audit.

| Запит hook | Налаштування GitLab | За замовчуванням, на хвилину | Розділ в Admin > Settings > Network |
| --- | --- | --- | --- |
| `GET /projects/:id`, один раз на push із запитами до API | `project_api_limit` | 400 | Projects API rate limits |
| `GET /projects/:id/members/all`, учасники для `allow` | `project_members_api_limit` | 200 | Projects API rate limits |
| `GET /users/:id/keys/:key_id`, чи ключ SSH особистий | `users_api_limit_ssh_key` | 120 | Users API rate limit |

Коли ліміт спрацьовує, GitLab відповідає 429, і hook вважає, що API не відповів. Push отримує ту саму відмову, що й при недоступному API: `could not check permissions: the GitLab API did not answer`. У полі `reason` події audit буде `HTTP 429`, а в `/var/log/gitlab/gitlab-rails/auth.log` - запис `Application_Rate_Limiter_Request` з `username` service account.

Ліміти задаються на весь інстанс, окремо для service account їх не змінити. Якщо push із запитами до API за хвилину більше, ніж ці ліміти, підніміть їх в Admin > Settings > Network або через API. Значення `0` вимикає ліміт для всіх користувачів:

```sh
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" -X PUT "$GITLAB/api/v4/application/settings" \
  --data project_api_limit=2000 --data project_members_api_limit=1000 --data users_api_limit_ssh_key=1000
```

На інстансі, оновленому до GitLab 18.0 або новішого, GitLab міг сам поставити цим лімітам `0`. Поточні значення показує `GET /api/v4/application/settings`.

## 4. Конфігурація

Відкрийте `/etc/gitlab-repopolicy/config.yaml` перед підключенням hook. Для Linux package на одній ноді поставка використовує unix socket Workhorse. Перевірте `mode: audit` і `default_branch`: поставка містить `[main, master]`, що обмежує ім'я основної гілки всіх групових проєктів. Для запуску без цього глобального обмеження використайте `[]`. Якщо Gitaly на окремій ноді, вкажіть HTTPS-адресу GitLab і за потреби CA:

```yaml
gitlab_url: https://gitlab.example.com
ca_file: /etc/gitlab-repopolicy/ca.crt
```

Параметри та відмінність програмних defaults від поставки пояснює [конфігурація сервера](configuration.md). Відредагуйте файл командою `sudoedit /etc/gitlab-repopolicy/config.yaml` до наступного кроку.

У першому запуску використайте `mode: audit`: рішення ядра записуються з `"enforced": false` без блокування за policy. Це не усуває помилок запуску, наприклад нечитабельного конфігу. Переходьте до `enforce` після перевірки доступів і журналу за [порядком інтеграції](integration-guide.md).

## 5. Підключити hook

Gitaly запускає глобальні hooks із каталогу `custom_hooks_dir`. Спершу задайте його в конфігурації GitLab:

| Інсталяція | Де задається `custom_hooks_dir` |
| --- | --- |
| Linux package | `/etc/gitlab/gitlab.rb`, далі `sudo gitlab-ctl reconfigure` |
| Docker | `GITLAB_OMNIBUS_CONFIG` або `gitlab.rb` у volume; каталог - у persistent volume |
| Self-compiled | `[hooks] custom_hooks_dir` у `gitaly/config.toml` |
| окремі ноди Gitaly, Gitaly Cluster | на кожній ноді Gitaly; у Gitaly Cluster hooks виконуються лише на primary |

Linux package:

```ruby
# /etc/gitlab/gitlab.rb
gitaly['configuration'] = {
  hooks: {
    custom_hooks_dir: '/var/opt/gitlab/gitaly/custom_hooks',
  },
}
```

Якщо `gitaly['configuration']` у `gitlab.rb` уже є, додайте ключ `hooks` у наявний хеш, а не в новий.

```sh
sudo gitlab-ctl reconfigure
```

`reconfigure` записує каталог у конфігурацію Gitaly, але сам каталог не створює. Його створює `install-hook`, якщо існує батьківський каталог (так помилка в шляху не створить зайвий каталог):

```sh
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
```

`install-hook`:
- створює `custom_hooks_dir/pre-receive.d/50-gitlab-repopolicy` - wrapper, який запускає `/usr/bin/gitlab-repopolicy hook pre-receive --config /etc/gitlab-repopolicy/config.yaml`. Власник - `--user`, режим 0755
- встановлює групу `config.yaml` і токена та власника каталогу audit-логу для `--user`. Якщо права каталогу логу ширші за 0750 або файлу audit ширші за 0640, звужує їх
- записує шлях `custom_hooks_dir` у `/var/lib/gitlab-repopolicy/hooks`, щоб видалення пакета прибрало wrapper
- повторний запуск нічого не змінює і так і пише: `unchanged`, `owner unchanged (0:998)`, `mode unchanged (0750)`. Інші файли в `pre-receive.d/` не чіпає

Gitaly виконує файли `pre-receive.d/` за алфавітом і зупиняється на першому, що завершився з помилкою. Окремий файл `custom_hooks_dir/pre-receive` поруч із `pre-receive.d/` Gitaly не виконує: свої hooks кладіть у `pre-receive.d/`. Файли з `~` у кінці імені Gitaly пропускає.

## 6. Перевірити

```sh
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

`selftest` перевіряє конфігурацію, модулі, audit-лог, каталог break-glass, файл токена, service account (`bot`, `external`, не адміністратор), scopes і строк токена, доступ до API і кількість груп, де service account має роль Maintainer. Будь-який `FAIL` - код виходу 1. Якщо service account не входить у жодну групу, рядок `groups` - `FAIL`: hook не зможе перевірити жоден груповий проєкт. Які саме групи пропущено, покаже `coverage`.

Запускайте `selftest` і від root, і від користувача git: hook працює від git, і лише так видно, що він може прочитати конфігурацію і токен. Наприклад, каталог `/etc/gitlab-repopolicy` з правами 0750 root не помітить жодна перевірка від root, а hook відмовлятиме кожному push з текстом "internal error: the hook configuration on the server is invalid". Нечитаємий токен hook помітить лише під час першого звернення до API: push, яким API не потрібен, пройдуть, а захищені зміни отримають "this project is not connected to gitlab-repopolicy correctly".

```text
$ sudo -u git gitlab-repopolicy selftest
info version     gitlab-repopolicy 1.0.0
ok   config      /etc/gitlab-repopolicy/config.yaml, mode enforce
ok   modules     enabled: pathpolicy, commitpolicy, tagpolicy, branchpolicy
ok   audit       /var/log/gitlab-repopolicy/audit.log does not exist yet; the directory belongs to 998:998
ok   breakglass  /etc/gitlab-repopolicy/breakglass, no active grants
ok   api         http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket reachable as gitlab-repopolicy (id 2)
ok   account     gitlab-repopolicy is a service account
ok   groups      Maintainer of 3 groups; gitlab-repopolicy coverage checks every top-level group
ok   token       scopes read_api, self_rotate, expires on 2027-10-13
info git         git version 2.55.0-rc1.g02bb39c in PATH of selftest; the hook uses the git of Gitaly
```

Рядок `git` довідковий: hook використовує git, який йому передає Gitaly, і саме його версію пише в audit.

Далі перевірте виклик hook: додайте PATHPOLICY у тестовий проєкт і зробіть push, який її порушує ([pathpolicy.md](../policies/pathpolicy.md)). У `audit` push має пройти, а очікувана відмова з `enforced: false` має бути в `/var/log/gitlab-repopolicy/audit.log`. Відмову у виводі push перевіряють лише після контрольованого переходу в `enforce`; глобально він впливає на весь інстанс.

## 7. Таймери

Пакет ставить два systemd-таймери вимкненими:

| Таймер | Що робить | Як часто |
| --- | --- | --- |
| `gitlab-repopolicy-token-rotate.timer` | `token rotate`: ротація токена за 30 днів до кінця строку | щодня |
| `gitlab-repopolicy-settings-check.timer` | `settings-check`: звірка налаштувань проєктів і перевірка, що hook може писати audit ([operations.md](operations.md#settings-check)) | щодня |

```sh
sudo systemctl enable --now gitlab-repopolicy-token-rotate.timer gitlab-repopolicy-settings-check.timer
```

Обидва таймери запускаються раз на добу з випадковою затримкою до години. Інший розклад задається drop-in файлом ([operations.md](operations.md#settings-check)).

Обидві служби працюють від root в обмеженому оточенні systemd: на запис лише свої каталоги, без нових привілеїв, з двома capabilities - `CAP_CHOWN` (власник токена й нового audit-логу) і `CAP_DAC_OVERRIDE` (каталог логу належить користувачу hook).

## Docker

**Пакет у контейнер GitLab не ставте.** Пакет живе в шарі контейнера, а wrapper hook лежить у `custom_hooks_dir`, тобто в persistent volume. Після `docker compose up --force-recreate` (так виглядає будь-яке оновлення GitLab у Docker) бінарника вже немає, а wrapper залишився. Тоді GitLab відхиляє кожен push інстансу з `exec: /usr/bin/gitlab-repopolicy: not found`. Таймери systemd у контейнері теж не працюють: в образі GitLab немає systemd.

Тому все, що має пережити перестворення контейнера, лежить на хості:

| На хості | У контейнері | Чому так |
| --- | --- | --- |
| каталог з бінарником, наприклад `/opt/gitlab-repopolicy/bin/` | `/opt/gitlab-repopolicy/bin`, read-only | монтується каталог, а не файл: новий бінарник, покладений на хості, одразу видно в контейнері |
| `/srv/gitlab-repopolicy/etc/` з `config.yaml` і `token` | `/etc/gitlab-repopolicy` | `token rotate` пише новий токен поруч і перейменовує його поверх старого, у змонтованому окремо файлі так не вийде |
| volume або каталог | `/var/log/gitlab-repopolicy` | audit-лог |
| volume даних GitLab | `/var/opt/gitlab` | там уже лежить `custom_hooks_dir` |

Фрагмент `compose.yaml`:

```yaml
services:
  gitlab:
    container_name: gitlab
    image: gitlab/gitlab-ce:19.4.2-ce.0
    environment:
      GITLAB_OMNIBUS_CONFIG: |
        gitaly['configuration'] = {
          hooks: { custom_hooks_dir: '/var/opt/gitlab/gitaly/custom_hooks' }
        }
    volumes:
      - gitlab-config:/etc/gitlab
      - gitlab-logs:/var/log/gitlab
      - gitlab-data:/var/opt/gitlab
      - /opt/gitlab-repopolicy/bin:/opt/gitlab-repopolicy/bin:ro
      - /srv/gitlab-repopolicy/etc:/etc/gitlab-repopolicy
      - repopolicy-log:/var/log/gitlab-repopolicy

volumes:
  gitlab-config:
  gitlab-logs:
  gitlab-data:
  repopolicy-log:
```

Установка. Бінарник з архіву релізу кладеться в `/opt/gitlab-repopolicy/bin/`, `config.yaml` зі зразка [config/config.yaml](../../../config/config.yaml) і токен кладуться в `/srv/gitlab-repopolicy/etc/`. Перед підключенням wrapper задайте `mode: audit` у змонтованому `config.yaml` і перевірте приклад `default_branch`. Далі в контейнері:

```sh
docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy install-hook \
  --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git \
  --binary /opt/gitlab-repopolicy/bin/gitlab-repopolicy
docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy selftest
docker exec -u git gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy selftest
```

`--binary` тут обов'язковий. Без нього wrapper запускає `/usr/bin/gitlab-repopolicy`, якого в контейнері немає, і GitLab одразу відхиляє кожен push з `exec: /usr/bin/gitlab-repopolicy: not found`. Коли бінарник лежить у змонтованому з хоста каталозі, перестворення контейнера його не прибирає, і ця помилка не з'являється.

`docker exec` працює від root контейнера, `docker exec -u git` - від користувача, яким Gitaly запускає hooks. `sudo` в образі GitLab немає, тому команди з цієї документації виду `sudo -u git gitlab-repopolicy ...` у Docker виглядають як `docker exec -u git gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy ...`.

**Таймери - на хості.** systemd timer хоста викликає команду в контейнері:

```ini
# /etc/systemd/system/gitlab-repopolicy-token-rotate.service
[Unit]
Description=Rotate the token of the gitlab-repopolicy service account

[Service]
Type=oneshot
ExecStart=/usr/bin/docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy token rotate

# /etc/systemd/system/gitlab-repopolicy-token-rotate.timer
[Timer]
OnCalendar=daily
RandomizedDelaySec=1h
Persistent=true

[Install]
WantedBy=timers.target
```

Для `settings-check` так само: служба з `ExecStart=... gitlab-repopolicy settings-check` і такий самий таймер. Те саме через cron хоста:

```text
# /etc/cron.d/gitlab-repopolicy
17 3 * * * root docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy token rotate
47 3 * * * root docker exec gitlab /opt/gitlab-repopolicy/bin/gitlab-repopolicy settings-check
```

Без ротації токен перестане діяти після свого строку, і всі захищені зміни отримуватимуть відмову `authorization_backend_failure`.

**Оновлення gitlab-repopolicy.** Новий бінарник кладеться поруч під тимчасовим ім'ям і перейменовується поверх старого. Push, які вже йдуть, доробляє старий бінарник, наступні беруть новий:

```sh
install -m 0755 gitlab-repopolicy /opt/gitlab-repopolicy/bin/.gitlab-repopolicy.new
mv /opt/gitlab-repopolicy/bin/.gitlab-repopolicy.new /opt/gitlab-repopolicy/bin/gitlab-repopolicy
```

Після цього `selftest` від root і від git.

**Оновлення GitLab.** Перестворення контейнера бінарник, конфігурацію, токен і `custom_hooks_dir` не зачіпає. Після оновлення - `selftest` від root і від git.

## Окремі ноди Gitaly і Gitaly Cluster

- бінарник, `config.yaml`, токен і `install-hook` - на кожній ноді Gitaly, версія всюди однакова. Її видно в кожній audit-події
- `gitlab_url` - HTTPS-адреса Rails, `ca_file` - якщо сертифікат не з системного сховища
- кожен вузол використовує власний токен або отримує новий файл після централізованої ротації. Незалежна ротація одного спільного токена з кількох вузлів зламає доступ на решті ([ротація](operations.md#ротація-токена))
- break-glass діє на тій ноді, де його видали: на кожній ноді, де може виконуватися hook

## Для розробників

Для локальної перевірки policy-файлів і повідомлень (`validate`, `lint-commit`) достатньо бінарника. У клоні цього репозиторію зберіть його й перевірте policy-файли:

```sh
make code-build
./bin/gitlab-repopolicy validate
```

Така збірка не підписана. На сервери ставте лише перевірені артефакти релізу. [Посібник validate](../reference/validate.md) пояснює локальні перевірки та CI, а [довідник CLI](../reference/cli.md) містить параметри всіх команд.

## Вимкнути і видалити

Швидко вимкнути перевірки без видалення - `mode: off` у конфігурації ([operations.md](operations.md)).

Видалення пакета - це явне вимкнення перевірок, і пакет прибирає за собою:

```sh
sudo apt remove gitlab-repopolicy        # або dnf remove
```

```text
gitlab-repopolicy: the hook is removed from /var/opt/gitlab/gitaly/custom_hooks: pushes are no longer checked
```

| Дія | Що відбувається |
| --- | --- |
| оновлення пакета | нічого не змінюється: hook, таймери, конфігурація й токен залишаються |
| `apt remove`, `dnf remove` | таймери вимикаються, wrapper видаляється з кожного `custom_hooks_dir`, куди його ставив `install-hook`. `config.yaml`, токен і break-glass залишаються |
| `apt purge` | ще видаляються `/etc/gitlab-repopolicy` (конфігурація, токен, break-glass) і `/var/lib/gitlab-repopolicy`. У rpm такого кроку немає, ці каталоги видаляються вручну |
| будь-яка | audit-лог `/var/log/gitlab-repopolicy` не видаляється ніколи: це запис рішень hook |

Якщо пакет не зміг прибрати wrapper, він пише `remove <dir>/pre-receive.d/50-gitlab-repopolicy by hand`. Тоді видаліть файл вручну або запустіть `gitlab-repopolicy uninstall-hook --hooks-dir <dir>` до видалення пакета. Wrapper без бінарника відхиляє кожен push інстансу. Без пакета (Docker, архів) wrapper завжди прибирається вручну через `uninstall-hook`.

Після видалення з усіх нод відкличте токен service account, щоб він не залишився діяти до кінця строку. Токеном адміністратора:

```sh
# id токена
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" "$GITLAB/api/v4/personal_access_tokens?user_id=$SA_ID&state=active"
curl --fail -sS -H "PRIVATE-TOKEN: $ADMIN_TOKEN" -X DELETE "$GITLAB/api/v4/personal_access_tokens/<token-id>"
```

Або видаліть сам service account у Admin area → Service accounts: разом з ним зникнуть його токени й членство в групах.

## Часті питання

**Чи можна поставити пакет у контейнер GitLab?** Ні: після перестворення контейнера бінарник зникне, а wrapper залишиться, і GitLab почне відхиляти кожен push. Як ставити в Docker - розділ [Docker](#docker).

**Що audit повідомляє про результат push?** У всіх подій одного запуску hook однаковий `receive_id`. Подія `"decision":"deny"` з `"enforced":true` означає відмову цього hook для всього push, навіть якщо порушення стосується одного ref. Дозвіл hook ще не підтверджує оновлення refs: наступний hook або GitLab може відхилити операцію. [Опис audit-подій](operations.md#audit) допоможе зіставити рішення й діагностику.

**Як помітити, що токен перестав діяти?** Кожна відмова через збій API пише подію `authorization_backend_failure`, зокрема коли GitLab відповідає 401 на токен. Алерт на серію таких подій ловить прострочений або відкликаний токен ([operations.md](operations.md#події-й-алерти)).

**`selftest` пише `ok`, а проєкти групи не перевіряються.** Service account не доданий у top-level групу. `selftest` показує кількість груп у рядку `groups`, а які саме пропущено, покаже `coverage` (крок 3).

**Commit-msg hook відхиляє `feat:`, хоча в main це дозволено.** `lint-commit` без `--rule` застосовує всі правила файлу, бо локально невідомо, у яку гілку піде коміт. Задайте правило гілок, куди йде робота: `lint-commit --rule conventional` ([commitpolicy.md](../policies/commitpolicy.md#перевірка-без-push)).

**Очікування `settings_check` одні на весь інстанс, а в infra-проєктів немає тегів.** `protected_tags` перевіряється лише за наявності PATHPOLICY і TAGPOLICY в основній гілці. Самої відсутності тегів недостатньо для пропуску: команда звіряє шаблони захисту в Settings. У проєктах із PATHPOLICY перевірка `ci_config_path` потрібна й тоді, коли стандартного `.gitlab-ci.yml` немає ([конфігурація settings_check](configuration.md#settings_check)).

**`gitlab-repopolicy test` нічого не знайшов у новій гілці.** Для звичайної робочої гілки перевірка може враховувати лише коміти, яких ще немає в жодній гілці чи тезі репозиторію. У робочому клоні вони часто вже доступні через іншу локальну гілку, тому `test` не відтворює стан сервера до push. Це не загальний виняток для всіх нових гілок: набір змін залежить від класу гілки й `match`. Використайте mirror-копію refs сервера за [інструкцією відтворення push](operations.md#перевірити-push-без-push).

**Де взяти ID проєкту для break-glass?** `breakglass grant` приймає і шлях `--project group/project`. Якщо API недоступний, потрібен `--project-id`: ID показано на головній сторінці проєкту під його назвою.

**Merge через API відповідає лише "Branch cannot be merged".** Причину відмови GitLab записує в поле `merge_error` merge request (`GET /projects/:id/merge_requests/:iid`), в UI вона видна на сторінці MR.

**Каталог `/var/log/gitlab-repopolicy` створено заздалегідь з правами 0755.** `install-hook` звужує його до 0750, а файл audit до 0640, і пише, що змінив. `selftest` попереджає, якщо audit може читати будь-хто.
