package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageCommit(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocal(root, 1024)
	if err != nil {
		t.Fatal(err)
	}

	st, err := s.Stage("balance.pdf", strings.NewReader("%PDF-1.4 test"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("%PDF-1.4 test"))
	if st.Name != "balance.pdf" || st.Size != 13 || st.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("staged = %+v", st)
	}

	if err := s.Commit(st, "app_1/doc_1.pdf"); err != nil {
		t.Fatal(err)
	}
	f, err := s.Open("app_1/doc_1.pdf")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(f)
	_ = f.Close()
	if string(body) != "%PDF-1.4 test" {
		t.Fatalf("body = %q", body)
	}
	if _, err := os.Stat(st.Path); !os.IsNotExist(err) {
		t.Fatalf("staging file must be moved: %v", err)
	}

	// откат: файл и опустевшая папка заявки удаляются
	s.Remove("app_1/doc_1.pdf")
	if _, err := os.Stat(filepath.Join(root, "app_1")); !os.IsNotExist(err) {
		t.Fatalf("application dir must be removed: %v", err)
	}
}

func TestStageTooLarge(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocal(root, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stage("big.pdf", strings.NewReader("12345")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if st, err := s.Stage("ok.pdf", strings.NewReader("1234")); err != nil || st.Size != 4 {
		t.Fatalf("file of exactly the limit: %+v, %v", st, err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, stagingDir))
	if len(entries) != 1 {
		t.Fatalf("too large file must be removed from staging, left %d files", len(entries))
	}
}

func TestDiscard(t *testing.T) {
	s, err := NewLocal(t.TempDir(), 1024)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.Stage("a.pdf", strings.NewReader("a"))
	if err != nil {
		t.Fatal(err)
	}
	s.Discard(st)
	if _, err := os.Stat(st.Path); !os.IsNotExist(err) {
		t.Fatalf("staged file must be removed: %v", err)
	}
}
