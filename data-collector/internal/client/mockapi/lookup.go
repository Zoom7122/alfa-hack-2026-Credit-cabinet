package mockapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// ErrRegistryUnavailable — ЕГРЮЛ не ответил: сеть, таймаут, ошибка сервера или капча.
var ErrRegistryUnavailable = errors.New("registry unavailable")

// LookupCompany — реквизиты компании по ИНН из ЕГРЮЛ/ЕГРИП, синхронно (подсказка на шаге «Компания»).
// false — компании в реестре нет. Ошибка оборачивает ErrRegistryUnavailable с причиной.
func (c *Client) LookupCompany(ctx context.Context, inn string) (domain.CompanyCard, bool, error) {
	r := &run{c: c, requestID: "lookup-" + inn, data: map[string]any{}}
	raw, ok := r.postForm(ctx, "egrul_search", "/fns/egrul/", url.Values{"query": {inn}})
	if !ok {
		return domain.CompanyCard{}, false, r.lookupError()
	}
	var tok struct {
		T               string `json:"t"`
		CaptchaRequired bool   `json:"captchaRequired"`
	}
	if err := json.Unmarshal(raw, &tok); err != nil {
		return domain.CompanyCard{}, false, fmt.Errorf("%w: decode search: %v", ErrRegistryUnavailable, err)
	}
	if tok.CaptchaRequired {
		return domain.CompanyCard{}, false, fmt.Errorf("%w: captcha required", ErrRegistryUnavailable)
	}
	if tok.T == "" {
		return domain.CompanyCard{}, false, nil
	}
	res, ok := r.get(ctx, "egrul", "/fns/egrul/search-result/"+url.PathEscape(tok.T), nil, nil)
	if !ok {
		return domain.CompanyCard{}, false, r.lookupError()
	}
	card, found := c.CompanyCard(marshal(map[string]any{"egrul": res}))
	return card, found, nil
}

// lookupError — ErrRegistryUnavailable с причиной последнего неудачного запроса.
func (r *run) lookupError() error {
	if n := len(r.calls); n > 0 && r.calls[n-1].Error != "" {
		return fmt.Errorf("%w: %s %s: %s", ErrRegistryUnavailable, r.calls[n-1].Method, r.calls[n-1].Path, r.calls[n-1].Error)
	}
	return ErrRegistryUnavailable
}
