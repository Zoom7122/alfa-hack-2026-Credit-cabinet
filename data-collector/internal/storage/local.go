// Package storage — файлы документов на диске (в docker — volume, общий с worker'ом проверки
// подписи и будущим OCR).
package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// stagingDir — временная папка внутри хранилища: оттуда файл переносится rename на том же диске.
const stagingDir = ".staging"

// ErrTooLarge — файл больше лимита.
var ErrTooLarge = errors.New("file is too large")

// Staged — файл, принятый во временную папку и ещё не привязанный к документу.
type Staged struct {
	Name   string // имя, с которым файл пришёл
	Path   string // где лежит во временной папке
	Size   int64
	SHA256 string
}

// Local хранит файлы в папке root: {root}/{key}, где key — путь вида {application_id}/{document_id}.{ext}.
type Local struct {
	root        string
	maxFileSize int64
}

// NewLocal создаёт хранилище и его временную папку. maxFileSize — лимит одного файла в байтах.
func NewLocal(root string, maxFileSize int64) (*Local, error) {
	if err := os.MkdirAll(filepath.Join(root, stagingDir), 0o750); err != nil {
		return nil, fmt.Errorf("documents dir: %w", err)
	}
	return &Local{root: root, maxFileSize: maxFileSize}, nil
}

// Stage пишет поток во временную папку, по дороге считая размер и sha256.
// Больше лимита — ErrTooLarge, принятая часть удаляется.
func (s *Local) Stage(name string, r io.Reader) (Staged, error) {
	f, err := os.CreateTemp(filepath.Join(s.root, stagingDir), "upload-*")
	if err != nil {
		return Staged{}, fmt.Errorf("create staging file: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, s.maxFileSize+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > s.maxFileSize {
		err = ErrTooLarge
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o640) // читает и worker подписи (тот же uid в образе)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		if errors.Is(err, ErrTooLarge) {
			return Staged{}, err
		}
		return Staged{}, fmt.Errorf("write staging file: %w", err)
	}
	return Staged{Name: name, Path: f.Name(), Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// Commit переносит принятый файл на постоянное место key.
func (s *Local) Commit(st Staged, key string) error {
	dst := s.path(key)
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return fmt.Errorf("create document dir: %w", err)
	}
	if err := os.Rename(st.Path, dst); err != nil {
		return fmt.Errorf("move %s: %w", st.Name, err)
	}
	return nil
}

// Discard удаляет принятые файлы из временной папки. Уже перенесённые не трогает.
func (s *Local) Discard(staged ...Staged) {
	for _, st := range staged {
		_ = os.Remove(st.Path)
	}
}

// Remove удаляет файлы с постоянного места (откат, если заявку не удалось сохранить)
// и опустевшие папки заявок.
func (s *Local) Remove(keys ...string) {
	for _, key := range keys {
		p := s.path(key)
		_ = os.Remove(p)
		_ = os.Remove(filepath.Dir(p)) // удалится, только если пустая
	}
}

// Open открывает файл документа на чтение. Ключ не выходит за папку хранилища (ErrUnsafeKey),
// файла нет — ErrNotFound.
func (s *Local) Open(key string) (*os.File, error) {
	name, err := localName(key)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenInRoot(s.root, name)
	if err != nil {
		return nil, classify(err)
	}
	return f, nil
}

func (s *Local) path(key string) string {
	return filepath.Join(s.root, filepath.FromSlash(key))
}
