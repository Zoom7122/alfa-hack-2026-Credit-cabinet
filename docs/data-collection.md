# Сбор данных для скоринга ЮЛ / ИП

Принцип: **клиент вводит минимум, остальное собираем сами и молча.** Вопрос клиенту задаём только тогда, когда расхождение реально меняет решение или сумму (см. `DECIDING → WAITING_FOR_INFO` в [fsm.md](fsm.md)).

## 1. Что вводит клиент

| Блок | Поля (`ApplicationInput`) | Зачем | ЮЛ | ИП |
|---|---|---|---|---|
| Компания | `inn` + роль представителя | ключ ко всем источникам; доверенность, если не директор | ✔ | ✔ |
| Запрос | `product_code`, `requested.amount`, `requested.term_months` | параметры кредита | ✔ | ✔ |
| Цель | `purpose.code`, `purpose.description` | правила продукта, риск цели («рефинансирование чужих долгов» ≈ табл. 2 ФЛ, −25) | ✔ | ✔ |
| Финансы | `annual_revenue`, `net_profit`, `avg_monthly_turnover`, `avg_monthly_expenses` | лимит и долговая нагрузка | ✔ | ✔ (у ИП нет бухотчётности, заявленные данные важнее) |
| Долги | `debts[]` или `has_no_debts` | DSCR; сверка с БКИ | ✔ | ✔ |
| Залог | `collateral[]` (+ кадастровый номер / VIN) | лимит, ЕГРН | если продукт требует | |
| Поручители | `guarantors[]` (ИНН, для ФЛ — доход и дата рождения) | снижение риска; ФЛ-поручитель скорится упрощённой моделью листа 2 | опц. | обычно сам владелец/супруг |
| Согласия | `consents.personal_data`, `consents.credit_bureau` | без согласия на БКИ отправка заблокирована (218-ФЗ) | ✔ | ✔ |
| Документы | по `Product.required_documents` | распознаются, извлечённые цифры идут в сверку | по сумме | по сумме |

Не спрашиваем: ОГРН, адрес, ОКВЭД, учредителей, директора, штат, отчётность — всё это даёт `POST /counterparty/lookup` и обогащение.

## 2. Источники (pgqueue-таски `enrich.source`)

Каждый источник — Go-адаптер с интерфейсом `Fetch(ctx, inn) (Snapshot, error)` и двумя реализациями: `live` и `mock`. Режим выбирается переменной окружения на каждый источник (`SOURCE_EGRUL_MODE=live|mock`).

| Код | Источник | Что берём | Live реалистичен? | Кэш TTL | ЮЛ | ИП |
|---|---|---|---|---|---|---|
| `EGRUL` / `EGRIP` | egrul.nalog.ru | статус, дата регистрации, адрес, ОКВЭД, уставный капитал, директор, учредители и доли, реорганизация/ликвидация | да, бесплатно; при частых запросах бывает капча | 24 ч | ✔ | ✔ |
| `PB_NALOG` | pb.nalog.ru «Прозрачный бизнес» | массовый адрес/руководитель, дисквалификация, недостоверность, налоговый режим, среднесписочная численность, уплаченные налоги, налоговая задолженность | частично, есть капча | 24 ч | ✔ | ✔ |
| `GIRBO` | bo.nalog.ru (ГИР БО) | баланс и ОФР за последние годы: выручка, чистая прибыль, активы, капитал, обязательства | да, самый полезный для ЮЛ | 30 дн | ✔ | — (ИП не сдают) |
| `FSSP` | fssp.gov.ru, банк исполнительных производств | число и сумма активных исполнительных производств | сложно, капча → mock | 24 ч | ✔ | ✔ |
| `FEDRESURS` | fedresurs.ru | сообщения о банкротстве, намерении кредиторов подать заявление | частично | 24 ч | ✔ | ✔ |
| `KAD` | kad.arbitr.ru | дела в роли ответчика/истца, суммы исков | сильная антибот-защита → mock | 7 дн | ✔ | ✔ |
| `RNP` | zakupki.gov.ru | реестр недобросовестных поставщиков | да | 7 дн | ✔ | ✔ |
| `EGRN` | Росреестр | права на объект залога, обременения | выписки платные → **только mock** | 30 дн | при залоге-недвижимости | |
| `BKI` | бюро кредитных историй | кредиты, просрочки, запросы за 30 дней, поручительства | нужен договор с бюро → **только mock** | на заявку | ✔ | ✔ (+ по ИП как ФЛ) |
| `BANK_INTERNAL` | «внутренние системы банка» | есть ли расчётный счёт и с какого времени, обороты, действующие кредиты в банке | **только mock** | 1 ч | ✔ | ✔ |

Про live-режим: где стоит капча или антибот-защита, обходить их мы не будем. Адаптер возвращает `FAILED`, дальше работает логика критичности источника (fsm.md). Для синтетических тестовых клиентов из сидов все источники всегда в `mock`, поэтому e2e-сценарий жюри не зависит от доступности сайтов. Live включаем только для показа на реальном ИНН.

**Снапшоты.** Каждый ответ сохраняется в `source_snapshots` (сырой ответ + sha256 + нормализованный JSON + `fetched_at` + URL). Это одновременно кэш по ИНН (вторая заявка той же компании берёт свежие снапшоты без похода наружу, `mode=CACHE`) и доказательная база, если клиент оспаривает данные документами.

### Пайплайн тасок

```
submit ─▶ enrich.fanout(app)                       -- ENRICHING
            ├─▶ enrich.source(app, EGRUL)  ┐
            ├─▶ enrich.source(app, GIRBO)  │  каждая: cache hit? → snapshot; иначе fetch с ретраями (3, backoff)
            ├─▶ ...                        │  по завершении: UPDATE source_runs; если все терминальны →
            └─▶ enrich.source(app, BKI)    ┘  enqueue verify(app)   (SELECT ... FOR UPDATE по заявке, без гонок)
verify(app)  ─▶ findings, stop-факторы            -- VERIFYING
score(app)   ─▶ scoring module (Go)              -- SCORING
decide(app)  ─▶ decision engine, офферы           -- DECIDING
```

После `WAITING_FOR_INFO` `enrich.fanout` перезапускает только источники, данные которых затронул ответ (например, загружена бухотчётность → пересчитать `GIRBO`-факторы из распознанного документа).

## 3. Нормализованный профиль компании (`ApplicantFull`)

```
registry:            status, registration_date, age_months, legal_form, authorized_capital,
                     okved_main, okved_additional[], region_code, director{name, inn, since},
                     founders[{name, inn, share_pct, type}], reorganization, liquidation
tax:                 tax_regime, employees_count, taxes_paid_last_year, tax_debt_amount
risk_flags[]:        MASS_REGISTRATION_ADDRESS, MASS_DIRECTOR, UNRELIABLE_ADDRESS, UNRELIABLE_DIRECTOR,
                     DISQUALIFIED_DIRECTOR, RNP_LISTED, BANKRUPTCY_NOTICE
financial_statements[]: year, revenue, net_profit, total_assets, equity, lt_liabilities, st_liabilities
credit_history:      loans[{lender, kind, amount, outstanding, monthly_payment, overdue_days_max, status}],
                     current_overdue_amount, worst_overdue_12m_days, inquiries_30d, guarantees_given
enforcement:         active_cases, active_amount
litigation:          as_defendant_count, as_defendant_amount_12m, as_plaintiff_count
bank_relationship:   has_settlement_account, account_age_months, avg_monthly_turnover_6m,
                     existing_bank_loans[]
collateral_checks[]: cadastral_number, owner_matches, encumbrances[]
```

## 4. Скоринговая карта для бизнеса (предложение для scoring-модуля)

Модуль на Go: интерфейс `Scorer.Score(ctx, Features) (ScoreResult, error)`. Реализация `scorecard` читает веса из YAML (`scorecard-sme-v1.yaml`), реализация `ml` подключается позже через тот же интерфейс (например, ONNX), режим `HYBRID` = scorecard + поправка PD. Модель ФЛ из xlsx (лист 2) используется только для поручителей-ФЛ, без признаков вроде семейного положения или военной обязанности.

| Фактор | Источник | Бакеты → баллы (черновик) |
|---|---|---|
| Возраст бизнеса | EGRUL/EGRIP | < 6 мес — stop · 6–12 мес 20 · 1–3 г 60 · 3–5 л 90 · > 5 л 120 |
| Годовая выручка | GIRBO, иначе заявленная с дисконтом 30% | < 10 млн 20 · 10–60 50 · 60–120 80 · > 120 100 |
| Рентабельность (ЧП / выручка) | GIRBO / заявлено | < 0 0 · 0–5% 40 · 5–15% 80 · > 15% 100 |
| Долговая нагрузка (DSCR) | BKI + заявлено + новый платёж | < 1.0 0 · 1.0–1.25 40 · 1.25–2 90 · > 2 130 |
| Кредитная история | BKI | нет истории 40 · положительная 120 · просрочки 30+ за 12 мес 0 · текущая просрочка → manual |
| Исполнительные производства | FSSP | нет 60 · < 1% выручки 30 · > 5% выручки 0 |
| Суды как ответчик (12 мес) | KAD | нет 50 · < 5% выручки 25 · > 10% выручки 0 |
| Налоговая задолженность | PB_NALOG | нет 40 · есть 0 |
| Флаги недостоверности / массовости | PB_NALOG | каждый −40; дисквалифицированный директор — stop |
| Клиент банка (аналог «зарплатного» +30 из xlsx) | BANK_INTERNAL | счёт > 12 мес 60 · < 12 мес 30 · нет 0 |
| Покрытие залогом | заявлено + EGRN | > 100% 80 · 50–100% 40 · нет 0 |
| Поручительство собственника | заявлено | +30 |
| Отраслевой риск ОКВЭД | справочник | высокий −30 · средний 0 · низкий +20 |
| Цель кредита | заявлено | рефинансирование просрочек −50 (аналог −25 из табл. 2) |

Грейды: **A** ≥ 700 · **B** 550–699 · **C** 400–549 · **D** 250–399 · **E** < 250.
Правила решения: A/B → авто-оффер; C → авто-оффер до порога суммы, выше — MANUAL_REVIEW; D → MANUAL_REVIEW; E или stop → REJECTED.

**Stop-факторы** (VERIFYING → REJECTED): ликвидация/реорганизация/банкротство, дисквалифицированный директор, возраст < 6 мес, РНП, исполнительные производства > 20% выручки.

### Максимальная сумма (аналог `СУМк` для бизнеса)

```
free_cf      = avg_monthly_turnover × margin − avg_monthly_expenses − existing_monthly_payments
max_payment  = free_cf / target_DSCR                      -- target_DSCR = 1.25
dscr_cap     = max_payment × (1 − (1 + r)^−T) / r          -- обратная аннуитетная формула, r = ставка/12
revenue_cap  = annual_revenue × k_product                 -- напр. 0.5 для оборотного кредита
collat_cap   = Σ(collateral.value × haircut[type])        -- если продукт требует залог
max_amount   = min(product.max, dscr_cap, revenue_cap, collat_cap?)
```

`ScoreResult.max_amount.limited_by` показывает, какое ограничение сработало. Если `max_amount < requested.amount`, decision engine собирает офферы `ALTERNATIVE`: уменьшить сумму (до `max_amount`), увеличить срок (чтобы уложиться в `max_payment`) или предложить вариант с залогом.

## 5. Сверка «заявлено ↔ найдено» (Findings)

| Код | Сравнение | material, если |
|---|---|---|
| `REVENUE_MISMATCH` | `financials.annual_revenue` vs GIRBO | расхождение > 20% **и** лимит по найденному < запрошенной суммы |
| `UNDECLARED_DEBT` | `debts[]` vs BKI | DSCR с найденными долгами падает ниже 1.25 |
| `DIRECTOR_MISMATCH` | подписант vs EGRUL | всегда (без этого не подписать) |
| `COLLATERAL_OWNER_MISMATCH` | `collateral.owner` vs EGRN | залог нужен для запрошенной суммы |
| `NO_POA` | роль ≠ DIRECTOR и нет доверенности | всегда → info-request на документ `POWER_OF_ATTORNEY` |

Нематериальные расхождения клиенту не показываются: они попадают в досье с `material=false`, скоринг считается по консервативному значению.
