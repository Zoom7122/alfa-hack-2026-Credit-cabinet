package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// newReaderRoot — хранилище с файлом app_1/doc_1.pdf и секретом рядом с ним, вне хранилища.
func newReaderRoot(t *testing.T) (root, outside string) {
	t.Helper()
	base := t.TempDir()
	root, outside = filepath.Join(base, "documents"), filepath.Join(base, "outside")
	for _, dir := range []string{filepath.Join(root, "app_1"), outside} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "app_1", "doc_1.pdf"), []byte("%PDF-1.4 balance"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o640); err != nil {
		t.Fatal(err)
	}
	return root, outside
}

func newTestReader(t *testing.T, root string, limit int64) *Reader {
	t.Helper()
	r, err := NewReader(root, limit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestReaderRead(t *testing.T) {
	root, _ := newReaderRoot(t)
	data, err := newTestReader(t, root, 1024).Read("app_1/doc_1.pdf")
	if err != nil || string(data) != "%PDF-1.4 balance" {
		t.Fatalf("read = %q, %v", data, err)
	}
}

// Ключ из БД не выводит за пределы хранилища: "..", абсолютный путь, symlink наружу и внутрь.
func TestReaderRejectsUnsafeKeys(t *testing.T) {
	root, outside := newReaderRoot(t)
	links := map[string]string{
		filepath.Join(root, "app_1", "escape.pdf"): filepath.Join(outside, "secret.txt"), // файл-symlink наружу
		filepath.Join(root, "app_2"):               outside,                              // папка-symlink наружу
		filepath.Join(root, "app_1", "inner.pdf"):  filepath.Join(root, "app_1", "doc_1.pdf"),
		filepath.Join(root, "app_3"):               "../outside", // относительный symlink наружу
	}
	for link, target := range links {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	r := newTestReader(t, root, 1024)

	for _, key := range []string{
		"../outside/secret.txt",
		"app_1/../../outside/secret.txt",
		"app_1/../app_1/doc_1.pdf", // не канонический путь
		"/etc/passwd",
		filepath.Join(outside, "secret.txt"),
		`app_1\doc_1.pdf`,
		"app_1//doc_1.pdf",
		"./app_1/doc_1.pdf",
		"app_1/doc_1.pdf\x00.sig",
		"",
		".",
		"app_1/escape.pdf",
		"app_2/secret.txt",
		"app_3/secret.txt",
		"app_1/inner.pdf",
	} {
		if data, err := r.Read(key); !errors.Is(err, ErrUnsafeKey) {
			t.Errorf("Read(%q) = %q, %v; want ErrUnsafeKey", key, data, err)
		}
	}
}

func TestReaderErrors(t *testing.T) {
	root, _ := newReaderRoot(t)
	r := newTestReader(t, root, 8)
	for key, want := range map[string]error{
		"app_1/missing.pdf":     ErrNotFound,
		"app_9/doc_1.pdf":       ErrNotFound,
		"app_1/doc_1.pdf/inner": ErrNotFound,
		"app_1":                 ErrNotRegular,
		"app_1/doc_1.pdf":       ErrTooLarge, // 16 байт при лимите 8
	} {
		if _, err := r.Read(key); !errors.Is(err, want) {
			t.Errorf("Read(%q): %v, want %v", key, err, want)
		}
	}
}
