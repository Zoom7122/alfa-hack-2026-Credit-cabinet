//go:build integration

package adminstore_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

func TestReleasePreviewDoesNotPersistAndMatchesPreparedManifest(t *testing.T) {
	db := isolatedDatabase(t)
	migrator, _ := adminstore.NewMigrator(db)
	_, err := migrator.Up(context.Background())
	migrationOK(t, err)
	repo, _ := adminstore.NewRepository(db)
	if err := repo.SeedBaseline(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := adminHTTPFixture(t, repo)
	counts := func() []int {
		result := []int{}
		for _, table := range []string{"model_drafts", "model_revisions", "model_versions", "prepared_releases", "change_history", "credit_events"} {
			var count int
			if err := db.QueryRow("SELECT count(*) FROM credit_engine_admin." + table).Scan(&count); err != nil {
				t.Fatal(err)
			}
			result = append(result, count)
		}
		return result
	}
	before := counts()
	request := adminstore.PrepareReleaseRequest{Version: "preview-acceptance", BaseVersion: "2.0.0", PublishedVersions: []string{"ip-v2", "legal-v2"}, Drafts: []adminstore.DraftReference{}, Notes: "Fictional preview acceptance"}
	first := h.send("POST", "/api/admin/v1/releases/preview", map[string]any{"release": request})
	adminWant(t, first, 200)
	second := h.send("POST", "/api/admin/v1/releases/preview", map[string]any{"release": request})
	adminWant(t, second, 200)
	if first.Body.String() != second.Body.String() || !reflect.DeepEqual(before, counts()) {
		t.Fatal("preview changed data or result")
	}
	preview := adminRead[struct {
		Manifest release.Manifest `json:"manifest"`
		SHA      string           `json:"manifest_sha256"`
	}](t, first)
	prepared := h.send("POST", "/api/admin/v1/releases", map[string]any{"release": request, "reason": "Fictional acceptance preparation"})
	adminWant(t, prepared, 200)
	actual := adminRead[adminstore.PreparedRelease](t, prepared)
	if !reflect.DeepEqual(preview.Manifest, actual.Manifest) || preview.SHA != actual.ManifestSHA256 {
		t.Fatal("preview differs from preparation")
	}
}
