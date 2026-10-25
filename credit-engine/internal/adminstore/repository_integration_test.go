//go:build integration

package adminstore_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

func repositoryFixture(t *testing.T) *adminstore.Repository {
	t.Helper()
	db := isolatedDatabase(t)
	migrator, err := adminstore.NewMigrator(db)
	migrationOK(t, err)
	_, err = migrator.Up(context.Background())
	migrationOK(t, err)
	repo, err := adminstore.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestRepositoryDraftsConcurrencyHistoryAndDeletion(t *testing.T) {
	repo := repositoryFixture(t)
	ctx := context.Background()
	if err := repo.SeedBaseline(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.SeedBaseline(ctx); err != nil {
		t.Fatal(err)
	}
	versions, err := repo.ListVersions(ctx, adminstore.Page{})
	if err != nil || len(versions) != 2 {
		t.Fatal(versions, err)
	}
	releases, err := repo.ListReleases(ctx, adminstore.Page{})
	if err != nil || len(releases) != 1 || len(releases[0].Archive) != 0 {
		t.Fatal("baseline import not idempotent", releases, err)
	}
	history, err := repo.History(ctx, "RELEASE", "2.0.0", adminstore.Page{})
	if err != nil || len(history) != 1 {
		t.Fatal(history, err)
	}
	draft, err := repo.CopyVersion(ctx, "ip-v2", "ip-repository-v3", "admin", "Copy baseline")
	if err != nil {
		t.Fatal(err)
	}
	draft.Definition.Criteria[0].MaxPoints--
	updated, err := repo.UpdateDraft(ctx, draft.ID, 1, draft.Definition, "admin", "Save invalid intermediate weights")
	if err != nil || updated.Revision != 2 {
		t.Fatal(updated, err)
	}
	old, err := repo.GetRevision(ctx, draft.ID, 1)
	if err != nil || old.Definition.Criteria[0].MaxPoints == updated.Definition.Criteria[0].MaxPoints {
		t.Fatal("old revision mutated", err)
	}
	if _, err := repo.PrepareRelease(ctx, adminstore.PrepareReleaseRequest{Version: "invalid-r1", BaseVersion: "2.0.0", PublishedVersions: []string{"ip-v2", "legal-v2"}, Drafts: []adminstore.DraftReference{{ID: draft.ID, Revision: 2}}}, "admin", "Must fail invalid weights"); err == nil {
		t.Fatal("invalid draft released")
	}
	if _, err := repo.GetVersion(ctx, "ip-repository-v3"); !errors.Is(err, adminstore.ErrNotFound) {
		t.Fatal("failed release left published model", err)
	}
	if _, err := repo.GetRelease(ctx, "invalid-r1"); !errors.Is(err, adminstore.ErrNotFound) {
		t.Fatal("failed release left archive", err)
	}
	var wg sync.WaitGroup
	results := [2]error{}
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = repo.UpdateDraft(ctx, draft.ID, 2, updated.Definition, "admin", "Concurrent change")
		}()
	}
	wg.Wait()
	ok, conflict := 0, 0
	for _, err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, adminstore.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(results)
	}
	current, err := repo.GetDraft(ctx, draft.ID)
	if err != nil || current.Revision != 3 {
		t.Fatal(current, err)
	}
	revisions, err := repo.ListRevisions(ctx, draft.ID, adminstore.Page{})
	if err != nil || len(revisions) != 3 {
		t.Fatal(revisions, err)
	}
	if err := repo.DeleteDraft(ctx, draft.ID, 2, "admin", "Stale delete"); !errors.Is(err, adminstore.ErrConflict) {
		t.Fatal(err)
	}
	if err := repo.DeleteDraft(ctx, draft.ID, 3, "admin", "Delete draft"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetDraft(ctx, draft.ID); !errors.Is(err, adminstore.ErrNotFound) {
		t.Fatal(err)
	}
	if drafts, err := repo.ListDrafts(ctx, adminstore.Page{}); err != nil || len(drafts) != 0 {
		t.Fatal(drafts, err)
	}
	if revisions, err := repo.ListRevisions(ctx, draft.ID, adminstore.Page{}); err != nil || len(revisions) != 3 {
		t.Fatal("deletion lost revisions", err)
	}
	history, err = repo.History(ctx, "DRAFT", draft.ID, adminstore.Page{})
	if err != nil || len(history) != 4 || history[0].Action != "DRAFT_DELETED" {
		t.Fatal(history, err)
	}
	for _, entry := range history {
		if entry.Author != "admin" || entry.Reason == "" || entry.OccurredAt.IsZero() || len(entry.Details) == 0 {
			t.Fatal("incomplete audit record", entry)
		}
	}
}

func TestRepositoryPreparedReleaseIsImmutableAndAtomic(t *testing.T) {
	repo := repositoryFixture(t)
	ctx := context.Background()
	if err := repo.SeedBaseline(ctx); err != nil {
		t.Fatal(err)
	}
	draft, err := repo.CopyVersion(ctx, "ip-v2", "ip-release-v3", "admin", "New version")
	if err != nil {
		t.Fatal(err)
	}
	draft.Definition.Criteria[0].Cases[0].Points = 4
	draft, err = repo.UpdateDraft(ctx, draft.ID, 1, draft.Definition, "admin", "Change one scale point")
	if err != nil {
		t.Fatal(err)
	}
	request := adminstore.PrepareReleaseRequest{Version: "repo-r1", BaseVersion: "2.0.0", PublishedVersions: []string{"ip-v2", "legal-v2"}, Drafts: []adminstore.DraftReference{{ID: draft.ID, Revision: 2}}, Notes: "New test model"}
	first, err := repo.PrepareRelease(ctx, request, "admin", "Prepare release")
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.PrepareRelease(ctx, request, "admin", "Repeat preparation")
	if err != nil || !adminstore.EqualPreparedContent(first, second) {
		t.Fatal("identical repeat changed content", err)
	}
	view, err := release.ReadArchive(first.Archive)
	if err != nil || view.Manifest().Version != "repo-r1" || len(view.Manifest().Models) != 3 {
		t.Fatal("invalid stored archive", err)
	}
	stored, err := repo.GetRelease(ctx, "repo-r1")
	if err != nil || !reflect.DeepEqual(first, stored) {
		t.Fatal("stored release changed", err)
	}
	history, err := repo.History(ctx, "RELEASE", "repo-r1", adminstore.Page{})
	if err != nil || len(history) != 1 {
		t.Fatal("repeat duplicated history", history, err)
	}
	request.Notes = "different contents"
	if _, err := repo.PrepareRelease(ctx, request, "admin", "Cannot rewrite release"); !errors.Is(err, adminstore.ErrImmutable) {
		t.Fatal(err)
	}
	if unchanged, err := repo.GetRelease(ctx, "repo-r1"); err != nil || !adminstore.EqualPreparedContent(first, unchanged) {
		t.Fatal(err)
	}
	copy, err := repo.CopyVersion(ctx, "ip-v2", "ip-release-v3", "admin", "Try existing version")
	if err != nil {
		t.Fatal(err)
	}
	bad := adminstore.PrepareReleaseRequest{Version: "repo-r2", BaseVersion: "repo-r1", PublishedVersions: []string{"ip-v2", "legal-v2"}, Drafts: []adminstore.DraftReference{{ID: copy.ID, Revision: 1}}}
	if _, err := repo.PrepareRelease(ctx, bad, "admin", "Cannot mutate model version"); !errors.Is(err, adminstore.ErrImmutable) {
		t.Fatal(err)
	}
	if _, err := repo.GetRelease(ctx, "repo-r2"); !errors.Is(err, adminstore.ErrNotFound) {
		t.Fatal("conflict left partial release", err)
	}
}

func TestRepositoryFailureRollsBackModelsArchiveAndHistory(t *testing.T) {
	db := isolatedDatabase(t)
	ctx := context.Background()
	migrator, err := adminstore.NewMigrator(db)
	migrationOK(t, err)
	_, err = migrator.Up(ctx)
	migrationOK(t, err)
	repo, err := adminstore.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SeedBaseline(ctx); err != nil {
		t.Fatal(err)
	}
	draft, err := repo.CopyVersion(ctx, "ip-v2", "ip-rollback-v3", "admin", "Rollback fixture")
	if err != nil {
		t.Fatal(err)
	}
	dbExec(t, db, `CREATE FUNCTION credit_engine_admin.fail_test_release() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected save failure'; END $$`)
	dbExec(t, db, `CREATE TRIGGER fail_test_release BEFORE INSERT ON credit_engine_admin.prepared_releases FOR EACH ROW EXECUTE FUNCTION credit_engine_admin.fail_test_release()`)
	request := adminstore.PrepareReleaseRequest{Version: "rollback-r1", BaseVersion: "2.0.0", PublishedVersions: []string{"ip-v2", "legal-v2"}, Drafts: []adminstore.DraftReference{{ID: draft.ID, Revision: 1}}}
	if _, err := repo.PrepareRelease(ctx, request, "admin", "Injected failure"); err == nil {
		t.Fatal("injected failure ignored")
	}
	if n := countRows(t, db, "SELECT count(*) FROM credit_engine_admin.model_versions"); n != 2 {
		t.Fatal("partial model publication", n)
	}
	if n := countRows(t, db, "SELECT count(*) FROM credit_engine_admin.prepared_releases"); n != 1 {
		t.Fatal("partial archive publication", n)
	}
	if n := countRows(t, db, "SELECT count(*) FROM credit_engine_admin.change_history WHERE entity_id IN ('ip-rollback-v3','rollback-r1')"); n != 0 {
		t.Fatal("partial publication history", n)
	}
	dbExec(t, db, "DROP TRIGGER fail_test_release ON credit_engine_admin.prepared_releases")
	dbExec(t, db, "DROP FUNCTION credit_engine_admin.fail_test_release()")
	if _, err := repo.PrepareRelease(ctx, request, "admin", "Retry after failure"); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryConcurrentSeedAndFailedDraftUpdate(t *testing.T) {
	db := isolatedDatabase(t)
	ctx := context.Background()
	migrator, err := adminstore.NewMigrator(db)
	migrationOK(t, err)
	_, err = migrator.Up(ctx)
	migrationOK(t, err)
	repo, err := adminstore.NewRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := [2]error{}
	for i := range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); failures[i] = repo.SeedBaseline(ctx) }()
	}
	wg.Wait()
	for _, err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := countRows(t, db, "SELECT count(*) FROM credit_engine_admin.model_versions"); n != 2 {
		t.Fatal("seed duplicated model versions", n)
	}
	if n := countRows(t, db, "SELECT count(*) FROM credit_engine_admin.change_history"); n != 3 {
		t.Fatal("seed duplicated publication history", n)
	}
	draft, err := repo.CopyVersion(ctx, "ip-v2", "ip-failed-update-v3", "admin", "Initial draft")
	if err != nil {
		t.Fatal(err)
	}
	dbExec(t, db, `CREATE FUNCTION credit_engine_admin.fail_test_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='DRAFT_UPDATED' THEN RAISE EXCEPTION 'injected history failure'; END IF; RETURN NEW; END $$`)
	dbExec(t, db, `CREATE TRIGGER fail_test_history BEFORE INSERT ON credit_engine_admin.change_history FOR EACH ROW EXECUTE FUNCTION credit_engine_admin.fail_test_history()`)
	draft.Definition.Title = "New title that must roll back"
	if _, err := repo.UpdateDraft(ctx, draft.ID, 1, draft.Definition, "admin", "Injected history failure"); err == nil {
		t.Fatal("injected failure ignored")
	}
	current, err := repo.GetDraft(ctx, draft.ID)
	if err != nil || current.Revision != 1 || current.Definition.Title == draft.Definition.Title {
		t.Fatal("draft update was partially saved", current, err)
	}
	if revisions, err := repo.ListRevisions(ctx, draft.ID, adminstore.Page{}); err != nil || len(revisions) != 1 {
		t.Fatal("failed update left revision", err)
	}
	if history, err := repo.History(ctx, "DRAFT", draft.ID, adminstore.Page{}); err != nil || len(history) != 1 {
		t.Fatal("failed update left history", err)
	}
}
