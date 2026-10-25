//go:build integration

package adminstore_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/simulation"
)

func TestSimulationHTTPPinnedRevisionsAndNoPersistentSideEffects(t *testing.T) {
	db := isolatedDatabase(t)
	migrator, _ := adminstore.NewMigrator(db)
	_, err := migrator.Up(context.Background())
	migrationOK(t, err)
	repo, _ := adminstore.NewRepository(db)
	if err := repo.SeedBaseline(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := adminHTTPFixture(t, repo)
	data, err := os.ReadFile("../../testdata/decisions/uc04_ip_approved.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Request contracts.DecisionRequest `json:"request"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	sample := simulation.Sample{EvaluationDate: "2026-10-23", Application: fixture.Request, AdditionalFacts: json.RawMessage(`{}`)}
	published := httpapi.AdminModelReference{Kind: "PUBLISHED", ID: "ip-v2"}
	w := h.send("POST", "/api/admin/v1/simulations", map[string]any{"model": published, "sample": sample})
	adminWant(t, w, 200)
	baseline := adminRead[simulation.Result](t, w)
	if baseline.Route != rules.Selection || baseline.Score == nil {
		t.Fatal("not a model preview")
	}
	draft, err := repo.CopyVersion(context.Background(), "ip-v2", "ip-simulation-v3", "editor", "Prepare simulation test")
	if err != nil {
		t.Fatal(err)
	}
	definition := draft.Definition
	total := baseline.Score.Total
	definition.Classes = []model.ClassBand{{Grade: contracts.GradeD, MinTotal: 0, MaxTotal: 549, Reason: &model.Reason{Code: "CLASS_D", Message: "D", Fields: []string{}}}, {Grade: contracts.GradeC, MinTotal: 550, MaxTotal: total, Reason: &model.Reason{Code: "CLASS_C", Message: "C", Fields: []string{}}}, {Grade: contracts.GradeB, MinTotal: total + 1, MaxTotal: 999}, {Grade: contracts.GradeA, MinTotal: 1000, MaxTotal: 1000}}
	draft, err = repo.UpdateDraft(context.Background(), draft.ID, draft.Revision, definition, "editor", "Change class boundary")
	if err != nil {
		t.Fatal(err)
	}
	pinned := draft.Revision
	reference := httpapi.AdminModelReference{Kind: "REVISION", ID: draft.ID, Revision: &pinned}
	// A later edit must not silently replace the selected simulation revision.
	definition.Title = "Later editor title"
	if _, err := repo.UpdateDraft(context.Background(), draft.ID, draft.Revision, definition, "editor", "Later revision"); err != nil {
		t.Fatal(err)
	}
	counts := func() []int {
		return []int{countRows(t, db, "SELECT count(*) FROM credit_engine_admin.model_drafts"), countRows(t, db, "SELECT count(*) FROM credit_engine_admin.model_revisions"), countRows(t, db, "SELECT count(*) FROM credit_engine_admin.model_versions"), countRows(t, db, "SELECT count(*) FROM credit_engine_admin.prepared_releases"), countRows(t, db, "SELECT count(*) FROM credit_engine_admin.change_history"), countRows(t, db, "SELECT count(*) FROM credit_engine_admin.credit_events")}
	}
	before := counts()
	w = h.send("POST", "/api/admin/v1/simulations/compare", map[string]any{"left": published, "right": reference, "sample": sample})
	adminWant(t, w, 200)
	compared := adminRead[simulation.Comparison](t, w)
	if !compared.RouteChanged || !compared.GradeChanged || compared.Right.Route != rules.Manual || compared.Right.DefinitionSHA256 != draft.DefinitionSHA256 {
		t.Fatal("comparison did not pin the requested revision")
	}
	w = h.send("POST", "/api/admin/v1/simulations", map[string]any{"model": reference, "sample": sample})
	adminWant(t, w, 200)
	again := adminRead[simulation.Result](t, w)
	if !reflect.DeepEqual(again, compared.Right) {
		t.Fatal("same explicit sample is not reproducible")
	}
	if !reflect.DeepEqual(before, counts()) {
		t.Fatal("simulation persisted a draft, release, history, or event")
	}
	catalog, err := modelbindings.Default()
	if err != nil || len(catalog.List()) != 2 {
		t.Fatal("simulation activated a model")
	}
	withoutCSRF := h
	withoutCSRF.csrf = ""
	adminWant(t, withoutCSRF.send("POST", "/api/admin/v1/simulations", map[string]any{"model": reference, "sample": sample}), 403)
	current, err := repo.GetDraft(context.Background(), draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	invalid := current.Definition
	invalid.Criteria[0].MaxPoints++
	current, err = repo.UpdateDraft(context.Background(), current.ID, current.Revision, invalid, "editor", "Invalid intermediate model")
	if err != nil {
		t.Fatal(err)
	}
	invalidRef := httpapi.AdminModelReference{Kind: "REVISION", ID: current.ID, Revision: &current.Revision}
	w = h.send("POST", "/api/admin/v1/simulations", map[string]any{"model": invalidRef, "sample": sample})
	adminWant(t, w, 422)
	problem := adminRead[struct {
		Code       string                 `json:"code"`
		Validation model.ValidationResult `json:"validation"`
	}](t, w)
	if problem.Code != "MODEL_VALIDATION_FAILED" || problem.Validation.Valid {
		t.Fatal("invalid model was simulated")
	}
}
