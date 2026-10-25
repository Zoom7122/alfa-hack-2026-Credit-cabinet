**Примеры анкет** — сценарии mock-api A–E (`examples/scenario-X.json`). Заявку регистрируйте примером того же сценария из POST /applicants: данные в моках привязаны к компании.

**A** — ООО «Северный ветер», ИНН 7799000012, опт продуктов — ожидается OFFERED. В documents указан тип файла: приложите в files `balance.pdf` и подпись `balance.pdf.sig` из `examples/files` или уберите documents.

```json
{
  "age": 42,
  "marital_status": "MARRIED",
  "children_count": 2,
  "product_code": "WORKING_CAPITAL",
  "requested": {
    "amount": 5000000,
    "term_months": 24
  },
  "purpose": {
    "code": "WORKING_CAPITAL",
    "description": "Закупка товара под сезон"
  },
  "documents": [
    {
      "file_name": "balance.pdf",
      "type": "FINANCIAL_STATEMENTS"
    }
  ],
  "director": {
    "inn": "779900010139",
    "last_name": "Ковалёв",
    "first_name": "Игорь",
    "middle_name": "Сергеевич",
    "birth_date": "1984-07-19"
  },
  "consents": {
    "bki_company": {
      "id": "CNS-2026-000781",
      "signed_at": "2026-10-21T14:02:11Z",
      "valid_until": "2027-04-21"
    },
    "bki_person": {
      "id": "CNS-2026-000782",
      "signed_at": "2026-10-21T14:02:40Z",
      "valid_until": "2027-04-21"
    },
    "tax": {
      "id": "TAX-CNS-2026-000781",
      "signed_at": "2026-10-21T14:03:02Z",
      "valid_until": "2027-04-21",
      "scope": [
        "DECLARATIONS",
        "ENS_BALANCE"
      ]
    }
  },
  "other_bank_biks": [
    "044525225"
  ],
  "ofd_token": "demo-ofd-token",
  "poa_number": "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37",
  "representative_inn": "779900010333"
}
```

**B** — ООО «Стройресурс», ИНН 7799000020, стройка — OFFERED, но меньше сумма или дольше срок.

```json
{
  "age": 47,
  "marital_status": "MARRIED",
  "children_count": 3,
  "director": {
    "inn": "779900020136",
    "last_name": "Орлов",
    "first_name": "Денис",
    "middle_name": "Павлович",
    "birth_date": "1979-02-11"
  },
  "consents": {
    "bki_company": {
      "id": "CNS-2026-000801",
      "signed_at": "2026-10-21T14:02:11Z",
      "valid_until": "2027-04-21"
    },
    "bki_person": {
      "id": "CNS-2026-000802",
      "signed_at": "2026-10-21T14:02:40Z",
      "valid_until": "2027-04-21"
    },
    "tax": {
      "id": "TAX-CNS-2026-000801",
      "signed_at": "2026-10-21T14:03:02Z",
      "valid_until": "2027-04-21",
      "scope": [
        "DECLARATIONS",
        "ENS_BALANCE"
      ]
    }
  },
  "other_bank_biks": [
    "044525777",
    "044525650"
  ],
  "collateral": {
    "cadastral_numbers": [
      "50:21:0110203:845",
      "50:21:0110203:412"
    ],
    "vins": [
      "XTC652005N1498213"
    ],
    "serials": [
      "JCB3CX2023118457"
    ]
  }
}
```

**C** — ООО «Альтаир», ИНН 7799000037, опт — REJECTED.

```json
{
  "age": 37,
  "marital_status": "DIVORCED",
  "children_count": 1,
  "director": {
    "inn": "779900030147",
    "last_name": "Белов",
    "first_name": "Артём",
    "middle_name": "Николаевич",
    "birth_date": "1988-11-03"
  },
  "consents": {
    "bki_company": {
      "id": "CNS-2026-000821",
      "signed_at": "2026-10-21T14:02:11Z",
      "valid_until": "2027-04-21"
    },
    "bki_person": {
      "id": "CNS-2026-000822",
      "signed_at": "2026-10-21T14:02:40Z",
      "valid_until": "2027-04-21"
    },
    "tax": {
      "id": "TAX-CNS-2026-000821",
      "signed_at": "2026-10-21T14:03:02Z",
      "valid_until": "2027-04-21",
      "scope": [
        "DECLARATIONS",
        "ENS_BALANCE"
      ]
    }
  }
}
```

**D** — ИП Смирнова А. В., ИНН 779900010202, интернет-торговля — WAITING_FOR_INFO.

```json
{
  "age": 33,
  "marital_status": "SINGLE",
  "children_count": 0,
  "director": {
    "inn": "779900010202",
    "last_name": "Смирнова",
    "first_name": "Анна",
    "middle_name": "Викторовна",
    "birth_date": "1993-05-27"
  },
  "consents": {
    "bki_company": {
      "id": "CNS-2026-000841",
      "signed_at": "2026-10-21T14:02:11Z",
      "valid_until": "2027-04-21"
    },
    "bki_person": {
      "id": "CNS-2026-000842",
      "signed_at": "2026-10-21T14:02:40Z",
      "valid_until": "2027-04-21"
    },
    "tax": {
      "id": "TAX-CNS-2026-000841",
      "signed_at": "2026-10-21T14:03:02Z",
      "valid_until": "2027-04-21",
      "scope": [
        "DECLARATIONS",
        "ENS_BALANCE"
      ]
    }
  },
  "marketplace_tokens": {
    "wildberries": "demo-wb-token",
    "ozon": "demo-ozon-token"
  }
}
```

**E** — ООО «Техно-Сервис», ИНН 7799000044, ремонт оборудования — MANUAL_REVIEW.

```json
{
  "age": 45,
  "marital_status": "MARRIED",
  "children_count": 2,
  "director": {
    "inn": "779900040158",
    "last_name": "Зайцева",
    "first_name": "Ольга",
    "middle_name": "Михайловна",
    "birth_date": "1981-09-14"
  },
  "consents": {
    "bki_company": {
      "id": "CNS-2026-000861",
      "signed_at": "2026-10-21T14:02:11Z",
      "valid_until": "2027-04-21"
    },
    "bki_person": {
      "id": "CNS-2026-000862",
      "signed_at": "2026-10-21T14:02:40Z",
      "valid_until": "2027-04-21"
    },
    "tax": {
      "id": "TAX-CNS-2026-000861",
      "signed_at": "2026-10-21T14:03:02Z",
      "valid_until": "2027-04-21",
      "scope": [
        "DECLARATIONS",
        "ENS_BALANCE"
      ]
    }
  }
}
```
