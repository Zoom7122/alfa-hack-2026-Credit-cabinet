package contracts_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func TestDecisionRequestModelVersionEncoding(t *testing.T) {
	for _, test := range []struct {
		name, input, version string
		invalid              bool
	}{
		{"legacy", `{}`, "", false},
		{"explicit", `{"model_version":"ip-v2"}`, "ip-v2", false},
		{"empty", `{"model_version":""}`, "", false},
		{"null", `{"model_version":null}`, "", true},
		{"number", `{"model_version":1}`, "", true},
		{"boolean", `{"model_version":false}`, "", true},
		{"array", `{"model_version":[]}`, "", true},
		{"object", `{"model_version":{}}`, "", true},
		{"duplicate", `{"model_version":"ip-v2","model_version":"ip-v2"}`, "", true},
		{"case_alias", `{"MODEL_VERSION":"ip-v2"}`, "", true},
		{"escaped_duplicate", `{"model_version":"ip-v2","model_versio\u006e":"ip-v2"}`, "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var r contracts.DecisionRequest
			if err := json.Unmarshal([]byte(test.input), &r); (err != nil) != test.invalid {
				t.Fatal(err)
			}
			if test.invalid {
				return
			}
			if test.name == "legacy" {
				if r.ModelVersion != nil {
					t.Fatal("legacy version became explicit")
				}
			} else if r.ModelVersion == nil || *r.ModelVersion != test.version {
				t.Fatal(r.ModelVersion)
			}
			data, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), `"model_version"`) != (test.name != "legacy") {
				t.Fatal(string(data))
			}
		})
	}
}
