package clients

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func httpClient(f roundTripper) *http.Client {
	return &http.Client{Transport: f, Timeout: 2 * time.Second}
}
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func jsonText(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile("../../testdata/" + path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	Initial  contracts.BankState        `json:"initial_bank"`
	Request  contracts.BankCheckRequest `json:"request"`
	Expected struct {
		Response contracts.BankCheckResponse `json:"response"`
		Event    contracts.AdminEvent        `json:"notification"`
	} `json:"expected"`
}

func deal(t *testing.T) fixture {
	t.Helper()
	var f fixture
	readJSON(t, "bank/bank_fixed_deal.json", &f)
	return f
}
func clone[T any](t *testing.T, v T) T {
	t.Helper()
	b := jsonText(t, v)
	var out T
	if err := json.Unmarshal([]byte(b), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func status(t *testing.T, err error, want int) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Status != want {
		t.Fatalf("error %v want %d", err, want)
	}
}
func statementsStatus(t *testing.T, err error, want int) {
	t.Helper()
	var e *scoring.Error
	if !errors.As(err, &e) || e.Status != want {
		t.Fatalf("statements error %v want %d", err, want)
	}
}
func ptr[T any](v T) *T { return &v }

func TestStatementsPathQueryAndNullPreservation(t *testing.T) {
	for _, id := range []string{"borrower-1", "ип/123?x=1% +", ".", ".."} {
		t.Run(id, func(t *testing.T) {
			calls := 0
			c, err := NewStatements("http://b.invalid/gateway%2Fv2/", httpClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json" || r.URL.Query().Get("years") != "2024,2025" {
					t.Errorf("request %s %s", r.Method, r.URL)
				}
				if !strings.HasPrefix(r.URL.EscapedPath(), "/gateway%2Fv2/api/v1/borrowers/") || !strings.HasSuffix(r.URL.EscapedPath(), "/statements") {
					t.Error(r.URL.EscapedPath())
				}
				if id == "." && !strings.Contains(r.URL.EscapedPath(), "/%2E/") || id == ".." && !strings.Contains(r.URL.EscapedPath(), "/%2E%2E/") {
					t.Error("ID was a structural dot segment")
				}
				if strings.Contains(id, "/") && !strings.Contains(r.URL.EscapedPath(), "%2F123%3Fx=1%25%20+") {
					t.Error(r.URL.EscapedPath())
				}
				if r.Body != nil {
					t.Error("GET has body")
				}
				return response(200, `{"borrower_id":`+jsonText(t, id)+`,"annual_reports":[{"year":2025,"balance":{"unit":"RUB","cash_and_equivalents":0,"receivables":null}}],"extra":"ignored"}`), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			r, err := c.Statements(context.Background(), id, []int{2024, 2025})
			if err != nil {
				t.Fatal(err)
			}
			b := r.AnnualReports[0].Balance
			if calls != 1 || b.CashAndEquivalents == nil || *b.CashAndEquivalents != 0 || b.Receivables != nil {
				t.Fatal("null/zero was changed")
			}
		})
	}
}

func TestStatementsStatusesAndMalformedBodies(t *testing.T) {
	for _, c := range []struct {
		name     string
		http     int
		body     string
		want     int
		notFound bool
	}{
		{"missing forms", 404, "not JSON", 0, true}, {"partial valid", 200, `{"borrower_id":"b","annual_reports":[]}`, 0, false},
		{"wrong borrower", 200, `{"borrower_id":"other"}`, 502, false}, {"missing borrower", 200, `{}`, 502, false},
		{"null", 200, "null", 502, false}, {"array", 200, "[]", 502, false}, {"wrong type", 200, `{"borrower_id":"b","annual_reports":0}`, 502, false},
		{"fractional money", 200, `{"borrower_id":"b","annual_reports":[{"balance":{"total_assets":1.5}}]}`, 502, false},
		{"bad JSON", 200, "{", 502, false}, {"trailing JSON", 200, `{"borrower_id":"b"} {}`, 502, false},
		{"unexpected success", 201, `{}`, 502, false}, {"unavailable", 503, "down", 502, false}, {"redirect", 302, "", 502, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			client, err := NewStatements("http://b.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(c.http, c.body), nil }))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Statements(context.Background(), "b", []int{2024, 2025})
			if c.notFound {
				if !errors.Is(err, scoring.ErrStatementsNotFound) {
					t.Fatal(err)
				}
			} else if c.want != 0 {
				statementsStatus(t, err, c.want)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
	c, _ := NewStatements("http://b.invalid", httpClient(func(*http.Request) (*http.Response, error) { t.Fatal("invalid outgoing request sent"); return nil, nil }))
	for _, arg := range []struct {
		id    string
		years []int
	}{{"", []int{2024, 2025}}, {"b", nil}, {"b", []int{-1, 2025}}} {
		_, err := c.Statements(context.Background(), arg.id, arg.years)
		statementsStatus(t, err, 500)
	}
}

func TestBankCallsPreserveDTOAndPercentRate(t *testing.T) {
	f := deal(t)
	for _, rate := range []float64{0, 1, 23.75, 100} {
		t.Run(jsonText(t, rate), func(t *testing.T) {
			r := clone(t, f.Request)
			r.Terms.AnnualRate = rate
			r.RequestedTerms.MaxAnnualRate = ptr(rate)
			client, err := NewBank("http://bank.invalid/prefix/", httpClient(func(req *http.Request) (*http.Response, error) {
				if req.Method != "POST" || req.URL.EscapedPath() != "/prefix/api/v1/bank/check-and-issue" || req.Header.Get("Content-Type") != "application/json" {
					t.Fatal(req)
				}
				var actual contracts.BankCheckRequest
				if err := json.NewDecoder(req.Body).Decode(&actual); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, r) || actual.Terms.AnnualRate != rate {
					t.Fatal("rate or payload changed")
				}
				return response(200, jsonText(t, f.Expected.Response)), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.Check(context.Background(), r)
			if err != nil || !reflect.DeepEqual(got, f.Expected.Response) {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
	m := contracts.ManualReviewRequest{PolicyID: f.Request.PolicyID, ApplicationID: f.Request.ApplicationID, Borrower: f.Request.Borrower, RequestedTerms: f.Request.RequestedTerms, Score: f.Request.Score, Reasons: []contracts.Reason{{Code: "MANUAL", Message: "Нужна проверка.", Fields: []string{}}}}
	want := contracts.ManualReviewResponse{ApplicationID: m.ApplicationID, EventID: m.ApplicationID + ":BORROWER_REVIEW", NotificationStatus: contracts.NotificationStatusPending}
	c, _ := NewBank("http://bank.invalid", httpClient(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/bank/manual-reviews" {
			t.Fatal(r.URL)
		}
		var got contracts.ManualReviewRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, m) {
			t.Fatal("manual payload changed")
		}
		return response(202, jsonText(t, want)), nil
	}))
	got, err := c.ManualReview(context.Background(), m)
	if err != nil || got != want {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestBankResponseValidation(t *testing.T) {
	f := deal(t)
	for _, change := range []struct {
		name  string
		apply func(*contracts.BankCheckResponse)
	}{
		{"wrong application", func(r *contracts.BankCheckResponse) { r.ApplicationID = "other" }}, {"unknown verdict", func(r *contracts.BankCheckResponse) { r.Verdict = "OK" }},
		{"missing loan ID", func(r *contracts.BankCheckResponse) { r.LoanID = nil }}, {"blank loan ID", func(r *contracts.BankCheckResponse) { r.LoanID = ptr("") }},
		{"foreign loan ID", func(r *contracts.BankCheckResponse) { r.LoanID = ptr("loan-other") }},
		{"unknown notification", func(r *contracts.BankCheckResponse) { r.NotificationStatus = "OK" }}, {"wrong event", func(r *contracts.BankCheckResponse) { r.EventID = "other" }},
		{"empty checks", func(r *contracts.BankCheckResponse) { r.Checks = []contracts.CheckResult{} }}, {"blank check", func(r *contracts.BankCheckResponse) { r.Checks[0].Code = "" }},
		{"blank detail", func(r *contracts.BankCheckResponse) { r.Checks[0].Detail = "" }}, {"issued with failed check", func(r *contracts.BankCheckResponse) { r.Checks[0].Passed = false }},
		{"duplicate check", func(r *contracts.BankCheckResponse) { r.Checks = append(r.Checks, r.Checks[0]) }},
		{"review with loan", func(r *contracts.BankCheckResponse) {
			r.Verdict = contracts.BankVerdictBankReview
			r.Checks[0].Passed = false
		}},
		{"review without problem", func(r *contracts.BankCheckResponse) { r.Verdict = contracts.BankVerdictBankReview; r.LoanID = nil }},
	} {
		t.Run(change.name, func(t *testing.T) {
			r := clone(t, f.Expected.Response)
			change.apply(&r)
			c, _ := NewBank("http://bank.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(200, jsonText(t, r)), nil }))
			_, err := c.Check(context.Background(), f.Request)
			status(t, err, 502)
		})
	}
	for _, body := range []string{"null", "[]", "{", `{} {}`, `{"application_id":0}`, `{}`, strings.Replace(jsonText(t, f.Expected.Response), `"passed":true`, `"passed":null`, 1), strings.Replace(jsonText(t, f.Expected.Response), `"passed":true,`, ``, 1)} {
		c, _ := NewBank("http://bank.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(200, body), nil }))
		_, err := c.Check(context.Background(), f.Request)
		status(t, err, 502)
	}
	r := clone(t, f.Expected.Response)
	r.Verdict = contracts.BankVerdictBankReview
	r.LoanID = nil
	r.Checks[0].Passed = false
	r.EventID = r.ApplicationID + ":BANK_REVIEW"
	r.NotificationStatus = contracts.NotificationStatusPending
	c, _ := NewBank("http://bank.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(200, jsonText(t, r)), nil }))
	got, err := c.Check(context.Background(), f.Request)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("valid bank review rejected: %+v %v", got, err)
	}
}

func TestManualResponseValidationAndUnexpectedStatuses(t *testing.T) {
	f := deal(t)
	m := contracts.ManualReviewRequest{ApplicationID: f.Request.ApplicationID}
	for _, body := range []string{"null", `{}`, `{"application_id":"other","event_id":"a:BORROWER_REVIEW","notification_status":"DELIVERED"}`, `{"application_id":"` + m.ApplicationID + `","event_id":"wrong","notification_status":"PENDING"}`, `{"application_id":"` + m.ApplicationID + `","event_id":"` + m.ApplicationID + `:BORROWER_REVIEW","notification_status":null}`} {
		c, _ := NewBank("http://bank.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(202, body), nil }))
		_, err := c.ManualReview(context.Background(), m)
		status(t, err, 502)
	}
	for _, code := range []int{200, 201, 204, 301, 400, 422, 500, 503} {
		c, _ := NewBank("http://bank.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(code, `{}`), nil }))
		_, err := c.ManualReview(context.Background(), m)
		status(t, err, 502)
	}
	for _, code := range []int{202, 204, 301, 400, 422, 500} {
		c, _ := NewBank("http://bank.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(code, `{}`), nil }))
		_, err := c.Check(context.Background(), f.Request)
		status(t, err, 502)
	}
}

func TestConflictsRemain409(t *testing.T) {
	f := deal(t)
	for _, body := range []string{`{"status":409,"code":"POLICY_MISMATCH","fields":["policy_id"]}`, "not JSON", `{"status":500,"code":"wrong"}`} {
		c, _ := NewBank("http://bank.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(409, body), nil }))
		_, err := c.Check(context.Background(), f.Request)
		status(t, err, 409)
		if strings.Contains(body, "POLICY_MISMATCH") {
			var e *Error
			_ = errors.As(err, &e)
			if e.Code != "POLICY_MISMATCH" || !reflect.DeepEqual(e.Fields, []string{"policy_id"}) {
				t.Fatal(e)
			}
		}
		_, err = c.ManualReview(context.Background(), contracts.ManualReviewRequest{ApplicationID: f.Request.ApplicationID})
		status(t, err, 409)
	}
}

func TestAdminAcknowledgementsAndAllEventKinds(t *testing.T) {
	f := deal(t)
	for _, kind := range []contracts.EventType{contracts.EventTypeLoanIssued, contracts.EventTypeBankReview, contracts.EventTypeBorrowerReview} {
		t.Run(string(kind), func(t *testing.T) {
			event := clone(t, f.Expected.Event)
			event.Kind = kind
			event.EventID = event.ApplicationID + ":" + string(kind)
			c, _ := NewAdmin("http://admin.invalid/base/", httpClient(func(r *http.Request) (*http.Response, error) {
				if r.Method != "POST" || r.URL.Path != "/base/api/v1/credit-events" {
					t.Fatal(r.URL)
				}
				var got contracts.AdminEvent
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, event) {
					t.Fatal("event changed")
				}
				return response(202, `{"event_id":`+jsonText(t, event.EventID)+`,"accepted":true,"extra":1}`), nil
			}))
			ack, err := c.Notify(context.Background(), event)
			if err != nil || !ack.Accepted || ack.EventID != event.EventID {
				t.Fatalf("%+v %v", ack, err)
			}
		})
	}
	for _, body := range []string{"null", "[]", "{", `{} {}`, `{}`, `{"event_id":"other","accepted":true}`, `{"event_id":` + jsonText(t, f.Expected.Event.EventID) + `,"accepted":false}`, `{"event_id":` + jsonText(t, f.Expected.Event.EventID) + `,"accepted":null}`, `{"event_id":` + jsonText(t, f.Expected.Event.EventID) + `,"accepted":"true"}`} {
		c, _ := NewAdmin("http://admin.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(202, body), nil }))
		_, err := c.Notify(context.Background(), f.Expected.Event)
		status(t, err, 502)
	}
	for _, code := range []int{200, 201, 204, 302, 400, 409, 422, 500, 504} {
		c, _ := NewAdmin("http://admin.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(code, `{"accepted":true}`), nil }))
		_, err := c.Notify(context.Background(), f.Expected.Event)
		status(t, err, 502)
	}
}

func TestOutgoingInvalidJSONDoesNotSend(t *testing.T) {
	f := deal(t)
	calls := 0
	c := httpClient(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not send") })
	b, _ := NewBank("http://bank.invalid", c)
	a, _ := NewAdmin("http://admin.invalid", c)
	f.Request.Selection.MaxNPVRUB = math.NaN()
	_, err := b.Check(context.Background(), f.Request)
	status(t, err, 500)
	event := f.Expected.Event
	event.Score.RawTotal = math.Inf(1)
	_, err = a.Notify(context.Background(), event)
	status(t, err, 500)
	if calls != 0 {
		t.Fatal(calls)
	}
}
