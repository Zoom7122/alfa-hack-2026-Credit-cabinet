//go:build integration

// Контрактный тест против живого mock-api: данные берутся только оттуда.
//
//	make test-integration                                   # mock-api на localhost:8090
//	BANK_API_URL=http://host:port/bank/v1 make test-integration
package bank_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const testINN = "7701234567"

func bankURL() string {
	if u := os.Getenv("BANK_API_URL"); u != "" {
		return u
	}
	return "http://localhost:8090/bank/v1"
}

// Клиент получает все пять ответов от mock-api без ошибок.
func TestIntegrationClient(t *testing.T) {
	c := bank.New(bankURL(), 5*time.Second)
	ctx := context.Background()

	if acc, err := c.Accounts(ctx, testINN); err != nil || len(acc.Data.Account) == 0 {
		t.Fatalf("accounts: %+v, %v", acc.Data, err)
	}
	if cr, err := c.Credits(ctx, testINN); err != nil || cr.Subject.INN == "" {
		t.Fatalf("credits: %+v, %v", cr.Subject, err)
	}
	if aq, err := c.Acquiring(ctx, testINN); err != nil || len(aq.MonthlyTurnover) == 0 {
		t.Fatalf("acquiring: %+v, %v", aq, err)
	}
	if sp, err := c.SalaryProject(ctx, testINN); err != nil || sp.ContractNumber == "" {
		t.Fatalf("salary project: %+v, %v", sp, err)
	}
	if ci, err := c.CardIndex(ctx, testINN); err != nil || ci.AccountNumber == "" {
		t.Fatalf("card index: %+v, %v", ci, err)
	}
}

// В доменных структурах есть каждое поле ответа mock-api: если в мок добавят поле,
// а в domain/bank.go — нет, тест упадёт, а не потеряет данные молча.
func TestIntegrationContract(t *testing.T) {
	targets := map[string]any{
		"accounts":       &domain.AccountsReport{},
		"credits":        &domain.CreditReport{},
		"acquiring":      &domain.AcquiringReport{},
		"salary-project": &domain.SalaryProjectReport{},
		"card-index":     &domain.CardIndexReport{},
	}
	for resource, dst := range targets {
		t.Run(resource, func(t *testing.T) {
			resp, err := http.Get(bankURL() + "/companies/" + testINN + "/" + resource)
			if err != nil {
				t.Fatalf("mock-api недоступен (%v): запустите его, например `make up` в корне", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: %s", resp.StatusCode, body)
			}

			dec := json.NewDecoder(bytes.NewReader(body))
			dec.DisallowUnknownFields()
			if err := dec.Decode(dst); err != nil {
				t.Fatalf("ответ mock-api не совпадает с domain: %v", err)
			}
		})
	}
}
