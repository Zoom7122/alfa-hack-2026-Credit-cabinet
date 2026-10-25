package adminstore

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/release"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/models"
)

func TestDraftDocumentAllowsInvalidWeightsAndPreservesHashes(t *testing.T) {
	view, err := release.ReadArchive(modelassets.InitialReleaseArchive())
	if err != nil {
		t.Fatal(err)
	}
	definitions, err := view.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	d := definitions[0]
	d.Criteria[0].MaxPoints--
	data, err := draftDocument(d)
	if err != nil {
		t.Fatal("invalid business weights must remain saveable", err)
	}
	decoded, err := storedDefinition(string(data), release.Hash(data))
	if err != nil || !reflect.DeepEqual(decoded, d) {
		t.Fatal(err)
	}
	if _, err := storedDefinition(string(data), strings.Repeat("0", 64)); err != ErrCorrupt {
		t.Fatal("corruption accepted", err)
	}
	d.Parameters = nil
	if _, err := draftDocument(d); err == nil {
		t.Fatal("invalid transport shape saved")
	}
	d = definitions[0]
	d.BorrowerType = "UNSUPPORTED"
	if _, err := draftDocument(d); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := NewRepository(nil); err == nil {
		t.Fatal("nil repository accepted")
	}
}

func TestModelChangePathsPreserveTypesAndEscapePointers(t *testing.T) {
	before := []byte(`{"a/b":{"flag":false,"amount":0,"items":[1,2]},"same":null}`)
	after := []byte(`{"a/b":{"flag":true,"amount":0.0,"items":[1,3,4]},"same":null,"new~key":false}`)
	var got struct {
		Before string   `json:"before_sha256"`
		After  string   `json:"after_sha256"`
		Paths  []string `json:"changed_paths"`
	}
	if err := json.Unmarshal(changeDetails(before, after, "before", "after"), &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"/a~1b/amount", "/a~1b/flag", "/a~1b/items/1", "/a~1b/items/2", "/new~0key"}
	if !reflect.DeepEqual(got.Paths, want) || got.Before != "before" || got.After != "after" {
		t.Fatal(got)
	}
	if err := json.Unmarshal(changeDetails(after, after, "same", "same"), &got); err != nil || len(got.Paths) != 0 {
		t.Fatal(got, err)
	}
	if err := json.Unmarshal(changeDetails(nil, after, "", "after"), &got); err != nil || !reflect.DeepEqual(got.Paths, []string{"/"}) {
		t.Fatal(got, err)
	}
}

func TestDraftIdentityAndPaging(t *testing.T) {
	seen := map[string]bool{}
	for range 32 {
		id, err := newDraftID()
		if err != nil || !uuidPattern.MatchString(id) || id[14] != '4' || seen[id] {
			t.Fatal(id, err)
		}
		seen[id] = true
	}
	if page, err := (Page{}).normalized(); err != nil || page.Limit != 50 {
		t.Fatal(page, err)
	}
	for _, page := range []Page{{Limit: -1}, {Limit: 201}, {Offset: -1}} {
		if _, err := page.normalized(); err != ErrInvalid {
			t.Fatal(page, err)
		}
	}
	for _, pair := range [][2]string{{"", "reason"}, {"admin", ""}, {" ", "reason"}} {
		if validActor(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
}
