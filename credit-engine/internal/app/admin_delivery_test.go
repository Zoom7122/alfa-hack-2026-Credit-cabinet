package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/app"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/gin-gonic/gin"
)

type unusedSessions struct{}

func (unusedSessions) CreateSession(context.Context, adminstore.Session, int) error {
	return errors.New("browser sessions must not be accessed")
}
func (unusedSessions) GetSession(context.Context, string, time.Time) (adminstore.Session, error) {
	return adminstore.Session{}, errors.New("browser sessions must not be accessed")
}
func (unusedSessions) DeleteSession(context.Context, string) error {
	return errors.New("browser sessions must not be accessed")
}

func TestAssembledMS2DeliversServiceCredentialSeparately(t *testing.T) {
	const token = "fictional-ms2-service-token-1234567890"
	adminCfg, err := config.LoadAdmin(environment(map[string]string{"ADMIN_DATABASE_URL": "postgres://unused.invalid/test", "ADMIN_USERNAME": "editor", "ADMIN_PASSWORD_HASH": "$2a$10$EWuECD9zM/WTJv1IbDlfG.LUvUkOWslI3qcfYVvlEqmZkq/jeYQiK", "ADMIN_EVENTS_TOKEN": token, "ADMIN_PUBLIC_ORIGIN": "https://admin.example"}))
	if err != nil {
		t.Fatal(err)
	}
	access, err := adminauth.New(adminCfg, unusedSessions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := httpapi.NewAdminRouter(access, time.Second)
	accepted := 0
	// The event handler is test-only; durable production acceptance is step 20.
	r.POST("/api/v1/credit-events", access.Service(), func(c *gin.Context) {
		var event contracts.AdminEvent
		if err := json.NewDecoder(c.Request.Body).Decode(&event); err != nil {
			t.Error(err)
			c.Status(400)
			return
		}
		accepted++
		c.JSON(202, contracts.EventAck{EventID: event.EventID, Accepted: true})
	})
	for _, provided := range []string{token, "another-fictional-service-token-1234567890", ""} {
		cfg, err := config.LoadBank(environment(map[string]string{"ADMIN_URL": "https://admin.example", "ADMIN_EVENTS_TOKEN": provided, "POLICY_FILE": "../../config/policy.json", "BANK_STATE_FILE": "../../config/bank.default.json"}))
		if err != nil {
			t.Fatal(err)
		}
		ms2, err := app.NewBank(cfg, nil)
		if err != nil {
			t.Fatal(err)
		}
		ms2.AdminHTTP.Transport = restRoundTripper(func(request *http.Request) (*http.Response, error) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			return w.Result(), nil
		})
		ack, err := ms2.AdminClient.Notify(context.Background(), contracts.AdminEvent{EventID: "fictional-auth-delivery"})
		if provided == token {
			if err != nil || !ack.Accepted {
				t.Fatal(ack, err)
			}
		} else if err == nil || strings.Contains(err.Error(), token) {
			t.Fatal("unauthorized service delivery accepted or secret exposed", err)
		}
	}
	if accepted != 1 {
		t.Fatal("service and browser authentication mixed", accepted)
	}
}
