// Package bank — HTTP-клиент к внутренним системам банка
// (локально и в демо это эмулятор mock-api/Bank).
package bank

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const maxBodySize = 10 << 20 // 10 МБ: выписка за длинный период бывает большой

type Client struct {
	baseURL string
	http    *http.Client
}

// New создаёт клиент. baseURL — корень API банка, например http://mock-api:8090/bank/v1.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
	}
}

func (c *Client) Accounts(ctx context.Context, inn string) (domain.AccountsReport, error) {
	return get[domain.AccountsReport](ctx, c, inn, "accounts")
}

func (c *Client) Credits(ctx context.Context, inn string) (domain.CreditReport, error) {
	return get[domain.CreditReport](ctx, c, inn, "credits")
}

func (c *Client) Acquiring(ctx context.Context, inn string) (domain.AcquiringReport, error) {
	return get[domain.AcquiringReport](ctx, c, inn, "acquiring")
}

func (c *Client) SalaryProject(ctx context.Context, inn string) (domain.SalaryProjectReport, error) {
	return get[domain.SalaryProjectReport](ctx, c, inn, "salary-project")
}

func (c *Client) CardIndex(ctx context.Context, inn string) (domain.CardIndexReport, error) {
	return get[domain.CardIndexReport](ctx, c, inn, "card-index")
}

// get запрашивает GET {baseURL}/companies/{inn}/{resource} и разбирает JSON в T.
func get[T any](ctx context.Context, c *Client, inn, resource string) (T, error) {
	var out T
	u := fmt.Sprintf("%s/companies/%s/%s", c.baseURL, url.PathEscape(inn), resource)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return out, fmt.Errorf("bank %s: build request: %w", resource, err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return out, fmt.Errorf("bank %s: %w", resource, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return out, fmt.Errorf("bank %s: status %d: %s", resource, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodySize)).Decode(&out); err != nil {
		return out, fmt.Errorf("bank %s: decode response: %w", resource, err)
	}
	return out, nil
}
