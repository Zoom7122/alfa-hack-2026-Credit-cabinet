package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var bcryptHashPattern = regexp.MustCompile(`^\$2[aby]\$(10|11|12|13|14)\$[./A-Za-z0-9]{53}$`)

type Admin struct {
	Addr            string
	DatabaseURL     string `json:"-"`
	Username        string
	PasswordHash    string `json:"-"`
	EventsToken     string `json:"-"`
	PublicOrigin    string
	CookieSecure    bool
	SessionTTL      time.Duration
	DatabaseTimeout time.Duration
	RequestTimeout  time.Duration
	WriteTimeout    time.Duration
	ScoringURL      string
	ScoringTimeout  time.Duration
}

func LoadAdmin(env LookupEnv) (Admin, error) {
	c := Admin{Addr: value(env, "ADMIN_ADDR", ":8084"), DatabaseURL: value(env, "ADMIN_DATABASE_URL", ""), Username: value(env, "ADMIN_USERNAME", ""), PasswordHash: value(env, "ADMIN_PASSWORD_HASH", ""), EventsToken: value(env, "ADMIN_EVENTS_TOKEN", ""), PublicOrigin: value(env, "ADMIN_PUBLIC_ORIGIN", "http://localhost:8084"), CookieSecure: true}
	var err error
	c.ScoringURL = value(env, "ADMIN_SCORING_URL", "")
	if c.ScoringTimeout, err = duration(env, "ADMIN_SCORING_TIMEOUT", 2*time.Second); err != nil {
		return c, err
	}
	if raw, ok := env("ADMIN_COOKIE_SECURE"); ok {
		c.CookieSecure, err = strconv.ParseBool(raw)
		if err != nil {
			return c, fmt.Errorf("ADMIN_COOKIE_SECURE must be a boolean")
		}
	}
	for _, item := range []struct {
		key      string
		target   *time.Duration
		fallback time.Duration
	}{{"ADMIN_SESSION_TTL", &c.SessionTTL, time.Hour}, {"ADMIN_DATABASE_TIMEOUT", &c.DatabaseTimeout, 5 * time.Second}, {"ADMIN_REQUEST_TIMEOUT", &c.RequestTimeout, 30 * time.Second}, {"HTTP_WRITE_TIMEOUT", &c.WriteTimeout, 45 * time.Second}} {
		if *item.target, err = duration(env, item.key, item.fallback); err != nil {
			return c, err
		}
	}
	return c, c.Validate()
}

// Validate reports names of invalid settings, never their secret values.
func (c Admin) Validate() error {
	if err := address("ADMIN_ADDR", c.Addr); err != nil {
		return err
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("ADMIN_DATABASE_URL is required")
	}
	if strings.TrimSpace(c.Username) == "" || len(c.Username) > 256 {
		return fmt.Errorf("ADMIN_USERNAME is required and must fit 256 bytes")
	}
	cost, err := bcrypt.Cost([]byte(c.PasswordHash))
	if err != nil || cost < 10 || cost > 14 || !bcryptHashPattern.MatchString(c.PasswordHash) {
		return fmt.Errorf("ADMIN_PASSWORD_HASH must be a bcrypt hash with cost 10..14")
	}
	if !ValidServiceToken(c.EventsToken) {
		return fmt.Errorf("ADMIN_EVENTS_TOKEN must contain 32..256 visible ASCII characters without spaces")
	}
	if err := baseURL("ADMIN_PUBLIC_ORIGIN", c.PublicOrigin); err != nil {
		return err
	}
	if c.ScoringURL != "" {
		if err := baseURL("ADMIN_SCORING_URL", c.ScoringURL); err != nil {
			return err
		}
		if c.ScoringTimeout <= 0 || c.ScoringTimeout >= c.RequestTimeout {
			return fmt.Errorf("ADMIN_SCORING_TIMEOUT must be positive and shorter than ADMIN_REQUEST_TIMEOUT")
		}
	}
	u, _ := url.Parse(c.PublicOrigin)
	if u.Path != "" || u.RawPath != "" {
		return fmt.Errorf("ADMIN_PUBLIC_ORIGIN must contain only scheme and authority")
	}
	if u.Scheme == "https" && !c.CookieSecure {
		return fmt.Errorf("ADMIN_COOKIE_SECURE must be true for HTTPS")
	}
	if c.CookieSecure && u.Scheme != "https" {
		return fmt.Errorf("ADMIN_PUBLIC_ORIGIN must use HTTPS when ADMIN_COOKIE_SECURE is true; local HTTP requires explicit false")
	}
	if c.SessionTTL < time.Minute || c.SessionTTL > 24*time.Hour {
		return fmt.Errorf("ADMIN_SESSION_TTL must be between 1m and 24h")
	}
	if c.DatabaseTimeout <= 0 || c.RequestTimeout <= 0 || c.WriteTimeout <= c.RequestTimeout {
		return fmt.Errorf("admin timeouts must be positive and HTTP_WRITE_TIMEOUT must exceed ADMIN_REQUEST_TIMEOUT")
	}
	return nil
}

func ValidServiceToken(value string) bool {
	if len(value) < 32 || len(value) > 256 {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

func (Admin) String() string     { return "admin settings (credentials redacted)" }
func (c Admin) GoString() string { return c.String() }
