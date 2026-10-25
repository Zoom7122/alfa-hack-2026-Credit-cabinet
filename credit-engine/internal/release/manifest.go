package release

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
)

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func DecodeManifest(reader io.Reader) (Manifest, error) {
	var manifest Manifest
	data, err := jsonstrict.Read(reader)
	if err != nil {
		return Manifest{}, err
	}
	if err := manifestKeys(data, "/", []string{"format_version", "generator_version", "version", "models", "files"}, []string{"previous_version", "previous_sha256", "notes"}); err != nil {
		return Manifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("release: invalid manifest: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Manifest{}, fmt.Errorf("release: expected one manifest document")
	}
	if err := ValidateManifest(manifest); err != nil {
		return Manifest{}, err
	}
	var object map[string]json.RawMessage
	_ = json.Unmarshal(data, &object)
	for _, collection := range []struct {
		name   string
		fields []string
	}{{"models", []string{"id", "version", "borrower_type", "definition_sha256", "facts_schema_version", "facts_schema_sha256", "uses_facts"}}, {"files", []string{"path", "sha256", "size"}}} {
		var entries []json.RawMessage
		if err := json.Unmarshal(object[collection.name], &entries); err != nil {
			return Manifest{}, err
		}
		for i, entry := range entries {
			if err := manifestKeys(entry, fmt.Sprintf("/%s/%d", collection.name, i), collection.fields, nil); err != nil {
				return Manifest{}, err
			}
		}
	}
	return manifest, nil
}

func manifestKeys(data []byte, location string, required, optional []string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	fields := append([]string{}, required...)
	for _, key := range optional {
		if _, exists := object[key]; exists {
			fields = append(fields, key)
		}
	}
	_, err := jsonstrict.Object(data, location, fields, nil)
	return err
}

func ValidateManifest(manifest Manifest) error {
	if manifest.FormatVersion != FormatVersion || manifest.GeneratorVersion != GeneratorVersion || !ValidVersion(manifest.Version) || len(manifest.Models) == 0 || len(manifest.Files) == 0 || !utf8.ValidString(manifest.Notes) || len(manifest.Notes) > 16<<10 || strings.ContainsRune(manifest.Notes, 0) {
		return fmt.Errorf("release: invalid manifest metadata")
	}
	if (manifest.PreviousVersion == "") != (manifest.PreviousSHA256 == "") || manifest.PreviousVersion != "" && (!ValidVersion(manifest.PreviousVersion) || manifest.PreviousVersion == manifest.Version || !hashPattern.MatchString(manifest.PreviousSHA256)) {
		return fmt.Errorf("release: invalid base release")
	}
	versions := map[string]ModelInfo{}
	for _, info := range manifest.Models {
		if !ValidVersion(info.Version) || info.ID == "" || !hashPattern.MatchString(info.DefinitionSHA256) || info.UsesFacts && (!hashPattern.MatchString(info.FactsSchemaSHA256) || !versionPattern.MatchString(info.FactsSchemaVersion)) || !info.UsesFacts && info.FactsSchemaSHA256 != "" || (info.BorrowerType != contracts.BorrowerTypeSoleProprietor && info.BorrowerType != contracts.BorrowerTypeLegalEntity) {
			return fmt.Errorf("release: invalid model metadata")
		}
		if _, exists := versions[strings.ToLower(info.Version)]; exists {
			return fmt.Errorf("release: duplicate model version")
		}
		versions[strings.ToLower(info.Version)] = info
	}
	files := map[string]FileInfo{}
	for _, file := range manifest.Files {
		if !AllowedArtifactPath(file.Path) || strings.EqualFold(file.Path, "models/release.json") || strings.EqualFold(file.Path, "models/SHA256SUMS") || strings.HasPrefix(file.Path, "models/releases/") || file.Size < 0 || !hashPattern.MatchString(file.SHA256) {
			return fmt.Errorf("release: invalid manifest file %s", file.Path)
		}
		if _, exists := files[strings.ToLower(file.Path)]; exists {
			return fmt.Errorf("release: duplicate manifest file")
		}
		files[strings.ToLower(file.Path)] = file
	}
	for _, info := range versions {
		if file, exists := files[strings.ToLower("models/"+info.Version+".json")]; !exists || file.Path != "models/"+info.Version+".json" || file.SHA256 != info.DefinitionSHA256 {
			return fmt.Errorf("release: missing or mismatched model definition %s", info.Version)
		}
	}
	for _, name := range []string{"models/changes.md", "internal/modelbindings/catalog_generated.go", "contracts/openapi.json", "contracts/scoring_facts.openapi.json", "docs/02_inputs_and_scoring.md"} {
		if _, exists := files[strings.ToLower(name)]; !exists {
			return fmt.Errorf("release: required manifest artifact missing: %s", name)
		}
	}
	return nil
}
