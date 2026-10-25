package httptransport_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const fullQuestionnaire = `{
	"age": 42, "marital_status": "MARRIED", "children_count": 2,
	"product_code": "WORKING_CAPITAL",
	"requested": {"amount": 5000000, "term_months": 24},
	"purpose": {"code": "WORKING_CAPITAL", "description": "Закупка товара под сезон"},
	"documents": [{"file_name": "balance.pdf", "type": "FINANCIAL_STATEMENTS"}],
	"director": {"inn": "779900010139", "last_name": "Ковалёв", "first_name": "Игорь", "middle_name": "Сергеевич",
		"birth_date": "1984-07-19"},
	"consents": {
		"bki_company": {"id": "CNS-1", "signed_at": "2026-10-21T14:02:11Z", "valid_until": "2027-04-21"},
		"bki_person": {"id": "CNS-2", "valid_until": "2027-04-21"},
		"tax": {"id": "TAX-1", "valid_until": "2027-04-21", "scope": ["DECLARATIONS", "ENS_BALANCE"]}
	},
	"other_bank_biks": ["044525225"],
	"marketplace_tokens": {"wildberries": "secret-wb"},
	"ofd_token": "secret-ofd",
	"collateral": {"cadastral_numbers": ["50:21:0110203:845"], "vins": ["xtc652005n1498213"], "serials": []},
	"poa_number": "0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37",
	"representative_inn": "779900010333"
}`

const minimalQuestionnaire = `{"age":34,"marital_status":"SINGLE","children_count":0}`

// submittedBody — ответ POST /applications/{id}/questionnaire.
type submittedBody struct {
	Application   applicationBody `json:"application"`
	Questionnaire struct {
		ID            string    `json:"id"`
		Age           int       `json:"age"`
		MaritalStatus string    `json:"marital_status"`
		ChildrenCount int       `json:"children_count"`
		CreatedAt     time.Time `json:"created_at"`
	} `json:"questionnaire"`
	Collection collectionBody `json:"collection"`
}

func decodeSubmitted(t *testing.T, w *httptest.ResponseRecorder) submittedBody {
	t.Helper()
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body)
	}
	var body submittedBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestSubmitQuestionnaire(t *testing.T) {
	a := newTestApp(t)
	appID := a.register(t, litvinova)

	w := a.submitMultipart(t, appID, fullQuestionnaire,
		file{"balance.pdf", "%PDF-1.4 balance"}, file{"balance.pdf.sig", "signature"}, file{"charter.pdf", "%PDF-1.4 charter"})

	body := decodeSubmitted(t, w)
	if loc := w.Header().Get("Location"); loc != "/api/v1/collections/tsk_test" {
		t.Fatalf("Location = %q", loc)
	}
	app := body.Application
	if app.ID != appID || app.Status != "SUBMITTED" || app.SubmittedAt == nil || app.CollectionTaskID != "tsk_test" ||
		app.ProductCode != "WORKING_CAPITAL" || app.Requested.Amount != 5000000 || app.Purpose.Code != "WORKING_CAPITAL" {
		t.Fatalf("application = %+v", app)
	}
	q := body.Questionnaire
	if q.ID != "qst_test" || q.Age != 42 || q.MaritalStatus != "MARRIED" || q.ChildrenCount != 2 || !q.CreatedAt.Equal(fixedNow) {
		t.Fatalf("questionnaire = %+v", q)
	}
	// задача на сбор: ИНН компании из регистрации, для полной анкеты пропусков нет
	c := body.Collection
	if c.ID != "tsk_test" || c.ApplicationID != appID || c.INN != "7799000012" || c.Status != "PENDING" ||
		c.SkippedSources == nil || len(c.SkippedSources) != 0 || len(c.DocumentIDs) != 3 || c.Sources != nil {
		t.Fatalf("collection = %+v", c)
	}

	docs := map[string]int{}
	for i, d := range app.Documents {
		docs[d.FileName] = i
	}
	balance, sig, charter := app.Documents[docs["balance.pdf"]], app.Documents[docs["balance.pdf.sig"]], app.Documents[docs["charter.pdf"]]
	if balance.Type != "FINANCIAL_STATEMENTS" || balance.SignatureState != "PENDING" || balance.RecognitionState != "PENDING" ||
		balance.MimeType != "application/pdf" || balance.SizeBytes != 16 || len(balance.SHA256) != 64 {
		t.Errorf("balance = %+v", balance)
	}
	if sig.Type != "SIGNATURE" || sig.SignedDocumentID != balance.ID || sig.SignatureState != "PENDING" {
		t.Errorf("signature = %+v", sig)
	}
	if charter.Type != "OTHER" || charter.SignatureState != "NOT_SIGNED" {
		t.Errorf("charter = %+v", charter)
	}
	// файлы лежат в хранилище под ключами документов
	entries, err := os.ReadDir(filepath.Join(a.root, appID))
	if err != nil || len(entries) != 3 {
		t.Fatalf("files on disk: %d (%v)", len(entries), err)
	}
	// в ответе токенов нет; worker'у они достанутся из задачи
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("response leaks tokens: %s", w.Body)
	}
	sub, ok := a.apps.Submission(appID)
	if !ok || sub.Task.Input.MarketplaceTokens["wildberries"] != "secret-wb" || sub.Task.Input.Director.LastName != "Ковалёв" ||
		sub.Task.Input.Collateral.VINs[0] != "XTC652005N1498213" || sub.Task.Input.RepresentativeINN != "779900010333" {
		t.Fatalf("submission = %+v", sub)
	}
	if out := a.tasks.Outbox(); len(out) != 2 {
		t.Fatalf("outbox: %d events, want task + documents", len(out))
	}

	// заявка видна через GET вместе с документами
	got := decodeApplication(t, a.get("/api/v1/applications/"+appID).Body.Bytes())
	if got.Status != "SUBMITTED" || len(got.Documents) != 3 || got.CollectionTaskID != "tsk_test" {
		t.Fatalf("get application = %+v", got)
	}
}

// Анкету без файлов можно отправить обычным JSON; системы без данных сразу в skipped_sources.
func TestSubmitQuestionnaireJSON(t *testing.T) {
	a := newTestApp(t)
	appID := a.register(t, litvinova)

	body := decodeSubmitted(t, a.postJSON("/api/v1/applications/"+appID+"/questionnaire", minimalQuestionnaire))

	want := []string{"BKI", "TAX_REPORTS", "EGRN", "GOSUSLUGI", "OPEN_BANKING", "MARKETPLACES", "OFD"}
	if !slices.Equal(body.Collection.SkippedSources, want) || len(body.Application.Documents) != 0 ||
		body.Collection.DocumentIDs == nil || len(body.Collection.DocumentIDs) != 0 {
		t.Fatalf("collection = %+v, documents %v", body.Collection, body.Application.Documents)
	}
}

func TestSubmitQuestionnaireTwice(t *testing.T) {
	a := newTestApp(t)
	appID := a.register(t, litvinova)
	decodeSubmitted(t, a.submitMultipart(t, appID, minimalQuestionnaire))

	w := a.submitMultipart(t, appID, minimalQuestionnaire, file{"late.pdf", "late"})
	if p := decodeProblem(t, w); w.Code != http.StatusConflict || p.Code != "INVALID_TRANSITION" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if entries, _ := os.ReadDir(filepath.Join(a.root, appID)); len(entries) != 0 {
		t.Fatalf("files of the rejected attempt are kept: %d", len(entries))
	}
}

func TestSubmitQuestionnaireUnknownApplication(t *testing.T) {
	a := newTestApp(t)
	w := a.submitMultipart(t, "app_unknown", minimalQuestionnaire, file{"a.pdf", "a"})
	if p := decodeProblem(t, w); w.Code != http.StatusNotFound || p.Code != "NOT_FOUND" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestSubmitQuestionnaireErrors(t *testing.T) {
	many := make([]file, 11)
	for i := range many {
		many[i] = file{fmt.Sprintf("%d.pdf", i), "x"}
	}
	tests := []struct {
		name          string
		questionnaire string
		files         []file
		wantStatus    int
		wantCode      string
		wantFields    []string
	}{
		{name: "no questionnaire part", files: []file{{"a.pdf", "a"}}, wantStatus: http.StatusBadRequest,
			wantCode: "VALIDATION_FAILED", wantFields: []string{"questionnaire"}},
		{name: "broken json", questionnaire: `{"age":`, wantStatus: http.StatusBadRequest, wantCode: "VALIDATION_FAILED"},
		{name: "missing children_count", questionnaire: `{"age":34,"marital_status":"SINGLE"}`, wantStatus: http.StatusBadRequest,
			wantCode: "VALIDATION_FAILED", wantFields: []string{"children_count"}},
		{name: "wrong nested type", questionnaire: `{"age":34,"marital_status":"SINGLE","children_count":1,"director":{"inn":779900010139}}`,
			wantStatus: http.StatusBadRequest, wantCode: "VALIDATION_FAILED", wantFields: []string{"director.inn"}},
		{name: "questionnaire, collection and file rules together",
			questionnaire: `{"age":12,"marital_status":"SINGLE","children_count":1,"other_bank_biks":["1"],"requested":{"amount":1,"term_months":0}}`,
			files:         []file{{"a.pdf", "a"}, {"a.pdf.sig", "1"}, {"a.sig", "2"}},
			wantStatus:    http.StatusUnprocessableEntity, wantCode: "VALIDATION_FAILED",
			wantFields: []string{"age", "other_bank_biks[0]", "requested.term_months", "files[2]"}},
		{name: "bad file type", questionnaire: minimalQuestionnaire, files: []file{{"virus.exe", "x"}},
			wantStatus: http.StatusUnprocessableEntity, wantCode: "VALIDATION_FAILED", wantFields: []string{"files[0]"}},
		{name: "too many files", questionnaire: minimalQuestionnaire, files: many,
			wantStatus: http.StatusUnprocessableEntity, wantCode: "VALIDATION_FAILED", wantFields: []string{"files"}},
		{name: "file too large", questionnaire: minimalQuestionnaire, files: []file{{"big.pdf", strings.Repeat("x", 1<<20+1)}},
			wantStatus: http.StatusUnprocessableEntity, wantCode: "VALIDATION_FAILED", wantFields: []string{"files[0]"}},
		{name: "body too large", questionnaire: minimalQuestionnaire,
			files:      []file{{"1.pdf", strings.Repeat("x", 1<<20-1)}, {"2.pdf", strings.Repeat("x", 1<<20-1)}, {"3.pdf", strings.Repeat("x", 1<<20-1)}, {"4.pdf", strings.Repeat("x", 1<<20-1)}, {"5.pdf", strings.Repeat("x", 1<<20-1)}},
			wantStatus: http.StatusRequestEntityTooLarge, wantCode: "PAYLOAD_TOO_LARGE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp(t)
			appID := a.register(t, litvinova)

			w := a.submitMultipart(t, appID, tt.questionnaire, tt.files...)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", w.Code, tt.wantStatus, w.Body)
			}
			p := decodeProblem(t, w)
			if p.Code != tt.wantCode || p.Status != tt.wantStatus || (tt.wantFields != nil && !slices.Equal(p.fields(), tt.wantFields)) {
				t.Fatalf("problem = %+v, want fields %v", p, tt.wantFields)
			}
			// ничего не сохранено: заявка — черновик, задачи нет, файлов нет
			if got := decodeApplication(t, a.get("/api/v1/applications/"+appID).Body.Bytes()); got.Status != "DRAFT" {
				t.Fatalf("application must stay a draft: %s", got.Status)
			}
			if len(a.tasks.Outbox()) != 0 {
				t.Fatal("invalid request must not enqueue a collection")
			}
			if staging, _ := os.ReadDir(filepath.Join(a.root, ".staging")); len(staging) != 0 {
				t.Fatalf("staging is not empty: %d files", len(staging))
			}
		})
	}
}

func TestSubmitQuestionnaireUnsupportedMediaType(t *testing.T) {
	a := newTestApp(t)
	appID := a.register(t, litvinova)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/"+appID+"/questionnaire", strings.NewReader(minimalQuestionnaire))
	req.Header.Set("Content-Type", "text/plain")
	w := a.do(req)
	if p := decodeProblem(t, w); w.Code != http.StatusUnsupportedMediaType || p.Code != "UNSUPPORTED_MEDIA_TYPE" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
