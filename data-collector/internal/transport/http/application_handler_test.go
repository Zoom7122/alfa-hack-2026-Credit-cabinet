package httptransport_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"
)

const litvinova = `{
	"representative": {"full_name": "Литвинова Марина Олеговна", "inn": "779900010333", "role": "ACCOUNTANT"},
	"company": {"inn": "7799000012", "name": "ООО «Северный ветер»"}
}`

// applicationBody — заявка в ответах POST /applicants и GET /applications/{id}.
type applicationBody struct {
	ID        string `json:"id"`
	Number    string `json:"number"`
	Status    string `json:"status"`
	Applicant struct {
		ID           string `json:"id"`
		INN          string `json:"inn"`
		Type         string `json:"type"`
		Name         string `json:"name"`
		OGRN         string `json:"ogrn"`
		AlreadyKnown *bool  `json:"already_known"`
	} `json:"applicant"`
	Representative struct {
		ID       string `json:"id"`
		FullName string `json:"full_name"`
		INN      string `json:"inn"`
		Role     string `json:"role"`
		CanSign  bool   `json:"can_sign"`
	} `json:"representative"`
	ProductCode string `json:"product_code"`
	Requested   *struct {
		Amount     int64 `json:"amount"`
		TermMonths int   `json:"term_months"`
	} `json:"requested"`
	Purpose *struct {
		Code string `json:"code"`
	} `json:"purpose"`
	Documents []struct {
		ID               string `json:"id"`
		Type             string `json:"type"`
		FileName         string `json:"file_name"`
		MimeType         string `json:"mime_type"`
		SizeBytes        int64  `json:"size_bytes"`
		SHA256           string `json:"sha256"`
		SignedDocumentID string `json:"signed_document_id"`
		SignatureState   string `json:"signature_state"`
		RecognitionState string `json:"recognition_state"`
	} `json:"documents"`
	CollectionTaskID string     `json:"collection_task_id"`
	CreatedAt        time.Time  `json:"created_at"`
	SubmittedAt      *time.Time `json:"submitted_at"`
}

func decodeApplication(t *testing.T, body []byte) applicationBody {
	t.Helper()
	var app applicationBody
	if err := json.Unmarshal(body, &app); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestRegisterApplicant(t *testing.T) {
	a := newTestApp(t)

	w := a.postJSON("/api/v1/applicants", litvinova)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	app := decodeApplication(t, w.Body.Bytes())
	if loc := w.Header().Get("Location"); loc != "/api/v1/applications/"+app.ID || app.ID == "" {
		t.Fatalf("Location = %q, id %q", loc, app.ID)
	}
	if app.Number != "A-2026-000001" || app.Status != "DRAFT" || app.Documents == nil || len(app.Documents) != 0 ||
		app.CollectionTaskID != "" || app.SubmittedAt != nil || !app.CreatedAt.Equal(fixedNow) {
		t.Fatalf("application = %+v", app)
	}
	c, r := app.Applicant, app.Representative
	if c.INN != "7799000012" || c.Type != "LEGAL_ENTITY" || c.Name != "ООО «Северный ветер»" || c.AlreadyKnown == nil || *c.AlreadyKnown {
		t.Fatalf("applicant = %+v", c)
	}
	// бухгалтер подписывает только по доверенности
	if r.FullName != "Литвинова Марина Олеговна" || r.INN != "779900010333" || r.Role != "ACCOUNTANT" || r.CanSign || r.ID == "" {
		t.Fatalf("representative = %+v", r)
	}

	// повторная регистрация: тот же черновик, компания уже известна
	again := decodeApplication(t, a.postJSON("/api/v1/applicants", litvinova).Body.Bytes())
	if again.ID != app.ID || again.Applicant.AlreadyKnown == nil || !*again.Applicant.AlreadyKnown {
		t.Fatalf("second registration = %+v", again)
	}

	// GET отдаёт ту же заявку; already_known — только в ответе на регистрацию
	w = a.get("/api/v1/applications/" + app.ID)
	got := decodeApplication(t, w.Body.Bytes())
	if w.Code != http.StatusOK || got.ID != app.ID || got.Representative.Role != "ACCOUNTANT" || got.Applicant.AlreadyKnown != nil {
		t.Fatalf("get: %d %+v", w.Code, got)
	}
}

// Новые роли формы: сотрудник, отвечающий за финансирование, и сотрудник по банковским продуктам.
func TestRegisterApplicantRoles(t *testing.T) {
	a := newTestApp(t)
	for _, role := range []string{"OWNER", "DIRECTOR", "CFO", "ACCOUNTANT", "FINANCE_EMPLOYEE", "BANKING_EMPLOYEE", "AUTHORIZED_EMPLOYEE"} {
		w := a.postJSON("/api/v1/applicants", `{"representative":{"full_name":"Кузнецов Павел","role":"`+role+`"},
			"company":{"inn":"7799000044"}}`)
		app := decodeApplication(t, w.Body.Bytes())
		if w.Code != http.StatusCreated || app.Representative.Role != role || app.Representative.CanSign != (role == "DIRECTOR") {
			t.Errorf("%s: %d %s", role, w.Code, w.Body)
		}
	}
}

func TestRegisterApplicantErrors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantFields []string
	}{
		{name: "empty body", body: ``, wantStatus: http.StatusBadRequest},
		{name: "broken json", body: `{"representative":`, wantStatus: http.StatusBadRequest},
		{name: "no representative", body: `{"company":{"inn":"7799000012"}}`, wantStatus: http.StatusBadRequest,
			wantFields: []string{"representative"}},
		{name: "no full name and inn", body: `{"representative":{"role":"CFO"},"company":{}}`, wantStatus: http.StatusBadRequest,
			wantFields: []string{"representative.full_name", "company.inn"}},
		{name: "wrong type", body: `{"representative":{"full_name":"А","role":"CFO"},"company":{"inn":7799000012}}`,
			wantStatus: http.StatusBadRequest, wantFields: []string{"company.inn"}},
		{name: "bad role and inn", body: `{"representative":{"full_name":"А","inn":"1","role":"CEO"},"company":{"inn":"123"}}`,
			wantStatus: http.StatusUnprocessableEntity, wantFields: []string{"representative.inn", "representative.role", "company.inn"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp(t)
			w := a.postJSON("/api/v1/applicants", tt.body)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", w.Code, tt.wantStatus, w.Body)
			}
			p := decodeProblem(t, w)
			if p.Code != "VALIDATION_FAILED" || (tt.wantFields != nil && !slices.Equal(p.fields(), tt.wantFields)) {
				t.Fatalf("problem = %+v, want fields %v", p, tt.wantFields)
			}
		})
	}
}

func TestGetApplicationNotFound(t *testing.T) {
	w := newTestApp(t).get("/api/v1/applications/app_unknown")
	if p := decodeProblem(t, w); w.Code != http.StatusNotFound || p.Code != "NOT_FOUND" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
