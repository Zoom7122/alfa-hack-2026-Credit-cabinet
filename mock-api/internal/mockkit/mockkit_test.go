package mockkit_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
)

func newRouter(captcha gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(mockkit.Middleware(50*time.Millisecond, captcha))
	r.GET("/ok", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return r
}

func do(r http.Handler, fault, requestID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	if fault != "" {
		req.Header.Set("X-Mock-Fault", fault)
	}
	if requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestFaults(t *testing.T) {
	r := newRouter(nil)
	cases := []struct {
		fault  string
		status int
		code   string
	}{
		{"", http.StatusOK, ""},
		{"500", http.StatusInternalServerError, mockkit.CodeInternalError},
		{"503", http.StatusServiceUnavailable, mockkit.CodeServiceUnavailable},
		{"timeout", http.StatusGatewayTimeout, mockkit.CodeTimeout},
		{"captcha", http.StatusForbidden, mockkit.CodeCaptchaRequired},
		{"boom", http.StatusBadRequest, mockkit.CodeBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.fault, func(t *testing.T) {
			w := do(r, tc.fault, "")
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			if tc.code != "" && !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("body = %s, want code %s", w.Body, tc.code)
			}
		})
	}
	if got := do(r, "503", "").Header().Get("Retry-After"); got == "" {
		t.Fatal("503 without Retry-After")
	}
}

func TestCustomCaptcha(t *testing.T) {
	r := newRouter(func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"captchaRequired": true}) })
	w := do(r, "captcha", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"captchaRequired":true`) {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
}

func TestRequestIDEcho(t *testing.T) {
	r := newRouter(nil)
	for _, fault := range []string{"", "500"} {
		if got := do(r, fault, "req-42").Header().Get("X-Request-Id"); got != "req-42" {
			t.Fatalf("fault %q: X-Request-Id = %q", fault, got)
		}
	}
}

func TestPaginate(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	if got := mockkit.Paginate(items, 2, 2); len(got) != 2 || got[0] != 3 {
		t.Fatalf("page 2 = %v", got)
	}
	if got := mockkit.Paginate(items, 3, 2); len(got) != 1 || got[0] != 5 {
		t.Fatalf("page 3 = %v", got)
	}
	if got := mockkit.Paginate(items, 4, 2); got == nil || len(got) != 0 {
		t.Fatalf("page 4 = %v, want empty non-nil", got)
	}
}
