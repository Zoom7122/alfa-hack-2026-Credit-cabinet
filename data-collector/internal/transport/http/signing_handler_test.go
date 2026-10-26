package httptransport_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	httptransport "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/transport/http"
)

type fakeSigningService struct{ err error }

func (f *fakeSigningService) Signing(context.Context, string, string) (domain.SigningSession, error) {
	return domain.SigningSession{ID: "sgn_1", State: domain.SigningAwaiting, OfferID: "ofr_1",
		Signer: domain.UserRef{ID: "usr_director_north", FullName: "Директор"}, Code: "123456", AttemptsLeft: 3,
		Document: domain.Document{ID: "doc_contract"}, CodeExpiresAt: time.Now().Add(10 * time.Minute)}, f.err
}
func (f *fakeSigningService) Accept(ctx context.Context, actor, app, _ string) (domain.SigningSession, error) {
	return f.Signing(ctx, actor, app)
}
func (f *fakeSigningService) Confirm(ctx context.Context, actor, app, _ string) (domain.SigningSession, error) {
	return f.Signing(ctx, actor, app)
}

func TestSigningHTTP(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	svc := &fakeSigningService{}
	r := gin.New()
	api := r.Group("/api/v1")
	httptransport.WithAuth(httptransport.NewAuthHandler(a.auth, discardLog))(r, api)
	httptransport.WithSigning(httptransport.NewSigningHandler(svc, discardLog))(r, api)
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/applications/app_1"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer mock."+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/signing", "/offers/ofr_1/accept", "/signing/confirm"} {
		method := "POST"
		if path == "/signing" {
			method = "GET"
		}
		if w := call(method, path, "", `{}`); w.Code != 401 {
			t.Fatalf("anonymous %s: %d %s", path, w.Code, w.Body)
		}
		if w := call(method, path, "usr_underwriter_orlov", `{}`); w.Code != 403 {
			t.Fatalf("underwriter %s: %d %s", path, w.Code, w.Body)
		}
	}
	for _, actor := range []string{"usr_director_north", "usr_accountant_north"} {
		w := call("GET", "/signing", actor, "")
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if w.Code != 200 {
			t.Fatalf("session: %d %s", w.Code, w.Body)
		}
		_, hasCode := body["demo_code"]
		if hasCode != (actor == "usr_director_north") {
			t.Fatalf("code visibility: %s", w.Body)
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{&domain.SigningCodeError{AttemptsLeft: 2}, 422, "SIGNATURE_INVALID"},
		{&domain.SigningCodeError{Expired: true}, 410, "CODE_EXPIRED"},
		{domain.ErrNotAuthorizedSigner, 403, "NOT_AUTHORIZED_SIGNER"},
	} {
		svc.err = tc.err
		w := call("POST", "/signing/confirm", "usr_director_north", `{"code":"123456"}`)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
			t.Fatalf("error: %d %s", w.Code, w.Body)
		}
	}
	svc.err = nil
	if w := call("POST", "/signing/confirm", "usr_director_north", `{"code":"123456","method":"QUALIFIED_SIGNATURE"}`); w.Code != 422 {
		t.Fatalf("unsupported method: %d %s", w.Code, w.Body)
	}
}
