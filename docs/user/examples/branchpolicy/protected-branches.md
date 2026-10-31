[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](protected-branches.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](protected-branches.en.md)

# Захист release-гілки до першого push

Release pipeline потребує protected CI/CD variables або protected runners. Якщо гілку забули захистити, ці ресурси можуть бути недоступними. `protected: true` дозволяє помітити відсутній захист під час створення гілки.

## Конфігурація та підготовка

У корені основної гілки збережіть цей `BRANCHPOLICY` після `gitlab-repopolicy validate -f BRANCHPOLICY`. Він перевіряє факт захисту, а не конкретні ролі, вказані в підказці `message`.

```yaml
version: 1
rules:
  release:
    match: ['/^release\/v\d+\.\d+\.\d+$/']
    priority: 100
    protected: true
    message: Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
```

Maintainer додає `release/*` у Settings > Repository > Protected branches з потрібними проєкту ролями до створення release-гілки. Protected branch - це налаштування GitLab; саме поле `protected` не створює його.

```text
remote: BRANCHPOLICY rule "release", branch release/v1.4.0: the branch must be protected, but the project does not protect it
remote:   Release branches look like release/v1.4.0. Protect release/* with "Allowed to push and merge Maintainers".
remote:   how to fix: a Maintainer protects release/v1.4.0 in Settings > Repository > Protected branches, then push again
```

У глобальному `enforce`, якщо API підтвердив відсутність захисту, нова `release/v1.4.0` отримує `branch_not_protected`. Після налаштування повторіть push - ця відмова має зникнути. Власні права користувача й інші перевірки GitLab залишаються обов'язковими. Якщо API не відповів, BRANCHPOLICY пропускає перевірку з попередженням.

## Що перевіряти після створення

Зняття захисту з уже наявної гілки не перевірятиметься кожним наступним комітом: BRANCHPOLICY працює лише на створенні. Запускайте `settings-check` із серверною конфігурацією й токеном, щоб виявити таку розбіжність для наявних гілок. Для нього потрібен доступ service account до проєкту.

`validate --branch release/v1.4.0` показує вимогу захисту, але не питає GitLab про його наявність навіть із `--online`. Не використовуйте його код завершення як перевірку налаштувань.

[Довідник BRANCHPOLICY](../../policies/branchpolicy.md)
