// Package clients implements the outgoing REST contracts for MS1 and MS2.
// Constructors do not contact dependencies; requests inherit caller context.
package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

// Error is a technical failure, never a credit decision. Cause is available to
// errors.Is/As, but its URL and dependency response body are not printed.
type Error struct {
	Status int
	Code   string
	Fields []string
	cause  error
}

func (e *Error) Error() string { return fmt.Sprintf("REST %s (%d)", e.Code, e.Status) }
func (e *Error) Unwrap() error { return e.cause }

type transport struct {
	base    url.URL
	http    *http.Client
	headers http.Header
}

func newTransport(base string, c *http.Client) (*transport, error) {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
		return nil, fmt.Errorf("REST base URL must be absolute HTTP(S), without credentials, query or fragment")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("REST base URL port must be between 1 and 65535")
		}
	}
	if c == nil || c.Timeout <= 0 {
		return nil, fmt.Errorf("REST client requires a positive timeout")
	}
	return &transport{base: *u, http: c}, nil
}

func (t *transport) endpoint(parts []string, query url.Values) string {
	u := t.base
	escapedBase := strings.TrimRight(u.EscapedPath(), "/")
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = escapedBase
	for _, part := range parts {
		escaped := url.PathEscape(part)
		if part == "." {
			escaped = "%2E"
		} else if part == ".." {
			escaped = "%2E%2E"
		}
		u.Path += "/" + part
		u.RawPath += "/" + escaped
	}
	u.RawQuery = query.Encode()
	return u.String()
}

// special is 404 for B or 409 for MS2. Those statuses have explicit meanings
// even without an error body; all other unexpected statuses are dependency errors.
func (t *transport) exchange(ctx context.Context, method string, parts []string, query url.Values, payload any, want, special int) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, &Error{Status: 500, Code: "INVALID_OUTGOING_JSON", cause: err}
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, t.endpoint(parts, query), body)
	if err != nil {
		return 0, nil, &Error{Status: 500, Code: "INVALID_OUTGOING_REQUEST", cause: err}
	}
	req.Header.Set("Accept", "application/json")
	for key, values := range t.headers {
		req.Header[key] = append([]string(nil), values...)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Preserve injected transport/timeout and connection reuse. A redirect is an
	// unexpected contract response; never send a POST body to another location.
	c := *t.http
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := c.Do(req)
	if err != nil {
		return 0, nil, dependencyError("DEPENDENCY_UNAVAILABLE", err)
	}
	defer response.Body.Close()
	status := response.StatusCode
	if status == special && special == http.StatusNotFound {
		return status, nil, nil
	}
	if status != want && status != special {
		return status, nil, &Error{Status: 502, Code: "UNEXPECTED_DEPENDENCY_STATUS"}
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		if status == http.StatusConflict && status == special {
			return status, nil, nil
		} // The conflict is already established by HTTP.
		return status, nil, dependencyError("DEPENDENCY_READ_FAILED", err)
	}
	return status, data, nil
}
func dependencyError(code string, err error) *Error {
	status := 502
	var timeout interface{ Timeout() bool }
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
		status = 504
	}
	return &Error{Status: status, Code: code, cause: err}
}
func invalidResponse(code string) error { return &Error{Status: 502, Code: code} }

// The map records JSON key presence only. All financial values are decoded into
// named DTOs; no business data or statement lines are computed through maps.
func decodeObject(data []byte, out any, required, nullable []string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return invalidResponse("INVALID_DEPENDENCY_JSON")
	}
	if err := json.Unmarshal(data, out); err != nil {
		return invalidResponse("INVALID_DEPENDENCY_JSON")
	}
	for _, key := range required {
		value, exists := fields[key]
		if !exists {
			return invalidResponse("INCOMPLETE_DEPENDENCY_RESPONSE")
		}
		allowNull := false
		for _, name := range nullable {
			allowNull = allowNull || name == key
		}
		if !allowNull && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalidResponse("INCOMPLETE_DEPENDENCY_RESPONSE")
		}
	}
	return nil
}
func conflict(data []byte) error {
	e := &Error{Status: 409, Code: "APPLICATION_CONFLICT", Fields: []string{}}
	var p contracts.Problem
	if json.Unmarshal(data, &p) == nil && p.Status == 409 && strings.TrimSpace(p.Code) != "" {
		e.Code = p.Code
		e.Fields = p.Fields
	}
	return e
}
