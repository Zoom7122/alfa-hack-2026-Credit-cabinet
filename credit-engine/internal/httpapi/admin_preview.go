package httpapi

import (
	"errors"
	"sort"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
	"github.com/gin-gonic/gin"
)

// Preview resolves immutable versions/revisions and generates the same bundle
// without storing a release, changing a draft, or activating the running MS1.
func (a adminModels) previewRelease(c *gin.Context) {
	fields, ok := adminBody(c, "release")
	if !ok {
		return
	}
	request, err := parseAdminRelease(fields["release"])
	if err != nil || !release.ValidVersion(request.Version) || !release.ValidVersion(request.BaseVersion) || len(request.Drafts)+len(request.PublishedVersions) == 0 {
		writeProblem(c, 400, "INVALID_RELEASE_REQUEST", nil)
		return
	}
	base, err := a.store.GetRelease(c.Request.Context(), request.BaseVersion)
	if err != nil {
		writeAdminError(c, err)
		return
	}
	view, err := release.ReadArchive(base.Archive)
	if err != nil {
		writeAdminError(c, adminstore.ErrCorrupt)
		return
	}
	manifest, err := release.MarshalManifest(view.Manifest())
	if err != nil || release.Hash(manifest) != base.ManifestSHA256 {
		writeAdminError(c, adminstore.ErrCorrupt)
		return
	}
	markdown, ok := view.File("docs/02_inputs_and_scoring.md")
	if !ok {
		writeAdminError(c, adminstore.ErrCorrupt)
		return
	}
	definitions := []model.ModelDefinition{}
	for _, version := range request.PublishedVersions {
		if !release.ValidVersion(version) {
			writeAdminError(c, adminstore.ErrInvalid)
			return
		}
		row, err := a.store.GetVersion(c.Request.Context(), version)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		definitions = append(definitions, row.Definition)
	}
	refs := append([]adminstore.DraftReference{}, request.Drafts...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	seen := map[string]bool{}
	for _, ref := range refs {
		if seen[strings.ToLower(ref.ID)] || ref.Revision < 1 {
			writeAdminError(c, adminstore.ErrInvalid)
			return
		}
		seen[strings.ToLower(ref.ID)] = true
		current, err := a.store.GetDraft(c.Request.Context(), ref.ID)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		if current.Revision != ref.Revision {
			writeAdminError(c, &adminstore.ConflictError{CurrentRevision: current.Revision})
			return
		}
		row, err := a.store.GetRevision(c.Request.Context(), ref.ID, ref.Revision)
		if err != nil {
			writeAdminError(c, err)
			return
		}
		definitions = append(definitions, row.Definition)
	}
	bundle, err := release.Prepare(definitions, release.Options{Version: request.Version, Previous: &base.Manifest, Markdown: markdown, Notes: request.Notes})
	if err != nil {
		if errors.Is(err, release.ErrImmutable) {
			writeAdminError(c, adminstore.ErrImmutable)
		} else {
			writeAdminError(c, errors.Join(adminstore.ErrInvalid, err))
		}
		return
	}
	if err := c.Request.Context().Err(); err != nil {
		writeAdminError(c, err)
		return
	}
	type document struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	documents := []document{}
	for _, artifact := range bundle.Artifacts() {
		switch artifact.Path {
		case "contracts/openapi.json", "contracts/scoring_facts.openapi.json", "docs/02_inputs_and_scoring.md", "models/changes.md":
			documents = append(documents, document{artifact.Path, string(artifact.Data)})
		}
	}
	c.JSON(200, struct {
		Manifest       release.Manifest `json:"manifest"`
		ManifestSHA256 string           `json:"manifest_sha256"`
		Documents      []document       `json:"documents"`
	}{bundle.Manifest(), bundle.SHA256(), documents})
}
