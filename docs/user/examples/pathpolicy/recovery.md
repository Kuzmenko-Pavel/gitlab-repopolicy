[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](recovery.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](recovery.en.md)

# Відновлення керування PATHPOLICY

alice була єдиним admin політики, але втратила доступ або стала Developer. Owner має призначити нового власника, не відкриваючи можливість довільно змінювати захищений CI в тому самому push.

## Початковий стан

У корені основної гілки вже збережено цей `PATHPOLICY`:

```yaml
version: 1
admins: [alice]
rules:
  ci:
    match: all
    paths: [".gitlab-ci.yml"]
    allow: [alice]
    message: CI belongs to alice.
```

Для recovery потрібна підтверджена відповідь GitLab, що жоден із `admins` не є активним Maintainer або Owner проєкту, включно з успадкованими ролями. Відпустка admin або відсутність його відповіді не є такою умовою.

| Хто змінює PATHPOLICY | Робочі admins за PATHPOLICY з default branch | Результат |
| --- | --- | --- |
| admin | будь-які | дозволено |
| Owner проєкту, не admin | є хоча б один | відмова `policy_admin_violation` |
| Owner проєкту, не admin | жодного, і API це підтвердив | дозволено, recovery |
| Owner проєкту, не admin | API не відповів | відмова `authorization_backend_failure` |
| deploy key | будь-які | відмова `unknown_principal` |
| усі інші | будь-які | відмова `policy_admin_violation` |

Збій API recovery ніколи не вмикає. Instance administrator не є учасником проєкту і recovery не отримує: його шлях - стати Owner'ом (це видно в аудиті GitLab) або break-glass ([operations.md](../../administration/operations.md#break-glass)).

Recovery-push змінює лише PATHPOLICY: захищені файли в тому ж push перевіряються за старими правилами. Owner бачить у виводі push `PATHPOLICY is changed by Owner <username> because it has no working admins. This is logged.`, а в audit пишеться подія `policy_recovery`. На неї варто поставити алерт.

## Порядок відновлення

На вузлі Gitaly оператор із доступом до серверного конфігу й токена запускає `gitlab-repopolicy admins --project group/project`. Помилка API є помилкою перевірки, а не підтвердженням відсутності admins.

Owner готує версію PATHPOLICY з новим `admins` і відповідним `allow`, перевіряє її `validate --online --config /etc/gitlab-repopolicy/config.yaml --project group/project -f PATHPOLICY` та надсилає лише виправлення політики. Новий admin має існувати й мати Maintainer або Owner. CI-файл у recovery-push продовжує перевірятися за старим правилом, тому його зміну надсилають окремо після активації нової політики.

Hook повідомляє `PATHPOLICY is changed by Owner <username> because it has no working admins. This is logged.` і створює `policy_recovery`, якщо допускає push. У глобальному `audit` це не доводить, що recovery був потрібний для дозволу, бо відмови не застосовуються.

Якщо API не працює або Owner не може виконати виправлення, root може застосувати [break-glass](../../administration/operations.md#break-glass). Адміністратор інстансу не отримує recovery автоматично лише через свою посаду.

Після відновлення перевірте `admins` ще раз, підтвердьте активацію нового файла в основній гілці й перевірте, що новий admin може виконати контрольну зміну. Тримайте кількох робочих admins, щоб звичайна відсутність однієї людини не потребувала аварійної процедури.

[Довідник PATHPOLICY](../../policies/pathpolicy.md)
