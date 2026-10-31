[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](SECURITY.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](SECURITY.en.md)

# Безпека

## Як повідомити про вразливість

Пишіть приватно через [GitHub Security Advisories](https://github.com/Kuzmenko-Pavel/gitlab-repopolicy/security/advisories/new). Не відкривайте публічне issue і не описуйте вразливість у pull request.

Корисно вказати:
- версію gitlab-repopolicy (`gitlab-repopolicy version`) і GitLab
- як відтворити: policy-файли, команди, вивід push
- що саме вдалося обійти або яку шкоду це дає

Ми підтвердимо отримання, узгодимо з вами строки виправлення і публікації та зазначимо вас в advisory, якщо ви цього хочете.

## Підтримувані версії

Виправлення безпеки виходять для останньої minor-версії. Оновлюйтеся до неї: схеми PATHPOLICY і TAGPOLICY `version: 1` заморожені, а поведінку при невідомій перевірці COMMITPOLICY описано в експлуатаційному керівництві ([docs/operations.md](docs/user/administration/operations.md#оновлення-і-відкат)).

## Що вважається вразливістю

Будь-який спосіб змінити захищений шлях, послабити policy для власного push, обійти перевірку формату комітів чи підпису тегу або змусити hook прийняти рішення не за policy з default branch - так. Відомі обмеження, які описано в [docs/threat-model.md](docs/user/administration/threat-model.md) (налаштування проєкту, особисті namespace, дії Owner'а й адміністратора), - ні, але пропозиції, як їх зменшити, вітаються в issue.

## Перевірка релізів

Процедура випуску формує keyless-підпис cosign, SBOM і SLSA provenance. Перевірку підписаного комплекту артефактів описано у [встановленні](docs/user/administration/install.md#1-підготувати-й-перевірити-артефакти).
