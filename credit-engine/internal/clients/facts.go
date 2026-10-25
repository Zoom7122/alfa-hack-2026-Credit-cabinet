package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/rules"
)

type FactsClient struct{ transport *transport }

func NewFacts(base string, c *http.Client) (*FactsClient, error) {
	t, err := newTransport(base, c)
	if err != nil {
		return nil, err
	}
	return &FactsClient{transport: t}, nil
}

// Fetch sends one stable aggregate request. Version selection is owned by the
// compiled catalog; this client never picks another model, retries or caches.
func (c *FactsClient) Fetch(ctx context.Context, entry *modelbindings.Entry, request contracts.ScoringFactsRequest, date time.Time) (modelbindings.Facts, error) {
	empty := modelbindings.Facts{Values: map[string]rules.Value{}}
	if entry == nil {
		return empty, &Error{Status: 500, Code: "INVALID_FACTS_REQUEST"}
	}
	metadata := entry.Metadata()
	if !metadata.UsesFacts {
		return empty, nil
	}
	if ctx == nil || date.IsZero() || strings.TrimSpace(request.ApplicationID) == "" || strings.TrimSpace(request.Borrower.ID) == "" || request.Borrower.Type != metadata.BorrowerType || request.ModelVersion != metadata.Version || request.FactsSchemaVersion != metadata.FactsSchemaVersion {
		return empty, &Error{Status: 500, Code: "INVALID_FACTS_REQUEST"}
	}
	if c == nil || c.transport == nil {
		return empty, &Error{Status: 502, Code: "FACTS_UNAVAILABLE"}
	}
	if err := ctx.Err(); err != nil {
		return empty, factsDependencyError(err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		return empty, &Error{Status: 500, Code: "INVALID_FACTS_REQUEST", cause: err}
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.transport.endpoint([]string{"api", "v1", "scoring-facts"}, nil), bytes.NewReader(data))
	if err != nil {
		return empty, &Error{Status: 500, Code: "INVALID_FACTS_REQUEST", cause: err}
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Content-Type", "application/json")
	h := *c.transport.http
	h.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := h.Do(r)
	if err != nil {
		return empty, factsDependencyError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return empty, &Error{Status: 502, Code: "FACTS_UNAVAILABLE"}
	}
	body, err := jsonstrict.Read(response.Body)
	if err != nil {
		var schema *jsonstrict.Error
		if errors.As(err, &schema) && schema.Code == "READ_ERROR" {
			return empty, factsDependencyError(err)
		}
		return empty, &Error{Status: 502, Code: "INVALID_FACTS_RESPONSE", cause: err}
	}
	if err := ctx.Err(); err != nil {
		return empty, factsDependencyError(err)
	}
	result, err := entry.DecodeFacts(bytes.NewReader(body), contracts.ScoringFactsExpectation{Request: request, Date: date})
	if err != nil {
		var protocol *contracts.ScoringFactsError
		if errors.As(err, &protocol) {
			return empty, &Error{Status: 502, Code: protocol.Code, Fields: append([]string{}, protocol.Fields...), cause: err}
		}
		return empty, &Error{Status: 502, Code: "INVALID_FACTS_RESPONSE", cause: err}
	}
	if err := ctx.Err(); err != nil {
		return empty, factsDependencyError(err)
	}
	return result, nil
}
func factsDependencyError(err error) *Error {
	failure := dependencyError("FACTS_UNAVAILABLE", err)
	if failure.Status == 504 {
		failure.Code = "DEPENDENCY_TIMEOUT"
	}
	return failure
}
