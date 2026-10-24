package postgres

import (
	"encoding/json"
	"reflect"
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
	want := len(fixedColumns) + len(domain.Systems)
	if n := strings.Count(insertRawCollection, "$"); n != want {
		t.Fatalf("insert has %d params, want %d", n, want)
	}
}

func TestInsertArgs(t *testing.T) {
	rc := domain.RawCollection{
		ID: "col_test", TaskID: "tsk_test", QuestionnaireID: "qst_test",
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
	fixed := len(fixedColumns)
	if len(args) != fixed+len(domain.Systems) || args[0] != "col_test" || args[1] != "tsk_test" || args[2] != "qst_test" ||
		args[3] != "7799000012" || args[4] != "779900010139" {
		t.Fatalf("args = %v", args[:5])
	}

	request := args[5].(string)
	if strings.Contains(request, "secret") || !strings.Contains(request, `"marketplaces":["ozon"]`) ||
		!strings.Contains(request, `"ofd_token_provided":true`) {
		t.Fatalf("request = %s", request)
	}
	var sources []sourceRecord
	if err := json.Unmarshal([]byte(args[6].(string)), &sources); err != nil || len(sources) != 2 ||
		sources[0].Calls[0].Status != 200 || sources[1].State != "SKIPPED" {
		t.Fatalf("sources = %s (%v)", args[6], err)
	}

	// колонки систем: данные — строкой jsonb, нет данных — NULL
	for i, sys := range domain.Systems {
		v := args[fixed+i]
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
	if args[4] != nil {
		t.Fatalf("director_inn = %v, want NULL", args[4])
	}
	if req := args[5].(string); !strings.Contains(req, `"other_bank_biks":[]`) || strings.Contains(req, `"director"`) {
		t.Fatalf("request = %s", req)
	}
}

// Входные данные задачи переживают запись в request + secrets и чтение обратно.
func TestRequestRoundTrip(t *testing.T) {
	consent := &domain.Consent{ID: "CNS-1", SignedAt: "2026-10-21T14:02:11Z", ValidUntil: "2027-04-21"}
	in := domain.CollectInput{
		INN: "7799000012",
		Director: &domain.Person{INN: "779900010139", LastName: "Ковалёв", FirstName: "Игорь", MiddleName: "Сергеевич",
			BirthDate: "1984-07-19"},
		BKICompanyConsent: consent,
		BKIPersonConsent:  consent,
		TaxConsent:        &domain.Consent{ID: "TAX-1", ValidUntil: "2027-04-21", Scope: []string{"ENS_BALANCE"}},
		OtherBankBIKs:     []string{"044525225"},
		MarketplaceTokens: map[string]string{"wildberries": "secret-wb"},
		OFDToken:          "secret-ofd",
		Collateral: domain.Collateral{CadastralNumbers: []string{"50:21:0110203:845"}, VINs: []string{"XTC652005N1498213"},
			Serials: []string{"JCB3CX2023118457"}},
		POANumber:         "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37",
		RepresentativeINN: "779900010333",
	}
	request, err := json.Marshal(toRequestRecord(in))
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := encodeSecrets(in)
	if err != nil {
		t.Fatal(err)
	}
	var rec requestRecord
	var sec secretsRecord
	if err := json.Unmarshal(request, &rec); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(secrets.(string)), &sec); err != nil {
		t.Fatal(err)
	}
	if got := fromRequestRecord(rec, &sec); !reflect.DeepEqual(got, in) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, in)
	}

	// токены стёрты (secrets = NULL) — остальное на месте
	got := fromRequestRecord(rec, nil)
	if got.MarketplaceTokens != nil || got.OFDToken != "" || got.Director.LastName != "Ковалёв" || got.Collateral.VINs[0] != "XTC652005N1498213" {
		t.Fatalf("without secrets = %+v", got)
	}
	// минимальный ввод: пустые списки возвращаются nil, токенов нет — secrets NULL
	minimal := domain.CollectInput{INN: "7799000012"}
	if s, err := encodeSecrets(minimal); err != nil || s != nil {
		t.Fatalf("secrets = %v, err %v", s, err)
	}
	if got := fromRequestRecord(toRequestRecord(minimal), nil); !reflect.DeepEqual(got, minimal) {
		t.Fatalf("minimal round trip = %+v", got)
	}
}

func TestSourceRecordsRoundTrip(t *testing.T) {
	results := []domain.SystemResult{
		{System: domain.SystemFNS, State: domain.CollectPartial, Reason: "charter: 404", Duration: 12 * time.Millisecond,
			Calls: []domain.Call{
				{Key: "egrul", Method: "GET", Path: "/fns/egrul/", Status: 200, Duration: 3 * time.Millisecond},
				{Key: "charter", Method: "GET", Path: "/fns/puchdoc/v1/7799000012", Status: 404, Error: "status 404"},
			}},
		{System: domain.SystemEGRN, State: domain.CollectSkipped, Reason: "нет недвижимости в залог"},
	}
	got := fromSourceRecords(toSourceRecords(results))
	if len(got) != 2 || got[0].State != domain.CollectPartial || got[0].Duration != 12*time.Millisecond ||
		len(got[0].Calls) != 2 || got[0].FailedCalls() != 1 || got[1].State != domain.CollectSkipped || got[1].Data != nil {
		t.Fatalf("sources = %+v", got)
	}
}
