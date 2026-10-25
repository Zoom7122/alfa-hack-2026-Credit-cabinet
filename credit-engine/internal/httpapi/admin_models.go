package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/sourcecatalog"
	"github.com/gin-gonic/gin"
)

type AdminModels interface {
	ListVersions(context.Context, adminstore.Page) ([]adminstore.PublishedModel, error)
	GetVersion(context.Context, string) (adminstore.PublishedModel, error)
	CopyVersion(context.Context, string, string, string, string) (adminstore.Draft, error)
	CreateDraft(context.Context, model.ModelDefinition, string, string) (adminstore.Draft, error)
	ListDrafts(context.Context, adminstore.Page) ([]adminstore.Draft, error)
	GetDraft(context.Context, string) (adminstore.Draft, error)
	UpdateDraft(context.Context, string, int64, model.ModelDefinition, string, string) (adminstore.Draft, error)
	DeleteDraft(context.Context, string, int64, string, string) error
	ListRevisions(context.Context, string, adminstore.Page) ([]adminstore.Revision, error)
	GetRevision(context.Context, string, int64) (adminstore.Revision, error)
	History(context.Context, string, string, adminstore.Page) ([]adminstore.HistoryEntry, error)
	ListReleases(context.Context, adminstore.Page) ([]adminstore.PreparedRelease, error)
	GetRelease(context.Context, string) (adminstore.PreparedRelease, error)
	PrepareRelease(context.Context, adminstore.PrepareReleaseRequest, string, string) (adminstore.PreparedRelease, error)
}

func writeAdminError(c *gin.Context, err error) {
	var conflict *adminstore.ConflictError
	var validation *modelgen.ValidationError
	var format *model.FormatError
	var database *adminstore.DatabaseError
	switch {
	case errors.As(err, &validation):
		c.Header("Content-Type", "application/problem+json")
		c.AbortWithStatusJSON(422, struct {
			contracts.Problem
			ModelVersion string                 `json:"model_version"`
			Validation   model.ValidationResult `json:"validation"`
		}{Problem: contracts.Problem{Type: "about:blank", Title: http.StatusText(422), Status: 422, Code: "MODEL_VALIDATION_FAILED", Detail: "Model validation failed", Fields: []string{}}, ModelVersion: validation.Version, Validation: validation.Result})
	case errors.As(err, &conflict):
		c.Header("Content-Type", "application/problem+json")
		c.AbortWithStatusJSON(409, struct {
			contracts.Problem
			CurrentRevision int64 `json:"current_revision"`
		}{Problem: contracts.Problem{Type: "about:blank", Title: http.StatusText(409), Status: 409, Code: "REVISION_CONFLICT", Detail: "Reload the current draft revision", Fields: []string{}}, CurrentRevision: conflict.CurrentRevision})
	case errors.Is(err, context.DeadlineExceeded):
		writeProblem(c, 504, "ADMIN_REQUEST_TIMEOUT", nil)
	case errors.Is(err, context.Canceled):
		writeProblem(c, 503, "ADMIN_REQUEST_CANCELED", nil)
	case errors.As(err, &format):
		writeProblem(c, 400, "INVALID_MODEL_DOCUMENT", []string{format.Path})
	case errors.Is(err, adminstore.ErrNotFound):
		writeProblem(c, 404, "ADMIN_OBJECT_NOT_FOUND", nil)
	case errors.Is(err, adminstore.ErrImmutable):
		writeProblem(c, 409, "IMMUTABLE_VERSION", nil)
	case errors.Is(err, adminstore.ErrConflict):
		writeProblem(c, 409, "ADMIN_CONFLICT", nil)
	case errors.Is(err, adminstore.ErrInvalid):
		writeProblem(c, 422, "INVALID_ADMIN_INPUT", nil)
	case errors.As(err, &database):
		writeProblem(c, 503, "ADMIN_DATABASE_UNAVAILABLE", nil)
	default:
		writeProblem(c, 500, "INTERNAL_ERROR", nil)
	}
}

func adminPagination(c *gin.Context) (adminstore.Page, bool) {
	limit, offset, ok := adminPage(c)
	return adminstore.Page{Limit: limit, Offset: offset}, ok
}
func adminActor(c *gin.Context) string { return c.MustGet(adminauth.PrincipalKey).(string) }
func adminVersion(c *gin.Context) bool {
	if !release.ValidVersion(c.Param("version")) {
		writeProblem(c, 400, "INVALID_VERSION", nil)
		return false
	}
	return true
}

func RegisterAdminModels(router *gin.Engine, access *adminauth.Access, store AdminModels) {
	a := adminModels{store: store}
	g := router.Group("/api/admin/v1", access.Browser())
	g.GET("/models", func(c *gin.Context) {
		page, ok := adminPagination(c)
		if !ok {
			return
		}
		result, err := store.ListVersions(c.Request.Context(), page)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.GET("/models/:version", func(c *gin.Context) {
		if !adminVersion(c) {
			return
		}
		result, err := store.GetVersion(c.Request.Context(), c.Param("version"))
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.POST("/models/:version/drafts", a.copy)
	g.GET("/drafts", func(c *gin.Context) {
		page, ok := adminPagination(c)
		if !ok {
			return
		}
		result, err := store.ListDrafts(c.Request.Context(), page)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.POST("/drafts", a.create)
	g.GET("/drafts/:id", func(c *gin.Context) {
		result, err := store.GetDraft(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.PUT("/drafts/:id", a.update)
	g.DELETE("/drafts/:id", a.remove)
	g.GET("/drafts/:id/validation", a.validation)
	g.GET("/drafts/:id/revisions", func(c *gin.Context) {
		page, ok := adminPagination(c)
		if !ok {
			return
		}
		result, err := store.ListRevisions(c.Request.Context(), c.Param("id"), page)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.GET("/drafts/:id/revisions/:revision", func(c *gin.Context) {
		revision, err := strconv.ParseInt(c.Param("revision"), 10, 64)
		if err != nil || revision < 1 {
			writeProblem(c, 400, "INVALID_REVISION", nil)
			return
		}
		result, err := store.GetRevision(c.Request.Context(), c.Param("id"), revision)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.POST("/compare", a.compare)
	g.GET("/history/:kind/:id", func(c *gin.Context) {
		page, ok := adminPagination(c)
		if !ok {
			return
		}
		result, err := store.History(c.Request.Context(), c.Param("kind"), c.Param("id"), page)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.GET("/releases", func(c *gin.Context) {
		page, ok := adminPagination(c)
		if !ok {
			return
		}
		result, err := store.ListReleases(c.Request.Context(), page)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.GET("/releases/:version", func(c *gin.Context) {
		if !adminVersion(c) {
			return
		}
		result, err := store.GetRelease(c.Request.Context(), c.Param("version"))
		if err != nil {
			writeAdminError(c, err)
			return
		}
		c.JSON(200, result)
	})
	g.POST("/releases", a.prepare)
	g.POST("/releases/preview", a.previewRelease)
	g.GET("/releases/:version/archive", a.download)
}

type adminModels struct{ store AdminModels }

func (a adminModels) copy(c *gin.Context) {
	if !adminVersion(c) {
		return
	}
	fields, ok := adminBody(c, "new_version", "reason")
	if !ok {
		return
	}
	version, err := adminString(fields["new_version"])
	if err != nil {
		writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
		return
	}
	reason, err := adminString(fields["reason"])
	if err != nil {
		writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
		return
	}
	result, err := a.store.CopyVersion(c.Request.Context(), c.Param("version"), version, adminActor(c), reason)
	if err != nil {
		writeAdminError(c, err)
		return
	}
	c.Header("Location", "/api/admin/v1/drafts/"+result.ID)
	c.JSON(201, result)
}
func (a adminModels) create(c *gin.Context) {
	fields, ok := adminBody(c, "definition", "reason")
	if !ok {
		return
	}
	definition, err := model.Decode(bytes.NewReader(fields["definition"]))
	if err != nil {
		writeProblem(c, 400, "INVALID_MODEL_DOCUMENT", nil)
		return
	}
	reason, err := adminString(fields["reason"])
	if err != nil {
		writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
		return
	}
	result, err := a.store.CreateDraft(c.Request.Context(), definition, adminActor(c), reason)
	if err != nil {
		writeAdminError(c, err)
		return
	}
	c.Header("Location", "/api/admin/v1/drafts/"+result.ID)
	c.JSON(201, result)
}
func (a adminModels) update(c *gin.Context) {
	fields, ok := adminBody(c, "expected_revision", "definition", "reason")
	if !ok {
		return
	}
	revision, err := adminRevision(fields["expected_revision"])
	if err != nil {
		writeProblem(c, 400, "INVALID_REVISION", nil)
		return
	}
	definition, err := model.Decode(bytes.NewReader(fields["definition"]))
	if err != nil {
		writeProblem(c, 400, "INVALID_MODEL_DOCUMENT", nil)
		return
	}
	reason, err := adminString(fields["reason"])
	if err != nil {
		writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
		return
	}
	result, err := a.store.UpdateDraft(c.Request.Context(), c.Param("id"), revision, definition, adminActor(c), reason)
	if err != nil {
		writeAdminError(c, err)
		return
	}
	c.JSON(200, result)
}
func (a adminModels) remove(c *gin.Context) {
	fields, ok := adminBody(c, "expected_revision", "reason")
	if !ok {
		return
	}
	revision, err := adminRevision(fields["expected_revision"])
	if err != nil {
		writeProblem(c, 400, "INVALID_REVISION", nil)
		return
	}
	reason, err := adminString(fields["reason"])
	if err != nil {
		writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
		return
	}
	if err := a.store.DeleteDraft(c.Request.Context(), c.Param("id"), revision, adminActor(c), reason); err != nil {
		writeAdminError(c, err)
		return
	}
	c.Status(204)
}
func (a adminModels) validation(c *gin.Context) {
	draft, err := a.store.GetDraft(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeAdminError(c, err)
		return
	}
	bindings, err := sourcecatalog.Bindings(draft.Definition.BorrowerType)
	if err != nil {
		writeAdminError(c, err)
		return
	}
	c.JSON(200, struct {
		DraftID          string                 `json:"draft_id"`
		Revision         int64                  `json:"revision"`
		DefinitionSHA256 string                 `json:"definition_sha256"`
		Validation       model.ValidationResult `json:"validation"`
	}{draft.ID, draft.Revision, draft.DefinitionSHA256, model.Validate(draft.Definition, bindings)})
}

// Pinned references prevent a comparison from silently selecting a newer draft.
type AdminModelReference struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Revision *int64 `json:"revision"`
}

func parseAdminReference(raw []byte) (AdminModelReference, error) {
	// revision is explicitly null for an immutable published model.
	var reference AdminModelReference
	fields, err := adminObjectNullable(raw, []string{"kind", "id", "revision"}, []string{"revision"})
	if err != nil {
		return reference, err
	}
	if reference.Kind, err = adminString(fields["kind"]); err != nil {
		return reference, err
	}
	if reference.ID, err = adminString(fields["id"]); err != nil {
		return reference, err
	}
	switch reference.Kind {
	case "PUBLISHED":
		if !bytes.Equal(bytes.TrimSpace(fields["revision"]), []byte("null")) || !release.ValidVersion(reference.ID) {
			return reference, adminstore.ErrInvalid
		}
	case "REVISION":
		revision, err := adminRevision(fields["revision"])
		if err != nil {
			return reference, err
		}
		reference.Revision = &revision
	default:
		return reference, adminstore.ErrInvalid
	}
	return reference, nil
}
func (a adminModels) resolve(ctx context.Context, reference AdminModelReference) (model.ModelDefinition, error) {
	if reference.Kind == "PUBLISHED" {
		version, err := a.store.GetVersion(ctx, reference.ID)
		return version.Definition, err
	}
	revision, err := a.store.GetRevision(ctx, reference.ID, *reference.Revision)
	return revision.Definition, err
}
func (a adminModels) compare(c *gin.Context) {
	fields, ok := adminBody(c, "left", "right")
	if !ok {
		return
	}
	left, err := parseAdminReference(fields["left"])
	if err != nil {
		writeProblem(c, 400, "INVALID_MODEL_REFERENCE", nil)
		return
	}
	right, err := parseAdminReference(fields["right"])
	if err != nil {
		writeProblem(c, 400, "INVALID_MODEL_REFERENCE", nil)
		return
	}
	before, err := a.resolve(c.Request.Context(), left)
	if err != nil {
		writeAdminError(c, err)
		return
	}
	after, err := a.resolve(c.Request.Context(), right)
	if err != nil {
		writeAdminError(c, err)
		return
	}
	if before.BorrowerType != after.BorrowerType {
		writeProblem(c, 422, "INCOMPATIBLE_MODELS", nil)
		return
	}
	comparison, err := model.CompareDefinitions(before, after)
	if err != nil {
		writeAdminError(c, err)
		return
	}
	c.JSON(200, struct {
		Left       AdminModelReference        `json:"left"`
		Right      AdminModelReference        `json:"right"`
		Comparison model.DefinitionComparison `json:"comparison"`
	}{left, right, comparison})
}
