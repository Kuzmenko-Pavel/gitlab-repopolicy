[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](ci-configuration.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](ci-configuration.en.md)

# Захист CI/CD конфігурації

`.gitlab-ci.yml`, локальні includes і deploy-скрипти можуть змінювати лише alice та sre-lead у будь-якій гілці. Pipeline виконує конфігурацію своєї гілки, тому перевірки лише під час merge у `main` недостатньо.

## Політика проєкту

Збережіть цей файл як `PATHPOLICY` у корені репозиторію. Перевірте його `gitlab-repopolicy validate -f PATHPOLICY`; першу версію в основну гілку додає Maintainer або Owner. Вказані username мають існувати, а хоча б один `admin` має бути активним Maintainer або Owner.

```yaml
# examples/ci-config/PATHPOLICY
# The platform team, alice and sre-lead, owns the CI configuration and the
# deploy scripts in every branch and the production values in main and
# release branches. Everything else is authorized by GitLab as usual.
version: 1

admins: [alice, sre-lead]

rules:
  ci-config:
    match: all
    paths:
      - ".gitlab-ci.yml"
      - ".gitlab/ci/**"
      - "scripts/deploy/**"
    allow: [alice, sre-lead]
    message: >-
      The platform team changes the CI/CD configuration and the deploy scripts.
      Ask alice or sre-lead to push this change.
    help:
      contact: "#platform"

  prod-values:
    match: [main, '/^release\//']
    paths: ["helm/**/values-prod.yaml"]
    allow: [alice, release-manager]
    message: >-
      Production values are changed in main and release branches by alice or
      release-manager. Propose the change in your branch and ask them to merge it.
```

`ci-config` використовує `match: all`, щоб охопити кожну гілку. `allow` визначає того, хто надсилає зміну, а не автора коміту. `prod-values` є окремим обмеженням production-файлів і не розширює доступ до CI.

| Хто | Що робить | Результат | Чому |
| --- | --- | --- | --- |
| bob | змінює `README.md` | дозволено | жодне правило не стосується файлу |
| bob | пушить гілку `feature` зі зміненим `.gitlab-ci.yml` | відмова | `ci-config` діє в усіх гілках, bob немає в `allow` |
| alice | пушить ту саму гілку | дозволено | alice є в `allow` |
| bob | ставить тег на коміт цієї гілки | дозволено | теги PATHPOLICY не перевіряє, коміт уже перевірено разом із гілкою |

Таблиця описує внесок PATHPOLICY для групового проєкту з основною гілкою `main`, у глобальному `enforce`. Інші політики й штатні права GitLab теж мають дозволяти дію. Тег у рядку bob указує на вже прийнятий коміт гілки; окрема TAGPOLICY може вимагати його підпис.

## Перевірка й типові помилки

Після активації попросіть bob змінити лише README, потім CI-файл у тестовій гілці; перший push має пройти, другий отримати `protected_path_violation`. Ту саму CI-зміну надсилає alice. У глобальному `audit` push проходять, а очікувані відмови шукають у журналі.

`match: default` залишить CI інших гілок поза цим правилом. Захист лише `.gitlab-ci.yml` також недостатній, якщо job викликає незахищений `Makefile` або інший скрипт: включіть усі шляхи, що визначають виконувані команди.

[Довідник PATHPOLICY](../../policies/pathpolicy.md)
