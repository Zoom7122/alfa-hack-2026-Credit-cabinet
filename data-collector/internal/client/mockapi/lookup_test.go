package mockapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLookupCompany(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		switch q := r.PostForm.Get("query"); {
		case r.URL.Path == "/fns/egrul/" && q == "7799000012":
			_, _ = io.WriteString(w, `{"t":"T1","captchaRequired":false}`)
		case r.URL.Path == "/fns/egrul/" && q == "7799000099":
			_, _ = io.WriteString(w, `{"t":"T2","captchaRequired":false}`)
		case r.URL.Path == "/fns/egrul/" && q == "7711111111":
			_, _ = io.WriteString(w, `{"t":"","captchaRequired":false}`)
		case r.URL.Path == "/fns/egrul/" && q == "7722222222":
			_, _ = io.WriteString(w, `{"t":"","captchaRequired":true}`)
		case r.URL.Path == "/fns/egrul/" && q == "7733333333":
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{}`)
		case r.URL.Path == "/fns/egrul/" && q == "7744444444":
			time.Sleep(200 * time.Millisecond)
			_, _ = io.WriteString(w, `{"t":"T1"}`)
		case r.URL.Path == "/fns/egrul/search-result/T1":
			_, _ = io.WriteString(w, `{"rows":[{"c":"ООО \"СЕВЕРНЫЙ ВЕТЕР\"","i":"7799000012","o":"1217700412345","k":"ul"}]}`)
		case r.URL.Path == "/fns/egrul/search-result/T2":
			_, _ = io.WriteString(w, `{"rows":[{"c":"ООО \"ТРАНЗИТ\"","i":"7799000099","k":"ul","e":"10.01.2025"}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, 100*time.Millisecond, "")
	ctx := context.Background()

	card, found, err := c.LookupCompany(ctx, "7799000012")
	if err != nil || !found || card.OGRN != "1217700412345" || card.TerminatedOn != "" {
		t.Fatalf("found: %+v %v %v", card, found, err)
	}
	if card, found, err := c.LookupCompany(ctx, "7799000099"); err != nil || !found || card.TerminatedOn != "2025-01-10" {
		t.Fatalf("liquidated: %+v %v %v", card, found, err)
	}
	if _, found, err := c.LookupCompany(ctx, "7711111111"); err != nil || found {
		t.Fatalf("not in registry: %v %v", found, err)
	}
	for inn, reason := range map[string]string{"7722222222": "captcha", "7733333333": "502", "7744444444": "Timeout"} {
		_, _, err := c.LookupCompany(ctx, inn)
		if !errors.Is(err, ErrRegistryUnavailable) || !strings.Contains(err.Error(), reason) {
			t.Errorf("%s: %v, want ErrRegistryUnavailable with %q", inn, err, reason)
		}
	}
}
