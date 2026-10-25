**Примеры** — заявители сценариев mock-api A–E (`examples/scenario-X.applicant.json`), в теле по умолчанию — сценарий A. Данные в моках привязаны к компании, поэтому анкету берите того же сценария: они в описании POST /applications/{id}/questionnaire.

**A** — ООО «Северный ветер», ИНН 7799000012, опт продуктов — ожидается OFFERED.

```json
{
  "representative": {
    "full_name": "Литвинова Марина Олеговна",
    "inn": "779900010333",
    "role": "ACCOUNTANT"
  },
  "company": {
    "inn": "7799000012",
    "name": "ООО «Северный ветер»"
  }
}
```

**B** — ООО «Стройресурс», ИНН 7799000020, стройка — OFFERED, но меньше сумма или дольше срок.

```json
{
  "representative": {
    "full_name": "Орлов Денис Павлович",
    "inn": "779900020136",
    "role": "DIRECTOR"
  },
  "company": {
    "inn": "7799000020",
    "name": "ООО «Стройресурс»"
  }
}
```

**C** — ООО «Альтаир», ИНН 7799000037, опт — REJECTED.

```json
{
  "representative": {
    "full_name": "Белов Артём Николаевич",
    "inn": "779900030147",
    "role": "DIRECTOR"
  },
  "company": {
    "inn": "7799000037"
  }
}
```

**D** — ИП Смирнова А. В., ИНН 779900010202, интернет-торговля — WAITING_FOR_INFO.

```json
{
  "representative": {
    "full_name": "Смирнова Анна Викторовна",
    "inn": "779900010202",
    "role": "OWNER"
  },
  "company": {
    "inn": "779900010202"
  }
}
```

**E** — ООО «Техно-Сервис», ИНН 7799000044, ремонт оборудования — MANUAL_REVIEW.

```json
{
  "representative": {
    "full_name": "Кузнецов Павел Андреевич",
    "role": "FINANCE_EMPLOYEE"
  },
  "company": {
    "inn": "7799000044",
    "name": "ООО «Техно-Сервис»"
  }
}
```
