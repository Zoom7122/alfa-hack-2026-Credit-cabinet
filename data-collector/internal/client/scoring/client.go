// Package scoring — клиент МС1 credit-engine (POST /api/v1/decisions).
package scoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
)

type Client struct {
	baseURL string
	http    *http.Client
}

// New — клиент МС1; timeout — на весь вызов (МС1 сам ходит в Б и МС2).
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{Timeout: timeout}}
}

// Decide отправляет заявку на оценку. Бизнес-итоги (в том числе NEED_INFO и REJECTED) — 200;
// остальное — ошибка с телом ответа.
func (c *Client) Decide(ctx context.Context, req decision.Request) (decision.Response, error) {
	return c.DecideWithKey(ctx, req, "")
}

func (c *Client) DecideWithKey(ctx context.Context, req decision.Request, key string) (decision.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return decision.Response{}, fmt.Errorf("encode decision request: %w", err)
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/decisions", bytes.NewReader(body))
	if err != nil {
		return decision.Response{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	resp, err := c.http.Do(r)
	if err != nil {
		return decision.Response{}, &Error{Code: "NETWORK_ERROR", Cause: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return decision.Response{}, &Error{Code: "RESPONSE_READ_FAILED", Cause: err}
	}
	if resp.StatusCode != http.StatusOK {
		var problem struct {
			Code string `json:"code"`
		}
		json.Unmarshal(data, &problem)
		wait := time.Duration(0)
		if n, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && n > 0 {
			wait = time.Duration(min(n, 86400)) * time.Second
		} else if date, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil && time.Until(date) > 0 {
			wait = min(time.Until(date), 24*time.Hour)
		}
		return decision.Response{}, &Error{Status: resp.StatusCode, Code: problem.Code, Wait: wait}
	}
	if len(data) > 1<<20 {
		return decision.Response{}, &Error{Status: 200, Code: "INVALID_RESPONSE"}
	}
	var out decision.Response
	if err := json.Unmarshal(data, &out); err != nil {
		return decision.Response{}, &Error{Status: 200, Code: "INVALID_RESPONSE"}
	}
	if out.ApplicationID != req.ApplicationID || (out.Status != "APPROVED" && out.Status != "REJECTED" && out.Status != "UNDER_REVIEW" && out.Status != "NEED_INFO") {
		return decision.Response{}, &Error{Status: 200, Code: "INVALID_RESPONSE"}
	}
	if out.Status == "APPROVED" && (out.Terms == nil || !out.TermsConfirmed || out.LoanID == nil || out.MonthlyPaymentRUB == nil) {
		return decision.Response{}, &Error{Status: 200, Code: "INVALID_RESPONSE"}
	}
	return out, nil
}

// Error exposes retry classification without putting response bodies in logs.
type Error struct {
	Status int
	Code   string
	Wait   time.Duration
	Cause  error
}

func (e *Error) Error() string {
	return fmt.Sprintf("credit-engine status=%d code=%s", e.Status, e.Code)
}
func (e *Error) Unwrap() error { return e.Cause }
func (e *Error) Retryable() bool {
	return e.Status == 0 || e.Status == 429 || e.Status >= 500 || e.Status == 409 && e.Code == "OPERATION_IN_PROGRESS"
}
func (e *Error) RetryAfter() time.Duration { return e.Wait }
func (e *Error) ErrorCode() string {
	if e.Code != "" {
		return e.Code
	}
	return fmt.Sprintf("HTTP_%d", e.Status)
}
