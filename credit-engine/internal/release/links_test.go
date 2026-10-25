package release_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

func directoryLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		// A Windows directory junction needs no symlink creation privilege.
		// Both ends are isolated test paths under the module's scoped GOTMPDIR.
		command := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), "/c", "mklink", "/J", link, target)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("cannot create isolated junction: %v %s", err, output)
		}
	}
	t.Cleanup(func() {
		if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	})
}

func TestWorkspaceRejectsDirectoryLinksBeforeAnyWrite(t *testing.T) {
	root, w := workspaceFixture(t)
	bundle := workspaceBundle(t, w, "r1", definitions(t))
	outside := filepath.Join(filepath.Dir(root), "outside-target")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel.txt")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0644); err != nil {
		t.Fatal(err)
	}
	directoryLink(t, outside, filepath.Join(root, "internal/modelbindings"))
	if _, err := w.Check(bundle); err == nil {
		t.Fatal("check followed link")
	}
	if err := w.Apply(bundle); err == nil {
		t.Fatal("generator followed link")
	}
	if _, err := os.Stat(filepath.Join(root, "models/release.json")); !os.IsNotExist(err) {
		t.Fatal("preflight failure changed module", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "unchanged" {
		t.Fatal(string(data), err)
	}
}

func TestWorkspaceRejectsLinkedRoot(t *testing.T) {
	root, _ := workspaceFixture(t)
	alias := filepath.Join(filepath.Dir(root), "alias", "credit-engine")
	directoryLink(t, root, alias)
	if opened, err := release.OpenWorkspace(alias); err == nil {
		opened.Close()
		t.Fatal("linked root accepted")
	}
}
