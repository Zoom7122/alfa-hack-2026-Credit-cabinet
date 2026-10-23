// Package mockapi — HTTP-клиент ко всем внешним системам
// (локально и в демо это эмулятор mock-api). Ответы не разбирает: складывает
// их как есть по ключам запросов, чтобы сохранить сырыми в raw_collections.
package mockapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

const (
	maxBodySize = 10 << 20 // 10 МБ: выписка за год бывает большой
	maxPages    = 20       // страниц транзакций и арбитражных дел за один сбор
	maxErrors   = 3        // ошибок в Reason системы, остальные — числом
)

// Client опрашивает внешние системы. Безопасен для параллельного использования.
type Client struct {
	baseURL string
	http    *http.Client
	ownBIK  string // БИК нашего банка: блокировки ФНС по нашим счетам
	now     func() time.Time
}

// New создаёт клиент. baseURL — корень mock-api, например http://mock-api:8090;
// timeout — на один HTTP-запрос; ownBankBIK — БИК нашего банка.
func New(baseURL string, timeout time.Duration, ownBankBIK string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
		ownBIK:  ownBankBIK,
		now:     time.Now,
	}
}

// step собирает одну систему: делает запросы через run и кладёт ответы в r.put.
type step func(ctx context.Context, r *run, in domain.CollectInput)

// CollectAll опрашивает все системы параллельно. Исключение — hh.ru: ему нужно
// название компании из ЕГРЮЛ, поэтому он идёт после остальных.
// Отказ системы не роняет сбор: он виден в её SystemResult.
func (c *Client) CollectAll(ctx context.Context, requestID string, in domain.CollectInput) []domain.SystemResult {
	steps := map[domain.System]step{
		domain.SystemCoreBanking:    c.coreBanking,
		domain.SystemFNS:            c.fns,
		domain.SystemBKI:            c.bki,
		domain.SystemCompliance:     c.compliance,
		domain.SystemFSSP:           c.fssp,
		domain.SystemKAD:            c.kad,
		domain.SystemFedresurs:      c.fedresurs,
		domain.SystemM4D:            c.m4d,
		domain.SystemTaxReports:     c.taxReports,
		domain.SystemPledgeRegistry: c.pledgeRegistry,
		domain.SystemEGRN:           c.egrn,
		domain.SystemGosuslugi:      c.gosuslugi,
		domain.SystemOpenBanking:    c.openBanking,
		domain.SystemMarketplaces:   c.marketplaces,
		domain.SystemOFD:            c.ofd,
		domain.SystemZakupki:        c.zakupki,
		domain.SystemSignals:        c.signals,
		domain.SystemCBR:            c.cbr,
	}

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make(map[domain.System]domain.SystemResult, len(domain.Systems))
	)
	for sys, st := range steps {
		wg.Go(func() {
			res := c.collect(ctx, sys, requestID, in, st)
			mu.Lock()
			results[sys] = res
			mu.Unlock()
		})
	}
	wg.Wait()

	name := companyName(results[domain.SystemFNS].Data)
	results[domain.SystemHH] = c.collect(ctx, domain.SystemHH, requestID, in,
		func(ctx context.Context, r *run, _ domain.CollectInput) { c.hh(ctx, r, name) })

	out := make([]domain.SystemResult, 0, len(domain.Systems))
	for _, sys := range domain.Systems {
		out = append(out, results[sys])
	}
	return out
}

// collect выполняет шаг одной системы и подводит итог по её запросам.
func (c *Client) collect(ctx context.Context, sys domain.System, requestID string, in domain.CollectInput, st step) domain.SystemResult {
	start := time.Now()
	r := &run{c: c, requestID: requestID, data: map[string]any{}}
	st(ctx, r, in)

	res := domain.SystemResult{System: sys, Duration: time.Since(start), Calls: r.calls}
	ok := len(r.calls) - res.FailedCalls()
	switch {
	case len(r.calls) == 0:
		res.State = domain.CollectSkipped
		res.Reason = strings.Join(r.notes, "; ")
	case ok == len(r.calls):
		res.State = domain.CollectDone
		res.Reason = strings.Join(r.notes, "; ")
	case ok == 0:
		res.State = domain.CollectFailed
		res.Reason = r.errorSummary()
	default:
		res.State = domain.CollectPartial
		res.Reason = strings.Join(append([]string{r.errorSummary()}, r.notes...), "; ")
	}
	if ok > 0 {
		res.Data = marshal(r.data)
	}
	return res
}

// run копит запросы и ответы одной системы. Не делится между горутинами.
type run struct {
	c         *Client
	requestID string
	data      map[string]any // ключ → json.RawMessage, строка или вложенный map
	calls     []domain.Call
	notes     []string // что пропущено внутри системы и почему
}

// get — GET с JSON-ответом.
func (r *run) get(ctx context.Context, key, path string, q url.Values, h http.Header) (json.RawMessage, bool) {
	return r.jsonCall(ctx, key, http.MethodGet, path, q, h, nil, "")
}

// postJSON — POST с JSON-телом и JSON-ответом.
func (r *run) postJSON(ctx context.Context, key, path string, body any, h http.Header) (json.RawMessage, bool) {
	payload, err := json.Marshal(body)
	if err != nil {
		r.calls = append(r.calls, domain.Call{Key: key, Method: http.MethodPost, Path: path, Error: "encode body: " + err.Error()})
		return nil, false
	}
	return r.jsonCall(ctx, key, http.MethodPost, path, nil, h, payload, "application/json")
}

// postForm — POST формы (так устроены сервисы ФНС).
func (r *run) postForm(ctx context.Context, key, path string, form url.Values) (json.RawMessage, bool) {
	return r.jsonCall(ctx, key, http.MethodPost, path, nil, nil, []byte(form.Encode()), "application/x-www-form-urlencoded")
}

func (r *run) jsonCall(ctx context.Context, key, method, path string, q url.Values, h http.Header,
	payload []byte, contentType string) (json.RawMessage, bool) {
	body, ok := r.do(ctx, key, method, path, q, h, payload, contentType)
	if !ok {
		return nil, false
	}
	if !json.Valid(body) {
		r.failLast("ответ не JSON")
		return nil, false
	}
	return body, true
}

// do выполняет запрос и записывает его в r.calls. Успех — только ответ 2xx.
func (r *run) do(ctx context.Context, key, method, path string, q url.Values, h http.Header,
	payload []byte, contentType string) ([]byte, bool) {
	target := path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	call := domain.Call{Key: key, Method: method, Path: target}
	start := time.Now()
	defer func() {
		call.Duration = time.Since(start)
		r.calls = append(r.calls, call)
	}()

	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.c.baseURL+target, body)
	if err != nil {
		call.Error = "build request: " + err.Error()
		return nil, false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Request-Id", r.requestID)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, vs := range h {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}

	resp, err := r.c.http.Do(req)
	if err != nil {
		call.Error = err.Error()
		return nil, false
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	call.Status = resp.StatusCode
	switch {
	case err != nil:
		call.Error = "read body: " + err.Error()
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		call.Error = describe(resp.StatusCode, raw)
	}
	return raw, call.Error == ""
}

// failLast помечает последний запрос неуспешным: ответ 2xx, но пользоваться им нельзя.
func (r *run) failLast(msg string) {
	if n := len(r.calls); n > 0 {
		r.calls[n-1].Error = msg
	}
}

// skip записывает, что часть данных системы не запрашивалась и почему.
func (r *run) skip(note string) { r.notes = append(r.notes, note) }

// put кладёт ответ в данные системы по пути ключей: put(raw, "open_banking", bik, "accounts").
// nil-ответ (запрос не удался) кладётся как null: набор ключей не зависит от сбоев.
func (r *run) put(v any, keys ...string) {
	m := r.data
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[keys[len(keys)-1]] = v
}

func (r *run) errorSummary() string {
	var errs []string
	for _, c := range r.calls {
		if !c.OK() {
			errs = append(errs, c.Key+": "+c.Error)
		}
	}
	if len(errs) > maxErrors {
		errs = append(errs[:maxErrors], fmt.Sprintf("и ещё %d", len(errs)-maxErrors))
	}
	return strings.Join(errs, "; ")
}

// describe превращает ответ не-2xx в текст ошибки; у mock-api ошибка — {"error":{"code","message"}}.
func describe(status int, body []byte) string {
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Code != "" {
		return fmt.Sprintf("%d %s: %s", status, e.Error.Code, e.Error.Message)
	}
	return fmt.Sprintf("status %d", status)
}

// marshal сериализует данные системы. Ответы (json.RawMessage) вставляются как есть,
// ключи упорядочены, HTML-символы не экранируются.
func marshal(v any) json.RawMessage {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil // в data только валидный JSON и строки, ошибки не бывает
	}
	return bytes.TrimRight(buf.Bytes(), "\n")
}

// uniq убирает пустые и повторяющиеся значения, сохраняя порядок.
func uniq(values ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// sortedKeys — ключи map по алфавиту, чтобы порядок запросов был стабильным.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
