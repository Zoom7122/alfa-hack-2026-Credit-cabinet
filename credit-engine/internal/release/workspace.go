package release

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
)

const ModulePath = "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine"

type Workspace struct{ root *os.Root }
type Difference struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

func OpenWorkspace(directory string) (*Workspace, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(filepath.Base(absolute), "credit-engine") {
		return nil, fmt.Errorf("release: root must be the credit-engine module directory")
	}
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return nil, fmt.Errorf("release: symbolic workspace paths are not allowed")
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(filepath.Clean(resolved), filepath.Clean(absolute)) {
		return nil, fmt.Errorf("release: symbolic workspace paths are not allowed")
	}
	root, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	w := &Workspace{root: root}
	data, err := w.Read("go.mod")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(data)), "module "+ModulePath+"\n") && !strings.HasPrefix(strings.TrimSpace(string(data)), "module "+ModulePath+"\r\n") {
		root.Close()
		return nil, fmt.Errorf("release: wrong Go module")
	}
	return w, nil
}
func (w *Workspace) Close() error { return w.root.Close() }

// os.Root enforces containment even during path resolution. Explicit Lstat
// checks additionally reject symlinks/reparse aliases inside the module.
func (w *Workspace) safe(name string) error {
	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:") {
		return fmt.Errorf("release: invalid relative path %q", name)
	}
	parts := strings.Split(name, "/")
	for i := range parts {
		current := strings.Join(parts[:i+1], "/")
		info, err := w.root.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("release: symbolic or special path rejected: %s", current)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return fmt.Errorf("release: parent is not a directory: %s", current)
		}
	}
	return nil
}
func (w *Workspace) Read(name string) ([]byte, error) {
	if err := w.safe(name); err != nil {
		return nil, err
	}
	return w.root.ReadFile(name)
}
func (w *Workspace) ReadManifest(name string) (*Manifest, error) {
	data, err := w.Read(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	manifest, err := DecodeManifest(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return &manifest, nil
}
func (w *Workspace) Current() (*Manifest, error) { return w.ReadManifest("models/release.json") }

func (w *Workspace) ModelPaths() ([]string, error) {
	current, err := w.Current()
	if err != nil {
		return nil, err
	}
	paths := []string{}
	if current != nil {
		for _, info := range current.Models {
			paths = append(paths, "models/"+info.Version+".json")
		}
		return paths, nil
	}
	if err := w.safe("models"); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(w.root.FS(), "models")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") && entry.Name() != "release.json" {
			paths = append(paths, "models/"+entry.Name())
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (w *Workspace) History() ([]Manifest, error) {
	if err := w.safe("models/releases"); err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(w.root.FS(), "models/releases")
	if errors.Is(err, fs.ErrNotExist) {
		return []Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	history := []Manifest{}
	for _, entry := range entries {
		if !ValidVersion(entry.Name()) {
			continue
		}
		if err := w.safe("models/releases/" + entry.Name()); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		manifest, err := w.ReadManifest(ManifestPath(entry.Name()))
		if err != nil {
			return nil, err
		}
		if manifest != nil {
			if manifest.Version != entry.Name() {
				return nil, fmt.Errorf("release: history version/path mismatch")
			}
			history = append(history, *manifest)
		}
	}
	return history, nil
}

// Base pins regeneration to the original base recorded by this immutable
// release; using the current release itself would change its change report.
func (w *Workspace) Base(version string) (base, existing *Manifest, err error) {
	if !ValidVersion(version) {
		return nil, nil, fmt.Errorf("release: invalid version")
	}
	existing, err = w.ReadManifest(ManifestPath(version))
	if err != nil {
		return nil, nil, err
	}
	if existing == nil {
		history, historyErr := w.History()
		if historyErr != nil {
			return nil, nil, historyErr
		}
		for _, old := range history {
			if strings.EqualFold(old.Version, version) && old.Version != version {
				return nil, nil, fmt.Errorf("release: release versions cannot differ only by case")
			}
		}
		base, err = w.Current()
		return base, nil, err
	}
	if existing.Version != version {
		return nil, nil, fmt.Errorf("release: release version/path differs in case or content")
	}
	if existing.PreviousVersion == "" {
		return nil, existing, nil
	}
	base, err = w.ReadManifest(ManifestPath(existing.PreviousVersion))
	if err != nil {
		return nil, nil, err
	}
	if base == nil {
		return nil, nil, fmt.Errorf("release: pinned base manifest is missing")
	}
	data, err := MarshalManifest(*base)
	if err != nil {
		return nil, nil, err
	}
	if Hash(data) != existing.PreviousSHA256 {
		return nil, nil, fmt.Errorf("release: pinned base checksum mismatch")
	}
	return base, existing, nil
}

func (w *Workspace) generatedFiles() ([]string, error) {
	result := []string{}
	for _, directory := range []string{"contracts", "internal/modelbindings"} {
		if err := w.safe(directory); err != nil {
			return nil, err
		}
		entries, err := fs.ReadDir(w.root.FS(), directory)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := directory + "/" + entry.Name()
			if generatedPathPattern.MatchString(name) || name == "internal/modelbindings/catalog_generated.go" {
				if err := w.safe(name); err != nil {
					return nil, err
				}
				result = append(result, name)
			}
		}
	}
	sort.Strings(result)
	return result, nil
}
func (w *Workspace) Check(bundle *Bundle) ([]Difference, error) {
	differences := []Difference{}
	expected := map[string]bool{}
	for _, artifact := range bundle.artifacts {
		expected[artifact.Path] = true
		data, err := w.Read(artifact.Path)
		if errors.Is(err, fs.ErrNotExist) {
			differences = append(differences, Difference{Path: artifact.Path, Kind: "MISSING"})
			continue
		}
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(data, artifact.Data) {
			differences = append(differences, Difference{Path: artifact.Path, Kind: "CHANGED"})
		}
	}
	files, err := w.generatedFiles()
	if err != nil {
		return nil, err
	}
	for _, name := range files {
		if !expected[name] {
			differences = append(differences, Difference{Path: name, Kind: "OBSOLETE"})
		}
	}
	sort.Slice(differences, func(i, j int) bool { return differences[i].Path < differences[j].Path })
	return differences, nil
}

func (w *Workspace) checkHistory(bundle *Bundle) error {
	archived, archiveErr := w.Read(ArchivePath(bundle.manifest.Version))
	if archiveErr == nil {
		expected, err := bundle.Archive()
		if err != nil {
			return err
		}
		if !bytes.Equal(archived, expected) {
			return fmt.Errorf("release: existing archive for %s is immutable", bundle.manifest.Version)
		}
	} else if !errors.Is(archiveErr, fs.ErrNotExist) {
		return archiveErr
	}
	history, err := w.History()
	if err != nil {
		return err
	}
	for _, old := range history {
		if strings.EqualFold(old.Version, bundle.manifest.Version) && old.Version != bundle.manifest.Version {
			return fmt.Errorf("release: release versions cannot differ only by case")
		}
		if old.Version == bundle.manifest.Version {
			data, err := MarshalManifest(old)
			if err != nil {
				return err
			}
			if Hash(data) != bundle.digest {
				return fmt.Errorf("release: version %s already has different immutable content", old.Version)
			}
		}
		for _, previous := range old.Models {
			for _, current := range bundle.manifest.Models {
				if strings.EqualFold(previous.Version, current.Version) && previous.Version != current.Version {
					return fmt.Errorf("release: published model versions cannot differ only by case")
				}
				if previous.Version == current.Version && previous.DefinitionSHA256 != current.DefinitionSHA256 {
					return fmt.Errorf("release: published model %s is immutable", current.Version)
				}
				if previous.FactsSchemaVersion != "" && previous.FactsSchemaVersion == current.FactsSchemaVersion && previous.FactsSchemaSHA256 != current.FactsSchemaSHA256 {
					return fmt.Errorf("release: published facts schema %s is immutable", current.FactsSchemaVersion)
				}
			}
		}
	}
	return nil
}

func (w *Workspace) Apply(bundle *Bundle) error {
	if err := w.checkHistory(bundle); err != nil {
		return err
	}
	current, err := w.Current()
	if err != nil {
		return err
	}
	if current != nil && current.Version != bundle.manifest.Version {
		data, err := MarshalManifest(*current)
		if err != nil {
			return err
		}
		if current.Version != bundle.manifest.PreviousVersion || Hash(data) != bundle.manifest.PreviousSHA256 {
			return fmt.Errorf("release: workspace changed since the selected base release")
		}
	}
	if current == nil && bundle.manifest.PreviousVersion != "" {
		return fmt.Errorf("release: base is not installed in this workspace")
	}
	expected := map[string]bool{}
	for _, artifact := range bundle.artifacts {
		expected[artifact.Path] = true
	}
	owned := map[string]FileInfo{}
	if current != nil {
		for _, file := range current.Files {
			owned[file.Path] = file
		}
	}
	files, err := w.generatedFiles()
	if err != nil {
		return err
	}
	obsolete := []string{}
	for _, name := range files {
		if expected[name] {
			continue
		}
		file, exists := owned[name]
		if !exists {
			return fmt.Errorf("release: refusing to remove unowned generated file %s", name)
		}
		data, err := w.Read(name)
		if err != nil {
			return err
		}
		if Hash(data) != file.SHA256 {
			return fmt.Errorf("release: obsolete generated file was edited: %s", name)
		}
		obsolete = append(obsolete, name)
	}
	return w.install(bundle.artifacts, obsolete)
}

func (w *Workspace) WriteArchive(bundle *Bundle) (string, string, error) {
	if err := w.checkHistory(bundle); err != nil {
		return "", "", err
	}
	data, err := bundle.Archive()
	if err != nil {
		return "", "", err
	}
	name := ArchivePath(bundle.manifest.Version)
	existing, err := w.Read(name)
	if err == nil {
		if !bytes.Equal(existing, data) {
			return "", "", fmt.Errorf("release: archive %s is immutable", name)
		}
		if manifest, err := w.ReadManifest(ManifestPath(bundle.manifest.Version)); err != nil {
			return "", "", err
		} else if manifest == nil {
			raw, err := MarshalManifest(bundle.manifest)
			if err != nil {
				return "", "", err
			}
			if err := w.install([]modelgen.Artifact{{Path: ManifestPath(bundle.manifest.Version), Data: raw}}, nil); err != nil {
				return "", "", err
			}
		}
		return name, Hash(data), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", "", err
	}
	manifest, err := MarshalManifest(bundle.manifest)
	if err != nil {
		return "", "", err
	}
	if err := w.install([]modelgen.Artifact{{Path: ManifestPath(bundle.manifest.Version), Data: manifest}, {Path: name, Data: data}}, nil); err != nil {
		return "", "", err
	}
	return name, Hash(data), nil
}

// Stage before mutation, rename each file atomically and commit the current
// manifest last. A failed operation returns an error; check detects any partial
// application. No previous source definitions or historical releases are deleted.
func (w *Workspace) install(artifacts []modelgen.Artifact, obsolete []string) error {
	ordered := append([]modelgen.Artifact{}, artifacts...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Path == "models/release.json" {
			return false
		}
		if ordered[j].Path == "models/release.json" {
			return true
		}
		return ordered[i].Path < ordered[j].Path
	})
	for _, artifact := range ordered {
		if !AllowedArtifactPath(artifact.Path) && !(strings.HasSuffix(artifact.Path, ".zip") && artifact.Path == ArchivePath(strings.TrimSuffix(path.Base(artifact.Path), ".zip")) && ValidVersion(strings.TrimSuffix(path.Base(artifact.Path), ".zip"))) {
			return fmt.Errorf("release: output path is not allowed: %s", artifact.Path)
		}
		if err := w.safe(artifact.Path); err != nil {
			return err
		}
	}
	if err := w.safe(".work/modelgen"); err != nil {
		return err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	stage := fmt.Sprintf(".work/modelgen/%x", random)
	if err := w.root.MkdirAll(".work/modelgen", 0700); err != nil {
		return err
	}
	if err := w.root.Mkdir(stage, 0700); err != nil {
		return err
	}
	staged := []string{}
	defer func() {
		for _, name := range staged {
			_ = w.root.Remove(name)
		}
		_ = w.root.Remove(stage)
	}()
	for i, artifact := range ordered {
		name := fmt.Sprintf("%s/%06d", stage, i)
		file, err := w.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		staged = append(staged, name)
		_, writeErr := file.Write(artifact.Data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	for i, artifact := range ordered {
		data, err := w.Read(artifact.Path)
		if err == nil && bytes.Equal(data, artifact.Data) {
			continue
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := w.safe(artifact.Path); err != nil {
			return err
		}
		if err := w.root.MkdirAll(path.Dir(artifact.Path), 0755); err != nil {
			return err
		}
		if err := w.root.Chmod(staged[i], 0644); err != nil {
			return err
		}
		if artifact.Path == "models/release.json" {
			for _, name := range obsolete {
				if err := w.safe(name); err != nil {
					return err
				}
				if err := w.root.Remove(name); err != nil {
					return err
				}
			}
		}
		if err := w.root.Rename(staged[i], artifact.Path); err != nil {
			return err
		}
	}
	return nil
}
