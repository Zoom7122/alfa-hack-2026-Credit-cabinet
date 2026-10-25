package storage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	// ErrUnsafeKey — ключ не простой относительный путь внутри хранилища: абсолютный путь,
	// "..", обратная косая черта или путь через symlink.
	ErrUnsafeKey = errors.New("storage key is not a plain path inside the documents dir")
	// ErrNotFound — файла по ключу нет.
	ErrNotFound = errors.New("file is not found")
	// ErrNotRegular — по ключу не обычный файл (папка, устройство, FIFO).
	ErrNotRegular = errors.New("not a regular file")
)

// Reader читает файлы документов только внутри папки хранилища — для worker'ов, которым
// volume смонтирован на чтение. Ключ берётся из documents.storage_key и всё равно проверяется:
// путь не выходит за корень (os.Root не пускает наружу ни через "..", ни через symlink), а
// symlink'и не читаются вовсе, даже внутри хранилища, — API их не создаёт.
type Reader struct {
	root        *os.Root
	maxFileSize int64
}

// NewReader открывает папку хранилища на чтение. maxFileSize — лимит одного файла в байтах.
func NewReader(dir string, maxFileSize int64) (*Reader, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("documents dir: %w", err)
	}
	return &Reader{root: root, maxFileSize: maxFileSize}, nil
}

// Close закрывает папку хранилища.
func (r *Reader) Close() error {
	return r.root.Close()
}

// Read читает файл по ключу целиком. ErrUnsafeKey, ErrNotFound, ErrNotRegular, ErrTooLarge —
// файл прочитать нельзя и повтор не поможет; остальные ошибки — сбой чтения, его можно повторить.
func (r *Reader) Read(key string) ([]byte, error) {
	name, err := localName(key)
	if err != nil {
		return nil, err
	}
	// каждый компонент пути — настоящая папка, последний — обычный файл; symlink'и не разыменовываем
	parts := strings.Split(name, string(filepath.Separator))
	var info fs.FileInfo
	for i := range parts {
		if info, err = r.root.Lstat(filepath.Join(parts[:i+1]...)); err != nil {
			return nil, classify(err)
		}
		switch last := i == len(parts)-1; {
		case info.Mode()&fs.ModeSymlink != 0:
			return nil, ErrUnsafeKey
		case !last && !info.IsDir():
			return nil, ErrNotFound
		case last && !info.Mode().IsRegular():
			return nil, ErrNotRegular
		}
	}

	f, err := r.root.Open(name)
	if err != nil {
		return nil, classify(err)
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", key, err)
	}
	if !os.SameFile(info, opened) { // подменили между проверкой и открытием
		return nil, ErrUnsafeKey
	}
	if opened.Size() > r.maxFileSize {
		return nil, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, r.maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	if int64(len(data)) > r.maxFileSize {
		return nil, ErrTooLarge
	}
	return data, nil
}

// localName — ключ вида {application_id}/{document_id}.{ext} как путь ОС. Ключ должен быть
// уже чистым относительным путём: никаких "..", ".", лишних "/", обратной косой черты и NUL.
func localName(key string) (string, error) {
	if key == "" || key == "." || strings.ContainsAny(key, "\\\x00") || path.Clean(key) != key {
		return "", ErrUnsafeKey
	}
	name := filepath.FromSlash(key)
	if !filepath.IsLocal(name) {
		return "", ErrUnsafeKey
	}
	return name, nil
}

// classify сводит ошибку os.Root к ошибкам Reader: файла нет или путь выходит за корень
// (гонка с подменой папки на symlink — os.Root наружу всё равно не пустит).
func classify(err error) error {
	var pe *fs.PathError
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	case errors.As(err, &pe) && strings.Contains(pe.Err.Error(), "escapes"):
		return ErrUnsafeKey
	}
	return err
}
