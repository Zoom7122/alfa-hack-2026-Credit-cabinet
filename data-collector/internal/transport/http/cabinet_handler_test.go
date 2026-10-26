package httptransport_test

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (a *testApp) getAs(path, token string) *httptest.ResponseRecorder {
	return a.withToken(httptest.NewRequest(http.MethodGet, path, nil), token)
}

func (a *testApp) postJSONAs(path, body, token string) *httptest.ResponseRecorder {
	return a.withToken(httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)), token)
}

func (a *testApp) registerAs(t *testing.T, token, inn, role string) applicationBody {
	t.Helper()
	body := `{"representative":{"full_name":"x","role":"` + role + `"},"company":{"inn":"` + inn + `"}}`
	w := a.postJSONAs("/api/v1/applicants", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	return decodeApplication(t, w.Body.Bytes())
}

func TestCabinetApplicants(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	if w := a.get("/api/v1/applicants"); w.Code != http.StatusUnauthorized {
		t.Fatalf("without token: %d", w.Code)
	}
	a.registerAs(t, "mock.usr_director_romashka", "779900010202", "OWNER")
	w := a.getAs("/api/v1/applicants", "mock.usr_director_romashka")
	if w.Code != http.StatusOK {
		t.Fatalf("applicants: %d %s", w.Code, w.Body)
	}
	resp := decode[struct {
		Items []struct {
			ID, INN, ShortName string
			MyRole             string `json:"my_role"`
			CanSign            bool   `json:"can_sign"`
			ApplicantType      string `json:"applicant_type"`
		} `json:"items"`
	}](t, w.Body.Bytes())
	// основная — заведённая «Ромашка», за ней добавленная регистрацией
	if len(resp.Items) != 2 || resp.Items[0].INN != "7701234567" || resp.Items[0].MyRole != "DIRECTOR" || !resp.Items[0].CanSign {
		t.Fatalf("items: %+v", resp.Items)
	}
	if resp.Items[1].ApplicantType != "SOLE_PROPRIETOR" {
		t.Errorf("second: %+v", resp.Items[1])
	}
}

func TestCabinetApplications(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	mine := a.registerAs(t, "mock.usr_director_north", "7799000012", "DIRECTOR")
	colleagueDraft := a.registerAs(t, "mock.usr_accountant_north", "7799000012", "ACCOUNTANT")
	other := a.registerAs(t, "mock.usr_director_romashka", "7701234567", "DIRECTOR")

	list := func(token, query string) map[string]bool {
		t.Helper()
		w := a.getAs("/api/v1/applications"+query, token)
		if w.Code != http.StatusOK {
			t.Fatalf("applications%s: %d %s", query, w.Code, w.Body)
		}
		m := map[string]bool{}
		for _, x := range decode[struct {
			Items []applicationBody `json:"items"`
		}](t, w.Body.Bytes()).Items {
			m[x.ID] = true
		}
		return m
	}
	// черновик коллеги не виден, чужие компании — тоже
	if got := list("mock.usr_director_north", ""); !got[mine.ID] || got[colleagueDraft.ID] || got[other.ID] {
		t.Errorf("director sees %v", got)
	}
	// отправленная заявка коллеги видна, с mine=true — нет
	decodeSubmitted(t, a.submitMultipartAs(t, "mock.usr_accountant_north", colleagueDraft.ID, fullQuestionnaire,
		file{"balance.pdf", "%PDF-1.4 balance"}))
	if got := list("mock.usr_director_north", ""); !got[mine.ID] || !got[colleagueDraft.ID] {
		t.Errorf("after submit director sees %v", got)
	}
	if got := list("mock.usr_director_north", "?mine=true"); !got[mine.ID] || got[colleagueDraft.ID] {
		t.Errorf("mine=true: %v", got)
	}
	if got := list("mock.usr_director_north", "?status=SUBMITTED"); len(got) != 1 || !got[colleagueDraft.ID] {
		t.Errorf("status filter: %v", got)
	}
	if got := list("mock.usr_director_north", "?company_id=apl_romashka"); len(got) != 0 {
		t.Errorf("company filter: %v", got)
	}
	if w := a.getAs("/api/v1/applications?status=NOPE", "mock.usr_director_north"); w.Code != http.StatusUnprocessableEntity {
		t.Errorf("bad status: %d", w.Code)
	}
	if w := a.get("/api/v1/applications"); w.Code != http.StatusUnauthorized {
		t.Errorf("without token: %d", w.Code)
	}
}

func TestSubmitOnlyByAuthor(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	app := a.registerAs(t, "mock.usr_director_north", "7799000012", "DIRECTOR")
	for token, want := range map[string]int{
		"mock.usr_accountant_north": http.StatusForbidden,    // коллега
		"":                          http.StatusUnauthorized, // без токена заявку тестового пользователя не отправить
	} {
		if w := a.submitMultipartAs(t, token, app.ID, fullQuestionnaire); w.Code != want {
			t.Errorf("token %q: %d %s, want %d", token, w.Code, w.Body, want)
		}
	}
	if w := a.submitMultipartAs(t, "mock.usr_director_north", app.ID, fullQuestionnaire, file{"balance.pdf", "%PDF"}); w.Code != http.StatusAccepted {
		t.Errorf("author: %d %s", w.Code, w.Body)
	}
}

func TestRegisterLiquidatedCompany(t *testing.T) {
	a := newTestApp(t)
	w := a.postJSON("/api/v1/applicants", `{"representative":{"full_name":"x","role":"DIRECTOR"},"company":{"inn":"7799000099"}}`)
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "company_liquidated") {
		t.Errorf("liquidated: %d %s", w.Code, w.Body)
	}
	// реестр не ответил — регистрация идёт как обычно
	a.register(t, `{"representative":{"full_name":"x","role":"DIRECTOR"},"company":{"inn":"7700000000"}}`)
}

func TestCabinetTimelineDocumentsAndReport(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	const token = "mock.usr_director_north"
	app := a.registerAs(t, token, "7799000012", "DIRECTOR")

	w := a.get("/api/v1/applications/" + app.ID + "/timeline")
	if w.Code != http.StatusOK {
		t.Fatalf("timeline: %d %s", w.Code, w.Body)
	}
	tl := decode[struct {
		Items []struct{ Type, Message string } `json:"items"`
	}](t, w.Body.Bytes())
	if len(tl.Items) != 1 || tl.Items[0].Type != "application.created" || tl.Items[0].Message == "" {
		t.Errorf("timeline: %+v", tl.Items)
	}
	if w := a.get("/api/v1/applications/app_nope/timeline"); w.Code != http.StatusNotFound {
		t.Errorf("timeline of unknown: %d", w.Code)
	}

	const q = `{"age":42,"marital_status":"MARRIED","children_count":2,
		"documents":[{"file_name":"Отчёт; итог.pdf","type":"FINANCIAL_STATEMENTS"},{"file_name":"balance.pdf","type":"FINANCIAL_STATEMENTS"}]}`
	decodeSubmitted(t, a.submitMultipartAs(t, token, app.ID, q,
		file{"Отчёт; итог.pdf", "%PDF-1.4 отчёт"}, file{"balance.pdf", "%PDF-1.4 balance"}))
	docs := decodeApplication(t, a.get("/api/v1/applications/"+app.ID).Body.Bytes()).Documents
	if len(docs) != 2 {
		t.Fatalf("documents: %+v", docs)
	}
	doc, other := docs[0], docs[1]
	w = a.get("/api/v1/applications/" + app.ID + "/documents/" + doc.ID + "/content")
	if w.Code != http.StatusOK || w.Body.String() != "%PDF-1.4 отчёт" {
		t.Fatalf("content: %d %q", w.Code, w.Body)
	}
	if _, params, err := mime.ParseMediaType(w.Header().Get("Content-Disposition")); err != nil || params["filename"] != "Отчёт; итог.pdf" {
		t.Errorf("content-disposition: %q %v", w.Header().Get("Content-Disposition"), err)
	}
	if w := a.get("/api/v1/applications/" + app.ID + "/documents/doc_nope/content"); w.Code != http.StatusNotFound {
		t.Errorf("content of unknown: %d", w.Code)
	}

	// документ чужой заявки по id этой заявки не отдаётся
	foreign := a.registerAs(t, "mock.usr_director_romashka", "7701234567", "DIRECTOR")
	if w := a.get("/api/v1/applications/" + foreign.ID + "/documents/" + doc.ID + "/content"); w.Code != http.StatusNotFound {
		t.Errorf("document of another application: %d", w.Code)
	}

	// файл пропал с диска — 404, а не 500
	if err := os.Remove(filepath.Join(a.root, app.ID, other.ID+".pdf")); err != nil {
		t.Fatal(err)
	}
	if w := a.get("/api/v1/applications/" + app.ID + "/documents/" + other.ID + "/content"); w.Code != http.StatusNotFound {
		t.Errorf("missing file: %d %s", w.Code, w.Body)
	}

	reportPath := "/api/v1/applications/" + app.ID + "/documents/" + doc.ID + "/signature-report"
	if w := a.get(reportPath); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "REPORT_NOT_READY") {
		t.Errorf("report before check: %d %s", w.Code, w.Body)
	}
	if w := a.get("/api/v1/applications/app_nope/documents/" + doc.ID + "/signature-report"); w.Code != http.StatusNotFound ||
		strings.Contains(w.Body.String(), "REPORT_NOT_READY") {
		t.Errorf("report of unknown application: %d %s", w.Code, w.Body)
	}
	a.cabinet.PutSignatureReport(doc.ID, []byte(`{"decision":"VERIFIED","verdict":"VALID_WITH_WARNINGS"}`))
	w = a.get(reportPath)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"decision":"VERIFIED"`) {
		t.Errorf("report: %d %s", w.Code, w.Body)
	}
}

func TestCounterpartyLookup(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	const token = "mock.usr_director_romashka"
	type prefill struct {
		ShortName      string `json:"short_name"`
		OGRN           string `json:"ogrn"`
		DirectorName   string `json:"director_name"`
		RegistryStatus string `json:"registry_status"`
		AlreadyKnown   bool   `json:"already_known"`
	}
	lookup := func(body string) *httptest.ResponseRecorder {
		return a.postJSONAs("/api/v1/counterparty/lookup", body, token)
	}
	if w := a.postJSON("/api/v1/counterparty/lookup", `{"inn":"7799000012"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("without token: %d", w.Code)
	}
	w := lookup(`{"inn":"7799000012"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("lookup: %d %s", w.Code, w.Body)
	}
	// «Северный ветер» заведён тестовым пользователям — компания уже известна
	if p := decode[prefill](t, w.Body.Bytes()); p.OGRN != "1217700412345" || p.DirectorName != "Ковалёв Игорь Сергеевич" ||
		p.RegistryStatus != "ACTIVE" || !p.AlreadyKnown {
		t.Errorf("prefill: %+v", p)
	}
	if p := decode[prefill](t, lookup(`{"inn":"7799000099"}`).Body.Bytes()); p.RegistryStatus != "LIQUIDATED" || p.AlreadyKnown {
		t.Errorf("liquidated: %+v", p)
	}
	for body, want := range map[string]int{
		`{"inn":"7711111111"}`: http.StatusNotFound,
		`{"inn":"123"}`:        http.StatusUnprocessableEntity,
		`{"inn":""}`:           http.StatusUnprocessableEntity,
		`{"inn":"7700000000"}`: http.StatusGatewayTimeout,
		`{}`:                   http.StatusBadRequest,
	} {
		if w := lookup(body); w.Code != want {
			t.Errorf("%s: %d, want %d", body, w.Code, want)
		}
	}
}
