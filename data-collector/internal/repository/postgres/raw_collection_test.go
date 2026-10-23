package postgres

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Каждая система пишется в свою колонку; колонки не повторяются.
func TestEverySystemHasColumn(t *testing.T) {
	seen := map[string]bool{}
	for _, sys := range domain.Systems {
		col := SystemColumns[sys]
		if col == "" || seen[col] {
			t.Fatalf("system %s: column %q is missing or duplicated", sys, col)
		}
		seen[col] = true
	}
	if len(SystemColumns) != len(domain.Systems) {
		t.Fatalf("%d columns for %d systems", len(SystemColumns), len(domain.Systems))
	}
	want := 8 + len(domain.Systems)
	if n := strings.Count(insertRawCollection, "$"); n != want {
		t.Fatalf("insert has %d params, want %d", n, want)
	}
}

func TestInsertArgs(t *testing.T) {
	rc := domain.RawCollection{
		ID: "col_test", QuestionnaireID: "qst_test",
		Input: domain.CollectInput{
			INN:               "7799000012",
			Director:          &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", BirthDate: "1984-07-19"},
			MarketplaceTokens: map[string]string{"ozon": "secret-ozon"},
			OFDToken:          "secret-ofd",
		},
		Systems: []domain.SystemResult{
			{System: domain.SystemCoreBanking, State: domain.CollectDone, Data: json.RawMessage(`{"client":{"inn":"7799000012"}}`),
				Calls: []domain.Call{{Key: "client", Method: "GET", Path: "/core-banking/v1/clients/7799000012", Status: 200}}},
			{System: domain.SystemEGRN, State: domain.CollectSkipped, Reason: "нет недвижимости в залог"},
		},
		StartedAt: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC), FinishedAt: time.Date(2026, 10, 22, 12, 0, 1, 0, time.UTC),
	}
	args, err := insertArgs(rc)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 8+len(domain.Systems) || args[0] != "col_test" || args[2] != "7799000012" || args[3] != "779900010139" {
		t.Fatalf("args = %v", args[:4])
	}

	request := args[4].(string)
	if strings.Contains(request, "secret") || !strings.Contains(request, `"marketplaces":["ozon"]`) ||
		!strings.Contains(request, `"ofd_token_provided":true`) {
		t.Fatalf("request = %s", request)
	}
	var sources []sourceRecord
	if err := json.Unmarshal([]byte(args[5].(string)), &sources); err != nil || len(sources) != 2 ||
		sources[0].Calls[0].Status != 200 || sources[1].State != "SKIPPED" {
		t.Fatalf("sources = %s (%v)", args[5], err)
	}

	// колонки систем: данные — строкой jsonb, нет данных — NULL
	for i, sys := range domain.Systems {
		v := args[8+i]
		if sys == domain.SystemCoreBanking {
			if v != `{"client":{"inn":"7799000012"}}` {
				t.Errorf("core_banking = %v", v)
			}
		} else if v != nil {
			t.Errorf("%s = %v, want NULL", sys, v)
		}
	}
}

func TestNoDirector(t *testing.T) {
	args, err := insertArgs(domain.RawCollection{ID: "col_test", Input: domain.CollectInput{INN: "7799000012"}})
	if err != nil {
		t.Fatal(err)
	}
	if args[3] != nil {
		t.Fatalf("director_inn = %v, want NULL", args[3])
	}
	if req := args[4].(string); !strings.Contains(req, `"other_bank_biks":[]`) || strings.Contains(req, `"director"`) {
		t.Fatalf("request = %s", req)
	}
}
