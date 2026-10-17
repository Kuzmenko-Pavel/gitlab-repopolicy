[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](CONTRIBUTING.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](CONTRIBUTING.en.md)

# Участь у проєкті

Дякуємо, що хочете допомогти. Помилки й пропозиції - в [issues](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/issues), зміни - pull request'ами. Про вразливості пишіть приватно, за [SECURITY.md](SECURITY.md), а не в issue.

## Перед pull request

- Більшу зміну (новий модуль, нове правило COMMITPOLICY, зміна поведінки hook) спершу обговоріть в issue
- Як працювати з кодом, тестами і стендом e2e - [docs/development.md](docs/development.md). Усе робиться через `make`, вхідна точка - `make help`. Потрібен GNU make 4 або новіший
- Зміна готова, коли проходять `make code-lint test-unit test-integration`. Зміни в ідентичності, клієнті API GitLab чи встановленні ще й `make test-e2e`; такий pull request позначте міткою `e2e`

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
- Карта пакетів і правила залежностей між ними - [docs/architecture.md](docs/architecture.md); лінтер їх перевіряє
- Схеми `version: 1` для PATHPOLICY і TAGPOLICY заморожені, коди відмов і формат audit-подій - публічний контракт
- `tests/corpus/*/valid/` лише поповнюється: ці файли мають приймати всі наступні версії

## Документація

Документація - українською (`<ім'я>.md`) з англійським перекладом поруч (`<ім'я>.en.md`). Зміна документа оновлює обидва файли в тому самому pull request; `make test-integration` перевіряє, що пари на місці, відносні посилання ведуть на наявні файли, а приклади policy в документах дослівно збігаються з `examples/`.

## Ревью

Pull request переглядає maintainer (див. `.github/CODEOWNERS`). Злиття - після зеленого CI і схвалення.

## Агенти

Для AI-агентів правила роботи з репозиторієм - у [AGENTS.md](AGENTS.md).
