package contracts_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func TestEconomicValueUnionPreservesWireAndKnownFalse(t *testing.T) {
	for _, value := range []string{"null", "0", "0.125", "-1.25", "true", "false"} {
		t.Run(value, func(t *testing.T) {
			raw := `{"id":"criterion","value":` + value + `,"points":3.75,"max_points":15}`
			var c contracts.EconomicCriterion
			if err := json.Unmarshal([]byte(raw), &c); err != nil {
				t.Fatal(err)
			}
			if err := c.ValidateValue(); err != nil {
				t.Fatal(err)
			}
			if value == "false" && (c.BooleanValue == nil || *c.BooleanValue || c.Value != nil) {
				t.Fatal("false became unknown")
			}
			if value == "null" && (c.BooleanValue != nil || c.Value != nil) {
				t.Fatal("unknown became known")
			}
			encoded, err := json.Marshal(c)
			if err != nil || string(encoded) != raw {
				t.Fatal(string(encoded), err)
			}
		})
	}
	for _, raw := range []string{`"false"`, `"1"`, `{}`, `[]`, `1e309`} {
		var c contracts.EconomicCriterion
		if err := json.Unmarshal([]byte(`{"id":"criterion","value":`+raw+`,"points":0,"max_points":0}`), &c); err == nil {
			t.Fatal("invalid value accepted", raw)
		}
	}
	number, flag := 1.0, false
	c := contracts.EconomicCriterion{Value: &number, BooleanValue: &flag}
	if err := c.ValidateValue(); err == nil {
		t.Fatal("ambiguous value valid")
	}
	if _, err := json.Marshal(c); err == nil {
		t.Fatal("ambiguous value serialized")
	}
	bad := math.Inf(1)
	if err := (contracts.EconomicCriterion{Value: &bad}).ValidateValue(); err == nil {
		t.Fatal("infinite value valid")
	}
}
func TestEconomicValueDecodeIsAtomicAndKeepsPartialUpdateBehavior(t *testing.T) {
	n := 12.5
	c := contracts.EconomicCriterion{ID: "old", Value: &n, Points: 2, MaxPoints: 15}
	if err := json.Unmarshal([]byte(`{"value":"bad","id":"changed"}`), &c); err == nil || c.ID != "old" || c.Value == nil || *c.Value != 12.5 {
		t.Fatal("failed decode changed object")
	}
	if err := json.Unmarshal([]byte(`{"points":3}`), &c); err != nil || c.ID != "old" || c.Value == nil || *c.Value != 12.5 || c.Points != 3 {
		t.Fatal(c, err)
	}
	if err := json.Unmarshal([]byte(`{"value":false}`), &c); err != nil || c.Value != nil || c.BooleanValue == nil || *c.BooleanValue {
		t.Fatal(c, err)
	}
	if err := json.Unmarshal([]byte(`{"value":null}`), &c); err != nil || c.Value != nil || c.BooleanValue != nil {
		t.Fatal(c, err)
	}
}
