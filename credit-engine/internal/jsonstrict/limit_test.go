package jsonstrict_test

import (
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
)

func TestEditorLimitDoesNotExpandProtocolLimit(t *testing.T) {
	data := []byte(`{"description":"` + strings.Repeat("x", jsonstrict.MaxBytes) + `"}`)
	if jsonstrict.Check(data) == nil {
		t.Fatal("default protocol limit expanded")
	}
	if err := jsonstrict.CheckLimit(data, len(data)); err != nil {
		t.Fatal(err)
	}
	if jsonstrict.CheckLimit(data, len(data)-1) == nil || jsonstrict.CheckLimit([]byte(`{}`), 0) == nil {
		t.Fatal("configured bounds ignored")
	}
	if jsonstrict.CheckLimit([]byte(`{"x":{"value":0,"value":false}}`), 4<<20) == nil {
		t.Fatal("larger limit bypassed duplicate check")
	}
}
