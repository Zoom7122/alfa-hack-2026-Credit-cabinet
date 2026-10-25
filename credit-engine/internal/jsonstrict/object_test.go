package jsonstrict_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
)

func TestStrictObjectsAndResources(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		valid     bool
	}{
		{"valid", `{"a":0,"b":false}`, true}, {"nullable", `{"a":null,"b":false}`, true},
		{"missing", `{"a":0}`, false}, {"extra", `{"a":0,"b":false,"c":1}`, false}, {"case", `{"A":0,"b":false}`, false}, {"case_alias", `{"a":0,"A":1,"b":false}`, false},
		{"null", `null`, false}, {"array", `[]`, false}, {"null_nonnullable", `{"a":0,"b":null}`, false},
		{"duplicate", `{"a":0,"b":false,"a":1}`, false}, {"escaped_duplicate", `{"a":0,"b":false,"\u0061":1}`, false},
		{"nested_duplicate", `{"a":{"x":0,"x":1},"b":false}`, false}, {"trailing", `{"a":0,"b":false} {}`, false},
		{"bad_json", `{"a":NaN,"b":false}`, false}, {"unterminated", `{"a":0,"b":false`, false}, {"invalid_utf8", "{\"a\":\"" + string([]byte{255}) + "\",\"b\":false}", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := jsonstrict.Object([]byte(test.raw), "/", []string{"a", "b"}, []string{"a"})
			if (err == nil) != test.valid {
				t.Fatal(err)
			}
		})
	}
	if err := jsonstrict.Check([]byte(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))); err == nil {
		t.Fatal("depth limit")
	}
	if _, err := jsonstrict.Read(strings.NewReader(strings.Repeat(" ", jsonstrict.MaxBytes+1))); err == nil {
		t.Fatal("size limit")
	}
	if _, err := jsonstrict.Read(nil); err == nil {
		t.Fatal("nil reader")
	}
	boom := errors.New("read failed")
	_, err := jsonstrict.Read(errorReader{boom})
	if !errors.Is(err, boom) {
		t.Fatal("reader failure lost", err)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

var _ io.Reader = errorReader{}

func TestNumbersKeepInt64AndNullableTypesDistinct(t *testing.T) {
	for _, test := range []struct {
		token string
		want  int64
		valid bool
	}{{"0", 0, true}, {"1.0", 1, true}, {"1e1", 10, true}, {"9007199254740993", 9007199254740993, true}, {"9223372036854775807", 9223372036854775807, true}, {"9223372036854775808", 0, false}, {"-9223372036854775809", 0, false}, {"0.1", 0, false}, {`"1"`, 0, false}, {"null", 0, false}, {"false", 0, false}, {"1e999999999", 0, false}} {
		got, err := jsonstrict.Int64([]byte(test.token), "/n", nil, nil)
		if (err == nil) != test.valid || test.valid && got != test.want {
			t.Fatal(test, got, err)
		}
	}
	for _, token := range []string{"1e309", "NaN", "Infinity", `"0"`, "null", "false"} {
		if _, err := jsonstrict.Float64([]byte(token), "/n", nil, nil); err == nil {
			t.Fatal("invalid float", token)
		}
	}
	for _, inclusive := range []bool{false, true} {
		bound := &jsonstrict.Bound{Value: "9007199254740993", Inclusive: inclusive}
		_, err := jsonstrict.Int64([]byte("9007199254740993"), "/n", bound, bound)
		if (err == nil) != inclusive {
			t.Fatal("exact inclusive bound", err)
		}
	}
	if value, err := jsonstrict.Boolean([]byte("false"), "/b"); err != nil || value {
		t.Fatal(value, err)
	}
	if _, err := jsonstrict.Boolean([]byte("null"), "/b"); err == nil {
		t.Fatal("null became false")
	}
	if _, err := jsonstrict.String([]byte("null"), "/s"); err == nil {
		t.Fatal("null became empty string")
	}
}
