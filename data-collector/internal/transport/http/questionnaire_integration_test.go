//go:build integration

// Сквозной тест сбора против живого mock-api: POST /questionnaires с примерами
// из examples/ (сценарии A–E) → событие из outbox → worker → GET /collections/{id}.
// Ответы систем — настоящие; очередь и хранилище — в памяти (Kafka и Postgres
// проверяются в internal/worker/pipeline_integration_test.go).
//
//	make test-integration                                # mock-api на localhost:8090
//	MOCK_API_URL=http://host:port make test-integration
package httptransport_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/mockapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/repository/memory"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

func mockAPIURL() string {
	if u := os.Getenv("MOCK_API_URL"); u != "" {
		return u
	}
	return "http://localhost:8090"
}

// at достаёт значение из сырых данных системы по пути ключей и индексов.
func at(t *testing.T, raw json.RawMessage, path ...any) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				t.Fatalf("path %v: %v is not an object", path, v)
			}
			v = m[k]
		case int:
			a, ok := v.([]any)
			if !ok || k >= len(a) {
				t.Fatalf("path %v: no element %d in %v", path, k, v)
			}
			v = a[k]
		}
	}
	return v
}

func TestIntegrationCollectScenarios(t *testing.T) {
	// Какие системы пропускаются, потому что в примере для них нет данных (токена, залога, банков).
	skipped := map[string][]domain.System{
		"a": {domain.SystemEGRN, domain.SystemMarketplaces},
		"b": {domain.SystemMarketplaces, domain.SystemOFD},
		"c": {domain.SystemEGRN, domain.SystemOpenBanking, domain.SystemMarketplaces, domain.SystemOFD},
		"d": {domain.SystemEGRN, domain.SystemOpenBanking, domain.SystemOFD},
		"e": {domain.SystemEGRN, domain.SystemOpenBanking, domain.SystemMarketplaces, domain.SystemOFD},
	}
	for _, sc := range []string{"a", "b", "c", "d", "e"} {
		t.Run(sc, func(t *testing.T) {
			body, err := os.ReadFile("../../../examples/scenario-" + sc + ".json")
			if err != nil {
				t.Fatal(err)
			}
			tasks := memory.NewCollectionTaskRepository()
			collections := service.NewCollectionService(tasks)
			r := httptransport.NewRouter(
				httptransport.NewQuestionnaireHandler(service.NewQuestionnaireService(memory.NewQuestionnaireRepository(), collections), discardLog),
				httptransport.NewCollectionHandler(collections, discardLog),
				httptransport.NewInternalDataHandler(stubInternalData{}, discardLog),
			)

			w := post(r, string(body))
			if w.Code != http.StatusAccepted {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body)
			}
			var created createdBody
			if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			// системы без данных видны сразу, до сбора
			var wantSkipped []string
			for _, sys := range skipped[sc] {
				wantSkipped = append(wantSkipped, string(sys))
			}
			if c := created.Collection; c.Status != "PENDING" || !slices.Equal(c.SkippedSources, wantSkipped) {
				t.Fatalf("created collection = %+v, want skipped %v", c, wantSkipped)
			}

			runWorker(t, mockapi.New(mockAPIURL(), 5*time.Second, "044525999"), tasks)

			code, c, _ := getCollection(t, r, created.Collection.ID)
			if code != http.StatusOK || c.Status != "COMPLETED" || len(c.Sources) != len(domain.Systems) {
				t.Fatalf("collection: %d %+v", code, c)
			}
			for _, s := range c.Sources {
				want := "DONE"
				if slices.Contains(skipped[sc], domain.System(s.Source)) {
					want = "SKIPPED"
				}
				if s.State != want {
					t.Errorf("%s: state %s, want %s (%s)", s.Source, s.State, want, s.Reason)
				}
			}

			rc, ok := tasks.Result(created.Collection.ID)
			if !ok {
				t.Fatal("collection is not saved")
			}
			data := map[domain.System]json.RawMessage{}
			for _, s := range rc.Systems {
				data[s.System] = s.Data
			}
			if got := at(t, data[domain.SystemCoreBanking], "client", "inn"); got != rc.Input.INN {
				t.Errorf("core_banking.client.inn = %v", got)
			}
			if got := at(t, data[domain.SystemFNS], "egrul", "rows", 0, "i"); got != rc.Input.INN {
				t.Errorf("fns.egrul.rows[0].i = %v", got)
			}

			switch sc {
			case "a":
				if got := at(t, data[domain.SystemM4D], "poa", "status"); got != "ACTIVE" {
					t.Errorf("m4d.poa.status = %v", got)
				}
				if got := at(t, data[domain.SystemHH], "vacancies", "found"); got != 3.0 {
					t.Errorf("hh.vacancies.found = %v", got)
				}
				if got := at(t, data[domain.SystemOpenBanking], "044525225", "balances", "acc-0b71", "Data", "Balance", 0,
					"Amount", "amount"); got != "184320.55" {
					t.Errorf("open banking balance = %v", got)
				}
				if got := at(t, data[domain.SystemOFD], "kkt", "kkt"); len(got.([]any)) != 2 {
					t.Errorf("ofd.kkt = %v", got)
				}
			case "b":
				if got := at(t, data[domain.SystemEGRN], "50:21:0110203:845", "encumbrances", 0, "type"); got != "MORTGAGE" {
					t.Errorf("egrn warehouse encumbrance = %v", got)
				}
				if got := at(t, data[domain.SystemPledgeRegistry], "by_vin", "XTC652005N1498213", "total"); got != 1.0 {
					t.Errorf("pledge by vin = %v", got)
				}
				if got := at(t, data[domain.SystemOpenBanking]); len(got.(map[string]any)) != 2 {
					t.Errorf("open banking banks = %v", got)
				}
			case "c":
				if got := at(t, data[domain.SystemFNS], "account_blocks", "044525999", "decisions", 0, "number"); got != "12873" {
					t.Errorf("fns block = %v", got)
				}
				if got := at(t, data[domain.SystemZakupki], "rnp", "listed"); got != true {
					t.Errorf("zakupki.rnp.listed = %v", got)
				}
				if got := at(t, data[domain.SystemFedresurs], "company_messages", "summary", "bankruptcy_stage"); got != "PETITION_FILED" {
					t.Errorf("fedresurs stage = %v", got)
				}
			case "d":
				if got := at(t, data[domain.SystemBKI], "legal", "has_history"); got != false {
					t.Errorf("bki.legal.has_history = %v", got)
				}
				if got := at(t, data[domain.SystemMarketplaces], "wildberries", "sales", "months", 0, "payout"); got != 251350.0 {
					t.Errorf("wb payout = %v", got)
				}
				if got := at(t, data[domain.SystemFNS], "charter"); got != nil {
					t.Errorf("fns.charter must not be requested for IP: %v", got)
				}
			case "e":
				if got := at(t, data[domain.SystemCompliance], "zsk", "risk_level"); got != "MEDIUM" {
					t.Errorf("compliance.zsk.risk_level = %v", got)
				}
			}
		})
	}
}

// Неизвестная компания: АБС её не знает, ЕГРЮЛ пуст, hh не с чем искать.
func TestIntegrationUnknownCompany(t *testing.T) {
	tasks := memory.NewCollectionTaskRepository()
	task, err := service.NewCollectionService(tasks).Enqueue(context.Background(), "qst_test", domain.CollectInput{INN: "7799000999"})
	if err != nil {
		t.Fatal(err)
	}
	runWorker(t, mockapi.New(mockAPIURL(), 5*time.Second, "044525999"), tasks)
	rc, ok := tasks.Result(task.ID)
	if !ok {
		t.Fatal("collection is not saved")
	}
	states := map[domain.System]domain.CollectState{}
	for _, s := range rc.Systems {
		states[s.System] = s.State
	}
	if states[domain.SystemCoreBanking] != domain.CollectFailed || states[domain.SystemHH] != domain.CollectSkipped {
		t.Fatalf("states = %v", states)
	}
}
