//go:build integration

package adminstore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

type adminHTTP struct {
	handler http.Handler
	cookies []*http.Cookie
	csrf    string
}

func (h adminHTTP) send(method, path string, payload any) *httptest.ResponseRecorder {
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			panic(err)
		}
	}
	request := httptest.NewRequest(method, "https://admin.example"+path, &body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://admin.example")
	if h.csrf != "" {
		request.Header.Set("X-CSRF-Token", h.csrf)
	}
	for _, cookie := range h.cookies {
		request.AddCookie(cookie)
	}
	request.Header.Set("X-Admin-User", "forged-author")
	w := httptest.NewRecorder()
	h.handler.ServeHTTP(w, request)
	return w
}
func adminHTTPFixture(t *testing.T, repo *adminstore.Repository) adminHTTP {
	t.Helper()
	cfg, err := config.LoadAdmin(func(key string) (string, bool) {
		v, ok := map[string]string{"ADMIN_DATABASE_URL": "postgres://unused.invalid/test", "ADMIN_USERNAME": "editor", "ADMIN_PASSWORD_HASH": "$2a$10$EWuECD9zM/WTJv1IbDlfG.LUvUkOWslI3qcfYVvlEqmZkq/jeYQiK", "ADMIN_EVENTS_TOKEN": "fictional-ms2-service-token-1234567890", "ADMIN_PUBLIC_ORIGIN": "https://admin.example"}[key]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := adminauth.New(cfg, repo, clock.Fixed{Time: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	router := httpapi.NewAdminRouter(access, cfg.RequestTimeout)
	httpapi.RegisterAdminModels(router, access, repo)
	httpapi.RegisterAdminEvents(router, access, repo)
	httpapi.RegisterAdminSimulation(router, access, repo)
	client := adminHTTP{handler: router}
	w := client.send("POST", "/api/admin/v1/auth/login", map[string]string{"username": "editor", "password": "fictional-admin-password"})
	adminWant(t, w, 200)
	var result struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	client.cookies = w.Result().Cookies()
	client.csrf = result.CSRF
	return client
}
func adminWant(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status {
		body := w.Body.String()
		if len(body) > 1000 {
			body = body[:1000]
		}
		t.Fatalf("wanted %d, got %d: %s", status, w.Code, body)
	}
}
func adminRead[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func cloneDefinition(t *testing.T, d model.ModelDefinition) model.ModelDefinition {
	t.Helper()
	data, err := model.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	result, err := model.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAdministrativeModelsHTTPWorkflowAndReleaseGate(t *testing.T) {
	db := isolatedDatabase(t)
	migrator, _ := adminstore.NewMigrator(db)
	_, err := migrator.Up(context.Background())
	migrationOK(t, err)
	repo, _ := adminstore.NewRepository(db)
	if err := repo.SeedBaseline(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := adminHTTPFixture(t, repo)
	models := adminRead[[]adminstore.PublishedModel](t, h.send("GET", "/api/admin/v1/models", nil))
	if len(models) != 2 {
		t.Fatal("baseline list", len(models))
	}
	w := h.send("GET", "/api/admin/v1/models/ip-v2", nil)
	adminWant(t, w, 200)
	original := adminRead[adminstore.PublishedModel](t, w)
	w = h.send("POST", "/api/admin/v1/models/ip-v2/drafts", map[string]any{"new_version": "ip-api-v3", "reason": "Prepare model revision"})
	adminWant(t, w, 201)
	draft := adminRead[adminstore.Draft](t, w)
	if draft.Revision != 1 || draft.CreatedBy != "editor" || w.Header().Get("Location") != "/api/admin/v1/drafts/"+draft.ID {
		t.Fatal("draft identity or author")
	}
	base := cloneDefinition(t, draft.Definition)
	validationPath := "/api/admin/v1/drafts/" + draft.ID + "/validation"
	w = h.send("GET", validationPath, nil)
	adminWant(t, w, 200)
	var initial struct {
		Validation model.ValidationResult `json:"validation"`
	}
	initial = adminRead[struct {
		Validation model.ValidationResult `json:"validation"`
	}](t, w)
	if !initial.Validation.Valid {
		t.Fatal(initial.Validation.Issues)
	}
	q, a := -1, -1
	for i, c := range base.Criteria {
		if c.Enabled && c.MaxPoints > 1 {
			if c.Block == model.Questionnaire && q < 0 {
				q = i
			}
			if c.Block == model.Additional && a < 0 {
				a = i
			}
		}
	}
	if q < 0 || a < 0 {
		t.Fatal("missing positive criteria")
	}
	for index, mutation := range []func(*model.ModelDefinition){func(d *model.ModelDefinition) { d.Criteria[q].MaxPoints++ }, func(d *model.ModelDefinition) { d.Criteria[q].MaxPoints-- }, func(d *model.ModelDefinition) { d.Criteria[q].MaxPoints++; d.Criteria[a].MaxPoints-- }} {
		t.Run([]string{"1001 points", "999 points", "1000 with wrong split"}[index], func(t *testing.T) {
			invalid := cloneDefinition(t, base)
			mutation(&invalid)
			w := h.send("PUT", "/api/admin/v1/drafts/"+draft.ID, map[string]any{"expected_revision": draft.Revision, "definition": invalid, "reason": "Save intermediate weights"})
			adminWant(t, w, 200)
			draft = adminRead[adminstore.Draft](t, w)
			w = h.send("GET", validationPath, nil)
			adminWant(t, w, 200)
			validation := adminRead[struct {
				Revision   int64                  `json:"revision"`
				Validation model.ValidationResult `json:"validation"`
			}](t, w)
			if validation.Revision != draft.Revision || validation.Validation.Valid {
				t.Fatal("invalid draft did not retain validation errors")
			}
			request := adminstore.PrepareReleaseRequest{Version: "2.1.0-api", BaseVersion: "2.0.0", PublishedVersions: []string{"ip-v2", "legal-v2"}, Drafts: []adminstore.DraftReference{{ID: draft.ID, Revision: draft.Revision}}, Notes: "API model release"}
			w = h.send("POST", "/api/admin/v1/releases", map[string]any{"release": request, "reason": "Attempt release"})
			adminWant(t, w, 422)
			problem := adminRead[struct {
				Code       string                 `json:"code"`
				Validation model.ValidationResult `json:"validation"`
			}](t, w)
			if problem.Code != "MODEL_VALIDATION_FAILED" || problem.Validation.Valid || len(problem.Validation.Issues) == 0 {
				t.Fatal("missing shared validation details")
			}
			if countRows(t, db, "SELECT count(*) FROM credit_engine_admin.prepared_releases WHERE release_version='2.1.0-api'") != 0 {
				t.Fatal("invalid release persisted")
			}
			if _, err := repo.GetVersion(context.Background(), "ip-api-v3"); !errors.Is(err, adminstore.ErrNotFound) {
				t.Fatal("invalid version published", err)
			}
		})
	}
	// A technically valid model above 1 MiB remains saveable; the model codec's
	// 4 MiB bound is independent of the 1 MiB supplier-protocol decoder.
	scratch := cloneDefinition(t, base)
	scratch.Version = "scratch-api-v1"
	scratch.Description = strings.Repeat("x", 1<<20+100)
	scratch.Criteria[q].MaxPoints--
	w = h.send("POST", "/api/admin/v1/drafts", map[string]any{"definition": scratch, "reason": "Intermediate draft above 1 MiB"})
	adminWant(t, w, 201)
	scratchDraft := adminRead[adminstore.Draft](t, w)
	if scratchDraft.Definition.Description != scratch.Description {
		t.Fatal("large draft truncated")
	}
	// Publication never includes an unrelated invalid draft implicitly.
	w = h.send("PUT", "/api/admin/v1/drafts/"+draft.ID, map[string]any{"expected_revision": draft.Revision, "definition": base, "reason": "Restore checked weights"})
	adminWant(t, w, 200)
	draft = adminRead[adminstore.Draft](t, w)
	w = h.send("PUT", "/api/admin/v1/drafts/"+draft.ID, map[string]any{"expected_revision": draft.Revision - 1, "definition": base, "reason": "Stale browser tab"})
	adminWant(t, w, 409)
	conflict := adminRead[struct {
		Current int64 `json:"current_revision"`
	}](t, w)
	if conflict.Current != draft.Revision {
		t.Fatal("current revision missing")
	}
	w = h.send("POST", "/api/admin/v1/compare", map[string]any{"left": httpapi.AdminModelReference{Kind: "PUBLISHED", ID: "ip-v2"}, "right": httpapi.AdminModelReference{Kind: "REVISION", ID: draft.ID, Revision: &draft.Revision}})
	adminWant(t, w, 200)
	compared := adminRead[struct {
		Comparison model.DefinitionComparison `json:"comparison"`
	}](t, w)
	if !reflect.DeepEqual(compared.Comparison.ChangedPaths, []string{"/version"}) {
		t.Fatal("unexpected comparison", compared.Comparison)
	}
	request := adminstore.PrepareReleaseRequest{Version: "2.1.0-api", BaseVersion: "2.0.0", PublishedVersions: []string{"ip-v2", "legal-v2"}, Drafts: []adminstore.DraftReference{{ID: draft.ID, Revision: draft.Revision}}, Notes: "API model release"}
	w = h.send("POST", "/api/admin/v1/releases", map[string]any{"release": request, "reason": "Prepare reviewed release"})
	adminWant(t, w, 200)
	prepared := adminRead[adminstore.PreparedRelease](t, w)
	if prepared.CreatedBy != "editor" || len(prepared.Manifest.Models) != 3 || bytes.Contains(w.Body.Bytes(), []byte(`"archive":`)) {
		t.Fatal("wrong release metadata")
	}
	w = h.send("POST", "/api/admin/v1/releases", map[string]any{"release": request, "reason": "Retry after lost response"})
	adminWant(t, w, 200)
	replayed := adminRead[adminstore.PreparedRelease](t, w)
	if replayed.ArchiveSHA256 != prepared.ArchiveSHA256 || countRows(t, db, "SELECT count(*) FROM credit_engine_admin.change_history WHERE entity_kind='RELEASE' AND entity_id='2.1.0-api'") != 1 {
		t.Fatal("release retry changed content/history")
	}
	request.Notes = "Different content"
	w = h.send("POST", "/api/admin/v1/releases", map[string]any{"release": request, "reason": "Attempt overwrite"})
	adminWant(t, w, 409)
	w = h.send("GET", "/api/admin/v1/releases/2.1.0-api/archive", nil)
	adminWant(t, w, 200)
	if w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("X-Archive-SHA256") != release.Hash(w.Body.Bytes()) || release.Hash(w.Body.Bytes()) != prepared.ArchiveSHA256 {
		t.Fatal("archive checksum/headers")
	}
	view, err := release.ReadArchive(w.Body.Bytes())
	if err != nil || len(view.Manifest().Models) != 3 {
		t.Fatal("invalid generated archive", err)
	}
	compiled, err := modelbindings.Default()
	if err != nil || len(compiled.List()) != 2 {
		t.Fatal("admin release changed compiled MS1")
	}
	w = h.send("GET", "/api/admin/v1/drafts/"+draft.ID+"/revisions?limit=2&offset=1", nil)
	adminWant(t, w, 200)
	if len(adminRead[[]adminstore.Revision](t, w)) != 2 {
		t.Fatal("revision pagination")
	}
	w = h.send("GET", "/api/admin/v1/history/DRAFT/"+draft.ID, nil)
	adminWant(t, w, 200)
	history := adminRead[[]adminstore.HistoryEntry](t, w)
	if len(history) != int(draft.Revision) {
		t.Fatal("missing draft history", len(history))
	}
	for _, entry := range history {
		if entry.Author != "editor" || entry.Reason == "" || entry.OccurredAt.IsZero() {
			t.Fatal("incomplete audit")
		}
	}
	withoutCSRF := h
	withoutCSRF.csrf = ""
	w = withoutCSRF.send("DELETE", "/api/admin/v1/drafts/"+draft.ID, map[string]any{"expected_revision": draft.Revision, "reason": "Remove draft"})
	adminWant(t, w, 403)
	w = h.send("DELETE", "/api/admin/v1/drafts/"+draft.ID, map[string]any{"expected_revision": draft.Revision, "reason": "Remove published draft"})
	adminWant(t, w, 204)
	w = h.send("GET", "/api/admin/v1/drafts/"+draft.ID, nil)
	adminWant(t, w, 404)
	w = h.send("GET", "/api/admin/v1/drafts/"+draft.ID+"/revisions/1", nil)
	adminWant(t, w, 200)
	w = h.send("GET", "/api/admin/v1/models/ip-api-v3", nil)
	adminWant(t, w, 200)
	unchanged, err := repo.GetVersion(context.Background(), "ip-v2")
	if err != nil || unchanged.DefinitionSHA256 != original.DefinitionSHA256 {
		t.Fatal("published baseline was changed", err)
	}
	w = h.send("GET", "/api/admin/v1/releases", nil)
	adminWant(t, w, 200)
	if len(adminRead[[]adminstore.PreparedRelease](t, w)) != 2 {
		t.Fatal("release list")
	}
}

func TestAdministrativeHTTPConcurrentEditsAndInputSafety(t *testing.T) {
	repo := repositoryFixture(t)
	if err := repo.SeedBaseline(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := adminHTTPFixture(t, repo)
	w := h.send("POST", "/api/admin/v1/models/ip-v2/drafts", map[string]any{"new_version": "ip-race-v3", "reason": "Concurrent editor"})
	adminWant(t, w, 201)
	draft := adminRead[adminstore.Draft](t, w)
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, title := range []string{"First editor", "Second editor"} {
		wg.Add(1)
		definition := cloneDefinition(t, draft.Definition)
		definition.Title = title
		go func(d model.ModelDefinition) {
			defer wg.Done()
			results <- h.send("PUT", "/api/admin/v1/drafts/"+draft.ID, map[string]any{"expected_revision": 1, "definition": d, "reason": "Concurrent save"})
		}(definition)
	}
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for w := range results {
		switch w.Code {
		case 200:
			ok++
		case 409:
			conflict++
		default:
			adminWant(t, w, 200)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal("lost edit", ok, conflict)
	}
	w = h.send("POST", "/api/admin/v1/drafts", map[string]any{"definition": draft.Definition, "reason": "Forged actor", "author": "forged-author"})
	adminWant(t, w, 400)
	w = h.send("POST", "/api/admin/v1/compare", map[string]any{"left": httpapi.AdminModelReference{Kind: "PUBLISHED", ID: "ip-v2"}, "right": httpapi.AdminModelReference{Kind: "PUBLISHED", ID: "legal-v2"}})
	adminWant(t, w, 422)
	w = h.send("GET", "/api/admin/v1/drafts?limit=201", nil)
	adminWant(t, w, 400)
	w = h.send("GET", "/api/admin/v1/models/absent", nil)
	adminWant(t, w, 404)
}
