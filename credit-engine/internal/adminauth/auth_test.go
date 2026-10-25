package adminauth_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminauth"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/httpapi"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const testPassword = "fictional-admin-password"
const testToken = "fictional-ms2-service-token-1234567890"
const origin = "https://admin.example"

var passwordHash = []byte("$2a$10$EWuECD9zM/WTJv1IbDlfG.LUvUkOWslI3qcfYVvlEqmZkq/jeYQiK")

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *testClock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

type memorySessions struct {
	mu         sync.Mutex
	values     map[string]adminstore.Session
	fail       bool
	failDelete bool
	limit      int
}

func (s *memorySessions) CreateSession(_ context.Context, value adminstore.Session, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("database password=do-not-expose")
	}
	s.limit = limit
	s.values[value.TokenSHA256] = value
	return nil
}
func (s *memorySessions) GetSession(_ context.Context, key string, now time.Time) (adminstore.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return adminstore.Session{}, errors.New("database password=do-not-expose")
	}
	v, ok := s.values[key]
	if !ok || !v.ExpiresAt.After(now) {
		return adminstore.Session{}, adminstore.ErrNotFound
	}
	return v, nil
}
func (s *memorySessions) DeleteSession(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail || s.failDelete {
		return errors.New("database password=do-not-expose")
	}
	delete(s.values, key)
	return nil
}

func settings(t *testing.T) config.Admin {
	t.Helper()
	// The test password is intentionally fictitious, never a deployable default.
	if bcrypt.CompareHashAndPassword(passwordHash, []byte(testPassword)) != nil {
		t.Fatal("test bcrypt fixture does not match")
	}
	c, err := config.LoadAdmin(func(key string) (string, bool) {
		v, ok := map[string]string{"ADMIN_DATABASE_URL": "postgres://unused.invalid/test", "ADMIN_USERNAME": "editor", "ADMIN_PASSWORD_HASH": string(passwordHash), "ADMIN_EVENTS_TOKEN": testToken, "ADMIN_PUBLIC_ORIGIN": origin}[key]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func fixture(t *testing.T) (*gin.Engine, *memorySessions, *testClock, *adminauth.Access) {
	t.Helper()
	store := &memorySessions{values: map[string]adminstore.Session{}}
	clock := &testClock{now: time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)}
	access, err := adminauth.New(settings(t), store, clock)
	if err != nil {
		t.Fatal(err)
	}
	r := httpapi.NewAdminRouter(access, time.Second)
	r.POST("/browser-write", access.Browser(), func(c *gin.Context) { c.JSON(200, gin.H{"author": c.MustGet(adminauth.PrincipalKey)}) })
	r.POST("/api/v1/credit-events", access.Service(), func(c *gin.Context) { c.Status(202) })
	return r, store, clock, access
}
func request(r http.Handler, method, path, body string, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, origin+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	result := httptest.NewRecorder()
	r.ServeHTTP(result, req)
	return result
}
func login(t *testing.T, r http.Handler) ([]*http.Cookie, string) {
	t.Helper()
	w := request(r, "POST", "/api/admin/v1/auth/login", `{"username":"editor","password":"`+testPassword+`"}`, nil, map[string]string{"Origin": origin})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var value struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if len(value.CSRF) != 43 || len(w.Result().Cookies()) != 2 {
		t.Fatal("missing session cookies or CSRF")
	}
	return w.Result().Cookies(), value.CSRF
}

func TestBrowserSessionLifecycleAndCredentialIsolation(t *testing.T) {
	r, store, clock, _ := fixture(t)
	w := request(r, "GET", "/api/admin/v1/auth/session", "", nil, map[string]string{"Authorization": "Bearer " + testToken})
	if w.Code != 401 {
		t.Fatal("service token became browser session", w.Code)
	}
	cookies, csrf := login(t, r)
	for _, cookie := range cookies {
		if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge != 3600 || cookie.Domain != "" {
			t.Fatal("unsafe cookie", cookie.Name)
		}
	}
	if store.limit != 10 || len(store.values) != 1 {
		t.Fatal("session limit not passed")
	}
	for key, value := range store.values {
		if len(key) != 64 || len(value.CSRFSHA256) != 64 || strings.Contains(key, cookies[0].Value) || strings.Contains(value.CSRFSHA256, csrf) {
			t.Fatal("raw secret persisted")
		}
	}
	w = request(r, "GET", "/api/admin/v1/auth/session", "", cookies, nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), csrf) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(r, "POST", "/browser-write", "{}", cookies, map[string]string{"Origin": origin, "X-CSRF-Token": csrf})
	if w.Code != 200 || !strings.Contains(w.Body.String(), "editor") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(r, "POST", "/api/v1/credit-events", "{}", cookies, nil)
	if w.Code != 401 {
		t.Fatal("session became service credential")
	}
	w = request(r, "POST", "/api/v1/credit-events", "{}", nil, map[string]string{"Authorization": "Bearer " + testToken})
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request(r, "POST", "/api/admin/v1/auth/logout", "{}", cookies, map[string]string{"Origin": origin, "X-CSRF-Token": csrf})
	if w.Code != 204 || len(store.values) != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.MaxAge != -1 {
			t.Fatal("cookie was not removed")
		}
	}
	w = request(r, "GET", "/api/admin/v1/auth/session", "", cookies, nil)
	if w.Code != 401 {
		t.Fatal("logout did not revoke token")
	}
	cookies, _ = login(t, r)
	clock.advance(time.Hour)
	w = request(r, "GET", "/api/admin/v1/auth/session", "", cookies, nil)
	if w.Code != 401 {
		t.Fatal("expiry boundary must reject", w.Code)
	}
}

func TestCSRFAndServiceRejections(t *testing.T) {
	r, _, _, _ := fixture(t)
	cookies, csrf := login(t, r)
	for _, tc := range []struct{ name, origin, csrf string }{{"missing origin", "", csrf}, {"wrong origin", "https://attacker.example", csrf}, {"null origin", "null", csrf}, {"wrong scheme", "http://admin.example", csrf}, {"missing csrf", origin, ""}, {"wrong csrf", origin, "incorrect"}} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(r, "POST", "/browser-write", "{}", cookies, map[string]string{"Origin": tc.origin, "X-CSRF-Token": tc.csrf})
			if w.Code != 403 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		name    string
		cookies []*http.Cookie
	}{{"missing", nil}, {"duplicate", append(append([]*http.Cookie{}, cookies...), cookies[0])}, {"malformed", []*http.Cookie{{Name: adminauth.SessionCookie, Value: "bad"}}}, {"no csrf", cookies[:1]}} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(r, "GET", "/api/admin/v1/auth/session", "", tc.cookies, nil)
			if w.Code != 401 && w.Code != 403 {
				t.Fatal(w.Code)
			}
		})
	}
	for _, value := range []string{"", "Bearer wrong", "Basic " + testToken, "bearer " + testToken, "Bearer " + testToken + " "} {
		t.Run("service "+fmt.Sprint(len(value)), func(t *testing.T) {
			w := request(r, "POST", "/api/v1/credit-events", "{}", nil, map[string]string{"Authorization": value})
			if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatal(w.Code)
			}
		})
	}
	for _, header := range []string{"Origin", "X-CSRF-Token", "Authorization"} {
		t.Run("duplicate header "+header, func(t *testing.T) {
			req := httptest.NewRequest("POST", origin+"/browser-write", nil)
			for _, c := range cookies {
				req.AddCookie(c)
			}
			req.Header.Set("Origin", origin)
			req.Header.Set("X-CSRF-Token", csrf)
			if header == "Authorization" {
				req.URL.Path = "/api/v1/credit-events"
				req.Header.Set(header, "Bearer "+testToken)
			}
			req.Header.Add(header, req.Header.Get(header))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 401 && w.Code != 403 {
				t.Fatal(w.Code)
			}
		})
	}
}

func TestLoginValidationRateLimitAndNoSecretDisclosure(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{{"wrong user", `{"username":"other","password":"` + testPassword + `"}`, 401}, {"wrong password", `{"username":"editor","password":"wrong"}`, 401}, {"null", `{"username":"editor","password":null}`, 400}, {"duplicate", `{"username":"editor","password":"x","password":"y"}`, 400}, {"alias", `{"Username":"editor","password":"x"}`, 400}, {"extra", `{"username":"editor","password":"x","admin":true}`, 400}, {"long", strings.Repeat("x", 4097), 400}, {"long password", `{"username":"editor","password":"` + strings.Repeat("x", 73) + `"}`, 400}} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _, _ := fixture(t)
			w := request(r, "POST", "/api/admin/v1/auth/login", tc.body, nil, map[string]string{"Origin": origin})
			if w.Code != tc.status || len(w.Result().Cookies()) != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
			for _, secret := range []string{testPassword, testToken, string(passwordHash), "do-not-expose"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("secret in response")
				}
			}
		})
	}
	r, _, clock, _ := fixture(t)
	for i := 0; i < 10; i++ {
		w := request(r, "POST", "/api/admin/v1/auth/login", `{}`, nil, map[string]string{"Origin": origin})
		if w.Code != 400 {
			t.Fatal(i, w.Code)
		}
	}
	w := request(r, "POST", "/api/admin/v1/auth/login", `{}`, nil, map[string]string{"Origin": origin})
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal(w.Code)
	}
	clock.advance(time.Minute)
	login(t, r)
	for _, headers := range []map[string]string{{}, {"Origin": "https://attacker.example"}, {"Origin": origin, "Sec-Fetch-Site": "cross-site"}} {
		w := request(r, "POST", "/api/admin/v1/auth/login", `{}`, nil, headers)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
}

func TestSessionStoreFailureRestartAndCredentialRotation(t *testing.T) {
	r, store, clock, _ := fixture(t)
	cookies, csrf := login(t, r)
	access, err := adminauth.New(settings(t), store, clock)
	if err != nil {
		t.Fatal(err)
	}
	restarted := httpapi.NewAdminRouter(access, time.Second)
	if w := request(restarted, "GET", "/api/admin/v1/auth/session", "", cookies, nil); w.Code != 200 {
		t.Fatal("session lost on restart", w.Code)
	}
	changed := settings(t)
	changed.Username = "another-editor"
	rotated, err := adminauth.New(changed, store, clock)
	if err != nil {
		t.Fatal(err)
	}
	if w := request(httpapi.NewAdminRouter(rotated, time.Second), "GET", "/api/admin/v1/auth/session", "", cookies, nil); w.Code != 401 {
		t.Fatal("old credential-bound cookie accepted")
	}
	changed = settings(t)
	rotatedHash, err := bcrypt.GenerateFromPassword([]byte("other-fictional-password"), 10)
	if err != nil {
		t.Fatal(err)
	}
	changed.PasswordHash = string(rotatedHash)
	rotated, err = adminauth.New(changed, store, clock)
	if err != nil {
		t.Fatal(err)
	}
	if w := request(httpapi.NewAdminRouter(rotated, time.Second), "GET", "/api/admin/v1/auth/session", "", cookies, nil); w.Code != 401 {
		t.Fatal("old password-bound cookie accepted")
	}
	store.failDelete = true
	if w := request(r, "POST", "/api/admin/v1/auth/logout", "{}", cookies, map[string]string{"Origin": origin, "X-CSRF-Token": csrf}); w.Code != 503 || len(w.Result().Cookies()) != 0 {
		t.Fatal("failed logout silently cleared cookie", w.Code)
	}
	store.failDelete = false
	store.fail = true
	for _, tc := range []struct {
		method, path, body string
		cookies            []*http.Cookie
		headers            map[string]string
	}{{"GET", "/api/admin/v1/auth/session", "", cookies, nil}, {"POST", "/api/admin/v1/auth/login", `{"username":"editor","password":"` + testPassword + `"}`, nil, map[string]string{"Origin": origin}}, {"POST", "/api/admin/v1/auth/logout", "{}", cookies, map[string]string{"Origin": origin, "X-CSRF-Token": csrf}}} {
		w := request(r, tc.method, tc.path, tc.body, tc.cookies, tc.headers)
		if w.Code != 503 || strings.Contains(w.Body.String(), "do-not-expose") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func TestConcurrentLoginLimitAndRandomSessions(t *testing.T) {
	r, store, _, _ := fixture(t)
	var wg sync.WaitGroup
	statuses := make(chan int, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request(r, "POST", "/api/admin/v1/auth/login", `{"username":"editor","password":"`+testPassword+`"}`, nil, map[string]string{"Origin": origin})
			statuses <- w.Code
		}()
	}
	wg.Wait()
	close(statuses)
	success, limited := 0, 0
	for status := range statuses {
		switch status {
		case 200:
			success++
		case 429:
			limited++
		default:
			t.Fatal(status)
		}
	}
	if success != 10 || limited != 10 || len(store.values) != 10 {
		t.Fatal("login limit or session entropy failed", success, limited, len(store.values))
	}
}

func TestLoginContentTypeAndConstruction(t *testing.T) {
	r, _, _, _ := fixture(t)
	for _, contentType := range []string{"", "text/plain", "application/x-www-form-urlencoded", "invalid;"} {
		w := request(r, "POST", "/api/admin/v1/auth/login", `{}`, nil, map[string]string{"Origin": origin, "Content-Type": contentType})
		if w.Code != 415 {
			t.Fatal(w.Code)
		}
	}
	c := settings(t)
	if _, err := adminauth.New(c, nil, nil); err == nil {
		t.Fatal("nil store accepted")
	}
	c.EventsToken = "bad"
	if _, err := adminauth.New(c, &memorySessions{}, nil); err == nil {
		t.Fatal("bad configuration accepted")
	}
}
