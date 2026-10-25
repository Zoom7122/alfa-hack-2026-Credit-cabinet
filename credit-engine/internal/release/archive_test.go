package release_test

import (
	"archive/zip"
	"bytes"
	"io"
	"reflect"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/models"
)

func TestHistoricalArchiveReaderIntegrityAndOwnedData(t *testing.T) {
	data := modelassets.InitialReleaseArchive()
	if release.Hash(data) != modelassets.InitialReleaseSHA256 {
		t.Fatal("baseline bytes changed")
	}
	view, err := release.ReadArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	if view.Manifest().Version != "2.0.0" || view.SHA256() != modelassets.InitialReleaseSHA256 {
		t.Fatal(view.Manifest())
	}
	definitions, err := view.Definitions()
	if err != nil || len(definitions) != 2 {
		t.Fatal(definitions, err)
	}
	if _, ok := view.File("../outside"); ok {
		t.Fatal("undeclared path returned")
	}
	file, _ := view.File("models/ip-v2.json")
	copy, _ := view.File("models/ip-v2.json")
	copy[0] ^= 1
	if again, _ := view.File("models/ip-v2.json"); !bytes.Equal(file, again) {
		t.Fatal("caller can mutate archive")
	}
	definitions[0].Criteria[0].MaxPoints--
	again, err := view.Definitions()
	if err != nil || reflect.DeepEqual(definitions, again) {
		t.Fatal("decoded definitions alias archive data")
	}
	for _, test := range []struct {
		name string
		edit func(string, []byte) (string, []byte, uint16)
	}{
		{"changed_payload", func(name string, data []byte) (string, []byte, uint16) {
			if name == "contracts/openapi.json" {
				data = append(data, ' ')
			}
			return name, data, zip.Store
		}},
		{"traversal", func(name string, data []byte) (string, []byte, uint16) {
			if name == "models/ip-v2.json" {
				name = "../outside"
			}
			return name, data, zip.Store
		}},
		{"wrong_method", func(name string, data []byte) (string, []byte, uint16) { return name, data, zip.Deflate }},
		{"undeclared", func(name string, data []byte) (string, []byte, uint16) {
			if name == "models/ip-v2.json" {
				name = "models/unknown.json"
			}
			return name, data, zip.Store
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := release.ReadArchive(rewriteZip(t, data, test.edit)); err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
	if _, err := release.ReadArchive(nil); err == nil {
		t.Fatal("empty archive accepted")
	}
	if _, err := release.ReadArchive([]byte("not zip")); err == nil {
		t.Fatal("invalid zip accepted")
	}
}
func rewriteZip(t *testing.T, data []byte, edit func(string, []byte) (string, []byte, uint16)) []byte {
	t.Helper()
	input, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, file := range input.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		name, content, method := edit(file.Name, content)
		header := &zip.FileHeader{Name: name, Method: method}
		header.SetMode(0644)
		out, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
