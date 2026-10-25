package release

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

const MaxArchiveBytes = 64 << 20
const MaxReleaseArtifacts = 2048

// ArchiveView is a checked historical record. It cannot be applied as generated
// code: publishing always creates a new Bundle from model definitions. This
// permits old transport documents to remain readable after templates evolve.
type ArchiveView struct {
	manifest Manifest
	files    map[string][]byte
	checksum string
}

func (v *ArchiveView) Manifest() Manifest {
	copy := v.manifest
	copy.Models = append([]ModelInfo{}, copy.Models...)
	copy.Files = append([]FileInfo{}, copy.Files...)
	return copy
}
func (v *ArchiveView) SHA256() string { return v.checksum }
func (v *ArchiveView) File(name string) ([]byte, bool) {
	data, ok := v.files[name]
	return append([]byte{}, data...), ok
}
func (v *ArchiveView) Definitions() ([]model.ModelDefinition, error) {
	result := []model.ModelDefinition{}
	for _, info := range v.manifest.Models {
		data := v.files["models/"+info.Version+".json"]
		definition, err := model.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		result = append(result, definition)
	}
	return result, nil
}

func ReadArchive(data []byte) (*ArchiveView, error) {
	if len(data) == 0 || len(data) > MaxArchiveBytes {
		return nil, fmt.Errorf("release: archive size exceeds supported limits")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("release: invalid ZIP")
	}
	if len(reader.File) == 0 || len(reader.File) > MaxReleaseArtifacts {
		return nil, fmt.Errorf("release: invalid archive member count")
	}
	files := map[string][]byte{}
	names := map[string]bool{}
	var total uint64
	for _, file := range reader.File {
		key := strings.ToLower(file.Name)
		if !AllowedArtifactPath(file.Name) || names[key] || file.Method != zip.Store || !file.Mode().IsRegular() {
			return nil, fmt.Errorf("release: invalid archive member %s", file.Name)
		}
		names[key] = true
		total += file.UncompressedSize64
		if total > MaxArchiveBytes || file.UncompressedSize64 > MaxArchiveBytes {
			return nil, fmt.Errorf("release: archive content exceeds supported limits")
		}
		stream, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("release: cannot read archive member")
		}
		content, readErr := io.ReadAll(io.LimitReader(stream, int64(file.UncompressedSize64)+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil || uint64(len(content)) != file.UncompressedSize64 {
			return nil, fmt.Errorf("release: invalid archive member content")
		}
		files[file.Name] = content
	}
	manifest, err := DecodeManifest(bytes.NewReader(files["models/release.json"]))
	if err != nil {
		return nil, err
	}
	canonical, err := MarshalManifest(manifest)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, files["models/release.json"]) || !bytes.Equal(canonical, files[ManifestPath(manifest.Version)]) {
		return nil, fmt.Errorf("release: mismatched archive manifests")
	}
	expected := map[string]bool{"models/release.json": true, ManifestPath(manifest.Version): true, "models/SHA256SUMS": true}
	for _, entry := range manifest.Files {
		content, exists := files[entry.Path]
		if !exists || int64(len(content)) != entry.Size || Hash(content) != entry.SHA256 {
			return nil, fmt.Errorf("release: archive checksum mismatch for %s", entry.Path)
		}
		expected[entry.Path] = true
	}
	if len(expected) != len(files) {
		return nil, fmt.Errorf("release: archive contains undeclared members")
	}
	keys := []string{}
	for name := range files {
		if !expected[name] {
			return nil, fmt.Errorf("release: undeclared archive member")
		}
		if name != "models/SHA256SUMS" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	var sums bytes.Buffer
	for _, name := range keys {
		fmt.Fprintf(&sums, "%s  %s\n", Hash(files[name]), name)
	}
	if !bytes.Equal(sums.Bytes(), files["models/SHA256SUMS"]) {
		return nil, fmt.Errorf("release: archive checksum index mismatch")
	}
	for _, info := range manifest.Models {
		definition, err := model.Decode(bytes.NewReader(files["models/"+info.Version+".json"]))
		if err != nil {
			return nil, err
		}
		canonical, err := model.Marshal(definition)
		if err != nil {
			return nil, err
		}
		schema, err := model.FactsSchemaSHA256(definition.Parameters)
		if err != nil {
			return nil, err
		}
		if definition.Version != info.Version || definition.ID != info.ID || definition.BorrowerType != info.BorrowerType || Hash(canonical) != info.DefinitionSHA256 || schema != info.FactsSchemaSHA256 || definition.FactsSchemaVersion != info.FactsSchemaVersion || info.UsesFacts != (schema != "") {
			return nil, fmt.Errorf("release: model metadata does not match archived definition")
		}
	}
	return &ArchiveView{manifest: manifest, files: files, checksum: Hash(data)}, nil
}
