package simulation_test

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/simulation"
)

func TestGeneratedSimulationParity(t *testing.T) {
	catalog, err := modelbindings.NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"ip-demo-v1", "ip-demo-v2", "legal-demo-v1"} {
		t.Run(version, func(t *testing.T) {
			borrower := contracts.BorrowerTypeSoleProprietor
			name := "uc04_ip_approved"
			if strings.HasPrefix(version, "legal") {
				borrower = contracts.BorrowerTypeLegalEntity
				name = "uc04_le_approved"
			}
			data, err := os.ReadFile("../../testdata/decisions/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var input struct {
				Request    contracts.DecisionRequest    `json:"request"`
				Statements contracts.StatementsResponse `json:"service_b_response"`
			}
			if err := json.Unmarshal(data, &input); err != nil {
				t.Fatal(err)
			}
			entry, err := catalog.Lookup(version, borrower)
			if err != nil {
				t.Fatal(err)
			}
			definition, err := entry.Definition()
			if err != nil {
				t.Fatal(err)
			}
			number := "0"
			if version == "ip-demo-v2" {
				number = "9007199254740993"
			}
			values := json.RawMessage(`{"annual_contract_value_rub":` + number + `,"has_confirmed_contracts":false}`)
			factsDate := "2026-10-20"
			sample := simulation.Sample{EvaluationDate: "2026-10-23", Application: input.Request, Statements: &input.Statements, FactsDate: &factsDate, AdditionalFacts: values}
			preview, err := simulation.Run(context.Background(), definition, sample)
			if err != nil {
				t.Fatal(err)
			}
			date, _ := time.Parse(time.DateOnly, sample.EvaluationDate)
			body, err := json.Marshal(struct {
				contracts.ScoringFactsMetadata
				AdditionalFacts json.RawMessage `json:"additional_facts"`
			}{ScoringFactsMetadata: contracts.ScoringFactsMetadata{ApplicationID: input.Request.ApplicationID, BorrowerID: input.Request.Borrower.ID, BorrowerType: borrower, ModelVersion: version, FactsSchemaVersion: entry.Metadata().FactsSchemaVersion, FactsDate: factsDate}, AdditionalFacts: values})
			if err != nil {
				t.Fatal(err)
			}
			expected := contracts.ScoringFactsExpectation{Request: contracts.ScoringFactsRequest{ApplicationID: input.Request.ApplicationID, Borrower: contracts.ScoringFactsBorrower{ID: input.Request.Borrower.ID, Type: borrower, INN: input.Request.Borrower.INN}, ModelVersion: version, FactsSchemaVersion: entry.Metadata().FactsSchemaVersion}, Date: date}
			facts, err := entry.DecodeFacts(strings.NewReader(string(body)), expected)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := scoring.PrepareApplication(input.Request, date)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err = scoring.WithSupplierFacts(prepared, facts.Values, facts.Metadata.FactsDate)
			if err != nil {
				t.Fatal(err)
			}
			if borrower == contracts.BorrowerTypeLegalEntity {
				prepared, err = scoring.WithStatements(prepared, input.Statements)
				if err != nil {
					t.Fatal(err)
				}
			}
			calculated, err := scoring.EvaluatePrepared(entry.Engine(), prepared)
			if err != nil {
				t.Fatal(err)
			}
			working, err := scoring.ToAssessment(calculated)
			if err != nil {
				t.Fatal(err)
			}
			if string(preview.Route) != string(working.Route) || !reflect.DeepEqual(preview.Score, working.Score) || !reflect.DeepEqual(preview.Reasons, working.Reasons) {
				t.Fatal("preview differs from generated Go model and named DTO")
			}
		})
	}
}
