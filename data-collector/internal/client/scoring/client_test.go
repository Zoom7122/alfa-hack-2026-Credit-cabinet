package scoring

import (
	"context"
	"errors"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDispatchHTTPClassification(t *testing.T) {
	for _, tc := range []struct {
		status     int
		code, body string
		retry      bool
	}{{429, "THROTTLED", "", true}, {503, "DOWN", "", true}, {409, "OPERATION_IN_PROGRESS", "", true}, {409, "IDEMPOTENCY_CONFLICT", "", false}, {422, "INVALID_VALUE", "", false}, {200, "", `{"application_id":"wrong","status":"APPROVED"}`, false}, {200, "", `{`, false}} {
		t.Run(tc.code+http.StatusText(tc.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Idempotency-Key") != "job" {
					t.Error("missing stable key")
				}
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(tc.status)
				if tc.body != "" {
					w.Write([]byte(tc.body))
				} else {
					w.Write([]byte(`{"code":"` + tc.code + `"}`))
				}
			}))
			defer server.Close()
			_, err := New(server.URL, time.Second).DecideWithKey(context.Background(), decision.Request{ApplicationID: "app"}, "job")
			var failure *Error
			if !errors.As(err, &failure) || failure.Retryable() != tc.retry {
				t.Fatalf("%v", err)
			}
			if tc.status != 200 && failure.RetryAfter() != 7*time.Second {
				t.Fatal("Retry-After ignored")
			}
		})
	}
}
