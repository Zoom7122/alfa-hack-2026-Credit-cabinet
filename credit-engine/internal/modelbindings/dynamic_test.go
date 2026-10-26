package modelbindings

import (
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

func TestDynamicSupplierWireContractAndExactIntegers(t *testing.T) {
	catalog, err := NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	base, err := catalog.Lookup("ip-v2", contracts.BorrowerTypeSoleProprietor)
	if err != nil {
		t.Fatal(err)
	}
	d, err := base.Definition()
	if err != nil {
		t.Fatal(err)
	}
	d.Version, d.FactsSchemaVersion = "ip-dynamic", "dynamic-facts"
	d.Parameters = append(d.Parameters,
		model.Parameter{ID: "supplier_flag", Enabled: true, Title: "Flag", Source: model.Source{Kind: model.Supplier, Path: "supplier_flag"}, JSONName: "supplier_flag", Type: model.Boolean, Unit: model.Flag},
		model.Parameter{ID: "supplier_amount", Enabled: true, Title: "Amount", Source: model.Source{Kind: model.Supplier, Path: "supplier_amount"}, JSONName: "supplier_amount", Type: model.Number, Unit: model.RUB, Number: &model.NumberDomain{Integer: true, Lower: &model.Bound{Value: "0", Inclusive: true}}},
	)
	entry, err := FromDefinition(d)
	if err != nil {
		t.Fatal(err)
	}
	expected := contracts.ScoringFactsExpectation{Request: contracts.ScoringFactsRequest{ApplicationID: "app", Borrower: contracts.ScoringFactsBorrower{ID: "borrower", Type: d.BorrowerType}, ModelVersion: d.Version, FactsSchemaVersion: d.FactsSchemaVersion}, Date: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)}
	envelope := `{"application_id":"app","borrower_id":"borrower","borrower_type":"SOLE_PROPRIETOR","model_version":"ip-dynamic","facts_schema_version":"dynamic-facts","facts_date":"2026-10-25","additional_facts":FACTS}`
	decode := func(facts string) (Facts, error) {
		return entry.DecodeFacts(strings.NewReader(strings.Replace(envelope, "FACTS", facts, 1)), expected)
	}
	got, err := decode(`{"supplier_flag":false,"supplier_amount":9007199254740993}`)
	if err != nil {
		t.Fatal(err)
	}
	if value, known := got.Values["supplier_flag"].Bool(); value || !known {
		t.Fatal("explicit false lost")
	}
	if got.Values["supplier_amount"].Format() != "9007199254740993" {
		t.Fatal("int64 rounded", got.Values)
	}
	got, err = decode(`{"supplier_flag":null,"supplier_amount":null}`)
	if err != nil || got.Values["supplier_flag"].Known() || got.Values["supplier_amount"].Known() {
		t.Fatal(got, err)
	}
	for _, facts := range []string{`null`, `{"supplier_flag":false}`, `{"supplier_flag":false,"supplier_amount":-1}`, `{"supplier_flag":false,"supplier_amount":1.5}`, `{"supplier_flag":false,"supplier_amount":9223372036854775808}`, `{"supplier_flag":false,"supplier_amount":1,"extra":0}`} {
		if _, err := decode(facts); err == nil {
			t.Fatalf("invalid supplier response accepted: %s", facts)
		}
	}
	// Captured parameter domains belong to the immutable entry, not the caller.
	d.Parameters[len(d.Parameters)-1].Number.Lower.Value = "100"
	if _, err := decode(`{"supplier_flag":false,"supplier_amount":1}`); err != nil {
		t.Fatal("caller mutated compiled decoder", err)
	}
}
