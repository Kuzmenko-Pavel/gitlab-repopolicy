[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](CONTRIBUTING.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](CONTRIBUTING.en.md)

# Участь у проєкті

Повідомте про помилку або запропонуйте зміну в [issues](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/issues). Для виправлення відкрийте pull request. Про вразливості повідомляйте приватно за [SECURITY](SECURITY.md).

## Перед pull request

- Більшу зміну (новий модуль, нове правило COMMITPOLICY, зміна поведінки hook) спершу обговоріть в issue
- Підготуйте інструменти за [керівництвом розробника](docs/development/development.md). Потрібен GNU make 4 або новіший; доступні операції показує `make help`
- Зміна готова, коли проходять `make code-lint test-unit test-integration`. Зміни в ідентичності, клієнті API GitLab чи встановленні ще й `make test-e2e`; такий pull request позначте міткою `e2e`. Вручну ci та e2e запускаються на будь-якій гілці: Actions > e2e > Run workflow

## Коміти

- [Conventional Commits](https://www.conventionalcommits.org/uk/v1.0.0/): `type(scope): subject`, типи `feat`, `fix`, `build`, `chore`, `ci`, `docs`, `perf`, `refactor`, `revert`, `style`, `test`, subject з малої літери і без крапки в кінці, header до 100 символів
- Кожен коміт підписаний за [DCO](https://developercertificate.org/): `git commit -s` додає `Signed-off-by`. Так ви підтверджуєте, що маєте право внести цей код під ліцензією проєкту. CLA немає
- Повідомлення англійською

Правила записані в [COMMITPOLICY](COMMITPOLICY), і CI перевіряє ними коміти та заголовок pull request тим самим бінарником, що й на GitLab. Локально:

```sh
make code-lint-commits BASE_REF=origin/main
```

Pull request зливається через squash: заголовок PR стає header коміту, тому він теж має відповідати Conventional Commits.

## Що варто знати про код

- Код, коментарі, godoc і вивід бінарника - англійською
- Нова Go-залежність - лише з письмовим обґрунтуванням у pull request: кожна залежність потрапляє в бінарник, який працює на серверах GitLab
- Дотримуйтеся [меж відповідальності пакетів](docs/development/architecture.md#основні-компоненти); залежності перевіряє лінтер
- Схеми `version: 1` для PATHPOLICY і TAGPOLICY заморожені, коди відмов і формат audit-подій - публічний контракт
- `tests/corpus/*/valid/` лише поповнюється: ці файли мають приймати всі наступні версії

## Документація

Документація пишеться українською (`<ім'я>.md`) з англійським перекладом поруч (`<ім'я>.en.md`). Змінюйте обидва файли в тому самому pull request і перевіряйте відповідність умов, прикладів та очікуваних результатів.

Для короткого циклу перевірки використайте `make test-docs`. Він рекурсивно перевіряє мовні пари, відносні посилання й anchors, повні policy-блоки, цитати конфігурацій та виконує позначені таблиці й приклади CLI. Цитата з першим рядком `# examples/...` або `# config/...` має дослівно збігатися з відповідним файлом. Для діаграм є `make test-docs-mermaid` з установленим Mermaid CLI (`mmdc`). Перед review потрібен також повний набір обов'язкових перевірок.

## Ревью

Pull request переглядає maintainer (див. `.github/CODEOWNERS`). Злиття - після зеленого CI і схвалення.

## Агенти

Для AI-агентів правила роботи з репозиторієм - у [AGENTS.md](AGENTS.md).
