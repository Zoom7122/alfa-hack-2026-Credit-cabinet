package httpapi

import (
	"encoding/json"
	"strconv"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/gin-gonic/gin"
)

func parseAdminRelease(raw []byte) (adminstore.PrepareReleaseRequest, error) {
	var request adminstore.PrepareReleaseRequest
	fields, err := adminObject(raw, []string{"version", "base_version", "published_versions", "drafts", "notes"})
	if err != nil {
		return request, err
	}
	for _, item := range []struct {
		key    string
		target *string
	}{{"version", &request.Version}, {"base_version", &request.BaseVersion}, {"notes", &request.Notes}} {
		if *item.target, err = adminString(fields[item.key]); err != nil {
			return request, err
		}
	}
	var versions []json.RawMessage
	if err = json.Unmarshal(fields["published_versions"], &versions); err != nil {
		return request, err
	}
	request.PublishedVersions = []string{}
	for _, raw := range versions {
		version, err := adminString(raw)
		if err != nil || string(raw) == "null" {
			return request, adminstore.ErrInvalid
		}
		request.PublishedVersions = append(request.PublishedVersions, version)
	}
	var refs []json.RawMessage
	if err = json.Unmarshal(fields["drafts"], &refs); err != nil {
		return request, err
	}
	request.Drafts = []adminstore.DraftReference{}
	for _, raw := range refs {
		fields, err := adminObject(raw, []string{"id", "revision"})
		if err != nil {
			return request, err
		}
		id, err := adminString(fields["id"])
		if err != nil {
			return request, err
		}
		revision, err := adminRevision(fields["revision"])
		if err != nil {
			return request, err
		}
		request.Drafts = append(request.Drafts, adminstore.DraftReference{ID: id, Revision: revision})
	}
	return request, nil
}
func (a adminModels) prepare(c *gin.Context) {
	fields, ok := adminBody(c, "release", "reason")
	if !ok {
		return
	}
	request, err := parseAdminRelease(fields["release"])
	if err != nil {
		writeProblem(c, 400, "INVALID_RELEASE_REQUEST", nil)
		return
	}
	reason, err := adminString(fields["reason"])
	if err != nil {
		writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
		return
	}
	result, err := a.store.PrepareRelease(c.Request.Context(), request, adminActor(c), reason)
	if err != nil {
		writeAdminError(c, err)
		return
	}
	c.Header("Location", "/api/admin/v1/releases/"+result.Version)
	// 200 also covers an identical idempotent replay of an existing release.
	c.JSON(200, result)
}
func (a adminModels) download(c *gin.Context) {
	if !adminVersion(c) {
		return
	}
	result, err := a.store.GetRelease(c.Request.Context(), c.Param("version"))
	if err != nil {
		writeAdminError(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="credit-engine-models-`+result.Version+`.zip"`)
	c.Header("Content-Length", strconv.Itoa(len(result.Archive)))
	c.Header("X-Archive-SHA256", result.ArchiveSHA256)
	c.Header("ETag", `"`+result.ArchiveSHA256+`"`)
	c.Data(200, "application/zip", result.Archive)
}
