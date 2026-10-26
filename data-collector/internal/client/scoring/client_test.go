package scoring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
)

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
	response, err := New(server.URL, time.Second).Decide(context.Background(), decision.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if response.ModelVersion != "ip-live-v3" || response.ModelSHA256 != "0123456789abcdef" {
		t.Fatal("model metadata not retained for assessment details", response)
	}
}
