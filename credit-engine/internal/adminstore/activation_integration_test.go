//go:build integration

package adminstore_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
	"os"
	"sync"
	"testing"
	"time"
)

func TestRuntimeJSONActivationAndStableRequestSnapshot(t *testing.T) {
	repo := repositoryFixture(t)
	ctx := context.Background()
	migrationOK(t, repo.SeedBaseline(ctx))
	migrationOK(t, repo.SeedActiveModels(ctx))
	runtime := adminstore.NewRuntimeModels(repo)
	initial, err := runtime.Resolve(ctx, "", contracts.BorrowerTypeSoleProprietor)
	migrationOK(t, err)
	same, err := runtime.Resolve(ctx, "", contracts.BorrowerTypeSoleProprietor)
	migrationOK(t, err)
	if initial != same {
		t.Fatal("immutable compiled snapshot not cached")
	}
	data, err := os.ReadFile("../../testdata/decisions/uc04_ip_approved.json")
	migrationOK(t, err)
	var fixture struct {
		Request contracts.DecisionRequest `json:"request"`
	}
	migrationOK(t, json.Unmarshal(data, &fixture))
	age := 40
	fixture.Request.Scoring.IP.OwnerAge = &age
	evaluator, err := scoring.NewEvaluatorWithProvider(nil, nil, clock.Fixed{Time: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}, runtime)
	migrationOK(t, err)
	pinned, metadata, err := evaluator.ModelSnapshot(ctx, fixture.Request)
	migrationOK(t, err)
	before, err := evaluator.Evaluate(pinned, fixture.Request)
	migrationOK(t, err)
	draft, err := repo.CopyVersion(ctx, "ip-v2", "ip-live-v3", "editor", "Live JSON test")
	migrationOK(t, err)
	// Keep the attainable maximum while changing the age-40 scale case.
	draft.Definition.Criteria[0].Cases[4].Points = 10
	draft, err = repo.UpdateDraft(ctx, draft.ID, draft.Revision, draft.Definition, "editor", "Change scale")
	migrationOK(t, err)
	active, err := repo.ApplyDraft(ctx, draft.ID, draft.Revision, 1, "editor", "Apply without restart")
	migrationOK(t, err)
	if active.Version != "ip-live-v3" || active.Revision != 2 {
		t.Fatal(active)
	}
	again, err := repo.ApplyDraft(ctx, draft.ID, draft.Revision, 1, "editor", "Retry lost response")
	migrationOK(t, err)
	if again.Revision != 2 {
		t.Fatal("retry created activation")
	}
	after, err := evaluator.Evaluate(ctx, fixture.Request)
	migrationOK(t, err)
	if after.Score.Model != "ip-live-v3" || after.Score.RawTotal == before.Score.RawTotal {
		t.Fatal("same evaluator did not use applied JSON", before.Score, after.Score)
	}
	old, err := evaluator.Evaluate(pinned, fixture.Request)
	migrationOK(t, err)
	if old.Score.Model != metadata.Version || old.Score.RawTotal != before.Score.RawTotal {
		t.Fatal("in-flight request mixed versions")
	}
	restarted := adminstore.NewRuntimeModels(repo)
	entry, err := restarted.Resolve(ctx, "", contracts.BorrowerTypeSoleProprietor)
	migrationOK(t, err)
	if entry.Metadata().Version != "ip-live-v3" {
		t.Fatal("restart lost activation")
	}
	migrationOK(t, repo.SeedActiveModels(ctx))
	entry, err = restarted.Resolve(ctx, "", contracts.BorrowerTypeSoleProprietor)
	migrationOK(t, err)
	if entry.Metadata().Version != "ip-live-v3" {
		t.Fatal("seed overwrote administrator choice")
	}
	if _, err := restarted.Resolve(ctx, "ip-v2", contracts.BorrowerTypeSoleProprietor); err != nil {
		t.Fatal("historical explicit version unavailable", err)
	}
	if _, err := restarted.Resolve(ctx, "missing", contracts.BorrowerTypeSoleProprietor); !errors.Is(err, modelbindings.ErrUnknownVersion) {
		t.Fatal(err)
	}
	invalid, err := repo.CopyVersion(ctx, "ip-v2", "ip-invalid-live", "editor", "Invalid draft")
	migrationOK(t, err)
	invalid.Definition.Criteria[0].Enabled = false
	invalid, err = repo.UpdateDraft(ctx, invalid.ID, invalid.Revision, invalid.Definition, "editor", "Intermediate weights")
	migrationOK(t, err)
	if _, err := repo.ApplyDraft(ctx, invalid.ID, invalid.Revision, 2, "editor", "Must reject"); err == nil {
		t.Fatal("invalid weights activated")
	}
	if _, err := repo.GetVersion(ctx, "ip-invalid-live"); !errors.Is(err, adminstore.ErrNotFound) {
		t.Fatal("failed apply published a version", err)
	}
	entry, err = runtime.Resolve(ctx, "", contracts.BorrowerTypeSoleProprietor)
	migrationOK(t, err)
	if entry.Metadata().Version != "ip-live-v3" {
		t.Fatal("failed apply changed active model")
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := evaluator.Evaluate(cancelCtx, fixture.Request); err == nil {
		t.Fatal("cancelled lookup fell back to cached default")
	}
}

func TestConcurrentActivationIsAtomic(t *testing.T) {
	repo := repositoryFixture(t)
	ctx := context.Background()
	migrationOK(t, repo.SeedBaseline(ctx))
	migrationOK(t, repo.SeedActiveModels(ctx))
	drafts := make([]adminstore.Draft, 2)
	for i, version := range []string{"ip-concurrent-a", "ip-concurrent-b"} {
		d, err := repo.CopyVersion(ctx, "ip-v2", version, "editor", "Concurrent activation")
		migrationOK(t, err)
		drafts[i] = d
	}
	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range drafts {
		wg.Go(func() { _, results[i] = repo.ApplyDraft(ctx, drafts[i].ID, 1, 1, "editor", "Concurrent apply") })
	}
	wg.Wait()
	success, conflict := 0, 0
	for _, err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, adminstore.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(results)
	}
	for i, err := range results {
		if err != nil {
			if _, err := repo.GetVersion(ctx, drafts[i].Definition.Version); !errors.Is(err, adminstore.ErrNotFound) {
				t.Fatal("losing transaction left a published version", err)
			}
		}
	}
}

func TestRuntimeStoreFailureDoesNotUseCachedActiveModel(t *testing.T) {
	db := isolatedDatabase(t)
	ctx := context.Background()
	migrator, err := adminstore.NewMigrator(db)
	migrationOK(t, err)
	_, err = migrator.Up(ctx)
	migrationOK(t, err)
	repo, err := adminstore.NewRepository(db)
	migrationOK(t, err)
	migrationOK(t, repo.SeedBaseline(ctx))
	migrationOK(t, repo.SeedActiveModels(ctx))
	runtime := adminstore.NewRuntimeModels(repo)
	_, err = runtime.Resolve(ctx, "", contracts.BorrowerTypeSoleProprietor)
	migrationOK(t, err)
	migrationOK(t, db.Close())
	if _, err := runtime.Resolve(ctx, "", contracts.BorrowerTypeSoleProprietor); err == nil {
		t.Fatal("closed store silently returned a cached active model")
	}
	if _, err := runtime.Resolve(ctx, "ip-v2", contracts.BorrowerTypeSoleProprietor); err == nil {
		t.Fatal("closed store silently returned an explicit cached version")
	}
}
