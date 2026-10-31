[![Українська](https://img.shields.io/badge/lang-Українська-blue.svg)](deny-family.md)
[![English](https://img.shields.io/badge/lang-English-lightgrey.svg)](deny-family.en.md)

# Точне правило й заборона решти родини

`release/v1.4.0` є правильним ім'ям, `release/foo` - помилкою. Один точний регулярний вираз не забороняє друге ім'я: гілка без збігу проходить без перевірки. Для відмови потрібне наступне правило для всієї родини `release/`.

## Повна конфігурація

Збережіть як `BRANCHPOLICY`, перевірте `gitlab-repopolicy validate -f BRANCHPOLICY` й активуйте в основній гілці. Maintainer має захистити `release/*` до створення правильної release-гілки.

```yaml
version: 1
rules:
  release:
    match: ['/^release\/v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$/']
    priority: 100
    protected: true
    message: Use release/vX.Y.Z and protect release/* in GitLab.
  release-bad:
    match: ['/^release\//']
    priority: 110
    deny: true
    message: Release branches look like release/v1.4.0.
```

| Ім'я | Вибране правило | Результат у enforce |
| --- | --- | --- |
| `release/v1.4.0` | `release` | дозволено, якщо GitLab захищає гілку |
| `release/foo` | `release-bad` | `branch_name_violation` |
| `feature/login` | немає | ім'я не перевіряється |

Менший `priority` точного правила є принциповим: інакше родинна заборона відхилить також правильні імена. Номери мають бути унікальними після злиття з пресетами.

```text
remote: BRANCHPOLICY rule "release-bad", branch release/foo: creating this branch is forbidden
remote:   Release branches look like release/v1.4.0.
```

Перевірте вибір через `validate --branch release/foo` та `validate --branch release/v1.4.0`. Обидва інформаційні запити можуть дати код 0. Остаточну відмову перевірте контрольним push нової гілки у глобальному `enforce`.

Без родинної заборони ім'я може перейти до наступного широкого правила `work` і помилково трактувати `release` як username. Пишіть відмову, яка пояснює правильне release-ім'я, а не змушує розробника шукати неіснуючого користувача.

[Довідник BRANCHPOLICY](../../policies/branchpolicy.md)
