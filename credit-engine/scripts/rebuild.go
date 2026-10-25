//go:build ignore

// Run explicitly with go run scripts/rebuild.go. All output stays in .work.
package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
)

func main() {
	output := flag.String("output", "", "new .work/.../credit-engine checkout")
	archive := flag.String("archive", "", "accepted release ZIP")
	flag.Parse()
	root, err := os.Getwd()
	must(err)
	target, err := filepath.Abs(*output)
	must(err)
	rel, err := filepath.Rel(root, target)
	must(err)
	if !strings.HasPrefix(rel, ".work"+string(filepath.Separator)) || filepath.Base(target) != "credit-engine" {
		panic("output must be a new checkout inside this module's .work")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		panic("output already exists")
	}
	data, err := os.ReadFile(*archive)
	must(err)
	_, err = release.ReadArchive(data)
	must(err)
	must(filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".work" || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unexpected source link")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if d.IsDir() {
			return os.MkdirAll(destination, 0755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, content, 0644)
	}))
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	must(err)
	for _, file := range reader.File {
		if !release.AllowedArtifactPath(file.Name) {
			panic("disallowed archive path")
		}
		source, err := file.Open()
		must(err)
		content, err := io.ReadAll(source)
		source.Close()
		must(err)
		destination := filepath.Join(target, filepath.FromSlash(file.Name))
		must(os.MkdirAll(filepath.Dir(destination), 0755))
		must(os.WriteFile(destination, content, 0644))
	}
	fmt.Println(target)
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
