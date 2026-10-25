package clients

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
)

func TestAdminServiceTokenDeliveryAndRedirectRefusal(t *testing.T) {
	const token = "fictional-ms2-service-token-1234567890"
	for _, status := range []int{202, 401, 307} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			httpClient := httpClient(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "Bearer "+token || r.URL.Path != "/api/v1/credit-events" {
					t.Fatal("service credential not attached correctly")
				}
				response := response(status, `{"event_id":"test-event","accepted":true}`)
				response.Header.Set("Location", "https://attacker.invalid/steal")
				return response, nil
			})
			client, err := NewAdminWithToken("https://admin.invalid", token, httpClient)
			if err != nil {
				t.Fatal(err)
			}
			ack, err := client.Notify(context.Background(), contracts.AdminEvent{EventID: "test-event"})
			if calls != 1 {
				t.Fatal("unexpected retry or redirect", calls)
			}
			if status == 202 {
				if err != nil || !ack.Accepted {
					t.Fatal(ack, err)
				}
			} else {
				var rest *Error
				if !errors.As(err, &rest) || rest.Status != 502 || strings.Contains(err.Error(), token) {
					t.Fatal("unsafe status handling", err)
				}
			}
		})
	}
	for _, token := range []string{"", "short-secret", strings.Repeat("x", 32) + "\n"} {
		if _, err := NewAdminWithToken("https://admin.invalid", token, httpClient(nil)); err == nil || strings.Contains(err.Error(), token) && token != "" {
			t.Fatal("unsafe invalid token handling", err)
		}
	}
}
