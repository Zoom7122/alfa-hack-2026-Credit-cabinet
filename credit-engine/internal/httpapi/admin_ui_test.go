package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestEmbeddedAdminUIAssetsAndSecurityHeaders(t *testing.T) {
	r := gin.New()
	RegisterAdminUI(r)
	for _, name := range []string{"forms.js", "model-edit.js", "editor.js", "rules-editor.js", "workbench.js", "audit.js"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/assets/"+name, nil))
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
			t.Fatal("editor module not served", name)
		}
	}
	for _, tc := range []struct{ path, content string }{{"/", "text/html"}, {"/assets/app.js", "text/javascript"}, {"/assets/api.js", "text/javascript"}, {"/assets/dom.js", "text/javascript"}, {"/assets/numbers.js", "text/javascript"}, {"/assets/styles.css", "text/css"}} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
			if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), tc.content) || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Frame-Options") != "DENY" {
				t.Fatal("invalid asset", w.Code, w.Header())
			}
			csp := w.Header().Get("Content-Security-Policy")
			if !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") || strings.Contains(csp, "unsafe-eval") {
				t.Fatal("unsafe script policy")
			}
		})
	}
	for _, path := range []string{"/assets/AGENT.md", "/assets/agent_log.md", "/assets/index.html", "/assets/../AGENT.md", "/assets/app.js.map", "/assets/missing.js"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code == 200 {
			t.Fatal("unexpected asset disclosed", path)
		}
	}
}
