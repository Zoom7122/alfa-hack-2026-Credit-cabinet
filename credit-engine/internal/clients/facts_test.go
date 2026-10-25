package clients

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
)

func transportFactsEntry(t *testing.T, decode modelbindings.FactsDecoder) *modelbindings.Entry {
	t.Helper()
	raw, err := os.ReadFile("../../models/ip-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	d, err := model.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	d.Version = "ip-transport-test"
	d.FactsSchemaVersion = "transport-test"
	d.Parameters = append(d.Parameters, model.Parameter{ID: "flag", Enabled: true, Title: "Test", Source: model.Source{Kind: model.Supplier, Path: "flag"}, JSONName: "flag", Type: model.Boolean, Unit: model.Flag, Required: false})
	data, err := model.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	shape, err := model.FactsSchemaSHA256(d.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if decode == nil {
		decode = func(reader io.Reader, expected contracts.ScoringFactsExpectation) (modelbindings.Facts, error) {
			_, err := io.ReadAll(reader)
			return modelbindings.Facts{Metadata: contracts.ScoringFactsMetadata{ModelVersion: expected.Request.ModelVersion}, Values: map[string]rules.Value{"flag": rules.Boolean(false)}}, err
		}
	}
	catalog, err := modelbindings.New([]modelbindings.Registration{{Metadata: modelbindings.Metadata{ID: d.ID, Version: d.Version, BorrowerType: d.BorrowerType, FactsSchemaVersion: d.FactsSchemaVersion, DefinitionSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), FactsSchemaSHA256: shape, UsesFacts: true, FormatVersion: d.FormatVersion}, Definition: func() model.ModelDefinition { return d }, DecodeFacts: decode}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := catalog.Lookup(d.Version, d.BorrowerType)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}
func transportFactsRequest() contracts.ScoringFactsRequest {
	return contracts.ScoringFactsRequest{ApplicationID: "app", Borrower: contracts.ScoringFactsBorrower{ID: "borrower", Type: contracts.BorrowerTypeSoleProprietor}, ModelVersion: "ip-transport-test", FactsSchemaVersion: "transport-test"}
}
func factsDate() time.Time { return time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC) }
func expectFactsError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var failure *Error
	if !errors.As(err, &failure) || failure.Status != status || failure.Code != code {
		t.Fatal(err, status, code)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("dependency error leaked source data")
	}
}

func TestFactsOneStableRequestAndNoV2Dependency(t *testing.T) {
	entry := transportFactsEntry(t, nil)
	calls := 0
	bodies := []string{}
	type traceKey struct{}
	ctx := context.WithValue(context.Background(), traceKey{}, "trace")
	c, err := NewFacts("http://facts.invalid/base/", httpClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/base/api/v1/scoring-facts" || r.Header.Get("Content-Type") != "application/json" || r.Context().Value(traceKey{}) != "trace" {
			t.Fatal("request/context contract")
		}
		if _, ok := r.Context().Deadline(); !ok {
			t.Fatal("client timeout not propagated")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(body))
		return response(200, `{}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := c.Fetch(ctx, entry, transportFactsRequest(), factsDate())
		if err != nil {
			t.Fatal(err)
		}
		if flag, known := result.Values["flag"].Bool(); !known || flag {
			t.Fatal("known false lost")
		}
	}
	if calls != 2 || bodies[0] != bodies[1] || !strings.Contains(bodies[0], `"inn":null`) {
		t.Fatal("unstable/duplicated request", calls, bodies)
	}
	catalog, err := modelbindings.NewDefault()
	if err != nil {
		t.Fatal(err)
	}
	v2, err := catalog.Lookup("ip-v2", contracts.BorrowerTypeSoleProprietor)
	if err != nil {
		t.Fatal(err)
	}
	var absent *FactsClient
	if _, err := absent.Fetch(ctx, v2, contracts.ScoringFactsRequest{}, time.Time{}); err != nil {
		t.Fatal("v2 required supplier", err)
	}
	if calls != 2 {
		t.Fatal("v2 called supplier")
	}
}

func TestFactsStatusesRedirectsAndMalformedBodies(t *testing.T) {
	entry := transportFactsEntry(t, nil)
	for _, status := range []int{204, 301, 302, 303, 307, 308, 400, 404, 422, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			c, err := NewFacts("http://facts.invalid", httpClient(func(*http.Request) (*http.Response, error) {
				calls++
				r := response(status, `{"secret":"body"}`)
				r.Header.Set("Location", "http://other.invalid/")
				return r, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Fetch(context.Background(), entry, transportFactsRequest(), factsDate())
			expectFactsError(t, err, 502, "FACTS_UNAVAILABLE")
			if calls != 1 {
				t.Fatal("status retried/redirected", calls)
			}
		})
	}
	for _, body := range []string{`{} {}`, `{"x":0,"x":1}`, `{"x":NaN}`, strings.Repeat(" ", jsonstrict.MaxBytes+1), strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66)} {
		c, err := NewFacts("http://facts.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(200, body), nil }))
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Fetch(context.Background(), entry, transportFactsRequest(), factsDate())
		expectFactsError(t, err, 502, "INVALID_FACTS_RESPONSE")
	}
}

func TestFactsProtocolErrorsContextAndTimeouts(t *testing.T) {
	for _, code := range []string{"INVALID_FACTS_RESPONSE", "FACTS_ID_MISMATCH", "FACTS_VERSION_MISMATCH"} {
		entry := transportFactsEntry(t, func(io.Reader, contracts.ScoringFactsExpectation) (modelbindings.Facts, error) {
			return modelbindings.Facts{}, &contracts.ScoringFactsError{Code: code, Fields: []string{"facts_schema_version"}}
		})
		c, _ := NewFacts("http://facts.invalid", httpClient(func(*http.Request) (*http.Response, error) { return response(200, `{}`), nil }))
		_, err := c.Fetch(context.Background(), entry, transportFactsRequest(), factsDate())
		expectFactsError(t, err, 502, code)
	}
	entry := transportFactsEntry(t, nil)
	for _, mode := range []string{"connection", "transport_timeout", "caller_deadline", "client_timeout", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			h := httpClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "connection" {
					return nil, errors.New("secret connection data")
				}
				if mode == "transport_timeout" {
					return nil, timeoutError{}
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			h.Timeout = 20 * time.Millisecond
			c, _ := NewFacts("http://facts.invalid", h)
			ctx := context.Background()
			cancel := func() {}
			if mode == "caller_deadline" {
				ctx, cancel = context.WithTimeout(ctx, 5*time.Millisecond)
			}
			if mode == "canceled" {
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			defer cancel()
			_, err := c.Fetch(ctx, entry, transportFactsRequest(), factsDate())
			status, code := 504, "DEPENDENCY_TIMEOUT"
			if mode == "connection" || mode == "canceled" {
				status, code = 502, "FACTS_UNAVAILABLE"
			}
			expectFactsError(t, err, status, code)
			if calls > 1 || mode == "canceled" && calls != 0 {
				t.Fatal("cancellation/retry", calls)
			}
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancel cause lost")
			}
		})
	}
	for _, base := range []string{"", "/relative", "http://user:secret@facts", "http://facts?", "http://facts:65536"} {
		if _, err := NewFacts(base, httpClient(nil)); err == nil {
			t.Fatal("invalid URL accepted")
		}
	}
	if _, err := NewFacts("http://facts.invalid", &http.Client{}); err == nil {
		t.Fatal("missing timeout accepted")
	}
}

type factsReadCloser struct {
	err    error
	closed *bool
}

func (r factsReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (r factsReadCloser) Close() error             { *r.closed = true; return nil }
func TestFactsBodyFailuresCloseAndInvalidRequestsDoNotCall(t *testing.T) {
	entry := transportFactsEntry(t, nil)
	for _, timeout := range []bool{false, true} {
		closed := false
		cause := error(errors.New("secret body read failure"))
		if timeout {
			cause = timeoutError{}
		}
		client, _ := NewFacts("http://facts.invalid", httpClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: factsReadCloser{cause, &closed}}, nil
		}))
		_, err := client.Fetch(context.Background(), entry, transportFactsRequest(), factsDate())
		status, code := 502, "FACTS_UNAVAILABLE"
		if timeout {
			status, code = 504, "DEPENDENCY_TIMEOUT"
		}
		expectFactsError(t, err, status, code)
		if !closed || !errors.Is(err, cause) {
			t.Fatal("body close/cause lost")
		}
	}
	calls := 0
	client, _ := NewFacts("http://facts.invalid", httpClient(func(*http.Request) (*http.Response, error) { calls++; return response(200, `{}`), nil }))
	for _, edit := range []func(*contracts.ScoringFactsRequest){func(r *contracts.ScoringFactsRequest) { r.ApplicationID = "" }, func(r *contracts.ScoringFactsRequest) { r.Borrower.ID = "" }, func(r *contracts.ScoringFactsRequest) { r.Borrower.Type = contracts.BorrowerTypeLegalEntity }, func(r *contracts.ScoringFactsRequest) { r.ModelVersion = "other" }, func(r *contracts.ScoringFactsRequest) { r.FactsSchemaVersion = "other" }} {
		request := transportFactsRequest()
		edit(&request)
		_, err := client.Fetch(context.Background(), entry, request, factsDate())
		expectFactsError(t, err, 500, "INVALID_FACTS_REQUEST")
	}
	if _, err := client.Fetch(context.Background(), entry, transportFactsRequest(), time.Time{}); err == nil {
		t.Fatal("implicit date")
	}
	if _, err := client.Fetch(context.Background(), nil, transportFactsRequest(), factsDate()); err == nil {
		t.Fatal("nil entry")
	}
	if calls != 0 {
		t.Fatal("invalid request reached provider")
	}
	var absent *FactsClient
	_, err := absent.Fetch(context.Background(), entry, transportFactsRequest(), factsDate())
	expectFactsError(t, err, 502, "FACTS_UNAVAILABLE")
}
