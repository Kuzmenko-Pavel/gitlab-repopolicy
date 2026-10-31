[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](README.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](README.en.md)

# Документація gitlab-repopolicy

Якщо ви ще не знайомі із застосунком, [README проєкту](../README.md) пояснює можливості й показує приклад відмови. Далі оберіть матеріал за своєю задачею.

## Користувачам політик

Політики - YAML-файли в корені репозиторію. Чинна версія читається з основної гілки до push; зміна правила не послаблює перевірку того самого push. Основна гілка (`default branch`) задається в налаштуваннях GitLab, а не визначається довільно назвою `main`.

| Задача | Матеріал |
| --- | --- |
| Обмежити зміни файлів певними користувачами | [PATHPOLICY](user/policies/pathpolicy.md) і [готові сценарії](user/policies/pathpolicy.md#приклади) |
| Установити формат повідомлень комітів | [COMMITPOLICY](user/policies/commitpolicy.md), [структура повідомлення](user/policies/commitpolicy.md#з-чого-складається-повідомлення-коміту) і [сценарії](user/policies/commitpolicy.md#приклади) |
| Вимагати підпис тегів | [TAGPOLICY](user/policies/tagpolicy.md) і [сценарії релізів](user/policies/tagpolicy.md#приклади) |
| Перевіряти нові імена гілок | [BRANCHPOLICY](user/policies/branchpolicy.md) і [приклади стандартів](user/policies/branchpolicy.md#приклади) |
| Вибрати гілки або теги для правила | [Поле match](user/policies/match.md) |
| Перевірити файл до push | [Локальна перевірка validate](user/reference/validate.md) |
| Вибрати інструмент діагностики | [Довідник CLI](user/reference/cli.md#яку-команду-використовувати) |

Якщо push відхилено, знайдіть ім'я policy в повідомленні й відкрийте розділ "Відмови" відповідного довідника. Рядок `checked as` показує користувача, чию дію перевіряв hook; він може відрізнятися від автора коміту.

## Адміністраторам GitLab

[Встановлення](user/administration/install.md) готує service account, серверні файли й hook Gitaly. Для робочого інстансу після нього використайте [порядок інтеграції](user/administration/integration-guide.md): спочатку спостереження в `audit`, потім контрольоване ввімкнення відмов.

[Конфігурація сервера](user/administration/configuration.md) розрізняє мінімальний запуск, поставку пакета й програмні defaults. [Експлуатація](user/administration/operations.md) пояснює журнал audit, алерти, токени, відновлення й діагностику. Для оцінки меж захисту прочитайте [модель загроз](user/administration/threat-model.md) та [послідовність перевірки](development/architecture.md#як-відбувається-перевірка-змін).

## Розробникам gitlab-repopolicy

[Вхід до документації розробника](development/README.md) веде від архітектури до локальних перевірок і релізу. Правила внесків, DCO та review визначає [CONTRIBUTING](../CONTRIBUTING.md), приватне повідомлення про вразливості - [SECURITY](../SECURITY.md).
