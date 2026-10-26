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

func TestDecisionCapturesActualModelSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/decisions" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Scoring-Model-Version", "ip-live-v3")
		w.Header().Set("X-Scoring-Model-SHA256", "0123456789abcdef")
		_, _ = w.Write([]byte(`{"application_id":"app","status":"NEED_INFO","reasons":[]}`))
	}))
	defer server.Close()
	response, err := New(server.URL, time.Second).Decide(context.Background(), decision.Request{ApplicationID: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if response.ModelVersion != "ip-live-v3" || response.ModelSHA256 != "0123456789abcdef" {
		t.Fatal("model metadata not retained for assessment details", response)
	}
}
