//go:build integration

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

func TestIntegrationDraftRoundTrip(t *testing.T) {
	pool := testApplicationsPool(t)
	ctx := context.Background()
	r := NewApplicationRepository(pool, "test.draft.tasks", "test.draft.docs")
	if _, err := r.SeedTestApplications(ctx); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := httptransport.NewRouter(httptransport.NewApplicationHandler(service.NewApplicationService(r), log), nil, nil, nil,
		httptransport.WithAuth(httptransport.NewAuthHandler(service.NewAuthService(NewUserRepository(pool)), log)),
		httptransport.WithDraft(httptransport.NewDraftHandler(service.NewDraftService(r), log)))
	id := testApplicationID(140)
	request := func(method, actor string, body any, status int) domain.SavedDraft {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "/api/v1/applications/"+id+"/draft", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if actor != "" {
			req.Header.Set("Authorization", "Bearer mock."+actor)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: want %d got %d: %s", method, actor, status, w.Code, w.Body.String())
		}
		var out struct {
			Data    json.RawMessage `json:"data"`
			Version int             `json:"version"`
		}
		if status == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return domain.SavedDraft{Data: out.Data, Version: out.Version}
	}
	owner := "usr_director_romashka"
	initial := request("GET", owner, nil, 200)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(initial.Data, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"product_code", "requested", "purpose", "age", "marital_status", "children_count", "director", "scoring", "consents", "collateral", "other_bank_biks"} {
		if len(fields[field]) == 0 {
			t.Errorf("test application is missing persisted %s", field)
		}
	}
	request("GET", "", nil, 401)
	request("GET", "usr_accountant_romashka", nil, 403)
	request("PUT", "usr_underwriter_sokolova", map[string]any{"data": map[string]any{}, "version": initial.Version}, 403)
	request("PUT", owner, map[string]any{"data": map[string]any{"status": "OFFERED"}, "version": initial.Version}, 400)
	request("PUT", owner, map[string]any{"data": map[string]any{"requested": map[string]any{"amount": "not-a-number"}}, "version": initial.Version}, 400)
	data := map[string]any{"product_code": "VEHICLE", "requested": map[string]any{"amount": 2_500_000},
		"age": 0, "children_count": 0, "director": map[string]any{"last_name": "Новая фамилия"}, "scoring": map[string]any{"free_cash": 0}}
	saved := request("PUT", owner, map[string]any{"data": data, "version": initial.Version}, 200)
	if saved.Version != initial.Version+1 {
		t.Fatal("version was not incremented")
	}
	request("PUT", owner, map[string]any{"data": map[string]any{}, "version": initial.Version}, 409)
	loaded := request("GET", owner, nil, 200)
	var actual map[string]any
	if err := json.Unmarshal(loaded.Data, &actual); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(data)
	var expected map[string]any
	_ = json.Unmarshal(encoded, &expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("round trip: got %s want %s", loaded.Data, encoded)
	}
	app, _, err := r.Get(ctx, id)
	if err != nil || app.Status != domain.ApplicationDraft || app.ProductCode != "VEHICLE" || app.Requested != nil {
		t.Fatalf("partial terms and status: %+v %v", app, err)
	}
	empty := request("PUT", owner, map[string]any{"data": map[string]any{}, "version": loaded.Version}, 200)
	// Даже повторное заполнение тестовыми данными не должно восстанавливать удалённые поля.
	if count, err := r.SeedTestApplications(ctx); err != nil || count != 0 {
		t.Fatalf("repeat seed: %d %v", count, err)
	}
	loaded = request("GET", owner, nil, 200)
	if string(loaded.Data) != "{}" || loaded.Version != empty.Version {
		t.Fatalf("empty data was repopulated: %s", loaded.Data)
	}
	app, _, err = r.Get(ctx, id)
	if err != nil || app.ProductCode != "" || app.Requested != nil {
		t.Fatalf("cleared terms: %+v %v", app, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE applications SET status='SUBMITTED' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	request("PUT", owner, map[string]any{"data": data, "version": empty.Version}, 409)
}

func TestIntegrationUpgradeSeedDraft(t *testing.T) {
	pool := testApplicationsPool(t)
	r := NewApplicationRepository(pool, "test.draft.tasks", "test.draft.docs")
	ctx := context.Background()
	if _, err := r.SeedTestApplications(ctx); err != nil {
		t.Fatal(err)
	}
	id := testApplicationID(140)
	if _, err := pool.Exec(ctx, `UPDATE applications SET form = jsonb_set(form - 'draft' - 'scoring', '{questionnaire}',
        (form -> 'questionnaire') || '{"age":55,"collect":{"inn":"7701234567"}}'::jsonb) WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if count, err := r.SeedTestApplications(ctx); err != nil || count != 0 {
		t.Fatalf("upgrade: %d %v", count, err)
	}
	draft, _, err := r.Draft(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		Age      int            `json:"age"`
		Director *personRecord  `json:"director"`
		Scoring  *scoringRecord `json:"scoring"`
	}
	if err := json.Unmarshal(draft.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Age != 55 || data.Director == nil || data.Scoring == nil {
		t.Fatalf("upgrade lost existing fields or failed to fill missing fields: %s", draft.Data)
	}
}
