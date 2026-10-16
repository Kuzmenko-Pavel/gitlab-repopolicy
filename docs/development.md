[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](development.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](development.en.md)

# Розробка

## Що потрібно

| Інструмент | Для чого |
| --- | --- |
| Go | версію задає директива `go` у `go.mod`. З `GOTOOLCHAIN=auto` (за замовчуванням) Go завантажить її сам |
| GNU make 4 або новіший | усі операції проєкту. На macOS типовий make 3.81 без `.SHELLFLAGS` не підійде |
| git | integration-тести роблять справжній push |
| pipx | `yamllint` у `make code-lint` |
| Docker з compose | лише для e2e (`make test-e2e`) |

Інші інструменти (golangci-lint, gitleaks, govulncheck, goreleaser, syft, cosign, actionlint, benchstat) make збирає сам у `bin/tools/` із зафіксованих версій у `tools/`. `gpg` і `ssh-keygen` для тестів не потрібні: тести самі створюють ключі й підписи.

## Make - єдина точка входу

```sh
make help
```

Збирати, перевіряти і тестувати проєкт треба лише через make targets: так само їх викликає CI. Зміна готова, коли проходять:

```sh
make code-lint test-unit test-integration
```

Зміни в ідентичності, клієнті API GitLab чи встановленні потребують ще `make test-e2e`.

| Target | Що робить |
| --- | --- |
| `code-fmt` | форматує Go-код |
| `code-lint` | golangci-lint з правилами залежностей пакетів, tidy всіх модулів, actionlint, yamllint, збіг `schemas/` з кодом, мовні пари й посилання документації |
| `code-lint-commits` | повідомлення комітів після `BASE_REF` за `COMMITPOLICY`, взятою з `BASE_REF`, а не з гілки |
| `code-generate` | перегенерує `schemas/` і golden-файли виводу push |
| `code-build` | бінарник у `bin/gitlab-repopolicy` |
| `test-unit` | unit-тести з race detector |
| `test-integration` | сценарії на справжньому `git receive-pack` з git із `GIT_BIN`, корпус policy |
| `test-coverage` | unit і integration з покриттям у `coverage/` |
| `test-fuzz` | кожна fuzz-ціль по `FUZZTIME` |
| `test-load` | бенчмарки hook |
| `test-e2e`, `test-e2e-down` | стенд GitLab CE і сценарії на ньому |
| `security-sast`, `security-dependency-scan`, `security-secret-scan` | gosec, govulncheck, gitleaks |
| `release-snapshot` | архіви й пакети без підпису і публікації |

Локальні значення змінних make (`GIT_BIN`, `GITLAB_VERSION`, `FUZZTIME` та інші) можна покласти в `.env`, зразок - `.env.example`. `.env` у git не потрапляє.

## Структура репозиторію

```text
cmd/gitlab-repopolicy/   main
internal/                увесь код продукту, карта пакетів - architecture.md
schemas/                 JSON Schema policy-файлів, генерується з коду
config/                  config.yaml пакета і конфігурація перевірки релізного тегу
packaging/               скрипти пакета і systemd-юніти
examples/                приклади policy-файлів; входять у корпус тестів
tests/integration/       сценарії на справжньому git receive-pack
tests/e2e/               сценарії на GitLab CE
tests/corpus/            корпус сумісності policy: valid і invalid за модулями
tools/                   версії інструментів розробки
docs/                    документація
```

## Тести

**Unit** - поруч із кодом. API GitLab підмінено фейковим сервером, git - інтерфейсами, час - фіксований.

**Integration** (`tests/integration`) роблять справжній push: git сам створює quarantine, передає stdin і викликає hook, лише API GitLab підмінено. Версія git впливає на результат, тому прогін варто повторювати на git, який постачає Gitaly ваших версій GitLab:

```sh
make test-integration GIT_BIN=/path/to/git
```

Як дістати git з образу GitLab (amd64):

```sh
v=19.4.2-ce.0; d=bin/gitaly-git/$v
mkdir -p "$d/lib"
id=$(docker create gitlab/gitlab-ce:$v)
docker cp "$id:/opt/gitlab/embedded/bin/gitaly-git-prev" "$d/"
for l in libiconv.so.2 libpcre2-8.so.0 libz.so.1 libcrypto.so.3; do
  docker cp -L "$id:/opt/gitlab/embedded/lib/$l" "$d/lib/"
done
docker rm "$id"
cat > "$d/git" <<'EOF'
#!/bin/sh
dir=$(dirname "$(readlink -f "$0")")
exec /lib64/ld-linux-x86-64.so.2 --library-path "$dir/lib" --argv0 "$0" "$dir/gitaly-git-prev" "$@"
EOF
chmod +x "$d/git"
for n in git-receive-pack git-upload-pack; do ln -sf git "$d/$n"; done
make test-integration GIT_BIN=$d/git
```

Gitaly має кілька збірок git; hook отримує `gitaly-git-prev`. Обгортка передає бібліотеки завантажувачу, не змінюючи оточення git. Тест перевіряє, що hook справді бачить git із `GIT_BIN`.

**Корпус сумісності.** `tests/corpus/<модуль>/valid` - файли, які поточна і кожна наступна версія мають приймати, `invalid` - відхиляти. Разом з `examples/` корпус лише поповнюється: файли з `valid` не видаляються і не змінюються. Під час релізу попередня minor-версія перевіряється на тих самих файлах PATHPOLICY і TAGPOLICY. Кожен приклад policy в документації дослівно збігається з файлом з `examples/`: так вимагає тест.

**Fuzz.** Цілі - у пакетах поруч із кодом, корпус - у `testdata/fuzz/`. `make test-fuzz` запускає кожну на `FUZZTIME` (`30s` за замовчуванням, у nightly - `10m`). Знайдене падіння лишається в `testdata/fuzz/` разом із виправленням.

**Навантаження.** `make test-load` - бенчмарки hook на синтетичних репозиторіях: 100 тисяч файлів, diff на 50 тисяч шляхів, merge request на 500 комітів, швидкий шлях без policy.

## Стенд e2e

```sh
make test-e2e
make test-e2e-down
```

`test-e2e` збирає бінарник під Linux, піднімає в docker compose GitLab CE `GITLAB_VERSION` і gitlab-runner, готує стенд через API (користувачі, групи, service account, проєкти, SSH-ключі, runner), встановлює hook командою `install-hook` і запускає сценарії з `tests/e2e`. Перший старт GitLab займає кілька хвилин; стенд живе між запусками, повторний `make test-e2e` лише заново готує стан. `test-e2e-down` видаляє стенд разом з volumes.

| Змінна | За замовчуванням | Значення |
| --- | --- | --- |
| `GITLAB_VERSION` | поточна minor-версія | тег образу `gitlab/gitlab-ce` |
| `E2E_HTTPS_PORT`, `E2E_SSH_PORT` | `8443`, `2222` | порти стенду на `127.0.0.1` |
| `E2E_STATE_ROOT` | `bin/e2e-stand` | тестовий CA, SSH-ключі й токени стенду, по каталогу на стенд |
| `E2E_NIGHTLY` | порожньо | `1` додає nightly-сценарій з паралельними merge |
| `COMPOSE_PROJECT_NAME` | з версії GitLab | ім'я стенду; у кожної версії свій |

Не запускайте два стенди одночасно: GitLab CE потребує кількох ГБ пам'яті.

## Локальна перевірка проєкту його ж інструментом

Проєкт перевіряє себе власним бінарником: коміти - за [COMMITPOLICY](../COMMITPOLICY), релізні теги - за [TAGPOLICY](../TAGPOLICY).

```sh
make code-lint-commits BASE_REF=origin/main
```

Як оформлювати коміти й pull request - [CONTRIBUTING.md](../CONTRIBUTING.md).
