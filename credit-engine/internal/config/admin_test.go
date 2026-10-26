package config_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/config"
)

const adminTestHash = "$2a$10$EWuECD9zM/WTJv1IbDlfG.LUvUkOWslI3qcfYVvlEqmZkq/jeYQiK"
const adminTestToken = "fictional-ms2-service-token-1234567890"

func adminEnv() map[string]string {
	return map[string]string{"ADMIN_DATABASE_URL": "postgres://fictional:fictional-password@unused.invalid/test", "ADMIN_USERNAME": "editor", "ADMIN_PASSWORD_HASH": adminTestHash, "ADMIN_EVENTS_TOKEN": adminTestToken, "ADMIN_PUBLIC_ORIGIN": "https://admin.example"}
}

func TestAdminSettingsAndSecretRedaction(t *testing.T) {
	c, err := config.LoadAdmin(lookup(adminEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8084" || c.SessionTTL != time.Hour || c.DatabaseTimeout != 5*time.Second || c.RequestTimeout != 30*time.Second || c.WriteTimeout != 45*time.Second || !c.CookieSecure {
		t.Fatal("incorrect defaults")
	}
	data, _ := json.Marshal(c)
	for _, text := range []string{string(data), fmt.Sprintf("%v %+v %#v", c, c, c)} {
		for _, secret := range []string{c.DatabaseURL, c.PasswordHash, c.EventsToken, "fictional-password"} {
			if strings.Contains(text, secret) {
				t.Fatal("credentials disclosed")
			}
		}
	}
	env := adminEnv()
	env["ADMIN_PUBLIC_ORIGIN"] = "http://localhost:8084"
	env["ADMIN_COOKIE_SECURE"] = "false"
	if _, err = config.LoadAdmin(lookup(env)); err != nil {
		t.Fatal("explicit local HTTP rejected", err)
	}
	if _, err = config.LoadBank(lookup(map[string]string{"ADMIN_URL": "http://admin:8084", "ADMIN_EVENTS_TOKEN": adminTestToken})); err != nil {
		t.Fatal(err)
	}
	if _, err = config.LoadBank(lookup(map[string]string{"ADMIN_URL": "http://admin:8084", "ADMIN_EVENTS_TOKEN": "short-secret"})); err == nil || strings.Contains(err.Error(), "short-secret") {
		t.Fatal("invalid service token not safely rejected")
	}
}
func TestInvalidAdminSettings(t *testing.T) {
	for _, values := range []map[string]string{
		{"ADMIN_SCORING_URL": "http://user:secret@scoring.invalid"},
		{"ADMIN_SCORING_URL": "http://scoring.invalid", "ADMIN_SCORING_TIMEOUT": "0s"},
		{"ADMIN_SCORING_URL": "http://scoring.invalid", "ADMIN_SCORING_TIMEOUT": "30s"},
	} {
		env := adminEnv()
		for key, value := range values {
			env[key] = value
		}
		if _, err := config.LoadAdmin(lookup(env)); err == nil {
			t.Fatal("invalid scoring metadata settings accepted")
		}
	}
	for _, tc := range []struct{ key, value string }{{"ADMIN_ADDR", ":0"}, {"ADMIN_DATABASE_URL", ""}, {"ADMIN_USERNAME", ""}, {"ADMIN_USERNAME", strings.Repeat("x", 257)}, {"ADMIN_PASSWORD_HASH", "secret-invalid-hash"}, {"ADMIN_PASSWORD_HASH", strings.Replace(adminTestHash, "$10$", "$04$", 1)}, {"ADMIN_PASSWORD_HASH", strings.Replace(adminTestHash, "$10$", "$31$", 1)}, {"ADMIN_PASSWORD_HASH", adminTestHash + "extra"}, {"ADMIN_EVENTS_TOKEN", "secret-short"}, {"ADMIN_EVENTS_TOKEN", strings.Repeat("x", 32) + "\r\n"}, {"ADMIN_PUBLIC_ORIGIN", "https://admin.example/"}, {"ADMIN_PUBLIC_ORIGIN", "https://name:secret@admin.example"}, {"ADMIN_PUBLIC_ORIGIN", "http://admin.example"}, {"ADMIN_COOKIE_SECURE", "false"}, {"ADMIN_COOKIE_SECURE", "perhaps"}, {"ADMIN_SESSION_TTL", "59s"}, {"ADMIN_SESSION_TTL", "25h"}, {"ADMIN_DATABASE_TIMEOUT", "0s"}, {"ADMIN_REQUEST_TIMEOUT", "45s"}, {"HTTP_WRITE_TIMEOUT", "10s"}} {
		t.Run(tc.key+"/"+fmt.Sprint(len(tc.value)), func(t *testing.T) {
			env := adminEnv()
			env[tc.key] = tc.value
			_, err := config.LoadAdmin(lookup(env))
			if err == nil {
				t.Fatal("accepted invalid setting")
			}
			if strings.Contains(err.Error(), "secret-") {
				t.Fatal("secret in configuration error")
			}
		})
	}
}
