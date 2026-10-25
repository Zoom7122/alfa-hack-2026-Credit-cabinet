package modelcli_test

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelcli"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "credit-engine")
	put(t, root, "go.mod", []byte("module "+release.ModulePath+"\n\ngo 1.26.8\n"))
	put(t, root, "docs/02_inputs_and_scoring.md", []byte("# Manual\nFormula stays.\n"))
	put(t, root, "README.md", []byte("public manual"))
	for _, version := range []string{"ip-v2", "legal-v2"} {
		data, err := os.ReadFile("../../models/" + version + ".json")
		if err != nil {
			t.Fatal(err)
		}
		put(t, root, "models/"+version+".json", data)
	}
	return root
}
func put(t *testing.T, root, name string, data []byte) {
	t.Helper()
	file := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func hashes(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	if err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		m[filepath.ToSlash(relative)] = release.Hash(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return m
}
func run(t *testing.T, root string, want int, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command := append([]string{args[0], "-root", root}, args[1:]...)
	rc := modelcli.Run(command, &stdout, &stderr)
	if rc != want {
		t.Fatalf("%v: rc=%d want=%d stdout=%s stderr=%s", args, rc, want, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func TestCLIValidateCheckGenerateArchiveCycle(t *testing.T) {
	root := fixture(t)
	before := hashes(t, root)
	if output := run(t, root, 0, "validate"); !strings.Contains(output, `"valid": true`) {
		t.Fatal(output)
	}
	if output := run(t, root, 1, "check", "-release", "r1"); !strings.Contains(output, "MISSING") {
		t.Fatal(output)
	}
	if !reflect.DeepEqual(before, hashes(t, root)) {
		t.Fatal("readonly commands wrote files")
	}
	run(t, root, 0, "generate", "-release", "r1", "-notes", "Initial tested bundle")
	generated := hashes(t, root)
	if output := run(t, root, 0, "check"); !strings.Contains(output, `"current": true`) {
		t.Fatal(output)
	}
	run(t, root, 0, "generate")
	if !reflect.DeepEqual(generated, hashes(t, root)) {
		t.Fatal("repeat generation changed result")
	}
	output := run(t, root, 0, "archive")
	var archive struct {
		Path   string `json:"path"`
		SHA256 string `json:"archive_sha256"`
	}
	if err := json.Unmarshal([]byte(output), &archive); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(archive.Path)))
	if err != nil || release.Hash(data) != archive.SHA256 {
		t.Fatal(err)
	}
	archived := hashes(t, root)
	run(t, root, 0, "archive")
	run(t, root, 0, "check")
	if !reflect.DeepEqual(archived, hashes(t, root)) {
		t.Fatal("repeat archive/check changed files")
	}
	put(t, root, "internal/modelbindings/catalog_generated.go", []byte("edited"))
	edited := hashes(t, root)
	if output := run(t, root, 1, "check"); !strings.Contains(output, "CHANGED") {
		t.Fatal(output)
	}
	if !reflect.DeepEqual(edited, hashes(t, root)) {
		t.Fatal("check repaired files")
	}
	run(t, root, 0, "generate")
	run(t, root, 0, "check")
	if !reflect.DeepEqual(archived, hashes(t, root)) {
		t.Fatal("generation did not restore exact bundle")
	}
	before = hashes(t, root)
	run(t, root, 1, "generate", "-notes", "different contents")
	if !reflect.DeepEqual(before, hashes(t, root)) {
		t.Fatal("conflicting release modified files")
	}
}

func TestCLIExplicitCatalogAndInvalidModelBeforeWrite(t *testing.T) {
	root := fixture(t)
	data, err := os.ReadFile(filepath.Join(root, "models/ip-v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	d.Version = "ip-cli-v3"
	data, err = model.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "models/candidate.json", data)
	run(t, root, 0, "generate", "-release", "r1", "-model", "models/ip-v2.json", "-model", "models/legal-v2.json", "-model", "models/candidate.json")
	run(t, root, 0, "validate")
	run(t, root, 0, "check")
	d.Criteria[0].MaxPoints--
	data, err = model.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "models/invalid.json", data)
	before := hashes(t, root)
	for _, command := range []string{"validate", "generate", "check", "archive"} {
		run(t, root, 1, command, "-release", "r2", "-model", "models/invalid.json")
		if !reflect.DeepEqual(before, hashes(t, root)) {
			t.Fatal(command, "wrote before validation")
		}
	}
	for _, path := range []string{"../escape.json", "/absolute.json", "C:/outside.json"} {
		run(t, root, 1, "generate", "-release", "r2", "-model", path)
	}
	if !reflect.DeepEqual(before, hashes(t, root)) {
		t.Fatal("invalid input path wrote files")
	}
}

func TestCLIUsageAndFirstReleaseRequirement(t *testing.T) {
	for _, test := range []struct {
		args []string
		code int
	}{{nil, 2}, {[]string{"help"}, 0}, {[]string{"--help"}, 0}, {[]string{"unknown"}, 2}, {[]string{"validate", "-bad"}, 2}, {[]string{"validate", "unexpected"}, 2}, {[]string{"validate", "-h"}, 0}} {
		var out, err bytes.Buffer
		if code := modelcli.Run(test.args, &out, &err); code != test.code {
			t.Fatal(test.args, code, test.code)
		}
	}
	root := fixture(t)
	before := hashes(t, root)
	run(t, root, 2, "generate")
	run(t, root, 1, "generate", "-release", "../escape")
	if !reflect.DeepEqual(before, hashes(t, root)) {
		t.Fatal("invalid release modified files")
	}
}
