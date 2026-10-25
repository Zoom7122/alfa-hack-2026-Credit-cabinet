package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelgen"
	"github.com/gin-gonic/gin"
)

type noAdminSessions struct{}

func (noAdminSessions) CreateSession(context.Context, adminstore.Session, int) error {
	return errors.New("unused")
}
func (noAdminSessions) GetSession(context.Context, string, time.Time) (adminstore.Session, error) {
	return adminstore.Session{}, adminstore.ErrNotFound
}
func (noAdminSessions) DeleteSession(context.Context, string) error { return errors.New("unused") }
func adminTestAccess(t *testing.T) *adminauth.Access {
	t.Helper()
	c, err := config.LoadAdmin(func(key string) (string, bool) {
		v, ok := map[string]string{"ADMIN_DATABASE_URL": "postgres://unused.invalid/test", "ADMIN_USERNAME": "editor", "ADMIN_PASSWORD_HASH": "$2a$10$EWuECD9zM/WTJv1IbDlfG.LUvUkOWslI3qcfYVvlEqmZkq/jeYQiK", "ADMIN_EVENTS_TOKEN": "fictional-ms2-service-token-1234567890", "ADMIN_PUBLIC_ORIGIN": "https://admin.example"}[key]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	access, err := adminauth.New(c, noAdminSessions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return access
}

func TestEveryModelRouteRequiresBrowserSession(t *testing.T) {
	r := NewAdminRouter(adminTestAccess(t), time.Second)
	RegisterAdminModels(r, adminTestAccess(t), nil)
	count := 0
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/api/admin/v1/") || strings.Contains(route.Path, "/auth/") {
			continue
		}
		count++
		t.Run(route.Method+route.Path, func(t *testing.T) {
			path := strings.NewReplacer(":version", "ip-v2", ":id", "b8d628a9-a212-4012-b42f-88d0032a8a35", ":revision", "1", ":kind", "DRAFT").Replace(route.Path)
			request := httptest.NewRequest(route.Method, path, strings.NewReader(`{}`))
			request.Header.Set("Authorization", "Bearer fictional-ms2-service-token-1234567890")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			if w.Code != 401 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	if count != 18 {
		t.Fatal("unexpected model API route count", count)
	}
}

func TestAdminEnvelopeAndReferenceValidation(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"reason":"x","reason":"y"}`, `{"Reason":"x"}`, `{"reason":null}`, `{"reason":"x","author":"forged"}`, `{"reason":"x"} {}`, `{"reason":"x"`} {
		if _, err := adminObject([]byte(raw), []string{"reason"}); err == nil {
			t.Fatal("bad envelope accepted", raw)
		}
	}
	for _, raw := range []string{`null`, `1`, `true`, `"\u0000"`} {
		if _, err := adminString([]byte(raw)); err == nil {
			t.Fatal("invalid string accepted")
		}
	}
	for _, raw := range []string{`null`, `0`, `-1`, `1.5`, `"1"`, `9223372036854775808`} {
		if _, err := adminRevision([]byte(raw)); err == nil {
			t.Fatal("invalid revision accepted", raw)
		}
	}
	for _, raw := range []string{`{"kind":"PUBLISHED","id":"ip-v2","revision":1}`, `{"kind":"PUBLISHED","id":"../ip-v2","revision":null}`, `{"kind":"PUBLISHED","id":"ip-v2","revision":null,"revision":null}`, `{"kind":"PUBLISHED","id":"ip-v2","revision":null,"Revision":null}`, `{"kind":"REVISION","id":"x","revision":null}`, `{"kind":"LATEST","id":"x","revision":null}`} {
		if _, err := parseAdminReference([]byte(raw)); err == nil {
			t.Fatal("bad reference accepted", raw)
		}
	}
	ref, err := parseAdminReference([]byte(`{"kind":"PUBLISHED","id":"null-model","revision":null}`))
	if err != nil || ref.ID != "null-model" || ref.Revision != nil {
		t.Fatal(ref, err)
	}
	for _, raw := range []string{`{"version":"v","base_version":"2.0.0","published_versions":[null],"drafts":[],"notes":""}`, `{"version":"v","base_version":"2.0.0","published_versions":[],"drafts":[{"id":"x","revision":1,"Revision":2}],"notes":""}`, `{"version":"v","base_version":"2.0.0","published_versions":[],"drafts":[{"id":"x","revision":0}],"notes":""}`} {
		if _, err := parseAdminRelease([]byte(raw)); err == nil {
			t.Fatal("bad release envelope accepted")
		}
	}
}

func TestAdminBodyLimitsAndPagination(t *testing.T) {
	for _, tc := range []struct {
		body, contentType string
		want              int
	}{{`{}`, "text/plain", 415}, {strings.Repeat("x", maxAdminBody+1), "application/json", 413}, {string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}), "application/json", 400}, {`{"reason":"x","reason":"y"}`, "application/json", 400}} {
		t.Run(fmt.Sprint(tc.want), func(t *testing.T) {
			r := gin.New()
			r.POST("/", func(c *gin.Context) {
				if _, ok := adminBody(c, "reason"); ok {
					c.Status(200)
				}
			})
			request := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, request)
			if w.Code != tc.want {
				t.Fatal(w.Code)
			}
		})
	}
	for _, query := range []string{"limit=0", "limit=201", "offset=-1", "limit=1&limit=2", "unknown=x", "offset=1.5", "limit=1;offset=2", "bad=%ZZ"} {
		r := gin.New()
		r.GET("/", func(c *gin.Context) {
			if _, _, ok := adminPage(c); ok {
				c.Status(200)
			}
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/?"+query, nil))
		if w.Code != 400 {
			t.Fatal("bad pagination accepted", query, w.Code)
		}
	}
}

func TestAdminProblemMappingDoesNotExposeCauses(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{adminstore.ErrNotFound, 404, "ADMIN_OBJECT_NOT_FOUND"}, {adminstore.ErrInvalid, 422, "INVALID_ADMIN_INPUT"}, {adminstore.ErrImmutable, 409, "IMMUTABLE_VERSION"}, {&adminstore.ConflictError{CurrentRevision: 9}, 409, "REVISION_CONFLICT"}, {context.DeadlineExceeded, 504, "ADMIN_REQUEST_TIMEOUT"}, {errors.New("password=do-not-expose"), 500, "INTERNAL_ERROR"}, {fmt.Errorf("%w: %w", adminstore.ErrInvalid, &modelgen.ValidationError{Version: "ip-invalid", Result: model.ValidationResult{Valid: false, Issues: []model.Issue{{Path: "/criteria", Code: "INVALID_TOTAL", Message: "Wrong maximum"}}}}), 422, "MODEL_VALIDATION_FAILED"}} {
		t.Run(tc.code, func(t *testing.T) {
			r := gin.New()
			r.GET("/", func(c *gin.Context) { writeAdminError(c, tc.err) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			var value map[string]json.RawMessage
			if json.Unmarshal(w.Body.Bytes(), &value) != nil || w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "do-not-expose") {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.code == "REVISION_CONFLICT" && string(value["current_revision"]) != "9" {
				t.Fatal("revision missing")
			}
			if tc.code == "MODEL_VALIDATION_FAILED" && value["validation"] == nil {
				t.Fatal("validation result missing")
			}
		})
	}
}
