package model

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fragment(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "models", "examples", name+"-format-fragment.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFormatFragmentsRoundTrip(t *testing.T) {
	for _, name := range []string{"ip", "legal", "supplier"} {
		t.Run(name, func(t *testing.T) {
			original, err := Decode(bytes.NewReader(fragment(t, name)))
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := Decode(bytes.NewReader(encoded))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(original, decoded) {
				t.Fatal("format roundtrip lost definition data")
			}
			again, err := Marshal(decoded)
			if err != nil || !bytes.Equal(encoded, again) {
				t.Fatal("serialization is not deterministic")
			}
		})
	}
}

func TestFormatRetainsNullableAndConditionalRepresentations(t *testing.T) {
	ip, err := Decode(bytes.NewReader(fragment(t, "ip")))
	if err != nil {
		t.Fatal(err)
	}
	if ip.Parameters[2].RequiredWhen == nil || ip.Parameters[2].RequiredWhen.Parameter != "business_location" {
		t.Fatal("conditional requiredness lost")
	}
	if len(ip.Criteria[1].Answer.Cases) != 1 || !strings.Contains(ip.Criteria[1].Answer.Cases[0].Template, "${location_stability_months}") {
		t.Fatal("conditional named answer lost")
	}
	if ip.Criteria[2].MaxPoints != 0 || ip.Criteria[2].UnknownAction != Zero || ip.Criteria[2].Answer.UnknownText != "OTHER_OR_UNKNOWN" {
		t.Fatal("optional zero-point control lost")
	}
	special := ip.Criteria[3].SpecialCases[0]
	if special.Result == nil || special.Result.Mode != NullResult || special.Result.Value != nil || special.ReviewReason.Code != "SPECIAL_IP_E07" || special.When.Value.Number == nil || numberFloat(*special.When.Value.Number) != 0 {
		t.Fatal("zero denominator/null/manual case lost")
	}
	if ip.Criteria[3].Cases[1].Points != 3.75 {
		t.Fatal("fractional score lost")
	}
	supplier, err := Decode(bytes.NewReader(fragment(t, "supplier")))
	if err != nil {
		t.Fatal(err)
	}
	falseLiteral := supplier.Criteria[0].Cases[1].When.Value
	if falseLiteral.Boolean == nil || *falseLiteral.Boolean || falseLiteral.Number != nil || falseLiteral.String != nil {
		t.Fatal("false literal became unknown or changed type")
	}
	if supplier.Parameters[0].Enabled || supplier.Parameters[1].Type != Boolean || supplier.Parameters[0].GoName != "AnnualContractValueRUB" {
		t.Fatal("supplier field metadata lost")
	}
}

func TestDefinitionDecodeRejectsAmbiguousDocuments(t *testing.T) {
	base := string(fragment(t, "ip"))
	cases := []struct{ name, data, code, path string }{
		{"empty", "", "INVALID_JSON", "/"},
		{"null", "null", "INVALID_JSON", "/"},
		{"array", "[]", "INVALID_JSON", "/"},
		{"broken", "{", "INVALID_JSON", "/"},
		{"trailing", base + " {}", "INVALID_JSON", "/"},
		{"missing_root", "{}", "MISSING_FIELD", "/format_version"},
		{"unknown_root", "{\"unexpected\":true," + base[1:], "", ""},
		{"unknown_nested", strings.Replace(base, `"source": {`, `"source": {"unexpected":true,`, 1), "", ""},
		{"duplicate_root", strings.Replace(base, `"format_version": 1`, `"format_version": 1, "format_version": 1`, 1), "DUPLICATE_KEY", "/format_version"},
		{"duplicate_nested", strings.Replace(base, `"id": "owner_age"`, `"id": "owner_age", "id": "owner_age"`, 1), "DUPLICATE_KEY", "/parameters/0/id"},
		{"case_alias_root", strings.Replace(base, `"format_version": 1`, `"format_version": 1, "FORMAT_VERSION": 2`, 1), "UNKNOWN_FIELD", "/FORMAT_VERSION"},
		{"case_alias_nested", strings.Replace(base, `"enabled": true`, `"enabled": true, "Enabled": false`, 1), "UNKNOWN_FIELD", "/parameters/0/Enabled"},
		{"missing_boolean", strings.Replace(base, `"enabled": true,`, "", 1), "MISSING_FIELD", "/parameters/0/enabled"},
		{"null_boolean", strings.Replace(base, `"enabled": true`, `"enabled": null`, 1), "NULL_FIELD", "/parameters/0/enabled"},
		{"wrong_boolean", strings.Replace(base, `"enabled": true`, `"enabled": "true"`, 1), "", ""},
		{"null_array", strings.Replace(base, `"parameters": [`, `"parameters": null, "ignored": [`, 1), "NULL_FIELD", "/parameters"},
		{"null_source", strings.Replace(base, `"source": {`, `"source": null, "ignored": {`, 1), "NULL_FIELD", "/parameters/0/source"},
		{"version", strings.Replace(base, `"format_version": 1`, `"format_version": 2`, 1), "UNSUPPORTED_FORMAT", "/format_version"},
		{"large", `{"description":"` + strings.Repeat("x", MaxDefinitionBytes) + `"}`, "DOCUMENT_TOO_LARGE", "/"},
		{"deep", `{"unknown":` + strings.Repeat("[", MaxDefinitionDepth+1) + "0" + strings.Repeat("]", MaxDefinitionDepth+1) + "}", "DOCUMENT_TOO_DEEP", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(tc.data))
			if err == nil {
				t.Fatal("invalid document accepted")
			}
			if tc.code != "" {
				var fe *FormatError
				if !errors.As(err, &fe) || fe.Code != tc.code || tc.path != "" && fe.Path != tc.path {
					t.Fatalf("got %v, want %s at %s", err, tc.code, tc.path)
				}
			}
		})
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestDefinitionIOFailuresAndNonfiniteValues(t *testing.T) {
	if _, err := Decode(nil); err == nil {
		t.Fatal("nil reader accepted")
	}
	if _, err := Decode(failingReader{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("reader failure lost: %v", err)
	}
	d, err := Decode(bytes.NewReader(fragment(t, "ip")))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		d.Criteria[0].Cases[0].Points = n
		if _, err := Marshal(d); err == nil {
			t.Fatal("nonfinite score encoded")
		}
	}
}

func TestDecodeAllowsInvalidDraftWeightsWithoutChangingThem(t *testing.T) {
	// A fragment is structurally meaningful, but has only 50+15 maximum points.
	// Model release validation belongs to Validate, not to the format codec.
	d, err := Decode(bytes.NewReader(fragment(t, "ip")))
	if err != nil {
		t.Fatal(err)
	}
	if d.Criteria[0].MaxPoints != 30 || d.Criteria[1].MaxPoints != 20 || d.Criteria[3].MaxPoints != 15 {
		t.Fatal("codec changed weights")
	}
	if _, err := Marshal(d); err != nil {
		t.Fatal("invalid draft cannot be saved", err)
	}
}

func TestMarshalNeverEmitsAnUnreadableDefinition(t *testing.T) {
	for _, name := range []string{"missing_array", "unsupported_format", "too_large", "too_deep"} {
		t.Run(name, func(t *testing.T) {
			d, err := Decode(bytes.NewReader(fragment(t, "ip")))
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "missing_array":
				d.Parameters = nil
			case "unsupported_format":
				d.FormatVersion = 2
			case "too_large":
				d.Title = strings.Repeat("x", MaxDefinitionBytes)
			case "too_deep":
				condition := Condition{Op: Always}
				for i := 0; i < MaxDefinitionDepth; i++ {
					condition = Condition{Op: Not, Conditions: []Condition{condition}}
				}
				d.Stops[0].When = condition
			}
			if _, err := Marshal(d); err == nil {
				t.Fatal("unreadable definition emitted")
			}
		})
	}
}
