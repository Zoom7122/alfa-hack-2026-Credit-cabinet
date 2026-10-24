package bki_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	bki "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/BKI"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mockkit"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/bki/v1", bki.Register)

var consent = map[string]any{"id": "CNS-TEST", "signed_at": "2026-10-21T14:02:11Z", "valid_until": "2027-04-21"}

type report struct {
	Subject struct {
		Type string `json:"type"`
		INN  string `json:"inn"`
	} `json:"subject"`
	HasHistory bool `json:"has_history"`
	Score      *struct {
		Value int `json:"value"`
	} `json:"score"`
	Summary struct {
		ActiveAccounts      int     `json:"active_accounts"`
		ClosedAccounts      int     `json:"closed_accounts"`
		TotalLimit          float64 `json:"total_limit"`
		TotalOutstanding    float64 `json:"total_outstanding"`
		TotalMonthlyPayment float64 `json:"total_monthly_payment"`
		CurrentOverdue      float64 `json:"current_overdue_amount"`
		Overdue30           int     `json:"overdue_30p_24m"`
		Overdue60           int     `json:"overdue_60p_24m"`
		Overdue90           int     `json:"overdue_90p_24m"`
		Inquiries7          int     `json:"inquiries_7d"`
		Inquiries30         int     `json:"inquiries_30d"`
		Inquiries90         int     `json:"inquiries_90d"`
	} `json:"summary"`
	Accounts []struct {
		Status         string   `json:"status"`
		Limit          float64  `json:"limit"`
		Outstanding    float64  `json:"outstanding"`
		MonthlyPayment *float64 `json:"monthly_payment"`
		CurrentOverdue float64  `json:"current_overdue_amount"`
		History        string   `json:"payment_history_24m"`
	} `json:"accounts"`
	Inquiries []struct {
		Date string `json:"date"`
	} `json:"inquiries"`
}

func legal(inn string) map[string]any {
	return map[string]any{"subject_type": "LEGAL", "inn": inn, "consent": consent, "purpose": "LOAN_APPLICATION"}
}

func person(p map[string]any) map[string]any {
	return map[string]any{"subject_type": "PERSON", "person": p, "consent": consent}
}

func post(t *testing.T, body any, status int) report {
	t.Helper()
	return mt.Decode[report](t, mt.PostJSON(t, router, "/bki/v1/reports", body), status)
}

func TestCompanies(t *testing.T) {
	scores := map[string]int{mt.InnA: 712, mt.InnB: 618, mt.InnC: 438, mt.InnE: 640}
	for inn, score := range scores {
		r := post(t, legal(inn), http.StatusOK)
		if !r.HasHistory || r.Subject.INN != inn || r.Score == nil || r.Score.Value != score {
			t.Errorf("%s: %+v", inn, r)
		}
	}
	// у ИП нет истории как у бизнеса — только как у физлица
	if r := post(t, legal(mt.InnD), http.StatusOK); r.HasHistory || r.Score != nil {
		t.Errorf("D legal: %+v", r)
	}
	if r := post(t, person(map[string]any{"inn": mt.InnD}), http.StatusOK); !r.HasHistory {
		t.Errorf("D person: %+v", r)
	}
}

func TestPersons(t *testing.T) {
	for _, inn := range mt.Directors {
		r := post(t, person(map[string]any{"inn": inn}), http.StatusOK)
		if !r.HasHistory || r.Subject.Type != "PERSON" || r.Subject.INN != inn {
			t.Errorf("%s: %+v", inn, r)
		}
	}
	// без ИНН — по ФИО и дате рождения; «Ковалев» без «ё» тоже находится
	r := post(t, person(map[string]any{"last_name": "Ковалев", "first_name": "Игорь", "middle_name": "Сергеевич",
		"birth_date": "1984-07-19"}), http.StatusOK)
	if !r.HasHistory || r.Subject.INN != mt.DirA {
		t.Fatalf("by name: %+v", r)
	}
	r = post(t, person(map[string]any{"last_name": "Ковалёв", "first_name": "Игорь", "middle_name": "Сергеевич",
		"birth_date": "1990-01-01"}), http.StatusOK)
	if r.HasHistory {
		t.Fatal("namesake with another birth date must not match")
	}
}

func TestNoHistory(t *testing.T) {
	r := post(t, legal(mt.Unknown), http.StatusOK)
	if r.HasHistory || r.Score != nil || r.Accounts == nil || len(r.Accounts) != 0 || r.Subject.INN != mt.Unknown {
		t.Fatalf("%+v", r)
	}
}

func TestErrors(t *testing.T) {
	noConsent := legal(mt.InnA)
	delete(noConsent, "consent")
	expired := legal(mt.InnA)
	expired["consent"] = map[string]any{"id": "CNS-OLD", "valid_until": "2026-10-21"}

	cases := []struct {
		name   string
		body   any
		status int
		code   string
	}{
		{"no consent", noConsent, http.StatusForbidden, mockkit.CodeConsentRequired},
		{"expired consent", expired, http.StatusForbidden, mockkit.CodeConsentExpired},
		{"bad json", "{", http.StatusBadRequest, mockkit.CodeBadRequest},
		{"bad subject type", map[string]any{"subject_type": "X", "consent": consent}, http.StatusBadRequest, mockkit.CodeBadRequest},
		{"bad inn", legal("123"), http.StatusBadRequest, mockkit.CodeInvalidINN},
		{"person without data", person(map[string]any{"last_name": "Иванов"}), http.StatusBadRequest, mockkit.CodeBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code := mt.ErrorCode(t, mt.PostJSON(t, router, "/bki/v1/reports", tc.body), tc.status); code != tc.code {
				t.Fatalf("code = %s, want %s", code, tc.code)
			}
		})
	}
}

// Сводка в seed сходится со счетами и запросами: правка seed руками не разъедется.
func TestSeedSummaries(t *testing.T) {
	asOf, _ := time.Parse(time.DateOnly, mockkit.AsOf)
	files, _ := filepath.Glob("seed/*.json")
	if len(files) == 0 {
		t.Fatal("no seeds")
	}
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var bySubject map[string]report
		if err := json.Unmarshal(body, &bySubject); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for typ, r := range bySubject {
			name := f + " " + typ
			var active, closed, o30, o60, o90 int
			var limit, outstanding, monthly, overdue float64
			for _, a := range r.Accounts {
				if a.Status == "CLOSED" {
					closed++
				} else {
					active++
					limit += a.Limit
					outstanding += a.Outstanding
					overdue += a.CurrentOverdue
					if a.MonthlyPayment != nil {
						monthly += *a.MonthlyPayment
					}
				}
				if len(a.History) != 24 {
					t.Errorf("%s: payment_history_24m length %d", name, len(a.History))
				}
				o30 += worstAtLeast(a.History, '2')
				o60 += worstAtLeast(a.History, '3')
				o90 += worstAtLeast(a.History, '4')
			}
			s := r.Summary
			if s.ActiveAccounts != active || s.ClosedAccounts != closed {
				t.Errorf("%s: accounts %d/%d, want %d/%d", name, s.ActiveAccounts, s.ClosedAccounts, active, closed)
			}
			if !eq(s.TotalLimit, limit) || !eq(s.TotalOutstanding, outstanding) ||
				!eq(s.TotalMonthlyPayment, monthly) || !eq(s.CurrentOverdue, overdue) {
				t.Errorf("%s: sums %+v, want limit %.2f outstanding %.2f monthly %.2f overdue %.2f",
					name, s, limit, outstanding, monthly, overdue)
			}
			if s.Overdue30 != o30 || s.Overdue60 != o60 || s.Overdue90 != o90 {
				t.Errorf("%s: overdue episodes %d/%d/%d, want %d/%d/%d", name, s.Overdue30, s.Overdue60, s.Overdue90,
					o30, o60, o90)
			}
			var q7, q30, q90 int
			for _, q := range r.Inquiries {
				d, err := time.Parse(time.DateOnly, q.Date)
				if err != nil {
					t.Fatalf("%s: inquiry date %q", name, q.Date)
				}
				days := asOf.Sub(d).Hours() / 24
				q7 += b2i(days < 7)
				q30 += b2i(days < 30)
				q90 += b2i(days < 90)
			}
			if s.Inquiries7 != q7 || s.Inquiries30 != q30 || s.Inquiries90 != q90 {
				t.Errorf("%s: inquiries %d/%d/%d, want %d/%d/%d", name, s.Inquiries7, s.Inquiries30, s.Inquiries90,
					q7, q30, q90)
			}
		}
	}
}

// worstAtLeast: 1, если в истории был месяц с просрочкой не ниже уровня lvl ('2' — 30+ дн.).
func worstAtLeast(history string, lvl byte) int {
	for i := 0; i < len(history); i++ {
		if c := history[i]; c >= lvl && c <= '9' {
			return 1
		}
	}
	return 0
}

func eq(a, b float64) bool { return a-b < 0.005 && b-a < 0.005 }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
