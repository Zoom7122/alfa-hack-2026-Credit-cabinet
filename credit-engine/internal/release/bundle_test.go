package release_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

func definitions(t *testing.T) []model.ModelDefinition {
	t.Helper()
	result := []model.ModelDefinition{}
	for _, name := range []string{"ip-v2", "legal-v2"} {
		data, err := os.ReadFile("../../models/" + name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		d, err := model.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, d)
	}
	return result
}
func extendedDefinitions(t *testing.T) []model.ModelDefinition {
	t.Helper()
	result := definitions(t)
	d := definitions(t)[0]
	d.Version = "ip-bundle-v3"
	d.FactsSchemaVersion = "bundle-facts-v1"
	d.Parameters = append(d.Parameters, model.Parameter{ID: "release_flag", Enabled: true, Title: "Test flag", Source: model.Source{Kind: model.Supplier, Path: "release_flag"}, JSONName: "release_flag", Type: model.Boolean, Unit: model.Flag, Required: false})
	return append(result, d)
}
func prepare(t *testing.T, defs []model.ModelDefinition, options release.Options) *release.Bundle {
	t.Helper()
	b, err := release.Prepare(defs, options)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func artifactMap(b *release.Bundle) map[string][]byte {
	m := map[string][]byte{}
	for _, a := range b.Artifacts() {
		m[a.Path] = a.Data
	}
	return m
}

func TestBundleDeterminismChecksumsArchiveAndOwnership(t *testing.T) {
	defs := extendedDefinitions(t)
	options := release.Options{Version: "bundle-1", Markdown: []byte("# Manual\r\nFormula: x/y.\r\n"), Notes: "Initial checked model bundle"}
	b := prepare(t, defs, options)
	files := artifactMap(b)
	manifest := b.Manifest()
	if release.Hash(files["models/release.json"]) != b.SHA256() || !bytes.Equal(files[release.ManifestPath("bundle-1")], files["models/release.json"]) {
		t.Fatal("manifest identity drift")
	}
	for _, file := range manifest.Files {
		data, exists := files[file.Path]
		if !exists || release.Hash(data) != file.SHA256 || int64(len(data)) != file.Size {
			t.Fatal(file)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(files["models/SHA256SUMS"])), "\n") {
		parts := strings.Split(line, "  ")
		if len(parts) != 2 || release.Hash(files[parts[1]]) != parts[0] {
			t.Fatal(line)
		}
	}
	foundFacts := false
	for name := range files {
		if !release.AllowedArtifactPath(name) {
			t.Fatal(name)
		}
		if strings.HasPrefix(name, "contracts/facts_") {
			foundFacts = true
		}
	}
	if !foundFacts || files["models/ip-bundle-v3.json"] == nil || files["models/changes.md"] == nil {
		t.Fatal("release omitted source, DTO or changes")
	}
	options.Markdown = files["docs/02_inputs_and_scoring.md"]
	defs[0], defs[2] = defs[2], defs[0]
	second := prepare(t, defs, options)
	if b.SHA256() != second.SHA256() || !reflect.DeepEqual(b.Artifacts(), second.Artifacts()) {
		t.Fatal("release is not reproducible")
	}
	archive, err := b.Archive()
	if err != nil {
		t.Fatal(err)
	}
	again, err := second.Archive()
	if err != nil || !bytes.Equal(archive, again) {
		t.Fatal("archive bytes changed", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != len(files) {
		t.Fatal("missing archive member")
	}
	last := ""
	for _, file := range reader.File {
		if file.Name <= last || file.Mode().Perm() != 0644 || file.Modified.Year() != 1980 || file.Method != zip.Store {
			t.Fatal(file.FileHeader)
		}
		last = file.Name
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(stream)
		stream.Close()
		if err != nil || !bytes.Equal(data, files[file.Name]) {
			t.Fatal(file.Name, err)
		}
	}
	copy := b.Artifacts()
	copy[0].Data[0] ^= 1
	copy[0].Path = "../outside"
	manifest.Models[0].Version = "changed"
	manifest.Files[0].Path = "changed"
	if !reflect.DeepEqual(files, artifactMap(b)) || b.Manifest().Models[0].Version == "changed" {
		t.Fatal("caller can mutate prepared bundle")
	}
	decoded, err := release.DecodeManifest(bytes.NewReader(files["models/release.json"]))
	if err != nil || !reflect.DeepEqual(decoded, b.Manifest()) {
		t.Fatal(decoded, err)
	}
}

func TestBundleBaseDiffAndImmutableModelVersions(t *testing.T) {
	base := prepare(t, definitions(t), release.Options{Version: "r1", Markdown: []byte("manual\n")})
	previous := base.Manifest()
	next := prepare(t, extendedDefinitions(t), release.Options{Version: "r2", Markdown: artifactMap(base)["docs/02_inputs_and_scoring.md"], Previous: &previous})
	if next.Manifest().PreviousSHA256 != base.SHA256() || !strings.Contains(string(artifactMap(next)["models/changes.md"]), "ADDED models/ip-bundle-v3.json") {
		t.Fatal(next.Manifest())
	}
	changed := definitions(t)
	changed[0].Criteria[0].Cases[0].Points = 4
	if b, err := release.Prepare(changed, release.Options{Version: "r2", Previous: &previous}); err == nil || b != nil {
		t.Fatal("published model content changed under same version")
	}
	if _, err := release.Prepare(definitions(t), release.Options{Version: "r1", Previous: &previous}); err == nil {
		t.Fatal("self base accepted")
	}
	for _, version := range []string{"../escape", "C:drive", "NUL", "CON.v1", "release.", "", "x\nname"} {
		if _, err := release.Prepare(definitions(t), release.Options{Version: version}); err == nil {
			t.Fatal(version)
		}
	}
	for _, version := range []string{"release", "Release", "CON", "ip-v2."} {
		defs := definitions(t)
		defs[0].Version = version
		if _, err := release.Prepare(defs, release.Options{Version: "r2"}); err == nil {
			t.Fatal("unsafe model source name", version)
		}
	}
	defs := definitions(t)
	defs[0].Criteria[0].MaxPoints--
	if b, err := release.Prepare(defs, release.Options{Version: "r2"}); err == nil || b != nil {
		t.Fatal("invalid model released")
	}
}

func TestManifestRejectsMalformedUntrustedMetadata(t *testing.T) {
	b := prepare(t, definitions(t), release.Options{Version: "r1"})
	data := artifactMap(b)["models/release.json"]
	for _, input := range [][]byte{nil, []byte("null"), append(append([]byte{}, data...), []byte(" {}")...), bytes.Replace(data, []byte(`"version": "r1"`), []byte(`"version": "r1", "version": "r1"`), 1), bytes.Replace(data, []byte(`"version": "r1"`), []byte(`"Version": "r1"`), 1), bytes.Replace(data, []byte(`"format_version": 1`), []byte(`"format_version": null`), 1), append([]byte{0xff}, data...), []byte(strings.Repeat(" ", 1<<20+1))} {
		if _, err := release.DecodeManifest(bytes.NewReader(input)); err == nil {
			t.Fatal("malformed manifest accepted")
		}
	}
	if _, err := release.DecodeManifest(nil); err == nil {
		t.Fatal("nil reader accepted")
	}
	for _, edit := range []func(*release.Manifest){func(m *release.Manifest) { m.Files[0].Path = "../outside" }, func(m *release.Manifest) { m.Files = append(m.Files, m.Files[0]) }, func(m *release.Manifest) { m.Models = append(m.Models, m.Models[0]) }, func(m *release.Manifest) { m.Models[0].DefinitionSHA256 = strings.Repeat("0", 64) }, func(m *release.Manifest) { m.PreviousVersion = "r0" }, func(m *release.Manifest) { m.Files[0].Size = -1 }, func(m *release.Manifest) { m.Models[0].BorrowerType = "UNKNOWN" }} {
		m := b.Manifest()
		edit(&m)
		raw, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := release.DecodeManifest(bytes.NewReader(raw)); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}

func TestReleaseOutputPathAllowlist(t *testing.T) {
	for _, name := range []string{"../x", "/absolute", "C:/absolute", "models/../README.md", "README.md", "go.mod", ".git/config", "models/a/other.json", "models/releases/NUL/manifest.json", "models/releases/r1/other.json", "contracts/facts_0123_generated.go", "internal/modelbindings/model_0123456789abcdef_generated.go/child", "models/..json", "models/COn.json", "models/a\\b.json"} {
		if release.AllowedArtifactPath(name) {
			t.Fatal("forbidden output", name)
		}
	}
	for _, name := range []string{"models/ip-v2.json", "models/releases/r1/manifest.json", "contracts/facts_0123456789abcdef_generated.go", "internal/modelbindings/catalog_generated.go", "models/SHA256SUMS"} {
		if !release.AllowedArtifactPath(name) {
			t.Fatal("valid output rejected", name)
		}
	}
}
