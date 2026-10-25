// Package release prepares reproducible immutable model release bundles.
// Preparation and archiving do not access the filesystem or external services.
package release

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
)

const FormatVersion = 1
const GeneratorVersion = "1"

var ErrImmutable = errors.New("release content is immutable")

type FileInfo struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type ModelInfo struct {
	ID                 string                 `json:"id"`
	Version            string                 `json:"version"`
	BorrowerType       contracts.BorrowerType `json:"borrower_type"`
	DefinitionSHA256   string                 `json:"definition_sha256"`
	FactsSchemaVersion string                 `json:"facts_schema_version"`
	FactsSchemaSHA256  string                 `json:"facts_schema_sha256"`
	UsesFacts          bool                   `json:"uses_facts"`
}
type Manifest struct {
	FormatVersion    int         `json:"format_version"`
	GeneratorVersion string      `json:"generator_version"`
	Version          string      `json:"version"`
	PreviousVersion  string      `json:"previous_version,omitempty"`
	PreviousSHA256   string      `json:"previous_sha256,omitempty"`
	Notes            string      `json:"notes,omitempty"`
	Models           []ModelInfo `json:"models"`
	Files            []FileInfo  `json:"files"`
}
type Options struct {
	Version  string
	Markdown []byte
	Notes    string
	Previous *Manifest
}
type Bundle struct {
	manifest  Manifest
	artifacts []modelgen.Artifact
	digest    string
}

func Hash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func MarshalManifest(manifest Manifest) ([]byte, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
func (b *Bundle) Manifest() Manifest {
	result := b.manifest
	result.Models = append([]ModelInfo{}, result.Models...)
	result.Files = append([]FileInfo{}, result.Files...)
	return result
}
func (b *Bundle) SHA256() string { return b.digest }
func (b *Bundle) Artifacts() []modelgen.Artifact {
	result := make([]modelgen.Artifact, len(b.artifacts))
	for i, artifact := range b.artifacts {
		result[i] = modelgen.Artifact{Path: artifact.Path, Data: append([]byte{}, artifact.Data...)}
	}
	return result
}

func Prepare(definitions []model.ModelDefinition, options Options) (*Bundle, error) {
	if !ValidVersion(options.Version) || !utf8.ValidString(options.Notes) || len(options.Notes) > 16<<10 || strings.ContainsRune(options.Notes, 0) {
		return nil, fmt.Errorf("release: invalid version or notes")
	}
	if options.Previous != nil {
		if err := ValidateManifest(*options.Previous); err != nil {
			return nil, err
		}
		if options.Previous.Version == options.Version {
			return nil, fmt.Errorf("release: the base must be a previous release, not this version")
		}
	}
	code, err := modelgen.GenerateModels(definitions)
	if err != nil {
		return nil, err
	}
	docs, err := modelgen.GenerateDocuments(definitions, modelgen.DocumentOptions{ReleaseVersion: options.Version, InputsMarkdown: options.Markdown})
	if err != nil {
		return nil, err
	}
	artifacts := append(code.Artifacts, docs.Artifacts...)
	manifest := Manifest{FormatVersion: FormatVersion, GeneratorVersion: GeneratorVersion, Version: options.Version, Notes: options.Notes, Models: []ModelInfo{}, Files: []FileInfo{}}
	for _, definition := range definitions {
		if !ValidVersion(definition.Version) || strings.EqualFold(definition.Version, "release") {
			return nil, fmt.Errorf("release: model version is not portable: %s", definition.Version)
		}
		data, err := model.Marshal(definition)
		if err != nil {
			return nil, err
		}
		schema, err := model.FactsSchemaSHA256(definition.Parameters)
		if err != nil {
			return nil, err
		}
		manifest.Models = append(manifest.Models, ModelInfo{ID: definition.ID, Version: definition.Version, BorrowerType: definition.BorrowerType, DefinitionSHA256: Hash(data), FactsSchemaVersion: definition.FactsSchemaVersion, FactsSchemaSHA256: schema, UsesFacts: schema != ""})
		artifacts = append(artifacts, modelgen.Artifact{Path: "models/" + definition.Version + ".json", Data: data})
	}
	sort.Slice(manifest.Models, func(i, j int) bool { return manifest.Models[i].Version < manifest.Models[j].Version })
	if previous := options.Previous; previous != nil {
		manifest.PreviousVersion = previous.Version
		data, err := MarshalManifest(*previous)
		if err != nil {
			return nil, err
		}
		manifest.PreviousSHA256 = Hash(data)
		for _, old := range previous.Models {
			for _, current := range manifest.Models {
				if strings.EqualFold(old.Version, current.Version) && old.Version != current.Version {
					return nil, fmt.Errorf("%w: model versions cannot differ only by case", ErrImmutable)
				}
				if old.Version == current.Version && old.DefinitionSHA256 != current.DefinitionSHA256 {
					return nil, fmt.Errorf("%w: model version %s requires a new version", ErrImmutable, old.Version)
				}
			}
		}
	}
	artifacts = append(artifacts, modelgen.Artifact{Path: "models/changes.md", Data: changesMarkdown(artifacts, manifest, options.Previous)})
	paths := map[string]bool{}
	for _, artifact := range artifacts {
		key := strings.ToLower(artifact.Path)
		if !AllowedArtifactPath(artifact.Path) || paths[key] || key == "models/release.json" || key == "models/sha256sums" {
			return nil, fmt.Errorf("release: invalid or colliding artifact path %s", artifact.Path)
		}
		paths[key] = true
		manifest.Files = append(manifest.Files, FileInfo{Path: artifact.Path, SHA256: Hash(artifact.Data), Size: int64(len(artifact.Data))})
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	if err := ValidateManifest(manifest); err != nil {
		return nil, err
	}
	data, err := MarshalManifest(manifest)
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, modelgen.Artifact{Path: "models/release.json", Data: data}, modelgen.Artifact{Path: ManifestPath(manifest.Version), Data: append([]byte{}, data...)})
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	var sums bytes.Buffer
	for _, artifact := range artifacts {
		fmt.Fprintf(&sums, "%s  %s\n", Hash(artifact.Data), artifact.Path)
	}
	artifacts = append(artifacts, modelgen.Artifact{Path: "models/SHA256SUMS", Data: sums.Bytes()})
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	var contentSize int64
	for _, artifact := range artifacts {
		contentSize += int64(len(artifact.Data))
	}
	if len(artifacts) > MaxReleaseArtifacts || contentSize > MaxArchiveBytes {
		return nil, fmt.Errorf("release: generated bundle exceeds supported limits")
	}
	return &Bundle{manifest: manifest, artifacts: artifacts, digest: Hash(data)}, nil
}

func (b *Bundle) Archive() ([]byte, error) {
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	for _, artifact := range b.artifacts {
		// Store avoids toolchain-dependent compression. DOS time is fixed at
		// 1980-01-01; no host paths, mtimes, clocks or random IDs are embedded.
		header := &zip.FileHeader{Name: artifact.Path, Method: zip.Store, ModifiedDate: 33}
		header.SetMode(0644)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(artifact.Data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if data.Len() > MaxArchiveBytes {
		return nil, fmt.Errorf("release: archive exceeds supported limits")
	}
	return data.Bytes(), nil
}

func changesMarkdown(artifacts []modelgen.Artifact, current Manifest, previous *Manifest) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# Изменения релиза %s\n\n", current.Version)
	if previous == nil {
		fmt.Fprintln(&out, "Первый комплект; исходные модели и артефакты перечислены ниже.")
	} else {
		fmt.Fprintf(&out, "Базовый релиз: %s.\n", previous.Version)
	}
	if current.Notes != "" {
		text := html.EscapeString(current.Notes)
		text = strings.NewReplacer("`", "&#96;", "[", "&#91;", "]", "&#93;", "|", "&#124;", "*", "&#42;", "_", "&#95;").Replace(text)
		fmt.Fprintf(&out, "\nПримечание:\n\n%s\n", text)
	}
	oldFiles := map[string]FileInfo{}
	if previous != nil {
		for _, file := range previous.Files {
			if file.Path != "models/changes.md" {
				oldFiles[file.Path] = file
			}
		}
	}
	changes := []string{}
	for _, artifact := range artifacts {
		hash := Hash(artifact.Data)
		old, exists := oldFiles[artifact.Path]
		delete(oldFiles, artifact.Path)
		if !exists {
			changes = append(changes, "- ADDED "+artifact.Path+" SHA256="+hash)
		} else if hash != old.SHA256 {
			changes = append(changes, "- CHANGED "+artifact.Path+" "+old.SHA256+" → "+hash)
		}
	}
	for name, old := range oldFiles {
		changes = append(changes, "- REMOVED "+name+" SHA256="+old.SHA256)
	}
	sort.Strings(changes)
	fmt.Fprint(&out, "\n## Состав изменений\n\n")
	if len(changes) == 0 {
		fmt.Fprintln(&out, "Содержимое моделей и генерируемых файлов не изменилось.")
	} else {
		fmt.Fprintln(&out, strings.Join(changes, "\n"))
	}
	fmt.Fprint(&out, "\nПодготовка комплекта не подтверждает сборку, тестирование или развёртывание.\n")
	return out.Bytes()
}
