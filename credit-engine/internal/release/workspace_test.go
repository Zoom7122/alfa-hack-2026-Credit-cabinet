package release_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

func workspaceFixture(t *testing.T) (string, *release.Workspace) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "credit-engine")
	put(t, root, "go.mod", []byte("module "+release.ModulePath+"\n\ngo 1.26.8\n"))
	put(t, root, "README.md", []byte("handwritten public guide"))
	put(t, root, "docs/02_inputs_and_scoring.md", []byte("# Manual\r\nFormula x/y stays.\r\n"))
	for _, d := range definitions(t) {
		data, err := model.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		put(t, root, "models/"+d.Version+".json", data)
	}
	w, err := release.OpenWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return root, w
}
func put(t *testing.T, root, name string, data []byte) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
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
		result[filepath.ToSlash(relative)] = release.Hash(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result
}
func workspaceBundle(t *testing.T, w *release.Workspace, version string, defs []model.ModelDefinition) *release.Bundle {
	t.Helper()
	base, existing, err := w.Base(version)
	if err != nil {
		t.Fatal(err)
	}
	md, err := w.Read("docs/02_inputs_and_scoring.md")
	if err != nil {
		t.Fatal(err)
	}
	notes := ""
	if existing != nil {
		notes = existing.Notes
	}
	return prepare(t, defs, release.Options{Version: version, Previous: base, Markdown: md, Notes: notes})
}

func TestWorkspaceGenerateCheckHistoryAndObsoleteSources(t *testing.T) {
	root, w := workspaceFixture(t)
	before := snapshot(t, root)
	b1 := workspaceBundle(t, w, "r1", definitions(t))
	diff, err := w.Check(b1)
	if err != nil || len(diff) == 0 {
		t.Fatal(diff, err)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("check wrote files")
	}
	if err := w.Apply(b1); err != nil {
		t.Fatal(err)
	}
	if diff, err := w.Check(b1); err != nil || len(diff) != 0 {
		t.Fatal(diff, err)
	}
	if got, err := w.Read("README.md"); err != nil || string(got) != "handwritten public guide" {
		t.Fatal(string(got), err)
	}
	after := snapshot(t, root)
	if err := w.Apply(workspaceBundle(t, w, "r1", definitions(t))); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, snapshot(t, root)) {
		t.Fatal("repeat generation changed content")
	}
	b2 := workspaceBundle(t, w, "r2", extendedDefinitions(t))
	if err := w.Apply(b2); err != nil {
		t.Fatal(err)
	}
	if diff, err := w.Check(b2); err != nil || len(diff) != 0 {
		t.Fatal(diff, err)
	}
	regenerated := workspaceBundle(t, w, "r2", extendedDefinitions(t))
	if regenerated.SHA256() != b2.SHA256() {
		t.Fatal("regeneration lost pinned base")
	}
	if paths, err := w.ModelPaths(); err != nil || len(paths) != 3 {
		t.Fatal(paths, err)
	}
	b3 := workspaceBundle(t, w, "r3", definitions(t))
	if err := w.Apply(b3); err != nil {
		t.Fatal(err)
	}
	for name := range artifactMap(b2) {
		if strings.HasSuffix(name, "_generated.go") && artifactMap(b3)[name] == nil {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); !os.IsNotExist(err) {
				t.Fatal("obsolete Go source survived", name, err)
			}
		}
	}
	if _, err := w.Read("models/ip-bundle-v3.json"); err != nil {
		t.Fatal("source definition was deleted", err)
	}
	if diff, err := w.Check(b3); err != nil || len(diff) != 0 {
		t.Fatal(diff, err)
	}
	if history, err := w.History(); err != nil || len(history) != 3 {
		t.Fatal(history, err)
	}
	archive, checksum, err := w.WriteArchive(b2)
	if err != nil || checksum == "" {
		t.Fatal(archive, checksum, err)
	}
	archived := snapshot(t, root)
	if _, again, err := w.WriteArchive(b2); err != nil || again != checksum || !reflect.DeepEqual(archived, snapshot(t, root)) {
		t.Fatal("archive repeat changed bytes", err)
	}
	current, err := w.Current()
	if err != nil || current.Version != "r3" {
		t.Fatal("archive changed the installed release", err)
	}
}

func TestWorkspaceBlocksEditedOrUnownedRemovalAndImmutableVersions(t *testing.T) {
	root, w := workspaceFixture(t)
	b1 := workspaceBundle(t, w, "r1", definitions(t))
	if err := w.Apply(b1); err != nil {
		t.Fatal(err)
	}
	b2 := workspaceBundle(t, w, "r2", extendedDefinitions(t))
	if err := w.Apply(b2); err != nil {
		t.Fatal(err)
	}
	b3 := workspaceBundle(t, w, "r3", definitions(t))
	var obsolete string
	for name := range artifactMap(b2) {
		if strings.HasSuffix(name, "_generated.go") && artifactMap(b3)[name] == nil {
			obsolete = name
			break
		}
	}
	if obsolete == "" {
		t.Fatal("fixture has no obsolete source")
	}
	put(t, root, obsolete, []byte("hand edited generated source"))
	before := snapshot(t, root)
	if err := w.Apply(b3); err == nil || !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("edited obsolete file was removed or failure wrote files", err)
	}
	put(t, root, obsolete, artifactMap(b2)[obsolete])
	if err := w.Apply(b3); err != nil {
		t.Fatal(err)
	}
	put(t, root, "contracts/facts_0000000000000000_generated.go", []byte("unowned"))
	before = snapshot(t, root)
	if diff, err := w.Check(b3); err != nil || len(diff) != 1 || diff[0].Kind != "OBSOLETE" {
		t.Fatal(diff, err)
	}
	if err := w.Apply(b3); err == nil || !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("unowned file was deleted", err)
	}
	if err := os.Remove(filepath.Join(root, "contracts/facts_0000000000000000_generated.go")); err != nil {
		t.Fatal(err)
	}
	base, _, err := w.Base("r3")
	if err != nil {
		t.Fatal(err)
	}
	md, err := w.Read("docs/02_inputs_and_scoring.md")
	if err != nil {
		t.Fatal(err)
	}
	changed := prepare(t, definitions(t), release.Options{Version: "r3", Previous: base, Markdown: md, Notes: "different release content"})
	before = snapshot(t, root)
	if err := w.Apply(changed); err == nil || !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("immutable release overwritten", err)
	}
	if _, _, err := w.WriteArchive(changed); err == nil || !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("immutable release archived with different data", err)
	}
}

func TestWorkspaceRejectsTraversalWrongModuleAndCaseAliases(t *testing.T) {
	root, w := workspaceFixture(t)
	for _, name := range []string{"../outside", "/absolute", "C:/outside", "models/../README.md", "models\\ip-v2.json"} {
		if _, err := w.Read(name); err == nil {
			t.Fatal("unsafe read accepted", name)
		}
	}
	if _, err := release.OpenWorkspace(filepath.Dir(root)); err == nil {
		t.Fatal("wrong root accepted")
	}
	put(t, root, "go.mod", []byte("module other/module\n"))
	if opened, err := release.OpenWorkspace(root); err == nil {
		opened.Close()
		t.Fatal("wrong module accepted")
	}
	put(t, root, "go.mod", []byte("module "+release.ModulePath+"\n\ngo 1.26.8\n"))
	b := workspaceBundle(t, w, "ReleaseOne", definitions(t))
	if err := w.Apply(b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Base("releaseone"); err == nil {
		t.Fatal("case-alias release accepted")
	}
}

func TestWorkspaceAtomicReplacementDoesNotModifyHardLinkTarget(t *testing.T) {
	root, w := workspaceFixture(t)
	b := workspaceBundle(t, w, "r1", definitions(t))
	name := "internal/modelbindings/catalog_generated.go"
	sentinel := filepath.Join(filepath.Dir(root), "outside-module-sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("outside target stays unchanged"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "internal/modelbindings"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(sentinel, filepath.Join(root, filepath.FromSlash(name))); err != nil {
		t.Fatal(err)
	}
	if err := w.Apply(b); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || !bytes.Equal(data, []byte("outside target stays unchanged")) {
		t.Fatal("hard-link target was modified", err)
	}
}

func TestWorkspaceProtectsArchiveWhenHistoryIndexIsMissing(t *testing.T) {
	root, w := workspaceFixture(t)
	bundle := workspaceBundle(t, w, "r1", definitions(t))
	if err := w.Apply(bundle); err != nil {
		t.Fatal(err)
	}
	name, _, err := w.WriteArchive(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(release.ManifestPath("r1")))); err != nil {
		t.Fatal(err)
	}
	md, err := w.Read("docs/02_inputs_and_scoring.md")
	if err != nil {
		t.Fatal(err)
	}
	changed := prepare(t, definitions(t), release.Options{Version: "r1", Markdown: md, Notes: "different content"})
	before := snapshot(t, root)
	if err := w.Apply(changed); err == nil || !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("orphaned immutable archive was ignored", err)
	}
	if _, _, err := w.WriteArchive(bundle); err != nil {
		t.Fatal("matching archive could not restore its index", err)
	}
	if _, err := w.Read(release.ManifestPath("r1")); err != nil {
		t.Fatal(err)
	}
	put(t, root, name, []byte("corrupted archive"))
	before = snapshot(t, root)
	if _, _, err := w.WriteArchive(bundle); err == nil || !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("existing archive was overwritten", err)
	}
}
