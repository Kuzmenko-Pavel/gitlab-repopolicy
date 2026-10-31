[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](user-prefix.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](user-prefix.en.md)

# Username на початку робочої гілки

Префікс має допомагати знайти людину, з якою пов'язана гілка. Це перевірка існування username, а не правило власності: створити гілку для колеги теж можна.

## Повна політика

Збережіть як `BRANCHPOLICY`, перевірте `gitlab-repopolicy validate -f BRANCHPOLICY` і злийте в основну гілку. Вираз також допускає необов'язковий номер issue чи ключ Jira між username й назвою.

```yaml
version: 1
rules:
  work:
    match: ['/^[a-z0-9][a-z0-9._-]*\/(([0-9]+|[A-Z][A-Z0-9]+-[0-9]+)\/)?[a-z0-9][a-z0-9._-]*$/']
    priority: 900
    user_prefix: true
    message: Work branches are <username>/[<issue>/]<name>, for example kpn/PAY-12/refunds; PAY is the Jira project of this team.
```

| Хто створює | Гілка | Результат за доступного API та enforce |
| --- | --- | --- |
| alice | `alice/disable_auth` | дозволено, username збігається з розпізнаним користувачем push |
| alice | `bob/fix` | дозволено, якщо bob активний |
| alice | `ghost/fix` | відмова, якщо ghost не існує або неактивний |
| alice | `fix-login` | правило не збігається, ім'я не перевіряється |

Умови таблиці стосуються нових гілок, крім основної, у груповому проєкті. Для власного username розпізнаного користувача додатковий пошук імені не потрібний; SSH може потребувати перевірки типу ключа. Інший username перевіряється через API. Збій API дає попередження, а не доводить, що користувача немає.

```text
remote: BRANCHPOLICY rule "work", branch ghost/fix: the name does not follow the branch standard
remote:   ghost is not a GitLab user; the name starts with a GitLab username and /
remote:   Work branches are <username>/[<issue>/]<name>, for example kpn/PAY-12/refunds; PAY is the Jira project of this team.
remote:   how to fix: rename the branch locally (git branch -m) and push again
```

`gitlab-repopolicy validate --branch bob/fix` офлайн лише назве вимогу. Для пошуку користувача використайте `validate --online --config /etc/gitlab-repopolicy/config.yaml --branch bob/fix` на вузлі з доступом до серверного токена. В обох випадках код 0 запиту не є дозволом на push.

Не намагайтеся встановити доступ до гілки через `user_prefix`. Для цього потрібні штатні права GitLab; щоб відхиляти імена без префікса, потрібне окреме останнє `deny` для решти імен.

[Довідник BRANCHPOLICY](../../policies/branchpolicy.md)
