# Демо-файлы проверки подписи (профиль TEST)

Пары «документ + открепленная подпись CMS/CAdES» и PDF с подписью внутри (PAdES) для worker'а подписи. Их выпускает
`make sigdemo` (`go run ./tools/sigdemo`) вместе с тестовым УЦ в `testdata/signature/trust`,
который монтирует `data-collector-signature-worker`. Это тестовые сертификаты, а не КЭП:
итог — проверка по профилю TEST, а не проверка УКЭП.

Загружайте вместе с анкетой сценария A (`examples/scenario-a.applicant.json`, `examples/scenario-a.json`):
заявитель — ООО «Северный ветер», представитель — Литвинова М. О. по МЧД `0f5a2c1e…`.

| Файлы | Подписант | Ожидаемый итог |
|---|---|---|
| `../balance.pdf` + `../balance.pdf.sig` | Литвинова, главный бухгалтер, по МЧД с полномочием `FINANCIAL_DOCS_SIGN` | `VERIFIED` (вердикт `VALID_WITH_WARNINGS`) |
| `balance-director.pdf` + `.sig` | Ковалёв, генеральный директор по ЕГРЮЛ | `VERIFIED` |
| `balance-tampered.pdf` + `.sig` | Ковалёв; документ изменён после подписания | `INVALID`: целостность |
| `balance-revoked.pdf` + `.sig` | Литвинова, сертификат отозван (есть в `root.crl`) | `INVALID`: сертификат |
| `balance-other-company.pdf` + `.sig` | Орлов, директор ООО «Стройресурс» | `INVALID`: личность (другая организация) |
| `balance-signed-inside.pdf` (подпись внутри PDF, без `.sig`) | Литвинова по МЧД | `VERIFIED`: `/ByteRange` покрывает весь файл, кроме `/Contents` |
| `balance-signed-inside-updated.pdf` | тот же PDF, дописанный после подписания | `INDETERMINATE`: изменения после подписи не разбираются |

Пять пар — ровно 10 файлов, лимит одной анкеты. PDF с подписью внутри загружайте второй анкетой
(заново зарегистрируйте заявителя сценария A) вместе с `../balance.pdf` и `../balance.pdf.sig`: в
`scenario-a.json` для `balance.pdf` указан тип документа, и без этого файла анкета не пройдёт (422). Если подписи проверяются раньше, чем закончился
сбор данных, worker ждёт его до `SIGNATURE_AUTHORITY_WAIT`; не дождался — полномочия `INDETERMINATE`.
