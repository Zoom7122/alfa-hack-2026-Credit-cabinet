// Package scoring — клиент МС1 credit-engine (POST /api/v1/decisions).
package scoring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	body, err := json.Marshal(req)
	if err != nil {
		return decision.Response{}, fmt.Errorf("encode decision request: %w", err)
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/decisions", bytes.NewReader(body))
	if err != nil {
		return decision.Response{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(r)
	if err != nil {
		return decision.Response{}, fmt.Errorf("call credit-engine: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return decision.Response{}, fmt.Errorf("read credit-engine response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return decision.Response{}, fmt.Errorf("credit-engine: %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out decision.Response
	if err := json.Unmarshal(data, &out); err != nil {
		return decision.Response{}, fmt.Errorf("decode credit-engine response: %w", err)
	}
	out.ModelVersion = resp.Header.Get("X-Scoring-Model-Version")
	out.ModelSHA256 = resp.Header.Get("X-Scoring-Model-SHA256")
	return out, nil
}
