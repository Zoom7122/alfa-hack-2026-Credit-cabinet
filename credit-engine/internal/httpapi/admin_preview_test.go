package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
	modelassets "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/models"
	"github.com/gin-gonic/gin"
)

type previewStore struct {
	AdminModels
	base       adminstore.PreparedRelease
	definition model.ModelDefinition
	current    int64
}

func (s previewStore) GetRelease(context.Context, string) (adminstore.PreparedRelease, error) {
	return s.base, nil
}
func (s previewStore) GetVersion(context.Context, string) (adminstore.PublishedModel, error) {
	return adminstore.PublishedModel{Definition: s.definition}, nil
}
func (s previewStore) GetDraft(context.Context, string) (adminstore.Draft, error) {
	return adminstore.Draft{Revision: s.current}, nil
}
func (s previewStore) GetRevision(context.Context, string, int64) (adminstore.Revision, error) {
	return adminstore.Revision{Definition: s.definition}, nil
}

func TestReleasePreviewIsDeterministicAndDoesNotPublish(t *testing.T) {
	data, err := os.ReadFile("../../models/ip-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := model.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	archive := modelassets.InitialReleaseArchive()
	view, err := release.ReadArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := release.MarshalManifest(view.Manifest())
	store := previewStore{base: adminstore.PreparedRelease{Archive: archive, Manifest: view.Manifest(), ManifestSHA256: release.Hash(manifest)}, definition: definition, current: 1}
	r := gin.New()
	r.POST("/", adminModels{store: store}.previewRelease)
	body := `{"release":{"version":"preview-test","base_version":"2.0.0","published_versions":["ip-v2"],"drafts":[],"notes":"preview"}}`
	call := func(raw string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("POST", "/", strings.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, request)
		return w
	}
	first, second := call(body), call(body)
	if first.Code != 200 || first.Body.String() != second.Body.String() {
		t.Fatal(first.Code, first.Body.String())
	}
	var result struct {
		Manifest  release.Manifest                 `json:"manifest"`
		Documents []struct{ Path, Content string } `json:"documents"`
		SHA       string                           `json:"manifest_sha256"`
	}
	if json.Unmarshal(first.Body.Bytes(), &result) != nil || result.Manifest.Version != "preview-test" || len(result.Documents) != 4 {
		t.Fatal("incomplete preview")
	}
	actual, _ := release.MarshalManifest(result.Manifest)
	if result.SHA != release.Hash(actual) {
		t.Fatal("manifest checksum mismatch")
	}
	for _, doc := range result.Documents {
		if doc.Path == "" || doc.Content == "" {
			t.Fatal("empty document")
		}
	}
	catalog, _ := modelbindings.Default()
	if len(catalog.List()) != 2 {
		t.Fatal("preview activated a model")
	}
	t.Run("stale pinned revision", func(t *testing.T) {
		stale := strings.Replace(body, `"published_versions":["ip-v2"],"drafts":[]`, `"published_versions":[],"drafts":[{"id":"draft","revision":2}]`, 1)
		if response := call(stale); response.Code != 409 {
			t.Fatal(response.Code, response.Body.String())
		}
	})
	t.Run("invalid maximum", func(t *testing.T) {
		store.definition.Criteria[0].MaxPoints++
		router := gin.New()
		router.POST("/", adminModels{store: store}.previewRelease)
		request := httptest.NewRequest("POST", "/", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, request)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "MODEL_VALIDATION_FAILED") {
			t.Fatal(w.Code, w.Body.String())
		}
	})
}

func TestRunningCatalogAndAdminProtection(t *testing.T) {
	catalog, err := modelbindings.Default()
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	RegisterScoringModels(router, catalog)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/models", nil))
	var got modelbindings.RuntimeMetadata
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.FormatVersion != 1 || len(got.Models) != 2 || got.Models[0].DefinitionSHA256 == "" {
		t.Fatal("invalid runtime catalog")
	}
	protected := gin.New()
	RegisterAdminRuntime(protected, adminTestAccess(t), nil)
	w = httptest.NewRecorder()
	protected.ServeHTTP(w, httptest.NewRequest("GET", "/api/admin/v1/runtime/models", nil))
	if w.Code != 401 {
		t.Fatal("runtime route exposed", w.Code)
	}
}
