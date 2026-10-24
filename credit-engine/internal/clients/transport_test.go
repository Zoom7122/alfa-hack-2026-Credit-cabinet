package clients

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type timeoutError struct{}

func (timeoutError) Error() string { return "read timed out" }
func (timeoutError) Timeout() bool { return true }

func TestClientConstructorsRejectInvalidSettings(t *testing.T) {
	for _, base := range []string{"", "/relative", "ftp://host", "http://", "http://u:p@host", "http://host?x=1", "http://host?", "http://host/#fragment", "http://host:0", "http://host:65536", "http://host:abc", "http://bad host"} {
		t.Run(base, func(t *testing.T) {
			c := httpClient(nil)
			if _, err := NewStatements(base, c); err == nil {
				t.Fatal("statements accepted URL")
			}
			if _, err := NewBank(base, c); err == nil {
				t.Fatal("bank accepted URL")
			}
			if _, err := NewAdmin(base, c); err == nil {
				t.Fatal("admin accepted URL")
			}
		})
	}
	for _, c := range []*http.Client{nil, {}, {Timeout: -time.Second}} {
		if _, err := NewAdmin("http://admin.invalid", c); err == nil {
			t.Fatal("unbounded timeout accepted")
		}
	}
}

func TestContextTimeoutAndConnectionFailures(t *testing.T) {
	f := deal(t)
	for _, mode := range []string{"client timeout", "caller deadline", "canceled", "connection", "transport timeout"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			h := httpClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "connection" {
					return nil, errors.New("connection failed: confidential URL/body")
				}
				if mode == "transport timeout" {
					return nil, timeoutError{}
				}
				if _, ok := r.Context().Deadline(); !ok {
					t.Error("deadline not propagated")
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})
			h.Timeout = 20 * time.Millisecond
			ctx := context.Background()
			cancel := func() {}
			if mode == "caller deadline" {
				ctx, cancel = context.WithTimeout(ctx, 5*time.Millisecond)
			} else if mode == "canceled" {
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			defer cancel()
			want := 504
			if mode == "canceled" || mode == "connection" {
				want = 502
			}
			a, _ := NewAdmin("http://admin.invalid", h)
			_, err := a.Notify(ctx, f.Expected.Event)
			status(t, err, want)
			if strings.Contains(err.Error(), "confidential") || strings.Contains(err.Error(), "admin.invalid") {
				t.Fatal("error exposed transport details")
			}
			b, _ := NewBank("http://bank.invalid", h)
			_, err = b.Check(ctx, f.Request)
			status(t, err, want)
			s, _ := NewStatements("http://b.invalid", h)
			_, err = s.Statements(ctx, "b", []int{2024, 2025})
			statementsStatus(t, err, want)
			if mode == "caller deadline" && calls > 3 {
				t.Fatal("automatic retry")
			}
		})
	}
}

func TestResponseBodyErrorsAndClosure(t *testing.T) {
	f := deal(t)
	for _, c := range []struct {
		name string
		err  error
		want int
	}{{"broken body", io.ErrUnexpectedEOF, 502}, {"deadline while reading", context.DeadlineExceeded, 504}, {"read timeout", timeoutError{}, 504}} {
		t.Run(c.name, func(t *testing.T) {
			var bodies []*trackedBody
			h := httpClient(func(*http.Request) (*http.Response, error) {
				body := &trackedBody{Reader: errorReader{c.err}}
				bodies = append(bodies, body)
				return &http.Response{StatusCode: 202, Header: make(http.Header), Body: body}, nil
			})
			a, _ := NewAdmin("http://admin.invalid", h)
			_, err := a.Notify(context.Background(), f.Expected.Event)
			status(t, err, c.want)
			for _, b := range bodies {
				if !b.closed {
					t.Fatal("response body not closed")
				}
			}
		})
	}
	for _, code := range []int{202, 200, 302, 500} {
		body := &trackedBody{Reader: strings.NewReader(`{"accepted":true,"event_id":` + jsonText(t, f.Expected.Event.EventID) + `}`)}
		h := httpClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Header: make(http.Header), Body: body}, nil
		})
		a, _ := NewAdmin("http://admin.invalid", h)
		_, _ = a.Notify(context.Background(), f.Expected.Event)
		if !body.closed {
			t.Fatalf("body not closed for %d", code)
		}
	}
	// Neither a missing-form body nor a broken conflict body changes the
	// explicit meaning of the already received 404/409 status.
	for _, code := range []int{404, 409} {
		body := &trackedBody{Reader: errorReader{io.ErrUnexpectedEOF}}
		h := httpClient(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Header: make(http.Header), Body: body}, nil
		})
		if code == 404 {
			s, _ := NewStatements("http://b.invalid", h)
			_, _ = s.Statements(context.Background(), "b", []int{2024, 2025})
		} else {
			b, _ := NewBank("http://bank.invalid", h)
			_, err := b.Check(context.Background(), f.Request)
			status(t, err, 409)
		}
		if !body.closed {
			t.Fatal("special response body not closed")
		}
	}
}

func TestRedirectsAreNotFollowedAndClientIsNotMutated(t *testing.T) {
	f := deal(t)
	for _, code := range []int{301, 302, 303, 307, 308} {
		calls := 0
		redirectPolicyCalls := 0
		h := httpClient(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: code, Header: http.Header{"Location": []string{"http://other.invalid/receiver"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})
		h.CheckRedirect = func(*http.Request, []*http.Request) error { redirectPolicyCalls++; return nil }
		a, _ := NewAdmin("http://admin.invalid", h)
		_, err := a.Notify(context.Background(), f.Expected.Event)
		status(t, err, 502)
		if calls != 1 || redirectPolicyCalls != 0 || h.CheckRedirect == nil {
			t.Fatal("redirect followed or caller client modified")
		}
	}
}

func TestContextValuesAndExactlyOneAttempt(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "trace")
	calls := 0
	h := httpClient(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Context().Value(key{}) != "trace" {
			t.Fatal("context lost")
		}
		return response(503, "unavailable"), nil
	})
	a, _ := NewAdmin("http://admin.invalid", h)
	_, err := a.Notify(ctx, contracts.AdminEvent{EventID: "a:LOAN_ISSUED"})
	status(t, err, 502)
	if calls != 1 {
		t.Fatal("client retried")
	}
}
