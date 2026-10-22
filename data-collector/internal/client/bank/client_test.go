package bank_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/client/bank"
)

// Юнит-тесты проверяют поведение клиента (URL, статусы, разбор JSON),
// а не данные банка. Данные отдаёт mock-api, их проверяет client_integration_test.go.

// server отвечает status и body на любой запрос и запоминает путь последнего.
func server(t *testing.T, status int, body string) (*httptest.Server, *string) {
	t.Helper()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &gotPath
}

func TestClientPaths(t *testing.T) {
	srv, gotPath := server(t, http.StatusOK, `{}`)
	c := bank.New(srv.URL+"/bank/v1/", time.Second) // лишний слэш в конце не ломает URL
	ctx := context.Background()
	const inn = "7701234567"

	calls := map[string]func() error{
		"accounts":       func() error { _, err := c.Accounts(ctx, inn); return err },
		"credits":        func() error { _, err := c.Credits(ctx, inn); return err },
		"acquiring":      func() error { _, err := c.Acquiring(ctx, inn); return err },
		"salary-project": func() error { _, err := c.SalaryProject(ctx, inn); return err },
		"card-index":     func() error { _, err := c.CardIndex(ctx, inn); return err },
	}
	for resource, call := range calls {
		if err := call(); err != nil {
			t.Fatalf("%s: %v", resource, err)
		}
		if want := "/bank/v1/companies/" + inn + "/" + resource; *gotPath != want {
			t.Fatalf("path = %q, want %q", *gotPath, want)
		}
	}
}

func TestClientDecodes(t *testing.T) {
	srv, _ := server(t, http.StatusOK, `{"summary":{"activeContracts":2,"totalDebt":"3800000.00"},"unknownField":1}`)
	c := bank.New(srv.URL, time.Second)

	cr, err := c.Credits(context.Background(), "7701234567")
	if err != nil {
		t.Fatal(err)
	}
	if cr.Summary.ActiveContracts != 2 || cr.Summary.TotalDebt != "3800000.00" {
		t.Fatalf("summary = %+v", cr.Summary)
	}
}

func TestClientErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{name: "non-200", status: http.StatusServiceUnavailable, body: `{"error":"down"}`, wantErr: "status 503"},
		{name: "broken json", status: http.StatusOK, body: `{"summary":`, wantErr: "decode response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := server(t, tt.status, tt.body)
			c := bank.New(srv.URL, time.Second)

			_, err := c.Credits(context.Background(), "7701234567")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestClientUnreachable(t *testing.T) {
	srv, _ := server(t, http.StatusOK, `{}`)
	srv.Close()
	c := bank.New(srv.URL, time.Second)

	if _, err := c.Accounts(context.Background(), "7701234567"); err == nil {
		t.Fatal("want error for closed server")
	}
}
