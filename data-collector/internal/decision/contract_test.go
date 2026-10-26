package decision

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The sender copies a contract from another Go module. Detect fields silently
// discarded before idempotent replay, including optional financial form lines.
func TestDecisionRequestPreservesEngineFixtures(t *testing.T) {
	for _, name := range []string{"uc04_ip_approved.json", "uc04_le_approved.json"} {
		raw, err := os.ReadFile("../../../credit-engine/testdata/decisions/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var f struct {
			Request    json.RawMessage `json:"request"`
			Statements json.RawMessage `json:"service_b_response"`
		}
		if err = json.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		var original map[string]any
		json.Unmarshal(f.Request, &original)
		var statements any
		json.Unmarshal(f.Statements, &statements)
		original["input_snapshot"] = map[string]any{"id": "fixture", "evaluation_date": "2026-10-23", "statements": statements}
		f.Request, _ = json.Marshal(original)
		var request Request
		if err = json.Unmarshal(f.Request, &request); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		var before, after any
		json.Unmarshal(f.Request, &before)
		json.Unmarshal(encoded, &after)
		assertFields(t, before, after, name)
	}
}
func assertFields(t *testing.T, before, after any, path string) {
	t.Helper()
	switch old := before.(type) {
	case map[string]any:
		current, ok := after.(map[string]any)
		if !ok {
			t.Fatal(path, "object lost")
		}
		for key, value := range old {
			assertFields(t, value, current[key], path+"."+key)
		}
	case []any:
		current, ok := after.([]any)
		if !ok || len(old) != len(current) {
			t.Fatal(path, "array lost")
		}
		for i := range old {
			assertFields(t, old[i], current[i], path)
		}
	default:
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("%s: %v became %v", path, before, after)
		}
	}
}
