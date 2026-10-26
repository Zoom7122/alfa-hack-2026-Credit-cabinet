package httpapi

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedAdminUIAssetsAndSecurityHeaders(t *testing.T) {
	r := gin.New()
	RegisterAdminUI(r)
	root := httptest.NewRecorder()
	r.ServeHTTP(root, httptest.NewRequest("GET", "/", nil))
	if root.Code != 200 || !strings.Contains(root.Body.String(), `id="root"`) {
		t.Fatal("React entry not embedded", root.Code)
	}
	paths := []string{"/"}
	for _, match := range regexp.MustCompile(`(?:src|href)="(/assets/[^\"]+)"`).FindAllStringSubmatch(root.Body.String(), -1) {
		paths = append(paths, match[1])
	}
	if len(paths) < 3 {
		t.Fatal("built JavaScript and CSS must be present")
	}
	for _, path := range paths {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatal("invalid asset", path, w.Code)
		}
		csp := w.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-eval") {
			t.Fatal("unsafe script policy")
		}
	}
	for _, path := range []string{"/assets/AGENT.md", "/assets/index.html", "/assets/../AGENT.md", "/assets/app.js.map", "/assets/missing.js", "/assets/src/App.tsx"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code == 200 {
			t.Fatal("unexpected asset disclosed", path)
		}
	}
}
