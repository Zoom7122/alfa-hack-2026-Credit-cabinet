//go:build integration

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

// Полный набор проверяем в отдельной схеме с настоящими миграциями: не трогаем
// пользовательские заявки и уже работающие API/worker'ы в public.
func testApplicationsPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	admin := testPool(t)
	schema := fmt.Sprintf("test_seed_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	})
	cfg := admin.Config()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	paths, err := filepath.Glob("../../../../db/migrations/*.sql")
	if err != nil || len(paths) == 0 {
		t.Fatalf("find migrations: %v (%d files)", err, len(paths))
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		up, _, _ := strings.Cut(string(body), "-- +goose Down")
		if _, err := pool.Exec(ctx, up); err != nil {
			t.Fatalf("apply %s: %v", path, err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO companies (id, inn, type, name, ogrn, registry)
        VALUES ('apl_existing_romashka', '7701234567', 'LEGAL_ENTITY', 'ООО «Ромашка»', 'existing-ogrn',
                '{"full_name":"Сохранённые реквизиты"}')`); err != nil {
		t.Fatal(err)
	}
	if err := NewUserRepository(pool).SeedTestUsers(ctx, service.TestUsers); err != nil {
		t.Fatal(err)
	}
	return pool
}

func testApplicationSnapshot(t *testing.T, pool *pgxpool.Pool) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range []string{"users", "companies", "company_members", "applications", "application_events", "decisions", "offers", "info_requests", "collection_tasks", "outbox"} {
		var contents string
		if err := pool.QueryRow(context.Background(), `SELECT coalesce(jsonb_agg(row_data ORDER BY row_data::text), '[]'::jsonb)::text
            FROM (SELECT to_jsonb(t) AS row_data FROM `+pgx.Identifier{table}.Sanitize()+` t) seed_rows`).Scan(&contents); err != nil {
			t.Fatal(err)
		}
		out[table] = contents
	}
	return out
}

func TestIntegrationSeedTestApplications(t *testing.T) {
	pool := testApplicationsPool(t)
	ctx := context.Background()
	r := NewApplicationRepository(pool, "test.seed.tasks", "test.seed.documents")
	live, err := r.Register(ctx, domain.Registration{UserID: "usr_director_north", ExistingUser: true,
		CompanyID: "apl_unused", ApplicationID: "app_live_before_seed", CreatedAt: time.Now().UTC(),
		Company:        domain.Company{INN: "7799000012", Type: domain.CompanyLegalEntity, Name: "Северный ветер"},
		Representative: domain.Representative{Role: domain.RoleDirector}})
	if err != nil {
		t.Fatal(err)
	}
	count, err := r.SeedTestApplications(ctx)
	if err != nil || count != 17 {
		t.Fatalf("seed: inserted=%d err=%v", count, err)
	}
	if got, ok, err := r.Get(ctx, live.ID); err != nil || !ok || got.Status != domain.ApplicationDraft {
		t.Fatalf("existing application: %+v %v %v", got, ok, err)
	}
	for _, seed := range testApplications {
		app, ok, err := r.Get(ctx, testApplicationID(seed.ID))
		if err != nil || !ok || app.Status != seed.Status || app.Requested == nil || app.Requested.Amount != seed.Amount {
			t.Fatalf("application %d: %+v found=%v err=%v", seed.ID, app, ok, err)
		}
		if seed.Company == "romashka" && app.Company.ID != "apl_existing_romashka" {
			t.Fatalf("existing company id was not reused: %s", app.Company.ID)
		}
		events, err := service.NewCabinetService(NewCabinetRepository(pool), r, nil, nil).Timeline(ctx, app.ID)
		if err != nil || len(events) == 0 {
			t.Fatalf("timeline %s: %v", app.ID, err)
		}
		for i := 1; i < len(events); i++ {
			if events[i].At.Before(events[i-1].At) {
				t.Fatalf("timeline %s is not chronological: %+v", app.ID, events)
			}
		}
	}
	var skipped []string
	if err := pool.QueryRow(ctx, `SELECT skipped_sources FROM collection_tasks WHERE application_id = $1`, testApplicationID(150)).Scan(&skipped); err != nil {
		t.Fatal(err)
	}
	expectedSkips := domain.SkippedSystems(domain.CollectInput{INN: testCompanies["beta"].INN})
	if len(skipped) != len(expectedSkips) {
		t.Fatalf("systems without input must be skipped: %v", skipped)
	}
	for _, system := range skipped {
		if _, ok := expectedSkips[domain.System(system)]; !ok {
			t.Fatalf("unexpected skipped system: %s", system)
		}
	}
	var ogrn, registry string
	if err := pool.QueryRow(ctx, `SELECT ogrn, registry ->> 'full_name' FROM companies WHERE inn = '7701234567'`).Scan(&ogrn, &registry); err != nil || ogrn != "existing-ogrn" || registry != "Сохранённые реквизиты" {
		t.Fatalf("existing registry changed: %s %s %v", ogrn, registry, err)
	}
	bo := NewBackofficeRepository(pool)
	counters, err := bo.Counters(ctx, "usr_underwriter_sokolova")
	if err != nil || counters.ManualReview != 7 || counters.Mine != 1 || counters.WaitingForInfo != 3 {
		t.Fatalf("queue counters: %+v %v", counters, err)
	}
	for _, seed := range testApplications {
		if seed.Status == domain.ApplicationDraft {
			continue
		}
		dossier, ok, err := bo.Dossier(ctx, testApplicationID(seed.ID))
		if err != nil || !ok || dossier.Card == nil || len(dossier.Events) < 2 {
			t.Fatalf("dossier %d: %+v %v %v", seed.ID, dossier, ok, err)
		}
		if seed.Status == domain.ApplicationRejected && dossier.Decision.MaxAmount != nil {
			t.Fatalf("rejected application %d must not have an approved limit", seed.ID)
		}
	}
	files, err := storage.NewLocal(t.TempDir(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := service.NewOutcomeService(bo, r, files, 4*time.Hour)
	offers, err := outcomes.Offers(ctx, testApplicationID(501))
	if err != nil || len(offers) != 1 || offers[0].Amount != 2_030_000 || offers[0].TermMonths != 36 {
		t.Fatalf("north offer: %+v %v", offers, err)
	}
	rejected, err := outcomes.Decision(ctx, testApplicationID(504))
	if err != nil || rejected.Outcome != "REJECTED" || len(rejected.Reasons) == 0 || rejected.ByEmployee {
		t.Fatalf("rejected: %+v %v", rejected, err)
	}
	questions, err := outcomes.InfoRequests(ctx, testApplicationID(505))
	if err != nil || len(questions) != 2 {
		t.Fatalf("smirnova questions: %+v %v", questions, err)
	}
	// Реальный ответ на вопрос: после рестарта он не должен исчезнуть или открыться заново.
	questions, err = outcomes.InfoRequests(ctx, testApplicationID(131))
	if err != nil || len(questions) != 1 {
		t.Fatalf("romashka questions: %+v %v", questions, err)
	}
	status, err := outcomes.Answer(ctx, "usr_accountant_romashka", testApplicationID(131), questions[0].ID,
		service.AnswerInput{Value: json.RawMessage(`"Полномочия подтверждены"`)})
	if err != nil || status != domain.ApplicationManualReview {
		t.Fatalf("answer: %s %v", status, err)
	}
	if _, err := bo.Assign(ctx, testApplicationID(147), "usr_underwriter_orlov"); err != nil {
		t.Fatal(err)
	}
	before := testApplicationSnapshot(t, pool)
	count, err = r.seedTestApplications(ctx, testApplications, time.Now().AddDate(0, 0, 30))
	if err != nil || count != 0 {
		t.Fatalf("restart: inserted=%d err=%v", count, err)
	}
	for table, contents := range testApplicationSnapshot(t, pool) {
		if contents != before[table] {
			t.Errorf("restart changed %s", table)
		}
	}
}

func TestIntegrationSeedTestApplicationsConcurrent(t *testing.T) {
	pool := testApplicationsPool(t)
	r := NewApplicationRepository(pool, "test.seed.tasks", "test.seed.documents")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	type result struct {
		count int
		err   error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			if err := NewUserRepository(pool).SeedTestUsers(ctx, service.TestUsers); err != nil {
				results <- result{err: err}
				return
			}
			count, err := r.SeedTestApplications(ctx)
			results <- result{count, err}
		}()
	}
	total := 0
	for range 2 {
		res := <-results
		if res.err != nil {
			t.Fatal(res.err)
		}
		total += res.count
	}
	if total != 17 {
		t.Fatalf("concurrent starts inserted %d applications", total)
	}
	var jobs, events int
	if err := pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM collection_tasks), (SELECT count(*) FROM outbox)`).Scan(&jobs, &events); err != nil || jobs != 1 || events != 1 {
		t.Fatalf("collection scheduled more than once: jobs=%d outbox=%d err=%v", jobs, events, err)
	}
}

func TestIntegrationSeedTestApplicationsRollback(t *testing.T) {
	pool := testApplicationsPool(t)
	r := NewApplicationRepository(pool, "test.seed.tasks", "test.seed.documents")
	before := testApplicationSnapshot(t, pool)
	seeds := []testApplication{testApplications[0], {ID: 999, Company: "missing-company"}}
	count, err := r.seedTestApplications(context.Background(), seeds, time.Now().UTC())
	if err == nil || count != 0 {
		t.Fatalf("expected rollback: inserted=%d err=%v", count, err)
	}
	for table, contents := range testApplicationSnapshot(t, pool) {
		if contents != before[table] {
			t.Errorf("failed seed left partial data in %s", table)
		}
	}
	if count, err := r.SeedTestApplications(context.Background()); err != nil || count != 17 {
		t.Fatalf("retry: inserted=%d err=%v", count, err)
	}
}
