package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/gin-gonic/gin"
)

func TestHealthHandler(t *testing.T) {
	router := httpapi.NewRouter()
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var body contracts.Health
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || body.Status != "ok" {
		t.Fatalf("health check failed: %d %s", response.Code, response.Body.String())
	}
}

func TestPanicRecoveryDoesNotExposeDetails(t *testing.T) {
	router := httpapi.NewRouter()
	router.GET("/panic", func(c *gin.Context) { panic("sensitive financial data") })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	var body contracts.Problem
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if response.Code != 500 || body.Code != "INTERNAL_ERROR" || body.Detail != "Internal server error" {
		t.Fatalf("unexpected problem: %+v", body)
	}
}

func TestServerConfigurationAndCancellation(t *testing.T) {
	server := httpapi.NewServer(":8082", 15*time.Second, httpapi.NewRouter())
	if server.Addr != ":8082" || server.WriteTimeout != 15*time.Second || server.ReadHeaderTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatalf("unexpected server configuration: %+v", server)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := httpapi.Serve(ctx, server); err != nil {
		t.Fatalf("cancelled startup must stop without binding a listener: %v", err)
	}
	// Invalid address fails before a socket is opened.
	server.Addr = "invalid-address"
	if err := httpapi.Serve(context.Background(), server); err == nil {
		t.Fatal("listener errors must propagate")
	}
}
