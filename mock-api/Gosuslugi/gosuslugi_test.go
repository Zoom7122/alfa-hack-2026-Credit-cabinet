package gosuslugi_test

import (
	"net/http"
	"testing"

	gosuslugi "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/Gosuslugi"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/gosuslugi/v1", gosuslugi.Register)

var allScopes = []string{"PASSPORT", "INN", "SNILS", "ADDRESS", "INCOME", "EMPLOYMENT", "NPD_STATUS"}

type profile struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Data      struct {
		Passport *struct {
			Valid bool `json:"valid"`
		} `json:"passport"`
		INN    *string `json:"inn"`
		Income []struct {
			Year    string `json:"year"`
			Sources []struct {
				Type   string  `json:"type"`
				Amount float64 `json:"amount"`
			} `json:"sources"`
		} `json:"income"`
		Employment []struct {
			EmployerINN string `json:"employer_inn"`
		} `json:"employment"`
	} `json:"data"`
}

func request(t *testing.T, person map[string]any, scopes []string, headers ...string) profile {
	t.Helper()
	created := mt.Decode[profile](t, mt.PostJSON(t, router, "/gosuslugi/v1/profile-requests",
		map[string]any{"person": person, "scopes": scopes, "purpose": "LOAN_APPLICATION"}, headers...), http.StatusAccepted)
	if created.Status != "WAITING_USER_CONSENT" {
		t.Fatalf("create: %+v", created)
	}
	return mt.Decode[profile](t, mt.Get(router, "/gosuslugi/v1/profile-requests/"+created.RequestID), http.StatusOK)
}

func TestDirectors(t *testing.T) {
	companies := map[string]string{mt.DirA: mt.InnA, mt.DirB: mt.InnB, mt.DirC: mt.InnC, mt.DirE: mt.InnE}
	for dir, company := range companies {
		p := request(t, map[string]any{"inn": dir}, allScopes)
		if p.Status != "APPROVED" || p.Data.Passport == nil || !p.Data.Passport.Valid || *p.Data.INN != dir ||
			len(p.Data.Employment) != 1 || p.Data.Employment[0].EmployerINN != company {
			t.Errorf("%s: %+v", dir, p)
		}
	}
	// ИП Смирнова: работодателя нет, доход — НПД с маркетплейсов
	d := request(t, map[string]any{"last_name": "Смирнова", "first_name": "Анна", "middle_name": "Викторовна",
		"birth_date": "1993-05-27"}, allScopes)
	if d.Status != "APPROVED" || len(d.Data.Employment) != 0 || len(d.Data.Income) != 2 || d.Data.Income[0].Sources[0].Type != "NPD" {
		t.Errorf("D: %+v", d)
	}
}

func TestScopesAndStop(t *testing.T) {
	p := request(t, map[string]any{"inn": mt.DirA}, []string{"PASSPORT"})
	if p.Data.Passport == nil || p.Data.INN != nil || p.Data.Income != nil {
		t.Errorf("only PASSPORT expected: %+v", p.Data)
	}
	if l := request(t, map[string]any{"inn": mt.Laptev}, []string{"PASSPORT"}); l.Data.Passport == nil || l.Data.Passport.Valid {
		t.Errorf("Laptev passport must be invalid: %+v", l.Data)
	}
}

func TestDenied(t *testing.T) {
	if p := request(t, map[string]any{"inn": mt.DirB}, allScopes, "X-Mock-Consent", "deny"); p.Status != "DENIED" {
		t.Errorf("deny on create: %+v", p)
	}
	created := mt.Decode[profile](t, mt.PostJSON(t, router, "/gosuslugi/v1/profile-requests",
		map[string]any{"person": map[string]any{"inn": mt.DirE}, "scopes": allScopes}), http.StatusAccepted)
	if p := mt.Decode[profile](t, mt.Get(router, "/gosuslugi/v1/profile-requests/"+created.RequestID, "X-Mock-Consent", "deny"),
		http.StatusOK); p.Status != "DENIED" {
		t.Errorf("deny on get: %+v", p)
	}
}

func TestErrors(t *testing.T) {
	if code := mt.ErrorCode(t, mt.PostJSON(t, router, "/gosuslugi/v1/profile-requests",
		map[string]any{"person": map[string]any{"inn": mt.Unknown + "00"}, "scopes": allScopes}), http.StatusNotFound); code != "PERSON_NOT_FOUND" {
		t.Errorf("code %s", code)
	}
	for _, b := range []any{
		"{",
		map[string]any{"person": map[string]any{"last_name": "Иванов"}, "scopes": allScopes},
		map[string]any{"person": map[string]any{"inn": mt.DirA}, "scopes": []string{}},
		map[string]any{"person": map[string]any{"inn": mt.DirA}, "scopes": []string{"BANK_CARDS"}},
	} {
		mt.ErrorCode(t, mt.PostJSON(t, router, "/gosuslugi/v1/profile-requests", b), http.StatusBadRequest)
	}
	mt.ErrorCode(t, mt.Get(router, "/gosuslugi/v1/profile-requests/DP-000000"), http.StatusNotFound)
}
