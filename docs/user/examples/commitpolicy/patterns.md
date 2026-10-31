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
    checks:
      trailer-required:
        trailer: {level: error, value: optional}
      pattern:
        issue-key: {level: error, location: header, value: '/^[A-Z][A-Z0-9]+-[0-9]+ /'}
    message: Start the header with an issue key.
```

Виконайте `gitlab-repopolicy validate -f COMMITPOLICY`, потім `lint-commit -m "PAY-12 Round refunds"`. Для кожного наступного рядка таблиці замініть у `checks.pattern.issue-key` поля `location` і `value`, а в `checks.trailer-required.trailer` - `value`. Решту файлу залиште тією самою. Кожен рядок є окремим експериментом.

`trailer-required` у файлі задає, як hook знаходить trailer ([розбір повідомлення](../../policies/commitpolicy.md#як-hook-розбирає-повідомлення)). З `optional` абзац одразу після header - body, а trailer є лише після ще одного порожнього рядка. З `required` trailer є завжди, коли після header є рядки.

<!-- check: pattern -->
| `location` | `value` | `trailer-required` | Повідомлення | Результат | Чому |
| --- | --- | --- | --- | --- | --- |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | `optional` | `PAY-12 Round refunds` | проходить | ключ задачі на початку header |
| `header` | `/^[A-Z][A-Z0-9]+-[0-9]+ /` | `optional` | `Round refunds PAY-12` | відмова | ключ є, але не на початку, а вираз має `^` |
| `header` | `!/^(WIP\|fixup!)/i` | `optional` | `wip: try` | відмова | `!` вимагає, щоб вираз не знайшовся, а `i` не зважає на регістр |
| `header` | `!/^(WIP\|fixup!)/i` | `optional` | `Fix login` | проходить | виразу немає |
| `body` | `/(?m)^- /` | `optional` | `Fix login\n\n- check the token` | проходить | `(?m)` робить `^` початком кожного рядка body |
| `body` | `/(?m)^- /` | `optional` | `Fix login\n\nChecks the token.` | відмова | у body немає рядка-списку |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `required` | `Fix login\n\nRefs: payments/api#12` | проходить | рядок `Refs` у trailer збігається |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `required` | `Fix login\n\nWhy it changed.\n\nRefs: #12\nSigned-off-by: A <a@example.com>` | проходить | `(?m)` знаходить рядок `Refs` серед інших рядків trailer |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `required` | `Fix login\n\nCloses: #12` | відмова | trailer є, але рядка `Refs` у ньому немає |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `required` | `Fix login\n\nSee #12 in the tracker` | відмова | trailer - останній абзац, але це звичайний текст без `Refs:` |
| `trailer` | `/(?m)^Refs: [A-Za-z0-9_.\/-]*#[0-9]+$/` | `optional` | `Fix login\n\nRefs: payments/api#12` | відмова | абзац одразу після header - body, trailer немає |
| `message` | `/#[0-9]+/` | `optional` | `Fix login (#12)` | проходить | `message` - усе повідомлення, header теж |
| `message` | `!/password/i` | `optional` | `Fix login\n\nThe Password is hunter2.` | відмова | вираз знайшовся в body |

У повідомленнях таблиці `\n` означає справжній перенос рядка. Збережіть повідомлення в `message.txt` і передайте його як `-m "$(cat message.txt)"`. Рядки "проходить" дають код 0, "відмова" - 1.

`!` перед виразом забороняє збіг, `i` вимикає чутливість до регістру, `(?m)` змінює межі рядків для `^` та `$`. Для trailer вираз шукається в усьому його тексті, тому ключ і значення рядка перевіряє один вираз, як `Refs: ...` вище. У прикладі з `password` перевіряється текстовий шаблон, а не всі можливі секрети.

Типова помилка - чекати trailer там, де розбір бачить body. Посилання одразу після header стає trailer лише з `trailer-required: required`. Якщо одночасно потрібні дві вимоги `pattern`, додайте в те саме правило дві перевірки `pattern` з різними іменами ([кілька вимог](multiple-requirements.md)).

[Довідник COMMITPOLICY](../../policies/commitpolicy.md)
