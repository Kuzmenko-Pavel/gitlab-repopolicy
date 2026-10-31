[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](patterns.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](patterns.en.md)

# Регулярний вираз у потрібній частині повідомлення

Ключ задачі у header, список у body й посилання в trailer є різними вимогами. Якщо шукати в усьому повідомленні, випадковий збіг в іншій частині може дозволити небажаний формат.

## Одна перевірка `pattern`

Для першого рядка таблиці створіть у поточному каталозі повний `COMMITPOLICY`:

```yaml
version: 1
rules:
  issue-key:
    match: all
    parser: free
    checks:
      pattern:
        issue-key: {level: error, location: header, value: '/^[A-Z][A-Z0-9]+-[0-9]+ /'}
    message: Start the header with an issue key.
```

Виконайте `gitlab-repopolicy validate -f COMMITPOLICY`, потім `lint-commit -m "PAY-12 Round refunds"`. Для кожного наступного рядка таблиці замініть у `checks.pattern.issue-key` поля `location`, `value` і, якщо непорожнє, `keys`. Решту файлу залиште тією самою. Кожен рядок є окремим експериментом.

Порожня комірка `keys` означає, що це поле не задають. Список `keys` дозволений лише з `location: trailer`; без нього вираз шукається у значеннях усіх trailers.

<!-- check: pattern -->
| `location` | `value` | `keys` | Повідомлення | Результат | Чому |
| --- | --- | --- | --- | --- | --- |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | | `PAY-12 Round refunds` | проходить | ключ задачі на початку header |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | | `Round refunds PAY-12` | відмова | ключ є, але не на початку, а вираз має `^` |
| `header` | `!/^(WIP\|fixup!)/i` | | `wip: try` | відмова | `!` вимагає, щоб вираз не знайшовся, а `i` не зважає на регістр |
| `header` | `!/^(WIP\|fixup!)/i` | | `Fix login` | проходить | виразу немає |
| `body` | `/(?m)^- /` | | `Fix login\n\n- check the token` | проходить | `(?m)` робить `^` початком кожного рядка body |
| `body` | `/(?m)^- /` | | `Fix login\n\nChecks the token.` | відмова | у body немає рядка-списку |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nRefs: payments/api#12` | проходить | значення trailer `Refs` збігається |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nCloses: #12` | відмова | trailer є, але з іншим ключем |
| `trailer` | `/^[A-Za-z0-9_.\/-]*#[0-9]+$/` | `[Refs]` | `Fix login\n\nSee #12 in the tracker` | відмова | останній абзац - звичайний текст, а не trailer |
| `message` | `/#[0-9]+/` | | `Fix login (#12)` | проходить | `message` - усе повідомлення, header теж |
| `message` | `!/password/i` | | `Fix login\n\nThe Password is hunter2.` | відмова | вираз знайшовся в body |

У повідомленнях таблиці `\n` означає справжній перенос рядка. Збережіть повідомлення в `message.txt` і передайте його як `-m "$(cat message.txt)"`. Рядки "проходить" дають код 0, "відмова" - 1.

`!` перед виразом забороняє збіг, `i` вимикає чутливість до регістру, `(?m)` змінює межі рядків для `^` та `$`. Для trailer перевіряється його значення, без ключа; `keys` обмежує, які ключі розглядати. У прикладі з `password` перевіряється текстовий шаблон, а не всі можливі секрети.

Типова помилка - написати звичайний абзац `See #12 in the tracker` і очікувати, що він стане trailer. Останній абзац має складатися лише з trailer-рядків. Якщо одночасно потрібні дві вимоги `pattern`, додайте в те саме правило дві перевірки `pattern` з різними іменами ([кілька вимог](multiple-requirements.md)).

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
