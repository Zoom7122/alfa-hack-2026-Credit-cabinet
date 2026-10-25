package adminstore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/models"
)

const preparationLockID int64 = 0x4352454c45415345

type PreparedRelease struct {
	Version         string           `json:"version"`
	Manifest        release.Manifest `json:"manifest"`
	ManifestSHA256  string           `json:"manifest_sha256"`
	ArchiveSHA256   string           `json:"archive_sha256"`
	ChangesMarkdown string           `json:"changes_markdown"`
	CreatedBy       string           `json:"created_by"`
	CreatedAt       time.Time        `json:"created_at"`
	Archive         []byte           `json:"-"`
}
type DraftReference struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}
type PrepareReleaseRequest struct {
	Version           string           `json:"version"`
	BaseVersion       string           `json:"base_version"`
	PublishedVersions []string         `json:"published_versions"`
	Drafts            []DraftReference `json:"drafts"`
	Notes             string           `json:"notes"`
}

const releaseMetadataSelect = "SELECT release_version,manifest_json,manifest_sha256,archive_sha256,changes_markdown,created_by,created_at,base_release_version,base_manifest_sha256,notes"

func scanRelease(row rowScanner, withArchive bool) (PreparedRelease, error) {
	var value PreparedRelease
	var manifest, notes string
	var base, baseSHA sql.NullString
	dest := []any{&value.Version, &manifest, &value.ManifestSHA256, &value.ArchiveSHA256, &value.ChangesMarkdown, &value.CreatedBy, &value.CreatedAt, &base, &baseSHA, &notes}
	if withArchive {
		dest = append(dest, &value.Archive)
	}
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PreparedRelease{}, ErrNotFound
		}
		return PreparedRelease{}, databaseError("read release", err)
	}
	if release.Hash([]byte(manifest)) != value.ManifestSHA256 {
		return PreparedRelease{}, ErrCorrupt
	}
	parsed, err := release.DecodeManifest(strings.NewReader(manifest))
	if err != nil {
		return PreparedRelease{}, ErrCorrupt
	}
	if parsed.Version != value.Version || parsed.PreviousVersion != base.String || parsed.PreviousSHA256 != baseSHA.String || parsed.Notes != notes {
		return PreparedRelease{}, ErrCorrupt
	}
	if withArchive && release.Hash(value.Archive) != value.ArchiveSHA256 {
		return PreparedRelease{}, ErrCorrupt
	}
	foundChanges := false
	for _, file := range parsed.Files {
		if file.Path == "models/changes.md" {
			foundChanges = file.SHA256 == release.Hash([]byte(value.ChangesMarkdown)) && file.Size == int64(len(value.ChangesMarkdown))
		}
	}
	if !foundChanges {
		return PreparedRelease{}, ErrCorrupt
	}
	value.Manifest = parsed
	value.Archive = append([]byte{}, value.Archive...)
	return value, nil
}
func (r *Repository) GetRelease(ctx context.Context, version string) (PreparedRelease, error) {
	if !release.ValidVersion(version) {
		return PreparedRelease{}, ErrInvalid
	}
	return scanRelease(r.db.QueryRowContext(ctx, releaseMetadataSelect+",archive FROM credit_engine_admin.prepared_releases WHERE release_version=$1", version), true)
}
func (r *Repository) ListReleases(ctx context.Context, page Page) ([]PreparedRelease, error) {
	page, err := page.normalized()
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, releaseMetadataSelect+" FROM credit_engine_admin.prepared_releases ORDER BY created_at DESC,release_version LIMIT $1 OFFSET $2", page.Limit, page.Offset)
	if err != nil {
		return nil, databaseError("list releases", err)
	}
	defer rows.Close()
	result := []PreparedRelease{}
	for rows.Next() {
		value, err := scanRelease(rows, false)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, databaseError("list releases", rows.Err())
}

func (r *Repository) PrepareRelease(ctx context.Context, request PrepareReleaseRequest, author, reason string) (PreparedRelease, error) {
	if !validActor(author, reason) || !release.ValidVersion(request.Version) || !release.ValidVersion(request.BaseVersion) || len(request.PublishedVersions)+len(request.Drafts) == 0 {
		return PreparedRelease{}, ErrInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return PreparedRelease{}, databaseError("prepare release", err)
	}
	defer tx.Rollback()
	if err := lockPreparation(ctx, tx); err != nil {
		return PreparedRelease{}, err
	}
	base, err := scanRelease(tx.QueryRowContext(ctx, releaseMetadataSelect+",archive FROM credit_engine_admin.prepared_releases WHERE release_version=$1", request.BaseVersion), true)
	if err != nil {
		return PreparedRelease{}, err
	}
	view, err := release.ReadArchive(base.Archive)
	if err != nil {
		return PreparedRelease{}, ErrCorrupt
	}
	archivedManifest, err := release.MarshalManifest(view.Manifest())
	if err != nil || release.Hash(archivedManifest) != base.ManifestSHA256 {
		return PreparedRelease{}, ErrCorrupt
	}
	markdown, ok := view.File("docs/02_inputs_and_scoring.md")
	if !ok {
		return PreparedRelease{}, ErrCorrupt
	}
	definitions := []model.ModelDefinition{}
	for _, version := range request.PublishedVersions {
		if !release.ValidVersion(version) {
			return PreparedRelease{}, ErrInvalid
		}
		value, err := scanPublished(tx.QueryRowContext(ctx, publishedSelect+" WHERE model_version=$1", version))
		if err != nil {
			return PreparedRelease{}, err
		}
		definitions = append(definitions, value.Definition)
	}
	refs := append([]DraftReference{}, request.Drafts...)
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	seen := map[string]bool{}
	for _, ref := range refs {
		if !uuidPattern.MatchString(ref.ID) || ref.Revision < 1 || seen[strings.ToLower(ref.ID)] {
			return PreparedRelease{}, ErrInvalid
		}
		seen[strings.ToLower(ref.ID)] = true
		value, err := lockedDraft(ctx, tx, ref.ID)
		if err != nil {
			return PreparedRelease{}, err
		}
		if value.Revision != ref.Revision {
			return PreparedRelease{}, &ConflictError{CurrentRevision: value.Revision}
		}
		definitions = append(definitions, value.Definition)
	}
	bundle, err := release.Prepare(definitions, release.Options{Version: request.Version, Previous: &base.Manifest, Markdown: markdown, Notes: request.Notes})
	if err != nil {
		if errors.Is(err, release.ErrImmutable) {
			return PreparedRelease{}, ErrImmutable
		}
		return PreparedRelease{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	archive, err := bundle.Archive()
	if err != nil {
		return PreparedRelease{}, err
	}
	manifest := bundle.Manifest()
	var changes []byte
	for _, artifact := range bundle.Artifacts() {
		if artifact.Path == "models/changes.md" {
			changes = artifact.Data
		}
	}
	if _, err := savePrepared(ctx, tx, manifest, archive, changes, definitions, refs, author, reason); err != nil {
		return PreparedRelease{}, err
	}
	value, err := scanRelease(tx.QueryRowContext(ctx, releaseMetadataSelect+",archive FROM credit_engine_admin.prepared_releases WHERE release_version=$1", request.Version), true)
	if err != nil {
		return PreparedRelease{}, err
	}
	if err := tx.Commit(); err != nil {
		return PreparedRelease{}, databaseError("prepare release", err)
	}
	return value, nil
}

func lockPreparation(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", preparationLockID)
	return databaseError("lock release preparation", err)
}

func savePrepared(ctx context.Context, tx *sql.Tx, manifest release.Manifest, archive, changes []byte, definitions []model.ModelDefinition, refs []DraftReference, author, reason string) (bool, error) {
	data, err := release.MarshalManifest(manifest)
	if err != nil {
		return false, ErrInvalid
	}
	var existingVersion, existingManifest, existingArchive string
	err = tx.QueryRowContext(ctx, "SELECT release_version,manifest_sha256,archive_sha256 FROM credit_engine_admin.prepared_releases WHERE lower(release_version)=lower($1)", manifest.Version).Scan(&existingVersion, &existingManifest, &existingArchive)
	if err == nil {
		if existingVersion != manifest.Version || existingManifest != release.Hash(data) || existingArchive != release.Hash(archive) {
			return false, ErrImmutable
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, databaseError("check release version", err)
	}
	for _, definition := range definitions {
		if err := insertPublished(ctx, tx, definition, author, reason); err != nil {
			return false, err
		}
	}
	var base, baseHash any
	if manifest.PreviousVersion != "" {
		base, baseHash = manifest.PreviousVersion, manifest.PreviousSHA256
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO credit_engine_admin.prepared_releases(release_version,manifest_json,manifest_sha256,archive,archive_sha256,changes_markdown,base_release_version,base_manifest_sha256,notes,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", manifest.Version, string(data), release.Hash(data), archive, release.Hash(archive), string(changes), base, baseHash, manifest.Notes, author)
	if err != nil {
		return false, databaseError("save prepared release", err)
	}
	details, _ := json.Marshal(struct {
		ManifestSHA256 string              `json:"manifest_sha256"`
		ArchiveSHA256  string              `json:"archive_sha256"`
		Models         []release.ModelInfo `json:"models"`
		Drafts         []DraftReference    `json:"drafts"`
	}{release.Hash(data), release.Hash(archive), manifest.Models, refs})
	if err := insertHistory(ctx, tx, "RELEASE", manifest.Version, "RELEASE_PREPARED", nil, author, reason, details); err != nil {
		return false, err
	}
	return true, nil
}

func (r *Repository) SeedBaseline(ctx context.Context) error {
	archive := modelassets.InitialReleaseArchive()
	if release.Hash(archive) != modelassets.InitialReleaseSHA256 {
		return ErrCorrupt
	}
	view, err := release.ReadArchive(archive)
	if err != nil {
		return ErrCorrupt
	}
	definitions, err := view.Definitions()
	if err != nil {
		return ErrCorrupt
	}
	changes, ok := view.File("models/changes.md")
	if !ok {
		return ErrCorrupt
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return databaseError("seed baseline", err)
	}
	defer tx.Rollback()
	if err := lockPreparation(ctx, tx); err != nil {
		return err
	}
	if _, err := savePrepared(ctx, tx, view.Manifest(), archive, changes, definitions, nil, "system", "Import approved v2 baseline"); err != nil {
		return err
	}
	return databaseError("seed baseline", tx.Commit())
}

// EqualPreparedContent intentionally ignores timestamps/actor on an identical
// retry; the immutable model, manifest and archive retain their original bytes.
func EqualPreparedContent(left, right PreparedRelease) bool {
	return left.ManifestSHA256 == right.ManifestSHA256 && left.ArchiveSHA256 == right.ArchiveSHA256 && bytes.Equal(left.Archive, right.Archive)
}
