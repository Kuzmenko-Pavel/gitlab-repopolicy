[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](install.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](install.en.md)

# Встановлення

gitlab-repopolicy ставиться на кожну ноду, де працює Gitaly, і підключається до GitLab як глобальний server hook. Пакет кладе файли лише у свої каталоги і GitLab не змінює: hook вмикає адміністратор окремою командою.

## Вимоги

- GitLab Self-Managed, CE або EE, будь-який tier. Підтримувані версії - у таблиці [README](../README.md). GitLab.com і GitLab Dedicated не підтримуються: server hooks там недоступні
- Linux `amd64` або `arm64`, права root на нодах Gitaly
- instance service account (у Free доступний з GitLab 18.11)

| Тип інсталяції | Підтримка |
| --- | --- |
| Linux package (Omnibus), одна нода | так |
| Docker-образ GitLab | так |
| Self-compiled | так |
| Gitaly на окремих нодах, Gitaly Cluster (Praefect) | так, бінарник і конфігурація на кожній ноді Gitaly |
| Helm chart (Kubernetes) | ні |
| Geo secondary | не потрібно: Geo не реплікує server hooks, push іде в primary |

Перевірено e2e лише на Linux package в Docker-образі. Інші типи описані за документацією GitLab і не входять у матрицю тестів.

## Коротко

```sh
sudo apt install ./gitlab-repopolicy_1.0.0_linux_amd64.deb
sudo sh -c 'umask 337; cat > /etc/gitlab-repopolicy/token'     # токен service account, Ctrl-D
sudo gitlab-repopolicy install-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks --user git
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

Перед цим потрібні service account у GitLab і `custom_hooks_dir` у конфігурації Gitaly. Далі - усе по кроках.

## 1. Завантажити й перевірити реліз

Артефакти релізу лежать у [GitHub Releases](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/releases). У прикладах версія `1.0.0`, тег - `v1.0.0`.

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
/usr/lib/systemd/system/gitlab-repopolicy-*.{service,timer}   вимкнені таймери
```

`<git-user>` - користувач, від якого Gitaly запускає hooks: у Linux package це `git`. Під час першого встановлення пакет бере його зі змінної `GITLAB_REPOPOLICY_USER` (за замовчуванням `git`). Якщо такого користувача ще немає (GitLab не встановлено), пакет лише попереджає, а власників потім призначає `install-hook --user`.

Оновлення не змінює права, які виставив адміністратор, і не перезаписує змінений `config.yaml`: dpkg запитає, яку версію залишити (з `--force-confold` залишить вашу, а нову покладе поруч як `.dpkg-dist`), rpm залишить вашу, а нову покладе як `.rpmnew`.

**Без пакета** (архів, Docker, інші дистрибутиви): розпакуйте `gitlab-repopolicy` в `/usr/bin/` і створіть ті самі каталоги й файли з тими самими правами. Зразок `config.yaml` - [config/config.yaml](../config/config.yaml).

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

## 4. Конфігурація

У більшості інсталяцій на одній ноді змінювати `/etc/gitlab-repopolicy/config.yaml` не треба: за замовчуванням hook звертається до API через unix socket Workhorse тієї ж ноди. Якщо Gitaly на окремій ноді, вкажіть HTTPS-адресу GitLab і за потреби CA:

```yaml
gitlab_url: https://gitlab.example.com
ca_file: /etc/gitlab-repopolicy/ca.crt
```

Усі параметри - [configuration.md](configuration.md).

Для першого запуску на робочому інстансі варто поставити `mode: audit`: hook нікому не відмовляє, а рішення пише в audit з `"enforced": false`. Як переходити до `enforce` - [integration-guide.md](integration-guide.md).

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
- встановлює групу `config.yaml` і токена та власника каталогу audit-логу для `--user`
- повторний запуск нічого не змінює (`unchanged`). Інші файли в `pre-receive.d/` не чіпає

Gitaly виконує файли `pre-receive.d/` за алфавітом і зупиняється на першому, що завершився з помилкою. Окремий файл `custom_hooks_dir/pre-receive` поруч із `pre-receive.d/` Gitaly не виконує: свої hooks кладіть у `pre-receive.d/`. Файли з `~` у кінці імені Gitaly пропускає.

## 6. Перевірити

```sh
sudo gitlab-repopolicy selftest
sudo -u git gitlab-repopolicy selftest
```

`selftest` перевіряє конфігурацію, модулі, audit-лог, каталог break-glass, файл токена, service account (`bot`, `external`, не адміністратор), scopes і строк токена, доступ до API. Будь-який `FAIL` - код виходу 1.

Запускайте `selftest` і від root, і від користувача git: hook працює від git, і лише так видно, що він може прочитати конфігурацію і токен. Наприклад, каталог `/etc/gitlab-repopolicy` з правами 0750 root не помітить жодна перевірка від root, а hook відмовлятиме кожному push у групові проєкти з текстом "push could not be verified".

```text
$ sudo -u git gitlab-repopolicy selftest
info version     gitlab-repopolicy 1.0.0
ok   config      /etc/gitlab-repopolicy/config.yaml, mode enforce
ok   modules     enabled: pathpolicy, commitpolicy, tagpolicy
ok   audit       /var/log/gitlab-repopolicy/audit.log does not exist yet; the directory belongs to 998:998
ok   breakglass  /etc/gitlab-repopolicy/breakglass, no active grants
ok   api         http+unix://%2Fvar%2Fopt%2Fgitlab%2Fgitlab-workhorse%2Fsockets%2Fsocket reachable as gitlab-repopolicy (id 2)
ok   account     gitlab-repopolicy is a service account
ok   token       scopes read_api, self_rotate, expires on 2027-10-13
info git         git version 2.55.0-rc1.g02bb39c in PATH of selftest; the hook uses the git of Gitaly
```

Рядок `git` довідковий: hook використовує git, який йому передає Gitaly, і саме його версію пише в audit.

Далі - справжня перевірка: додайте PATHPOLICY у тестовий проєкт і зробіть push, який її порушує ([pathpolicy.md](pathpolicy.md)). Рішення видно у виводі push і в `/var/log/gitlab-repopolicy/audit.log`.

## 7. Таймери

Пакет ставить два systemd-таймери вимкненими:

| Таймер | Що робить | Як часто |
| --- | --- | --- |
| `gitlab-repopolicy-token-rotate.timer` | `token rotate`: ротація токена за 30 днів до кінця строку | щодня |
| `gitlab-repopolicy-settings-check.timer` | `settings-check`: звірка налаштувань проєктів ([operations.md](operations.md)) | кожні 15 хвилин |

```sh
sudo systemctl enable --now gitlab-repopolicy-token-rotate.timer gitlab-repopolicy-settings-check.timer
```

Обидві служби працюють від root в обмеженому оточенні systemd: на запис лише свої каталоги, без нових привілеїв, з двома capabilities - `CAP_CHOWN` (власник токена й нового audit-логу) і `CAP_DAC_OVERRIDE` (каталог логу належить користувачу hook).

## Docker

- бінарник монтуйте в контейнер GitLab read-only, наприклад у `/usr/bin/gitlab-repopolicy`
- `config.yaml` і токен кладіть у контейнер файлами, які може змінити root контейнера: `install-hook` змінює їхню групу, а `token rotate` замінює токен
- `custom_hooks_dir` і `/var/log/gitlab-repopolicy` - у persistent volume, щоб пережили перестворення контейнера
- `install-hook` і `selftest` запускайте в контейнері: `docker exec -it gitlab gitlab-repopolicy ...`

## Окремі ноди Gitaly і Gitaly Cluster

- бінарник, `config.yaml`, токен і `install-hook` - на кожній ноді Gitaly, версія всюди однакова. Її видно в кожній audit-події
- `gitlab_url` - HTTPS-адреса Rails, `ca_file` - якщо сертифікат не з системного сховища
- break-glass діє на тій ноді, де його видали: на кожній ноді, де може виконуватися hook

## Для розробників

Для локальної перевірки policy-файлів і commit messages (`validate`, `lint-commit`) достатньо зібрати бінарник:

```sh
go install github.com/Kuzmenko-Pavel/gitlab-repopolicy/cmd/gitlab-repopolicy@v1.0.0
```

Така збірка не підписана. На сервери ставте лише перевірені артефакти релізу.

## Вимкнути і видалити

```sh
sudo gitlab-repopolicy uninstall-hook --hooks-dir /var/opt/gitlab/gitaly/custom_hooks
sudo apt remove gitlab-repopolicy        # або dnf remove
```

`uninstall-hook` видаляє лише свій wrapper. Видалення пакета вимикає таймери, але hook не чіпає: якщо забути `uninstall-hook`, wrapper указуватиме на відсутній бінарник, і GitLab відхилятиме кожен push з текстом "Prevented by server hooks". `config.yaml` і токен після `apt remove` лишаються на місці, видаліть їх вручну, якщо вони більше не потрібні. Швидко вимкнути перевірки без видалення - `mode: off` у конфігурації ([operations.md](operations.md)).
