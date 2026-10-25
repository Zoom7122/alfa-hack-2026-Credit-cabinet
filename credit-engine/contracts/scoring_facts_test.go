package contracts_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
)

func expectation(version, schema string) contracts.ScoringFactsExpectation {
	return contracts.ScoringFactsExpectation{Request: contracts.ScoringFactsRequest{ApplicationID: "app", Borrower: contracts.ScoringFactsBorrower{ID: "borrower", Type: contracts.BorrowerTypeSoleProprietor}, ModelVersion: version, FactsSchemaVersion: schema}, Date: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
}
func response(version, schema, facts string) string {
	return `{"application_id":"app","borrower_id":"borrower","borrower_type":"SOLE_PROPRIETOR","model_version":"` + version + `","facts_schema_version":"` + schema + `","facts_date":"2026-10-20","additional_facts":` + facts + `}`
}
func code(err error) string {
	var failure *contracts.ScoringFactsError
	if errors.As(err, &failure) {
		return failure.Code
	}
	return ""
}

func TestGeneratedFactsProtocolExamples(t *testing.T) {
	var fixture struct {
		Date  string `json:"evaluation_date"`
		Cases []struct {
			Name           string                        `json:"name"`
			Request        contracts.ScoringFactsRequest `json:"request"`
			Response       json.RawMessage               `json:"response"`
			Status         *int                          `json:"provider_status"`
			Classification string                        `json:"expected_classification"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("examples/scoring_facts.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	date, err := time.Parse("2006-01-02", fixture.Date)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, example := range fixture.Cases {
		if example.Status == nil || *example.Status != 200 {
			continue
		}
		checked++
		t.Run(example.Name, func(t *testing.T) {
			expected := contracts.ScoringFactsExpectation{Request: example.Request, Date: date}
			var parseErr error
			if example.Request.Borrower.Type == contracts.BorrowerTypeLegalEntity {
				dto, err := contracts.DecodeFixtureLegalFactsDemoV1Response(bytes.NewReader(example.Response), expected)
				parseErr = err
				if err == nil {
					values := FixtureLegalFactsDemoV1Values(dto.AdditionalFacts)
					if !values["annual_contract_value_rub"].Known() {
						t.Fatal("known legal fact lost")
					}
				}
			} else {
				dto, err := contracts.DecodeFixtureIPFactsDemoV1Response(bytes.NewReader(example.Response), expected)
				parseErr = err
				if err == nil {
					values := FixtureIPFactsDemoV1Values(dto.AdditionalFacts)
					if example.Classification == "NEED_INFO" && values["annual_contract_value_rub"].Known() {
						t.Fatal("null became known")
					}
					if example.Classification == "CONTINUE_SCORING" && !values["annual_contract_value_rub"].Known() {
						t.Fatal("known number lost")
					}
				}
			}
			switch example.Classification {
			case "CONTINUE_SCORING", "NEED_INFO":
				if parseErr != nil {
					t.Fatal(parseErr)
				}
			default:
				if code(parseErr) != example.Classification {
					t.Fatalf("error %v code %s want %s", parseErr, code(parseErr), example.Classification)
				}
			}
		})
	}
	if checked != 19 {
		t.Fatal("unexpected protocol example count", checked)
	}
}

func TestGeneratedDTOsKeepZeroFalseNullAndNativeInteger(t *testing.T) {
	for _, facts := range []string{`{"annual_contract_value_rub":0,"has_confirmed_contracts":false}`, `{"annual_contract_value_rub":null,"has_confirmed_contracts":null}`} {
		dto, err := contracts.DecodeFixtureIPFactsDemoV1Response(strings.NewReader(response("ip-demo-v1", "demo-facts-v1", facts)), expectation("ip-demo-v1", "demo-facts-v1"))
		if err != nil {
			t.Fatal(err)
		}
		values := FixtureIPFactsDemoV1Values(dto.AdditionalFacts)
		if strings.Contains(facts, "null") {
			if dto.AdditionalFacts.AnnualContractValueRUB != nil || dto.AdditionalFacts.HasConfirmedContracts != nil || values["has_confirmed_contracts"].Known() {
				t.Fatal("null lost")
			}
		} else {
			if dto.AdditionalFacts.AnnualContractValueRUB == nil || *dto.AdditionalFacts.AnnualContractValueRUB != 0 || dto.AdditionalFacts.HasConfirmedContracts == nil || *dto.AdditionalFacts.HasConfirmedContracts {
				t.Fatal("zero/false lost")
			}
			flag, known := values["has_confirmed_contracts"].Bool()
			amount, numeric := values["annual_contract_value_rub"].Float()
			if !known || flag || !numeric || amount != 0 {
				t.Fatal("typed projection lost known zero/false")
			}
		}
		raw, err := json.Marshal(dto.AdditionalFacts)
		if err != nil || string(raw) != facts {
			t.Fatal("nullable wire changed", string(raw), err)
		}
	}
	for _, token := range []string{"9007199254740993", "9223372036854775807", "1.0", "1e1"} {
		facts := `{"annual_contract_value_rub":` + token + `,"has_confirmed_contracts":false}`
		dto, err := contracts.DecodeFixtureIPFactsDemoV2Response(strings.NewReader(response("ip-demo-v2", "demo-facts-v2", facts)), expectation("ip-demo-v2", "demo-facts-v2"))
		if err != nil {
			t.Fatal(err)
		}
		values := FixtureIPFactsDemoV2Values(dto.AdditionalFacts)
		r, ok := values["annual_contract_value_rub"].Rational()
		if !ok || !r.IsInt() || r.Num().Int64() != *dto.AdditionalFacts.AnnualContractValueRUB {
			t.Fatal("native int64 rounded")
		}
	}
}

func TestGeneratedStrictDecoderFailuresAndPriority(t *testing.T) {
	good := response("ip-demo-v1", "demo-facts-v1", `{"annual_contract_value_rub":0,"has_confirmed_contracts":false}`)
	expected := expectation("ip-demo-v1", "demo-facts-v1")
	for _, test := range []struct{ name, raw, want string }{
		{"duplicate", strings.Replace(good, `"borrower_id":"borrower"`, `"borrower_id":"borrower","borrower_id":"other"`, 1), "INVALID_FACTS_RESPONSE"},
		{"case_alias", strings.Replace(good, `"application_id":"app"`, `"application_id":"app","Application_id":"other"`, 1), "INVALID_FACTS_RESPONSE"},
		{"escaped_duplicate", strings.Replace(good, `"has_confirmed_contracts":false`, `"has_confirmed_contracts":false,"\u0068as_confirmed_contracts":true`, 1), "INVALID_FACTS_RESPONSE"},
		{"trailing", good + `{}`, "INVALID_FACTS_RESPONSE"},
		{"bad_json", strings.Replace(good, `:0`, `:NaN`, 1), "INVALID_FACTS_RESPONSE"},
		{"id_before_block", strings.Replace(strings.Replace(good, `"application_id":"app"`, `"application_id":"other"`, 1), `"has_confirmed_contracts":false`, `"has_confirmed_contracts":"false"`, 1), "FACTS_ID_MISMATCH"},
		{"version_before_block", strings.Replace(strings.Replace(good, `"model_version":"ip-demo-v1"`, `"model_version":"ip-other"`, 1), `"has_confirmed_contracts":false`, `"has_confirmed_contracts":"false"`, 1), "FACTS_VERSION_MISMATCH"},
		{"date_before_block", strings.Replace(strings.Replace(good, `2026-10-20`, `2026-10-24`, 1), `"has_confirmed_contracts":false`, `"has_confirmed_contracts":"false"`, 1), "INVALID_FACTS_RESPONSE"},
		{"overflow", strings.Replace(good, `:0`, `:1e309`, 1), "INVALID_FACTS_RESPONSE"},
		{"null_envelope", strings.Replace(good, `"application_id":"app"`, `"application_id":null`, 1), "INVALID_FACTS_RESPONSE"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := contracts.DecodeFixtureIPFactsDemoV1Response(strings.NewReader(test.raw), expected)
			if code(err) != test.want {
				t.Fatal(code(err), err)
			}
		})
	}
	if _, err := contracts.DecodeFixtureIPFactsDemoV1Response(strings.NewReader(strings.Repeat(" ", jsonstrict.MaxBytes+1)), expected); code(err) != "INVALID_FACTS_RESPONSE" {
		t.Fatal(err)
	}
	readErr := errors.New("read failure")
	_, err := contracts.DecodeFixtureIPFactsDemoV1Response(failedReader{readErr}, expected)
	if !errors.Is(err, readErr) {
		t.Fatal("read cause lost", err)
	}
	var dto contracts.FixtureIPFactsDemoV1
	known := float64(12)
	dto.AnnualContractValueRUB = &known
	if err := json.Unmarshal([]byte(`{"annual_contract_value_rub":"bad","has_confirmed_contracts":false}`), &dto); err == nil || dto.AnnualContractValueRUB == nil || *dto.AnnualContractValueRUB != 12 {
		t.Fatal("failed unmarshal changed existing DTO")
	}
	for _, facts := range []string{`{"annual_contract_value_rub":0.1,"has_confirmed_contracts":true}`, `{"annual_contract_value_rub":9223372036854775808,"has_confirmed_contracts":true}`} {
		if _, err := contracts.DecodeFixtureIPFactsDemoV2Response(strings.NewReader(response("ip-demo-v2", "demo-facts-v2", facts)), expectation("ip-demo-v2", "demo-facts-v2")); code(err) != "INVALID_FACTS_RESPONSE" {
			t.Fatal("invalid int64 accepted", err)
		}
	}
	raw, err := json.Marshal(expected.Request)
	if err != nil || !bytes.Contains(raw, []byte(`"inn":null`)) {
		t.Fatal("request INN key omitted", string(raw), err)
	}
}

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }

var _ io.Reader = failedReader{}
