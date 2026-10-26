package app_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/app"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

func TestAdminConstructionNeverConnectsOrMigrates(t *testing.T) {
	cfg, err := config.LoadAdmin(environment(map[string]string{"ADMIN_DATABASE_URL": "postgres://unused.invalid/test", "ADMIN_USERNAME": "editor", "ADMIN_PASSWORD_HASH": "$2a$10$EWuECD9zM/WTJv1IbDlfG.LUvUkOWslI3qcfYVvlEqmZkq/jeYQiK", "ADMIN_EVENTS_TOKEN": "fictional-ms2-service-token-1234567890", "ADMIN_PUBLIC_ORIGIN": "https://admin.example"}))
	if err != nil {
		t.Fatal(err)
	}
	a, err := app.NewAdmin(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.Database.Stats().OpenConnections != 0 || a.Server.Addr != ":8084" || a.Server.ReadTimeout != 45*time.Second || a.Repository == nil || a.Access == nil {
		t.Fatal("invalid dependency assembly")
	}
	w := httptest.NewRecorder()
	a.Server.Handler.ServeHTTP(w, httptest.NewRequest("GET", "https://admin.example/healthz", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Initialize(ctx); err == nil || err.Error() != "admin database connection failed" {
		t.Fatal("canceled startup did not fail safely", err)
	}
	cfg.PasswordHash = "bad"
	if _, err := app.NewAdmin(cfg, nil); err == nil {
		t.Fatal("invalid configuration accepted")
	}
}
