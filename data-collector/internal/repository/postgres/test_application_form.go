package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func testApplicationForm(seed testApplication, id string, now time.Time) ([]byte, error) {
	c := testCompanies[seed.Company]
	ages := map[string]int{"romashka": 42, "smirnov": 35, "vector": 39, "north": 42, "stroyresurs": 47, "altair": 37, "smirnova": 33, "techno": 45}
	age := ages[seed.Company]
	if age == 0 {
		age = 40
	}
	inns := map[string]string{"romashka": "770112345695", "vector": "772412345678", "technoline": "770912345678", "sever_agro": "290112345678", "bakery": "500112345678", "beta": "781112345678", "north": "779900010139", "stroyresurs": "779900020136", "altair": "779900030147", "techno": "779900040158"}
	directorINN := inns[seed.Company]
	if len(c.INN) == 12 {
		directorINN = c.INN
	}
	names := strings.Fields(c.DirectorName)
	director := map[string]any{"inn": directorINN, "last_name": names[0], "first_name": names[1], "middle_name": "",
		"birth_date": now.AddDate(-age, 0, -1).Format(time.DateOnly)}
	if len(names) > 2 {
		director["middle_name"] = names[2]
	}
	marital, children := "MARRIED", 2
	if seed.Company == "smirnova" {
		marital, children = "SINGLE", 0
	}
	signed := now.Add(-24 * time.Hour).Format(time.RFC3339)
	consents := map[string]any{}
	for _, key := range []string{"bki_company", "bki_person", "tax"} {
		value := map[string]any{"id": fmt.Sprintf("DEMO-%d-%s", seed.ID, key), "signed_at": signed, "valid_until": now.AddDate(0, 6, 0).Format(time.DateOnly)}
		if key == "tax" {
			value["scope"] = []string{"DECLARATIONS", "ENS_BALANCE"}
		}
		consents[key] = value
	}
	banks := []string{}
	if seed.Company == "north" {
		banks = []string{"044525225"}
	}
	if seed.Company == "stroyresurs" {
		banks = []string{"044525777", "044525650"}
	}
	collect := map[string]any{"inn": c.INN, "director": director, "consents": consents, "other_bank_biks": banks,
		"collateral": map[string]any{"cadastral_numbers": []string{}, "vins": []string{}, "serials": []string{}}}
	scoring := map[string]any{"max_annual_rate": 28, "min_acceptable_amount": seed.Amount * 3 / 10,
		"max_acceptable_term_months": seed.Term + 12, "need_cost": seed.Amount * 11 / 10, "own_contribution": seed.Amount / 10,
		"free_cash": 100_000, "minimum_cash": 20_000, "need_evidence": "VERIFIED_ESTIMATE", "experience_years": 8}
	if len(c.INN) == 12 {
		for key, value := range map[string]any{"primary_income_source": "BUSINESS", "net_monthly_income": 100_000,
			"qualification": "SPECIALIST", "total_experience_months": 60, "education_level": "HIGHER", "car_class": "ECONOMY",
			"real_estate_status": "NONE", "buyer_contracts": "REPEAT_ORDERS", "business_location": "VERIFIED_ONLINE_OR_MOBILE"} {
			scoring[key] = value
		}
	}
	purpose := seed.Product
	if purpose == "CREDIT_LINE" || purpose == "EXPRESS" {
		purpose = "WORKING_CAPITAL"
	}
	form := map[string]any{"product_code": seed.Product, "requested": map[string]any{"amount": seed.Amount, "term_months": seed.Term},
		"purpose": map[string]any{"code": purpose, "description": "Тестовая заявка для демонстрации кабинета"},
		"questionnaire": map[string]any{"id": strings.Replace(id, "app_", "qst_", 1), "age": age, "marital_status": marital,
			"children_count": children, "created_at": now.Add(-24 * time.Hour), "collect": collect}, "scoring": scoring}
	return json.Marshal(form)
}

// Дополняем один раз ранее созданные нами тестовые анкеты. Сохранённый пользователем
// draft, даже пустой, никогда не заполняется заново тестовыми значениями.
func upgradeTestApplicationForm(ctx context.Context, tx pgx.Tx, seed testApplication, id string, now time.Time) error {
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT form FROM applications WHERE id=$1 AND NOT (form ? 'draft')
        AND form #>> '{questionnaire,id}' = $2 FOR UPDATE`, id, strings.Replace(id, "app_", "qst_", 1)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	defaults, err := testApplicationForm(seed, id, now)
	if err != nil {
		return err
	}
	var current, initial map[string]any
	if err := json.Unmarshal(raw, &current); err != nil {
		return err
	}
	if err := json.Unmarshal(defaults, &initial); err != nil {
		return err
	}
	mergeMissingForm(current, initial)
	merged, err := json.Marshal(current)
	if err != nil {
		return err
	}
	draft, err := draftFromForm(merged)
	if err != nil {
		return err
	}
	current["draft"] = json.RawMessage(draft)
	merged, err = json.Marshal(current)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE applications SET form=$2, version=version+1 WHERE id=$1`, id, merged)
	return err
}

func mergeMissingForm(current, initial map[string]any) {
	for key, value := range initial {
		if previous, ok := current[key]; !ok || previous == nil {
			current[key] = value
		} else if target, ok := previous.(map[string]any); ok {
			if source, ok := value.(map[string]any); ok {
				mergeMissingForm(target, source)
			}
		}
	}
}
