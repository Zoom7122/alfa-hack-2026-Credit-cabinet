package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

type BankClient struct{ transport *transport }

func NewBank(base string, c *http.Client) (*BankClient, error) {
	t, err := newTransport(base, c)
	if err != nil {
		return nil, err
	}
	return &BankClient{transport: t}, nil
}
func (c *BankClient) Check(ctx context.Context, r contracts.BankCheckRequest) (contracts.BankCheckResponse, error) {
	var result contracts.BankCheckResponse
	status, data, err := c.transport.exchange(ctx, http.MethodPost, []string{"api", "v1", "bank", "check-and-issue"}, nil, r, http.StatusOK, http.StatusConflict)
	if err != nil {
		return result, err
	}
	if status == http.StatusConflict {
		return result, conflict(data)
	}
	if err = decodeObject(data, &result, []string{"application_id", "verdict", "loan_id", "checks", "event_id", "notification_status"}, []string{"loan_id"}); err != nil {
		return contracts.BankCheckResponse{}, err
	}
	if result.ApplicationID == "" || result.ApplicationID != r.ApplicationID || !validNotification(result.NotificationStatus) || len(result.Checks) == 0 {
		return contracts.BankCheckResponse{}, invalidResponse("INVALID_BANK_RESPONSE")
	}
	var wire struct {
		Checks []json.RawMessage `json:"checks"`
	}
	if err = json.Unmarshal(data, &wire); err != nil {
		return contracts.BankCheckResponse{}, invalidResponse("INVALID_BANK_RESPONSE")
	}
	failed := false
	seen := make(map[string]bool, len(result.Checks))
	for i, check := range result.Checks {
		var decoded contracts.CheckResult
		if err = decodeObject(wire.Checks[i], &decoded, []string{"code", "before", "after", "operator", "limit", "passed", "detail"}, []string{"before", "after", "operator", "limit"}); err != nil {
			return contracts.BankCheckResponse{}, err
		}
		if strings.TrimSpace(check.Code) == "" || strings.TrimSpace(check.Detail) == "" || seen[check.Code] {
			return contracts.BankCheckResponse{}, invalidResponse("INVALID_BANK_CHECK")
		}
		seen[check.Code] = true
		failed = failed || !check.Passed
	}
	kind := contracts.EventTypeBankReview
	switch result.Verdict {
	case contracts.BankVerdictIssued:
		kind = contracts.EventTypeLoanIssued
		if result.LoanID == nil || *result.LoanID != "loan-"+r.ApplicationID || failed {
			return contracts.BankCheckResponse{}, invalidResponse("INVALID_BANK_RESPONSE")
		}
	case contracts.BankVerdictBankReview:
		if result.LoanID != nil || !failed {
			return contracts.BankCheckResponse{}, invalidResponse("INVALID_BANK_RESPONSE")
		}
	default:
		return contracts.BankCheckResponse{}, invalidResponse("INVALID_BANK_VERDICT")
	}
	if result.EventID != r.ApplicationID+":"+string(kind) {
		return contracts.BankCheckResponse{}, invalidResponse("BANK_EVENT_MISMATCH")
	}
	return result, nil
}
func (c *BankClient) ManualReview(ctx context.Context, r contracts.ManualReviewRequest) (contracts.ManualReviewResponse, error) {
	var result contracts.ManualReviewResponse
	status, data, err := c.transport.exchange(ctx, http.MethodPost, []string{"api", "v1", "bank", "manual-reviews"}, nil, r, http.StatusAccepted, http.StatusConflict)
	if err != nil {
		return result, err
	}
	if status == http.StatusConflict {
		return result, conflict(data)
	}
	if err = decodeObject(data, &result, []string{"application_id", "event_id", "notification_status"}, nil); err != nil {
		return contracts.ManualReviewResponse{}, err
	}
	if result.ApplicationID == "" || result.ApplicationID != r.ApplicationID || result.EventID != r.ApplicationID+":"+string(contracts.EventTypeBorrowerReview) || !validNotification(result.NotificationStatus) {
		return contracts.ManualReviewResponse{}, invalidResponse("INVALID_MANUAL_REVIEW_RESPONSE")
	}
	return result, nil
}
func validNotification(status contracts.NotificationStatus) bool {
	return status == contracts.NotificationStatusDelivered || status == contracts.NotificationStatusPending
}
