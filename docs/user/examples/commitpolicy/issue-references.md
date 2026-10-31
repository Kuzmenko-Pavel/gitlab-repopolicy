[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](issue-references.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](issue-references.en.md)

# Посилання на issue, merge request і задачу Jira

Команда хоче, щоб кожен коміт пояснював, навіщо він: посилався хоча б на одну issue чи merge request GitLab або на задачу Jira. Посилань у повідомленні може бути кілька, і стояти вони можуть будь-де: у header, у body або в trailers. Header при цьому - Conventional Commits.

## Які посилання розпізнаються

GitLab сам пов'язує коміт з issue або merge request, коли в повідомленні є посилання на них: це crosslinking ([документація GitLab](https://docs.gitlab.com/user/project/issues/crosslinking_issues/)). Політика приймає ті самі форми посилань, що й GitLab, і ключі задач Jira:

| Форма | Приклад | На що посилається |
| --- | --- | --- |
| `#номер` | `#121` | issue цього проєкту |
| `проєкт#номер` | `backend#121` | issue іншого проєкту тієї самої групи |
| `група/проєкт#номер` | `prozorro-sale/prozorro-deployment#321`, `group/sub/project#1` | issue будь-якого проєкту, також у підгрупі |
| `!номер`, `група/проєкт!номер` | `!45`, `prozorro-sale/app!12` | merge request |
| повний URL GitLab | `https://gitlab.example.com/group/project/-/issues/7`, `.../-/merge_requests/7`, `.../-/work_items/7` | issue, merge request або work item, також на іншому інстансі |
| ключ задачі Jira | `INFRA-4390`, `https://jira.example.com/browse/INFRA-4390` | задача Jira з проєктів, перелічених у політиці |

Hook перевіряє лише текст повідомлення: чи issue, merge request або задача справді існують, він не питає ні GitLab, ні Jira.

## Політика

Збережіть як `COMMITPOLICY` у корені репозиторію, перевірте `gitlab-repopolicy validate -f COMMITPOLICY` і злийте в основну гілку.

```yaml
# examples/issue-references/COMMITPOLICY
# Every commit references at least one GitLab issue or merge request, or a
# task of the Jira projects INFRA and PAY, anywhere in the message. GitLab
# references are the forms that GitLab crosslinks: #12, project#12,
# group/project#12, !12 for a merge request and full URLs.
version: 1

rules:
  references:
    match: all
    parser: conventional
    except:
      merge_commit: true
      git_reverts: true
    checks:
      header-format:
        conventional: { level: error }
      pattern:
        issue-or-mr: { level: error, location: message, value: '/(^|[^A-Za-z0-9_])([A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)*)?[#!][0-9]+\b|https?:\/\/\S+\/-\/(issues|merge_requests|work_items)\/[0-9]+|\b(INFRA|PAY)-[0-9]+\b/' }
    message: >-
      Reference a GitLab issue or merge request, or a Jira task, anywhere in
      the message, for example "issue: #121", "close: group/project#321" or
      "jira: INFRA-4390".
```

`location: message` шукає посилання в усьому повідомленні, тож байдуже, де воно стоїть: `fix: round refunds (#12)` у header, `See !45` у body чи `close: group/project#321` у trailer. Вираз у `/.../` без `!` має знайтися хоча б один раз, тому одного посилання досить, а кілька не заважають.

Вираз складається з трьох частин через `|`, і досить збігу будь-якої з них:

| Частина виразу | Що знаходить |
| --- | --- |
| `(^\|[^A-Za-z0-9_])([A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)*)?[#!][0-9]+\b` | посилання GitLab: `#` для issue або `!` для merge request, номер, перед ними за потреби проєкт із групами. Перед посиланням - початок рядка або не літера, тож `#fff` і `C#` не проходять: після `#` має бути номер |
| `https?:\/\/\S+\/-\/(issues\|merge_requests\|work_items)\/[0-9]+` | повний URL issue, merge request або work item GitLab |
| `\b(INFRA\|PAY)-[0-9]+\b` | ключ задачі Jira з проєктів `INFRA` і `PAY`, також усередині URL Jira |

Одна перевірка з альтернативами - єдиний спосіб записати "хоча б одне з кількох". Перевірки правила діють разом (AND): дві перевірки `pattern`, одна для GitLab і одна для Jira, вимагали б обидва посилання в кожному коміті.

Merge-коміт GitLab має header `Merge branch ...`, і `header-format` його не прийняв би, тому правило його пропускає (`except.merge_commit`). Посилання на merge request у ньому й так є: GitLab пише `See merge request group/project!12`. Повідомлення `git revert` посилання не має, його пропускає `except.git_reverts`.

## Свій проєкт Jira

Ключі Jira перелічені в третій частині виразу: `(INFRA|PAY)`. Щоб додати проєкт `OPS`, допишіть його через `|`:

```text
\b(INFRA|PAY|OPS)-[0-9]+\b
```

Перелік проєктів потрібен, бо загальна форма ключа `[A-Z]+-[0-9]+` збігається з багатьма звичайними словами. Тоді коміт `fix: use SHA-256 for checksums` пройшов би як посилання на задачу `SHA-256`, так само `UTF-8` чи `ISO-8601`. З переліком проходять лише ключі ваших проєктів.

Форму `GL-121`, яку згадує документація GitLab для повідомлень, що не можуть починатися з `#`, ця політика приймає, якщо додати `GL` до переліку проєктів: за формою це такий самий ключ.

Якщо посилання мають стояти лише в trailers, наприклад `issue:`, `jira:` і `close:`, замініть `location: message` на `location: trailer` і додайте `keys: [issue, jira, close]`. Тоді посилання в header чи body не враховується.

## Повідомлення та перевірка

Збережіть повідомлення зі справжніми переносами в `message.txt` і запустіть `gitlab-repopolicy lint-commit -f COMMITPOLICY -m "$(cat message.txt)"`. У таблиці `\n` позначає перенос рядка.

<!-- check: lint-commit examples/issue-references/COMMITPOLICY -->
| Повідомлення | Результат | Чому |
| --- | --- | --- |
| `auth: update broker legal name\n\nissue: #121\n\njira: INFRA-4390\n\nclose: prozorro-sale/prozorro-deployment#321` | проходить | три посилання; досить одного |
| `fix: round refunds (#12)` | проходить | посилання на issue в header |
| `fix: round refunds\n\nSee !45 for the review` | проходить | merge request у body |
| `fix: round refunds\n\nRefs: prozorro-sale/sub/project!12` | проходить | merge request проєкту в підгрупі |
| `fix: round refunds\n\nhttps://gitlab.example.com/group/project/-/issues/7` | проходить | повний URL issue |
| `fix: round refunds\n\njira: PAY-12` | проходить | ключ задачі Jira |
| `fix: round refunds\n\nhttps://jira.example.com/browse/INFRA-4390` | проходить | URL Jira містить ключ задачі |
| `fix: round refunds` | відмова | посилань немає |
| `fix: use SHA-256 for checksums` | відмова | `SHA` немає в переліку проєктів Jira |
| `fix: colour #fff in the header` | відмова | після `#` не номер |

Відмова для коміту без посилання:

```text
$ gitlab-repopolicy lint-commit -f COMMITPOLICY -m "fix: round refunds"
message: [references] error: pattern issue-or-mr: no match of /(^|[^A-Za-z0-9_])([A-Za-z0-9_.-]+(\/[A-Za-z0-9_.-]+)*)?[#!][0-9]+\b|https?:\/\/\S+\/-\/(issues|merge_requests|work_items)\/[0-9]+|\b(INFRA|PAY)-[0-9]+\b/ in the message
```

У відмові друкується весь вираз, а не пояснення людською мовою. Тому в `message` правила написано, які посилання потрібні, з прикладами: автор коміту бачить цей текст під час push.

Правило має `match: all` і за замовчуванням `check_history: new-content`: кожен коміт перевіряється, коли вперше з'являється в репозиторії, у будь-якій гілці. Повторно під час merge у `main` перевіряти нічого ([порівняння `new-content` і `incoming`](check-history.md)).

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
