# Тестовые данные для ввода в настоящий backend

Актуальность: 26.10.2026. Это справочник значений для формы заявки и HTTP API. Команды запуска — в [отдельной инструкции](run-scenarios.md).

Для обработки заявок настоящим backend нужен `FRONTEND_COLLECTOR_MOCK=false` и поднятый стек data-collector, Kafka, mock-api и credit-engine. Прямой API: `http://localhost:8081/api/v1`; через frontend: `http://localhost:3000/collector/api/v1`. В примерах ниже используется прямой API.

**Пять компаний A–E имеют согласованные наборы ответов внешних систем.** Их ИНН и данные руководителей нужно сохранять вместе. Возраст, сумма, срок и ответы для оценки — вводимые поля; ОГРН, обороты, кредитная история, налоги и реестровые сведения backend получает сам. Принятие анкеты и одобрение кредита — разные результаты: корректная заявка также может получить отказ или запрос уточнений.

Готовые заявки при `SEED_TEST_APPLICATIONS=true` также проходят сбор и расчёт: один черновик и 16 отправленных анкет. Во вкладках досье банка доступны результаты источников, подробный скоринг при достаточных данных и встроенный тестовый PDF. PDF записывается на диск при запуске и исключается из оценки. Итог готовой заявки определяется движком, а не заранее выбранным статусом. Уже обработанные пользователем демонстрационные заявки автоматически не перезапускаются; подробнее — [заполнение кабинета](../data-collector/README.md).

## 1. Все основные компании

| Набор | Что ввести в «ИНН компании или ИП» | Компания | Какой результат демонстрирует |
|---|---|---|---|
| A | `7799000012` | ООО «Северный ветер» | Полные исходные данные, оценка и предложение при достаточных ответах клиента |
| B | `7799000020` | ООО «Стройресурс» | Высокая долговая нагрузка и много кредитных обращений, ручное рассмотрение |
| C | `7799000037` | ООО «Альтаир» | Подтверждённые стоп-факторы, отказ |
| D | `779900010202` | ИП Смирнова Анна Викторовна | Короткая история бизнеса/счёта, маркетплейсы, запрос данных |
| E | `7799000044` | ООО «Техно-Сервис» | Финансовый сотрудник без МЧД, запрос подтверждения полномочий |

На живом полном наборе в README data-collector описаны A → `OFFERED`, B → `MANUAL_REVIEW`, C → `REJECTED`, D/E → `WAITING_FOR_INFO`. Конкретный итог зависит от введённых ответов, результатов источников и состояния банка; один ИНН не фиксирует одобрение. В браузерном MSW исход E отличается — этот документ ориентирован на backend.

Для контроля backend вернёт такие реквизиты из ФНС; вручную их вводить не требуется:

| Набор | ОГРН / ОГРНИП | Дата регистрации | КПП |
|---|---|---|---|
| A | `1217700412345` | `2021-03-14` | `779901001` |
| B | `1197700523456` | `2019-05-20` | `779901001` |
| C | `1207700634567` | `2020-08-17` | `779901001` |
| D | `326774600012345` | `2026-07-28` | Нет |
| E | `1187700745678` | `2018-04-09` | `779901001` |

## 2. Под кем входить и кого указывать представителем

В кабинете выбрать персонажа на экране входа. Пароля клиента нет. Для API отправить `POST /auth/mock-login` с `{"user_id":"..."}` и использовать полученный Bearer-токен. В запросах регистрации и отправки анкеты сохранять одного автора.

| Набор | Пользователь (`user_id`) | ФИО представителя | `representative.role` | `representative.inn` |
|---|---|---|---|---|
| A, директор | `usr_director_north` | Ковалёв Игорь Сергеевич | `DIRECTOR` | `779900010139` |
| A, бухгалтер | `usr_accountant_north` | Литвинова Марина Олеговна | `ACCOUNTANT` | `779900010333` |
| B | `usr_director_stroyresurs` | Орлов Денис Павлович | `DIRECTOR` | `779900020136` |
| C | `usr_director_altair` | Белов Артём Николаевич | `DIRECTOR` | `779900030147` |
| D | `usr_ip_smirnova` | Смирнова Анна Викторовна | `OWNER` | `779900010202` |
| E | `usr_finance_techno` | Кузнецов Павел Андреевич | `FINANCE_EMPLOYEE` | Оставить пустым: в исходном примере отсутствует |

В таблице и JSON дальше A оформляется **бухгалтером Литвиновой**, поскольку готовая `balance.pdf.sig` подписана ею по МЧД. Для A от директора выбрать Ковалёва и `DIRECTOR`, убрать `poa_number`/`representative_inn`, а для подписанного документа использовать пару `balance-director.pdf` / `.sig` из раздела 6.

Роль уже связанного с компанией пользователя берётся из профиля. Тело нового запроса не должно использоваться как способ сменить эту роль. ФИО вошедшего представителя также берётся из его профиля.

## 3. Что вводить в шаги анкеты

### 3.1. Руководитель и личные сведения

| Поле | A | B | C | D | E |
|---|---|---|---|---|---|
| Фамилия | Ковалёв | Орлов | Белов | Смирнова | Зайцева |
| Имя | Игорь | Денис | Артём | Анна | Ольга |
| Отчество | Сергеевич | Павлович | Николаевич | Викторовна | Михайловна |
| ИНН физлица | `779900010139` | `779900020136` | `779900030147` | `779900010202` | `779900040158` |
| Дата рождения для UI | `19.07.1984` | `11.02.1979` | `03.11.1988` | `27.05.1993` | `14.09.1981` |
| Дата рождения для JSON | `1984-07-19` | `1979-02-11` | `1988-11-03` | `1993-05-27` | `1981-09-14` |
| Возраст (`age`) | `42` | `47` | `37` | `33` | `45` |
| Семейное положение | В браке / `MARRIED` | В браке / `MARRIED` | В разводе / `DIVORCED` | Не в браке / `SINGLE` | В браке / `MARRIED` |
| Дети и иждивенцы (`children_count`) | `2` | `3` | `1` | `0` | `2` |

У D руководитель — сама предприниматель. У E представитель — Кузнецов, а руководитель — Зайцева; это разные поля. Возраст и даты взяты из синтетических backend-примеров и не пересчитываются автоматически под текущий календарь. Даты некоторых персонажей в браузерном MSW отличаются — для backend использовать таблицу выше.

### 3.2. Сумма, срок, цель и «Для оценки»

Для всех пяти наборов выбрать **«Оборотный кредит»** (`product_code=WORKING_CAPITAL`), цель **«Пополнить оборот»** (`purpose.code=WORKING_CAPITAL`). Описание: `Закупка товара под сезон`.

| Поле UI / JSON | A | B | C | D | E |
|---|---|---|---|---|---|
| Запрошенная сумма / `requested.amount` | `5000000` | `8000000` | `5000000` | `1000000` | `5000000` |
| Срок / `requested.term_months` | `24` | `36` | `24` | `12` | `24` |
| Максимальная ставка / `scoring.max_annual_rate` | `28` | `28` | `28` | `28` | `28` |
| Минимальная сумма / `scoring.min_acceptable_amount` | `1500000` | `2400000` | `1500000` | `300000` | `1500000` |
| Максимальный срок / `scoring.max_acceptable_term_months` | `36` | `48` | `36` | `24` | `36` |
| Сколько это стоит / `scoring.need_cost` | `5500000` | `8800000` | `5500000` | `1100000` | `5500000` |
| Собственный вклад / `scoring.own_contribution` | `500000` | `800000` | `500000` | `100000` | `500000` |
| Свободные деньги / `scoring.free_cash` | `100000` | `50000` | `100000` | `20000` | `100000` |
| Неснижаемый остаток / `scoring.minimum_cash` | `20000` | `20000` | `20000` | `10000` | `20000` |
| Опыт в этом бизнесе / `scoring.experience_years` | `8` | `8` | `8` | `3` | `8` |
| Чем подтверждена стоимость / `scoring.need_evidence` | `VERIFIED_ESTIMATE` | `VERIFIED_ESTIMATE` | `VERIFIED_ESTIMATE` | `VERIFIED_ESTIMATE` | `VERIFIED_ESTIMATE` |

`VERIFIED_ESTIMATE` на экране — **«Коммерческие предложения или смета»**. Суммы указаны в рублях, сроки — в месяцах, ставка `28` — это 28 %. A использует ответы из теста сборки запроса, B — из e2e рабочего места банка. Числа C–E — подготовленный допустимый ввод для демонстрации соответствующих данных источников, а не зафиксированные исходы расчёта.

Дополнительные поля **только для ИП, набор D**:

| Поле UI | Поле JSON | Ввод |
|---|---|---|
| Основной источник личного дохода | `primary_income_source` | Предпринимательская деятельность / `BUSINESS` |
| Личный доход после налогов в месяц | `net_monthly_income` | `100000` |
| Квалификация | `qualification` | Специалист или служащий / `SPECIALIST` |
| Общий трудовой стаж, мес. | `total_experience_months` | `60` |
| Образование | `education_level` | Высшее / `HIGHER` |
| Автомобиль | `car_class` | Эконом-класс / `ECONOMY` |
| Недвижимость в собственности | `real_estate_status` | Нет / `NONE` |
| Отношения с покупателями | `buyer_contracts` | Повторные разовые заказы / `REPEAT_ORDERS` |
| Где ведёте бизнес | `business_location` | Онлайн или выездная работа / `VERIFIED_ONLINE_OR_MOBILE` |

Все эти поля входят в `scoring`. Они не добавляют недостающие месяцы банковской истории ИП. Если оставить ответы для оценки пустыми, backend всё равно может принять анкету, но далее сформировать вопросы вместо предложения.

### 3.3. Согласия

В UI включить три флажка: БКИ компании, БКИ руководителя, налоговая отчётность. Для налогового согласия включить декларации и ЕНС.

В JSON для каждого согласия нужны `id` и `valid_until`; в готовых наборах также задан `signed_at`:

| Набор | `bki_company.id` | `bki_person.id` | `tax.id` |
|---|---|---|---|
| A | `CNS-2026-000781` | `CNS-2026-000782` | `TAX-CNS-2026-000781` |
| B | `CNS-2026-000801` | `CNS-2026-000802` | `TAX-CNS-2026-000801` |
| C | `CNS-2026-000821` | `CNS-2026-000822` | `TAX-CNS-2026-000821` |
| D | `CNS-2026-000841` | `CNS-2026-000842` | `TAX-CNS-2026-000841` |
| E | `CNS-2026-000861` | `CNS-2026-000862` | `TAX-CNS-2026-000861` |

У всех `valid_until=2027-04-21`. Время подписания: БКИ компании `2026-10-21T14:02:11Z`, БКИ физлица `2026-10-21T14:02:40Z`, налоги `2026-10-21T14:03:02Z`. Налоговый `scope=["DECLARATIONS","ENS_BALANCE"]`.

Это demo-ID согласий. В mock-api дата зафиксирована на **2026-10-22**, она же используется общим стеком для оценки. Согласие БКИ физлица без `director` не принимается. Без согласия источник может быть пропущен; это не означает ошибку формата анкеты.

## 4. Счета, маркетплейсы, ОФД

| Где вводить | Для какой компании есть подготовленный ответ | Значение |
|---|---|---|
| БИК банка / `other_bank_biks` | A | `044525225` |
| БИК банка / `other_bank_biks` | B | `044525777`, `044525650` — два отдельных банка |
| Wildberries / `marketplace_tokens.wildberries` | D | `demo-wb-token` |
| Ozon / `marketplace_tokens.ozon` | D | `demo-ozon-token` |
| Токен ОФД / `ofd_token` | A–E; в базовой анкете используется A | `demo-ofd-token` |

У C/D/E в seed OpenBanking нет дополнительных счетов: произвольный правильный по формату БИК не создаст выписку. Данные продавца маркетплейсов есть только у ИП Смирновой. Мок принимает непустой токен, не начинающийся с `invalid`; реальные учётные данные не нужны. Ключи площадок — только `wildberries`, `ozon`.

Примеры отрицательного ввода токенов: `invalid-wb-token`, `invalid-ozon-token`, `invalid-ofd-token` — проверка отказа источника. Чтобы источник именно опросили, передать непустой токен; отсутствие токена означает пропуск системы.

## 5. Все подготовленные МЧД и объекты залога

### 5.1. МЧД

Вводятся `poa_number` и `representative_inn`. Все три доверенности выданы **компанией A**, не B–E.

| `poa_number` | `representative_inn` | Представитель | Ожидаемый статус |
|---|---|---|---|
| `0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37` | `779900010333` | Литвинова Марина Олеговна | `ACTIVE`; подача заявки и подписание финансовых документов |
| `3c9e7d10-55a2-4f61-8b0e-2a7c4f9d6b12` | `779900010333` | Литвинова Марина Олеговна | `EXPIRED`; срок до 31.12.2025 |
| `a71b4e02-9d3c-4c88-b5f0-81e6c2d3a945` | `779900010417` | Гусев Роман Ильич | `REVOKED`; отозвана 12.05.2026 |

Последняя МЧД выдана **Гусеву**, а не Литвиновой. Для изолированной проверки можно искать её напрямую в реестре mock-api. В профилях входа отдельного пользователя Гусева нет. Для E действующей доверенности в seed нет: номер из A не подтверждает полномочия Кузнецова.

### 5.2. Залог

| Поле анкеты | Что ввести | Чей объект / что вернёт источник |
|---|---|---|
| `collateral.cadastral_numbers` | `50:21:0110203:845` | Склад ООО «Стройресурс», ипотека |
| `collateral.cadastral_numbers` | `50:21:0110203:412` | Участок директора B, свободный |
| `collateral.vins` | `XTC652005N1498213` | Самосвал КАМАЗ, действующий залог B |
| `collateral.serials` | `JCB3CX2023118457` | Экскаватор-погрузчик JCB, действующий залог B |

Эти значения добавлены в готовую анкету B ниже. Другой корректно записанный кадастровый номер не создаст объект в ЕГРН. В C есть залог товаров в обороте без VIN/серийного номера: его источник находит по ИНН `7799000037`, отдельное значение для поля залога вводить не требуется.

## 6. Все готовые файлы для загрузки

Основной каталог: `data-collector/examples/files/`. Оригиналу PDF выбрать тип **«Бухгалтерская отчётность»** (`FINANCIAL_STATEMENTS`). Оригинал и откреплённую `.sig` загрузить вместе. Для подписи внутри PDF отдельная `.sig` не нужна.

| Файл относительно основного каталога | Как загрузить | Результат worker подписи |
|---|---|---|
| `balance.pdf` + `balance.pdf.sig` | Вместе, A от Литвиновой с её МЧД | `VERIFIED`, вердикт `VALID_WITH_WARNINGS` |
| `signature-demo/balance-director.pdf` + `balance-director.pdf.sig` | Вместе, A, директор Ковалёв | `VERIFIED` |
| `signature-demo/balance-tampered.pdf` + `balance-tampered.pdf.sig` | Вместе, отдельная заявка A | `INVALID`: нарушена целостность |
| `signature-demo/balance-revoked.pdf` + `balance-revoked.pdf.sig` | Вместе, A и данные полномочий Литвиновой | `INVALID`: сертификат отозван |
| `signature-demo/balance-other-company.pdf` + `balance-other-company.pdf.sig` | Вместе, заявка A | `INVALID`: подписант другой компании |
| `signature-demo/balance-signed-inside.pdf` | Один файл, A и полномочия Литвиновой | `VERIFIED` |
| `signature-demo/balance-signed-inside-updated.pdf` | Один файл, отдельная заявка A | `INDETERMINATE`: PDF дописан после подписи |

У пар в `signature-demo/` оба файла находятся в **том же подкаталоге**. Все подписи — профиль **TEST**: это проверка тестовых сертификатов и реестров, не проверка УКЭП. Состояние обработки `DONE` и бизнес-вердикт `INVALID` — разные поля отчёта.

До 10 файлов на анкету, включая подписи; каждый до 20 МиБ. Для отрицательных проверок лучше брать отдельные заявки: поддельный документ может повлиять на оценку. Если меняете файлы A, исправить `documents[].file_name` в копии JSON; нельзя объявить `balance.pdf` и не приложить его.

Другие типы документов, которые понимает контракт: `TAX_RETURN`, `BANK_STATEMENT`, `MANAGEMENT_REPORT`, `CHARTER`, `POWER_OF_ATTORNEY`, `DIRECTOR_PASSPORT`, `COLLATERAL_TITLE`, `EGRN_EXTRACT`, `LEASE_AGREEMENT`, `CONTRACT_WITH_CUSTOMER`, `LOAN_AGREEMENT_DRAFT`, `SIGNATURE`, `OTHER`. Указание типа само по себе не делает файл достоверным финансовым документом.

## 7. Готовые JSON: регистрация и полная анкета A–E

Для каждого набора ниже:

1. Войти указанным пользователем через `POST /auth/mock-login`.
2. Передать блок **«Регистрация»** в `POST /applicants` с Bearer-токеном.
3. Взять из ответа id **заявки** `app_…`.
4. Передать блок **«Анкета»** в `POST /applications/{id}/questionnaire` с тем же токеном. Для A приложить файлы через multipart. B–E без файлов можно отправлять как `application/json`.

В отличие от исходных коротких `examples/scenario-*.json`, эти анкеты уже содержат продукт, запрошенные сумму/срок и ответы `scoring`. Перечни, реквизиты, источники и согласия взяты из backend-примеров; числовые дополнения описаны в разделе 3. Отправка одной анкеты не требует сначала пройти остальные сценарии.

### 7.A. ООО «Северный ветер»

Пользователь: `usr_accountant_north`.

**Регистрация:**

```json
{
  "representative": {"full_name": "Литвинова Марина Олеговна", "inn": "779900010333", "role": "ACCOUNTANT"},
  "company": {"inn": "7799000012", "name": "ООО «Северный ветер»"}
}
```

**Анкета:**

```json
{
  "age": 42,
  "marital_status": "MARRIED",
  "children_count": 2,
  "product_code": "WORKING_CAPITAL",
  "requested": {"amount": 5000000, "term_months": 24},
  "purpose": {"code": "WORKING_CAPITAL", "description": "Закупка товара под сезон"},
  "documents": [{"file_name": "balance.pdf", "type": "FINANCIAL_STATEMENTS"}],
  "director": {"inn": "779900010139", "last_name": "Ковалёв", "first_name": "Игорь", "middle_name": "Сергеевич", "birth_date": "1984-07-19"},
  "consents": {
    "bki_company": {"id": "CNS-2026-000781", "signed_at": "2026-10-21T14:02:11Z", "valid_until": "2027-04-21"},
    "bki_person": {"id": "CNS-2026-000782", "signed_at": "2026-10-21T14:02:40Z", "valid_until": "2027-04-21"},
    "tax": {"id": "TAX-CNS-2026-000781", "signed_at": "2026-10-21T14:03:02Z", "valid_until": "2027-04-21", "scope": ["DECLARATIONS", "ENS_BALANCE"]}
  },
  "other_bank_biks": ["044525225"],
  "ofd_token": "demo-ofd-token",
  "poa_number": "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37",
  "representative_inn": "779900010333",
  "scoring": {
    "max_annual_rate": 28,
    "min_acceptable_amount": 1500000,
    "max_acceptable_term_months": 36,
    "need_cost": 5500000,
    "own_contribution": 500000,
    "free_cash": 100000,
    "minimum_cash": 20000,
    "need_evidence": "VERIFIED_ESTIMATE",
    "experience_years": 8
  }
}
```

### 7.B. ООО «Стройресурс»

Пользователь: `usr_director_stroyresurs`.

**Регистрация:**

```json
{
  "representative": {"full_name": "Орлов Денис Павлович", "inn": "779900020136", "role": "DIRECTOR"},
  "company": {"inn": "7799000020", "name": "ООО «Стройресурс»"}
}
```

**Анкета:**

```json
{
  "age": 47,
  "marital_status": "MARRIED",
  "children_count": 3,
  "director": {"inn": "779900020136", "last_name": "Орлов", "first_name": "Денис", "middle_name": "Павлович", "birth_date": "1979-02-11"},
  "consents": {
    "bki_company": {"id": "CNS-2026-000801", "signed_at": "2026-10-21T14:02:11Z", "valid_until": "2027-04-21"},
    "bki_person": {"id": "CNS-2026-000802", "signed_at": "2026-10-21T14:02:40Z", "valid_until": "2027-04-21"},
    "tax": {"id": "TAX-CNS-2026-000801", "signed_at": "2026-10-21T14:03:02Z", "valid_until": "2027-04-21", "scope": ["DECLARATIONS", "ENS_BALANCE"]}
  },
  "other_bank_biks": ["044525777", "044525650"],
  "collateral": {"cadastral_numbers": ["50:21:0110203:845", "50:21:0110203:412"], "vins": ["XTC652005N1498213"], "serials": ["JCB3CX2023118457"]},
  "product_code": "WORKING_CAPITAL",
  "requested": {"amount": 8000000, "term_months": 36},
  "purpose": {"code": "WORKING_CAPITAL", "description": "Закупка товара под сезон"},
  "scoring": {
    "max_annual_rate": 28,
    "min_acceptable_amount": 2400000,
    "max_acceptable_term_months": 48,
    "need_cost": 8800000,
    "own_contribution": 800000,
    "free_cash": 50000,
    "minimum_cash": 20000,
    "need_evidence": "VERIFIED_ESTIMATE",
    "experience_years": 8
  }
}
```

### 7.C. ООО «Альтаир»

Пользователь: `usr_director_altair`.

**Регистрация:**

```json
{
  "representative": {"full_name": "Белов Артём Николаевич", "inn": "779900030147", "role": "DIRECTOR"},
  "company": {"inn": "7799000037"}
}
```

**Анкета:**

```json
{
  "age": 37,
  "marital_status": "DIVORCED",
  "children_count": 1,
  "director": {"inn": "779900030147", "last_name": "Белов", "first_name": "Артём", "middle_name": "Николаевич", "birth_date": "1988-11-03"},
  "consents": {
    "bki_company": {"id": "CNS-2026-000821", "signed_at": "2026-10-21T14:02:11Z", "valid_until": "2027-04-21"},
    "bki_person": {"id": "CNS-2026-000822", "signed_at": "2026-10-21T14:02:40Z", "valid_until": "2027-04-21"},
    "tax": {"id": "TAX-CNS-2026-000821", "signed_at": "2026-10-21T14:03:02Z", "valid_until": "2027-04-21", "scope": ["DECLARATIONS", "ENS_BALANCE"]}
  },
  "product_code": "WORKING_CAPITAL",
  "requested": {"amount": 5000000, "term_months": 24},
  "purpose": {"code": "WORKING_CAPITAL", "description": "Закупка товара под сезон"},
  "scoring": {
    "max_annual_rate": 28,
    "min_acceptable_amount": 1500000,
    "max_acceptable_term_months": 36,
    "need_cost": 5500000,
    "own_contribution": 500000,
    "free_cash": 100000,
    "minimum_cash": 20000,
    "need_evidence": "VERIFIED_ESTIMATE",
    "experience_years": 8
  }
}
```

### 7.D. ИП Смирнова Анна Викторовна

Пользователь: `usr_ip_smirnova`.

**Регистрация:**

```json
{
  "representative": {"full_name": "Смирнова Анна Викторовна", "inn": "779900010202", "role": "OWNER"},
  "company": {"inn": "779900010202"}
}
```

**Анкета:**

```json
{
  "age": 33,
  "marital_status": "SINGLE",
  "children_count": 0,
  "director": {"inn": "779900010202", "last_name": "Смирнова", "first_name": "Анна", "middle_name": "Викторовна", "birth_date": "1993-05-27"},
  "consents": {
    "bki_company": {"id": "CNS-2026-000841", "signed_at": "2026-10-21T14:02:11Z", "valid_until": "2027-04-21"},
    "bki_person": {"id": "CNS-2026-000842", "signed_at": "2026-10-21T14:02:40Z", "valid_until": "2027-04-21"},
    "tax": {"id": "TAX-CNS-2026-000841", "signed_at": "2026-10-21T14:03:02Z", "valid_until": "2027-04-21", "scope": ["DECLARATIONS", "ENS_BALANCE"]}
  },
  "marketplace_tokens": {"wildberries": "demo-wb-token", "ozon": "demo-ozon-token"},
  "product_code": "WORKING_CAPITAL",
  "requested": {"amount": 1000000, "term_months": 12},
  "purpose": {"code": "WORKING_CAPITAL", "description": "Закупка товара под сезон"},
  "scoring": {
    "max_annual_rate": 28,
    "min_acceptable_amount": 300000,
    "max_acceptable_term_months": 24,
    "need_cost": 1100000,
    "own_contribution": 100000,
    "free_cash": 20000,
    "minimum_cash": 10000,
    "need_evidence": "VERIFIED_ESTIMATE",
    "experience_years": 3,
    "primary_income_source": "BUSINESS",
    "net_monthly_income": 100000,
    "qualification": "SPECIALIST",
    "total_experience_months": 60,
    "education_level": "HIGHER",
    "car_class": "ECONOMY",
    "real_estate_status": "NONE",
    "buyer_contracts": "REPEAT_ORDERS",
    "business_location": "VERIFIED_ONLINE_OR_MOBILE"
  }
}
```

### 7.E. ООО «Техно-Сервис»

Пользователь: `usr_finance_techno`.

**Регистрация:**

```json
{
  "representative": {"full_name": "Кузнецов Павел Андреевич", "role": "FINANCE_EMPLOYEE"},
  "company": {"inn": "7799000044", "name": "ООО «Техно-Сервис»"}
}
```

**Анкета:**

```json
{
  "age": 45,
  "marital_status": "MARRIED",
  "children_count": 2,
  "director": {"inn": "779900040158", "last_name": "Зайцева", "first_name": "Ольга", "middle_name": "Михайловна", "birth_date": "1981-09-14"},
  "consents": {
    "bki_company": {"id": "CNS-2026-000861", "signed_at": "2026-10-21T14:02:11Z", "valid_until": "2027-04-21"},
    "bki_person": {"id": "CNS-2026-000862", "signed_at": "2026-10-21T14:02:40Z", "valid_until": "2027-04-21"},
    "tax": {"id": "TAX-CNS-2026-000861", "signed_at": "2026-10-21T14:03:02Z", "valid_until": "2027-04-21", "scope": ["DECLARATIONS", "ENS_BALANCE"]}
  },
  "product_code": "WORKING_CAPITAL",
  "requested": {"amount": 5000000, "term_months": 24},
  "purpose": {"code": "WORKING_CAPITAL", "description": "Закупка товара под сезон"},
  "scoring": {
    "max_annual_rate": 28,
    "min_acceptable_amount": 1500000,
    "max_acceptable_term_months": 36,
    "need_cost": 5500000,
    "own_contribution": 500000,
    "free_cash": 100000,
    "minimum_cash": 20000,
    "need_evidence": "VERIFIED_ESTIMATE",
    "experience_years": 8
  }
}
```

## 8. Как отправить готовый набор и убедиться, что backend его обрабатывает

Пример A из корня репозитория, нужны Bash, `curl`, `jq`. Сохранить два JSON-блока A из раздела 7 в `/tmp/backend-a.applicant.json` и `/tmp/backend-a.questionnaire.json`. Это **новые файлы с полным вводом из документа**, а не сокращённый исходный `scenario-a.json`.

```bash
DEMO_API=http://localhost:8081/api/v1
DEMO_TOKEN=$(curl --fail-with-body -sS "$DEMO_API/auth/mock-login" \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"usr_accountant_north"}' | jq -er '.token')

DEMO_APP=$(curl --fail-with-body -sS "$DEMO_API/applicants" \
  -H "Authorization: Bearer $DEMO_TOKEN" -H 'Content-Type: application/json' \
  --data-binary @/tmp/backend-a.applicant.json | jq -er '.id')

curl --fail-with-body -sS -i "$DEMO_API/applications/$DEMO_APP/questionnaire" \
  -H "Authorization: Bearer $DEMO_TOKEN" \
  -F 'questionnaire=</tmp/backend-a.questionnaire.json' \
  -F 'files=@data-collector/examples/files/balance.pdf' \
  -F 'files=@data-collector/examples/files/balance.pdf.sig'

curl --fail-with-body -sS "$DEMO_API/applications/$DEMO_APP" \
  -H "Authorization: Bearer $DEMO_TOKEN" | jq .
```

Для B–E заменить пользователя и оба тела на соответствующий набор. Если файлов нет, отправка анкеты выглядит так; переменная `DEMO_QUESTIONNAIRE` — абсолютный путь к сохранённому JSON выбранного набора:

```bash
curl --fail-with-body -sS -i "$DEMO_API/applications/$DEMO_APP/questionnaire" \
  -H "Authorization: Bearer $DEMO_TOKEN" -H 'Content-Type: application/json' \
  --data-binary "@$DEMO_QUESTIONNAIRE"
```

Ожидается `202` от отправки анкеты и `Location` с id задачи. В заявке появится `collection_task_id`. Далее проверить:

| Запрос с тем же Bearer-токеном | Что смотреть |
|---|---|
| `GET /applications/{app_id}` | Статус заявки, документы, `collection_task_id` |
| `GET /collections/{task_id}` | `PENDING → RUNNING → COMPLETED` либо причина `FAILED`; результаты систем |
| `GET /applications/{app_id}/decision` | Итог оценки; до готовности возможен `404` |
| `GET /applications/{app_id}/offers` | Предложение, если оно сформировано |
| `GET /applications/{app_id}/info-requests` | Какие ответы/документы реально требуются |

Использовать возвращённые id, а не выдуманные `app_…`/`tsk_…`. Регистрация того же автора по той же компании может вернуть его прежний черновик. После отправки анкеты повторная отправка в ту же заявку не является новым сценарием: нужна следующая регистрация и новый черновик. В UI для просмотра A войти как Литвинова — автор API-примера.

## 9. Что вводить в ответы банку и решения сотрудника

### Ответы клиента

Вопросы живого backend строятся из недостающих фактов. Не создавать заранее фиктивный id вопроса: взять его из `/info-requests`.

| Что спросил backend | Пример ввода |
|---|---|
| Свободные деньги D | `20000`, число рублей |
| Максимальная ставка | `28`, число процентов |
| Минимальная сумма | Значение своего набора из раздела 3 |
| Стоимость / собственный вклад / опыт | Соответствующее значение из раздела 3 |
| Зачем были кредитные обращения у B | `Рефинансировали лизинг техники под меньшую ставку` |
| Документ / выписка | Соответствующий файл, тип из вопроса; для полноценного анализа нужна выписка, а не переименованный баланс |

Для вопроса с денежным вводом пример тела `POST /applications/{id}/info-requests/{requestId}/answer`:

```json
{"value":20000,"comment":"Уточнённый остаток свободных денег"}
```

Для текстового вопроса:

```json
{"value":"Рефинансировали лизинг техники под меньшую ставку"}
```

При требовании документа используется multipart с частью `answer` и частями `files`; один текст не заменяет обязательное вложение. Готовых содержательных банковских выписок и МЧД для E в репозитории нет. После ответов банк может передать заявку специалисту; загрузка файла не гарантирует автоматическое появление всех финансовых фактов.

### Сотрудники банка

| ФИО | `user_id` | Для чего |
|---|---|---|
| Соколова Мария Игоревна | `usr_underwriter_sokolova` | Взять B в работу, задать вопрос, одобрить или отказать |
| Орлов Дмитрий Андреевич | `usr_underwriter_orlov` | Взять другую B и проверить ограничение решения чужим исполнителем |

Для демонстрации ручного одобрения B: сумма `8000000`, срок `36`, ставка `23,5` %; внутренний комментарий `Обращения — рефинансирование лизинга`. Для запроса данных: `Для чего брали кредиты за последние 90 дней?`. Для отказа выбрать `Негативная кредитная история`. Это ввод сотрудника в соответствующей форме решения, а не JSON анкеты клиента.

## 10. Дополнительные подготовленные стоп-проверки

Это входы **конкретных внешних эмуляторов**, а не дополнительные полные компании A–E. Их можно использовать для отдельной проверки источника либо намеренной замены данных проверяемого физлица; все остальные источники для этих субъектов не обеспечены.

### 10.1. Дисквалификация и недействительный паспорт

| Поле | Ввод |
|---|---|
| Фамилия | Лаптев |
| Имя | Сергей |
| Отчество | Юрьевич |
| ИНН физлица | `779900099018` |
| Дата рождения | `1975-12-01` / `01.12.1975` |

В ФНС есть запись дисквалификации с 01.03.2026 до 01.03.2027; в Госуслугах `passport.valid=false`. Не подменять этим человеком Ковалёва и одновременно ожидать подтверждения, что он директор компании A.

### 10.2. Все субъекты из тестовых санкционных списков

| Тип `type` | ИНН, если есть | `name` / `full_name` | Дата рождения | Список |
|---|---|---|---|---|
| `PERSON` | `779900099015` | Громов Виктор Андреевич | `1979-03-02` | `RFM_TERRORISM_EXTREMISM` |
| `LEGAL` | `7799000099` | ООО «ТРАНЗИТ-ИНВЕСТ» | — | `RFM_FREEZE`, `OFAC_SDN` |
| `PERSON` | Нет | Ковалев Игорь Петрович | `1962-01-15` | `EU` |
| `PERSON` | Нет | Белов Артем Николаевич | `1971-06-30` | `UK` |
| `LEGAL` | `7799000507` | ООО «СЕВЕРНЫЙ ВЕТЕР ТРЕЙД» | — | `OFAC_SDN` |
| `PERSON` | Нет | Орлов Денис Петрович | `1990-12-01` | `RFM_TERRORISM_EXTREMISM` |

Одна компания «ТРАНЗИТ-ИНВЕСТ» имеет две записи списка; всего в seed 7 записей для 6 субъектов. Тёзки Ковалёва/Белова/Орлова имеют другие персональные данные, чем руководители A/B/C. У физлица без совпадающего ИНН для совпадения нужны ФИО **и та же дата рождения**.

Прямой вызов источника (порт 8090, другой контракт):

```bash
curl --fail-with-body -sS http://localhost:8090/compliance/v1/screening \
  -H 'Content-Type: application/json' \
  -d '{"lists":["RFM_TERRORISM_EXTREMISM","RFM_FREEZE","OFAC_SDN"],"subjects":[{"ref":"test-person","type":"PERSON","inn":"779900099015","full_name":"Громов Виктор Андреевич","birth_date":"1979-03-02"},{"ref":"test-company","type":"LEGAL","inn":"7799000099","name":"ООО ТРАНЗИТ-ИНВЕСТ"}]}'
```

Ожидается `listed=true` для обоих. Изменение тела этого запроса не создаёт заявку в data-collector.

## 11. Допустимые значения остальных полей

### Продукты: какие ещё суммы/сроки backend принимает

| `product_code` | Сумма, ₽ | Срок, мес. | Кому |
|---|---|---|---|
| `WORKING_CAPITAL` | 300 000–30 000 000 | 3–36 | ООО, ИП |
| `CREDIT_LINE` | 1 000 000–50 000 000 | 6–24 | ООО |
| `EXPRESS` | 100 000–5 000 000 | 3–24 | ООО, ИП |
| `VEHICLE` | 500 000–20 000 000 | 12–60 | ООО, ИП |
| `EQUIPMENT` | 500 000–50 000 000 | 12–60 | ООО, ИП |
| `REAL_ESTATE` | 1 000 000–100 000 000 | 12–120 | ООО, ИП |
| `REFINANCING` | 500 000–50 000 000 | 12–60 | ООО, ИП |

Коды целей `purpose.code`: `WORKING_CAPITAL`, `EQUIPMENT`, `VEHICLE`, `REAL_ESTATE`, `REFINANCING`, `BUSINESS_EXPANSION`, `TENDER`, `OTHER`. Для основного примера брать одинаковую цель из раздела 3; смена продукта/цели может поменять маршрут оценки.

### Все коды ролей и ответов

| Поле | Что принимает контракт |
|---|---|
| `representative.role` | `OWNER`, `DIRECTOR`, `CFO`, `ACCOUNTANT`, `FINANCE_EMPLOYEE`, `BANKING_EMPLOYEE`, `AUTHORIZED_EMPLOYEE` |
| `marital_status` | `SINGLE`, `MARRIED`, `DIVORCED`, `DIVORCED_MULTIPLE` |
| `scoring.need_evidence` | `CONTRACT_AND_COST`, `VERIFIED_ESTIMATE`, `ORAL_ONLY` |
| `scoring.primary_income_source` | `BUSINESS`, `PUBLIC_EMPLOYMENT`, `PRIVATE_EMPLOYMENT`, `PENSION`, `SELF_EMPLOYED_OR_GPH`, `NONE` |
| `scoring.qualification` | `NONE`, `SUPPORT`, `SPECIALIST`, `MANAGER_OR_HIGHLY_QUALIFIED` |
| `scoring.car_class` | `NONE`, `ECONOMY`, `COMFORT`, `PREMIUM` |
| `scoring.education_level` | `BELOW_GENERAL`, `GENERAL`, `VOCATIONAL`, `HIGHER`, `ACADEMIC_DEGREE` |
| `scoring.real_estate_status` | `UNENCUMBERED_WHOLE`, `UNENCUMBERED_SHARE`, `ENCUMBERED_ONLY`, `NONE` |
| `scoring.business_location` | `VERIFIED_PHYSICAL`, `VERIFIED_ONLINE_OR_MOBILE`, `UNVERIFIED` |
| `scoring.buyer_contracts` | `ACTIVE_AND_PERFORMED`, `REPEAT_ORDERS`, `PLANS_ONLY` |

Для `VERIFIED_PHYSICAL` в UI также указать `scoring.location_stability_months`, например `24`.

Основные ограничения:

- ИНН компании — 10 или 12 цифр, ИНН физлица — 12; использовать строки, не числа.
- `age`: 18–100, `children_count`: 0–20. Все три поля личной анкеты обязательны, включая явный ноль детей.
- БИК — 9 цифр, банков до 10. VIN — 17 символов, без I/O/Q. Серийный номер — 1–64 символа; всего объектов залога до 20.
- Для руководителя нужны ИНН, фамилия, имя и корректная дата рождения; согласие его БКИ требует присутствия `director`.
- Ставка `scoring.max_annual_rate` больше 0 и не больше 100; денежные значения и счётчики неотрицательны. Минимальную приемлемую сумму в UI задавать не больше запрошенной, максимальный срок — не короче запрошенного.
- `poa_number` — UUID в нижнем регистре. Отсутствующий номер и неверный формат — разные случаи.
- Название компании и ОГРН из ответа источника не являются полями, которыми можно подменить компанию после регистрации. ИНН для сбора берётся из созданной заявки.

## 12. Какие источники получат данные по этим значениям

| Источник | Какие данные из документа используются |
|---|---|
| `CORE_BANKING` | ИНН A–E; обороты, операции, собственные кредиты банка |
| `FNS` | ИНН A–E, ИНН руководителя; ЕГРЮЛ/ЕГРИП, БФО, блокировки, дисквалификация |
| `BKI` | ИНН A–E и руководителя, соответствующие согласия |
| `COMPLIANCE` | ИНН компании, данные руководителя; дополнительные стоп-субъекты раздела 10 |
| `FSSP` | ИНН A–E и руководителей |
| `KAD` | ИНН A–E |
| `FEDRESURS` | ИНН A–E и руководителей |
| `M4D` | Номер доверенности / данные компании и представителя |
| `TAX_REPORTS` | ИНН A–E и налоговое согласие со scope |
| `PLEDGE_REGISTRY` | ИНН A–E, VIN и серийный номер B |
| `EGRN` | Два кадастровых номера B |
| `GOSUSLUGI` | Данные пяти руководителей; отдельно Лаптев |
| `OPEN_BANKING` | ИНН и БИК A/B; у остальных наборов список дополнительных счетов пуст |
| `MARKETPLACES` | Только D и токены Wildberries/Ozon |
| `OFD` | ИНН A–E и токен ОФД |
| `ZAKUPKI` | ИНН A–E |
| `SIGNALS` | ИНН A–E |
| `CBR` | Общая тестовая ключевая ставка; вручную ставка ЦБ не вводится |
| `HH` | Подготовленные работодатели и вакансии; отдельного поля hh.ru в анкете нет |

Наличие записи в seed не гарантирует непустой список по каждой ручке: например, отсутствие судебных дел — нормальный ответ. Статусы источников `DONE`, `PARTIAL`, `FAILED`, `SKIPPED` смотреть отдельно от итогового статуса заявки.

## 13. Данные для отдельного API credit-engine

Это другой вход: `POST http://localhost:8082/api/v1/decisions`. В нём уже передаются финансовая история, риски и факты, а не анкета data-collector. Для 17 независимых историй подготовлены следующие JSON. Отправляется только поле `request`; `expected` — эталон ответа, а `service_b_response` — данные отдельного поставщика отчётности.

| Набор / JSON | ИНН заёмщика | Запрошено, ₽ / мес. | Ожидаемый статус |
|---|---|---|---|
| [uc01_ip_need_data](../credit-engine/testdata/decisions/uc01_ip_need_data.json) | `990000000115` | 3000000 / 24 | `NEED_INFO` |
| [uc02_ip_declined](../credit-engine/testdata/decisions/uc02_ip_declined.json) | `990000000299` | 3000000 / 24 | `REJECTED` |
| [uc03_ip_manual](../credit-engine/testdata/decisions/uc03_ip_manual.json) | `990000000362` | 3000000 / 24 | `UNDER_REVIEW` |
| [uc04_ip_approved](../credit-engine/testdata/decisions/uc04_ip_approved.json) | `990000000436` | 3000000 / 24 | `APPROVED` |
| [uc05_ip_bank_review](../credit-engine/testdata/decisions/uc05_ip_bank_review.json) | `990000000500` | 3000000 / 24 | `UNDER_REVIEW` |
| [uc01_le_need_data](../credit-engine/testdata/decisions/uc01_le_need_data.json) | `9900000063` | 3000000 / 24 | `NEED_INFO` |
| [uc02_le_declined](../credit-engine/testdata/decisions/uc02_le_declined.json) | `9900000070` | 3000000 / 24 | `REJECTED` |
| [uc03_le_manual](../credit-engine/testdata/decisions/uc03_le_manual.json) | `9900000088` | 3000000 / 24 | `UNDER_REVIEW` |
| [uc04_le_approved](../credit-engine/testdata/decisions/uc04_le_approved.json) | `9900000095` | 3000000 / 24 | `APPROVED` |
| [uc05_le_bank_review](../credit-engine/testdata/decisions/uc05_le_bank_review.json) | `9900000105` | 3000000 / 24 | `UNDER_REVIEW` |
| [extra_ip_confirmed_stop](../credit-engine/testdata/decisions/extra_ip_confirmed_stop.json) | `990000001310` | 3000000 / 24 | `REJECTED` |
| [extra_le_stop_before_missing_data](../credit-engine/testdata/decisions/extra_le_stop_before_missing_data.json) | `9900000144` | 3000000 / 24 | `REJECTED` |
| [auto_ip_extend_term](../credit-engine/testdata/decisions/auto_ip_extend_term.json) | `990000006460` | 3000000 / 12 | `APPROVED` |
| [auto_le_reduce_amount](../credit-engine/testdata/decisions/auto_le_reduce_amount.json) | `9900000659` | 5000000 / 12 | `APPROVED` |
| [auto_ip_no_acceptable_amount](../credit-engine/testdata/decisions/auto_ip_no_acceptable_amount.json) | `990000006607` | 5000000 / 12 | `UNDER_REVIEW` |
| [auto_le_rate_too_low](../credit-engine/testdata/decisions/auto_le_rate_too_low.json) | `9900000673` | 3000000 / 24 | `UNDER_REVIEW` |
| `uc06_dynamic_state`: [uc06_01_ip_issued](../credit-engine/testdata/decisions/uc06_01_ip_issued.json) → [uc06_02_le_bank_review](../credit-engine/testdata/decisions/uc06_02_le_bank_review.json) | `990000001172` → `9900000120` | 3000000 / 24 → 2000000 / 24 | `APPROVED` → `UNDER_REVIEW` |


Дата оценки этих эталонов — **2026-10-23**, начальное состояние банка и порядок шагов заданы в [manifest](../credit-engine/testdata/scenarios.json). ИНН `990…` из таблицы не являются дополнительными готовыми компаниями для формы A–E. Для воспроизведения нужен соответствующий поставщик отчётности и профиль банка. Команды и все зависимости — в [приёмке credit-engine](../credit-engine/docs/37_reproducible_acceptance.md).

Также есть 7 готовых запросов проверки самого банка в [testdata/bank](../credit-engine/testdata/bank). Их тела предназначены для API МС2, не для `/decisions` и не для анкеты.

## 14. Финансовые файлы и данные, которые не являются готовыми полными профилями

### Финансовые документы

Для проверки загрузки без распознавания используйте кнопку «Добавить тестовый PDF» в кабинете
или файл `frontend/public/test-documents/test-financial-document.pdf`. Backend принимает точный
образец с `is_test: true` без подписи и финансового контекста. Его показатели и наличие файла
не учитываются в скоринге. Изменённые байты или одно только такое же имя не дают исключения.
Подробности: [тестовый PDF](financial-documents.md#тестовый-pdf-для-проверки-кабинета).

Два поддерживаемых ввода: ИП на УСН «Доходы» — КУДиР XLS/XLSX типа `TAX_RETURN`; ООО — PDF ГИРБО типа `FINANCIAL_STATEMENTS`. Файл должен содержать реквизиты нужного заёмщика. Нужен контекст существующей заявки: `financial_document_context.tax_regime=USN_INCOME` либо `financial_document_context.legal_form=OOO`.

Собственные локальные образцы называются `kudir-2026.xls`, `girbo-2025.pdf`, а реквизиты и ожидаемые числа задаются в локальном `cases.json`. **Эти реальные образцы в Git не поставляются**; `balance.pdf` из проверки подписи не объявляется готовой КУДиР/отчётностью ГИРБО. Отдельного публичного setter API контекста заявки нет, поэтому это ввод для подготовленной backend-интеграции. Детали: [финансовые документы](financial-documents.md), [локальные образцы](../data-collector/financial_parser/testdata/README.md).

### Три дополнительные компании тестовых пользователей

| Компания | ИНН | Пользователи |
|---|---|---|
| ООО «Ромашка» | `7701234567` | `usr_director_romashka`, `usr_accountant_romashka` |
| ИП Смирнов К. А. | `771234567890` | `usr_ip_smirnov` |
| ООО «Вектор Строй» | `7724567890` | `usr_director_vector` |

Эти компании заведены в backend для входа/профилей и старых демонстрационных экранов, но согласованных наборов A–E во внешних системах для них нет. Наличие строки в `companies` не означает, что по этому ИНН будут получены все данные для расчёта.

`app_000123` и другие короткие id старых демо-заявок, фиксированный исход по ИНН `7799000077`, произвольные файлы `dogovor.pdf.sig` из UI-тестов — данные браузерных моков. Для реальной обработки использовать id `app_…`, возвращённый backend, и файлы/профили из этого документа.

## 15. Что проверено при подготовке

- Пять запросов `POST /api/v1/counterparty/lookup` с токенами соответствующих тестовых пользователей выполнены на запущенном backend: для всех A–E получен HTTP **200**, реквизиты совпали с seed ФНС. Без входа этот endpoint на проверенном стенде вернул 401, поэтому команды используют Bearer-токен.
- Готовые JSON сверены с исходными регистрациями/анкетами, перечнями значений и ограничениями backend; источники и пути файлов проверены по репозиторию.
- Новые заявки и выдачи при подготовке справочника не создавались. Полный сбор/скоринг всех пяти JSON в рамках этой задачи не запускался; бизнес-итоги описаны как ожидаемые, а не подтверждённый новый прогон.

Источники: [примеры A–E](../data-collector/examples), [пользователи](../data-collector/internal/service/testusers.go), [DTO HTTP API](../data-collector/internal/transport/http/dto.go), [проверка анкеты](../data-collector/internal/service/questionnaire.go), [проверка входов сбора](../data-collector/internal/service/collection.go), [ответы для оценки](../data-collector/internal/domain/scoring.go), [ФНС](../mock-api/FNS/seed), [Госуслуги](../mock-api/Gosuslugi/seed), [МЧД](../mock-api/M4D/seed), [санкционные списки](../mock-api/Compliance/seed/lists.json), [файлы подписи](../data-collector/examples/files/signature-demo/README.md).
