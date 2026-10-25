# Демо-файлы проверки подписи (профиль TEST)

Пары «документ + открепленная подпись CMS/CAdES» для worker'а подписи. Их выпускает
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

Все пять пар помещаются в одну анкету (10 файлов). Если подписи проверяются раньше, чем закончился
сбор данных, worker ждёт его до `SIGNATURE_AUTHORITY_WAIT`; не дождался — полномочия `INDETERMINATE`.
