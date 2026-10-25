package clients

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

type StatementsClient struct{ transport *transport }

func NewStatements(base string, c *http.Client) (*StatementsClient, error) {
	t, err := newTransport(base, c)
	if err != nil {
		return nil, err
	}
	return &StatementsClient{transport: t}, nil
}
func (c *StatementsClient) Statements(ctx context.Context, id string, years []int) (contracts.StatementsResponse, error) {
	var empty contracts.StatementsResponse
	if strings.TrimSpace(id) == "" || len(years) != 2 || years[0] <= 0 || years[1] <= 0 {
		return empty, &scoring.Error{Status: 500, Code: "INVALID_STATEMENTS_REQUEST"}
	}
	query := url.Values{"years": {strconv.Itoa(years[0]) + "," + strconv.Itoa(years[1])}}
	status, data, err := c.transport.exchange(ctx, http.MethodGet, []string{"api", "v1", "borrowers", id, "statements"}, query, nil, http.StatusOK, http.StatusNotFound)
	if err != nil {
		var failure *Error
		if errors.As(err, &failure) {
			return empty, &scoring.Error{Status: failure.Status, Code: failure.Code, Fields: failure.Fields}
		}
		return empty, err
	}
	if status == http.StatusNotFound {
		return empty, scoring.ErrStatementsNotFound
	}
	r, err := scoring.DecodeStatements(bytes.NewReader(data))
	if err != nil {
		return empty, err
	}
	if r.BorrowerID != id {
		return empty, &scoring.Error{Status: 502, Code: "STATEMENTS_BORROWER_MISMATCH", Fields: []string{"service_b.borrower_id"}}
	}
	// Partial forms and unknown business denominators remain the responsibility
	// of scoring, which can request data rather than inventing zeros here.
	return r, nil
}

var _ scoring.StatementsSource = (*StatementsClient)(nil)
