// Package adminauth separates browser sessions from MS2 service credentials.
package adminauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/adminstore"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clock"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const SessionCookie = "credit_admin_session"
const CSRFCookie = "credit_admin_csrf"
const PrincipalKey = "admin.username"
const MaxSessions = 10

type Sessions interface {
	CreateSession(context.Context, adminstore.Session, int) error
	GetSession(context.Context, string, time.Time) (adminstore.Session, error)
	DeleteSession(context.Context, string) error
}
type Access struct {
	settings config.Admin
	sessions Sessions
	clock    clock.Clock
	mu       sync.Mutex
	attempts int
	window   time.Time
}

func New(settings config.Admin, sessions Sessions, source clock.Clock) (*Access, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if sessions == nil {
		return nil, errors.New("admin session store is required")
	}
	if source == nil {
		source = clock.System{}
	}
	return &Access{settings: settings, sessions: sessions, clock: source}, nil
}

func problem(c *gin.Context, status int, code string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(status, contracts.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: code, Detail: http.StatusText(status), Fields: []string{}})
}
func hash(value string) string {
	result := sha256.Sum256([]byte(value))
	return hex.EncodeToString(result[:])
}
func same(left, right string) bool {
	a, b := sha256.Sum256([]byte(left)), sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
func randomToken() (string, error) {
	var value [32]byte
	_, err := rand.Read(value[:])
	return base64.RawURLEncoding.EncodeToString(value[:]), err
}
func validToken(value string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(value)
	return err == nil && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
}

// Credential binding revokes old cookies after a password/username change.
func (a *Access) sessionHash(token string) string {
	return hash(token + "\x00" + a.settings.Username + "\x00" + a.settings.PasswordHash)
}

func (a *Access) allowedOrigin(c *gin.Context) bool {
	return len(c.Request.Header.Values("Origin")) == 1 && c.GetHeader("Origin") == a.settings.PublicOrigin && c.GetHeader("Sec-Fetch-Site") != "cross-site"
}
func (a *Access) permitLogin(now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.window.IsZero() || now.Sub(a.window) >= time.Minute || now.Before(a.window) {
		a.window = now
		a.attempts = 0
	}
	if a.attempts >= 10 {
		return false
	}
	a.attempts++
	return true
}
func (a *Access) setCookie(c *gin.Context, name, value string, expires time.Time, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, MaxAge: maxAge, Secure: a.settings.CookieSecure, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}
func (a *Access) clearCookies(c *gin.Context) {
	for _, name := range []string{SessionCookie, CSRFCookie} {
		a.setCookie(c, name, "", time.Unix(1, 0).UTC(), -1)
	}
}
func cookie(request *http.Request, name string) (string, bool) {
	var value string
	count := 0
	for _, item := range request.Cookies() {
		if item.Name == name {
			value = item.Value
			count++
		}
	}
	return value, count == 1 && validToken(value)
}

func (a *Access) Login(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if !a.allowedOrigin(c) {
		problem(c, 403, "INVALID_ORIGIN")
		return
	}
	if !a.permitLogin(a.clock.Now()) {
		c.Header("Retry-After", "60")
		problem(c, 429, "LOGIN_RATE_LIMIT")
		return
	}
	contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || contentType != "application/json" {
		problem(c, 415, "JSON_REQUIRED")
		return
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, 4097))
	if err != nil || len(data) > 4096 || jsonstrict.Check(data) != nil {
		problem(c, 400, "INVALID_LOGIN_REQUEST")
		return
	}
	fields, err := jsonstrict.Object(data, "/", []string{"username", "password"}, nil)
	if err != nil {
		problem(c, 400, "INVALID_LOGIN_REQUEST")
		return
	}
	var username, password string
	if json.Unmarshal(fields["username"], &username) != nil || json.Unmarshal(fields["password"], &password) != nil || len(password) > 72 || len(username) > 256 {
		problem(c, 400, "INVALID_LOGIN_REQUEST")
		return
	}
	passwordOK := bcrypt.CompareHashAndPassword([]byte(a.settings.PasswordHash), []byte(password)) == nil
	if !same(username, a.settings.Username) || !passwordOK {
		problem(c, 401, "INVALID_CREDENTIALS")
		return
	}
	token, err := randomToken()
	if err != nil {
		problem(c, 500, "INTERNAL_ERROR")
		return
	}
	csrf, err := randomToken()
	if err != nil {
		problem(c, 500, "INTERNAL_ERROR")
		return
	}
	now := a.clock.Now()
	expires := now.Add(a.settings.SessionTTL)
	s := adminstore.Session{TokenSHA256: a.sessionHash(token), Username: a.settings.Username, CSRFSHA256: hash(csrf), CreatedAt: now, ExpiresAt: expires}
	if err = a.sessions.CreateSession(c.Request.Context(), s, MaxSessions); err != nil {
		problem(c, 503, "SESSION_STORE_UNAVAILABLE")
		return
	}
	a.setCookie(c, SessionCookie, token, expires, int(a.settings.SessionTTL/time.Second))
	a.setCookie(c, CSRFCookie, csrf, expires, int(a.settings.SessionTTL/time.Second))
	c.JSON(200, gin.H{"username": s.Username, "expires_at": s.ExpiresAt, "csrf_token": csrf})
}

// Browser authenticates every request against persistent, unexpired sessions.
// An unsafe method additionally requires the exact public Origin and CSRF nonce.
func (a *Access) Browser() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		token, ok := cookie(c.Request, SessionCookie)
		if !ok {
			problem(c, 401, "AUTHENTICATION_REQUIRED")
			return
		}
		s, err := a.sessions.GetSession(c.Request.Context(), a.sessionHash(token), a.clock.Now())
		if errors.Is(err, adminstore.ErrNotFound) {
			a.clearCookies(c)
			problem(c, 401, "SESSION_EXPIRED")
			return
		}
		if err != nil {
			problem(c, 503, "SESSION_STORE_UNAVAILABLE")
			return
		}
		if s.Username != a.settings.Username || !s.ExpiresAt.After(a.clock.Now()) {
			a.clearCookies(c)
			problem(c, 401, "SESSION_EXPIRED")
			return
		}
		csrf, csrfOK := cookie(c.Request, CSRFCookie)
		if !csrfOK || !same(hash(csrf), s.CSRFSHA256) {
			problem(c, 403, "INVALID_CSRF_TOKEN")
			return
		}
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if !a.allowedOrigin(c) {
				problem(c, 403, "INVALID_ORIGIN")
				return
			}
			if len(c.Request.Header.Values("X-CSRF-Token")) != 1 || !same(c.GetHeader("X-CSRF-Token"), csrf) {
				problem(c, 403, "INVALID_CSRF_TOKEN")
				return
			}
		}
		c.Set(PrincipalKey, s.Username)
		c.Set("admin.session", s)
		c.Set("admin.csrf", csrf)
		c.Next()
	}
}

func (a *Access) Session(c *gin.Context) {
	s := c.MustGet("admin.session").(adminstore.Session)
	c.JSON(200, gin.H{"username": s.Username, "expires_at": s.ExpiresAt, "csrf_token": c.MustGet("admin.csrf")})
}
func (a *Access) Logout(c *gin.Context) {
	s := c.MustGet("admin.session").(adminstore.Session)
	if err := a.sessions.DeleteSession(c.Request.Context(), s.TokenSHA256); err != nil {
		problem(c, 503, "SESSION_STORE_UNAVAILABLE")
		return
	}
	a.clearCookies(c)
	c.Status(http.StatusNoContent)
}

// Service never accepts a browser session as an MS2 credential.
func (a *Access) Service() gin.HandlerFunc {
	return func(c *gin.Context) {
		values := c.Request.Header.Values("Authorization")
		if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") || !same(strings.TrimPrefix(values[0], "Bearer "), a.settings.EventsToken) {
			c.Header("WWW-Authenticate", "Bearer")
			problem(c, 401, "SERVICE_AUTHENTICATION_REQUIRED")
			return
		}
		c.Next()
	}
}
