package httptransport_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

type companyAccessBody struct {
	ApplicantID string `json:"applicant_id"`
	ShortName   string `json:"short_name"`
	INN         string `json:"inn"`
	Role        string `json:"role"`
	CanSign     bool   `json:"can_sign"`
}

type meBody struct {
	ID                string              `json:"id"`
	FullName          string              `json:"full_name"`
	Roles             []string            `json:"roles"`
	Persona           string              `json:"persona"`
	Companies         []companyAccessBody `json:"companies"`
	PendingSignatures []string            `json:"pending_signatures"`
}

func decode[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return v
}

func TestTestUsers(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	w := a.get("/api/v1/auth/test-users")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	resp := decode[struct {
		Items []meBody `json:"items"`
	}](t, w.Body.Bytes())
	if len(resp.Items) != len(service.TestUsers) {
		t.Fatalf("test users: %d", len(resp.Items))
	}
	// порядок — как в service.TestUsers: первые — персонажи моков фронта
	first := resp.Items[0]
	if first.ID != "usr_director_romashka" || first.Persona == "" || first.Roles[0] != "CLIENT" {
		t.Errorf("first user: %+v", first)
	}
	if got := first.Companies; len(got) != 1 || got[0].ApplicantID != "apl_romashka" || got[0].Role != "DIRECTOR" || !got[0].CanSign {
		t.Errorf("first user companies: %+v", got)
	}
	var underwriter, accountant meBody
	for _, u := range resp.Items {
		switch u.ID {
		case "usr_underwriter_sokolova":
			underwriter = u
		case "usr_accountant_north":
			accountant = u
		}
	}
	if underwriter.Roles[0] != "UNDERWRITER" || len(underwriter.Companies) != 0 {
		t.Errorf("underwriter: %+v", underwriter)
	}
	// бухгалтер договор без доверенности не подписывает
	if len(accountant.Companies) != 1 || accountant.Companies[0].INN != "7799000012" || accountant.Companies[0].CanSign {
		t.Errorf("accountant: %+v", accountant)
	}
}

func TestMockLoginAndMe(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	w := a.postJSON("/api/v1/auth/mock-login", `{"user_id": "usr_ip_smirnova"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login: status %d: %s", w.Code, w.Body)
	}
	login := decode[struct {
		Token string `json:"token"`
		User  meBody `json:"user"`
	}](t, w.Body.Bytes())
	if login.Token != "mock.usr_ip_smirnova" || login.User.FullName != "Смирнова Анна Викторовна" {
		t.Errorf("login: %+v", login)
	}
	// ИП-собственник подписывает сам
	if c := login.User.Companies; len(c) != 1 || c[0].Role != "OWNER" || !c[0].CanSign {
		t.Errorf("login companies: %+v", c)
	}
	if login.User.PendingSignatures == nil {
		t.Error("pending_signatures must be an empty array, not null")
	}

	w = a.withToken(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil), login.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("me: status %d: %s", w.Code, w.Body)
	}
	if me := decode[meBody](t, w.Body.Bytes()); me.ID != "usr_ip_smirnova" || len(me.Companies) != 1 {
		t.Errorf("me: %+v", me)
	}
}

func TestAuthErrors(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	cases := []struct {
		name string
		w    *httptest.ResponseRecorder
		want int
		code string
	}{
		{"login без user_id", a.postJSON("/api/v1/auth/mock-login", `{}`), http.StatusBadRequest, "VALIDATION_FAILED"},
		{"login неизвестного", a.postJSON("/api/v1/auth/mock-login", `{"user_id": "usr_nobody"}`), http.StatusNotFound, "NOT_FOUND"},
		{"me без токена", a.get("/api/v1/me"), http.StatusUnauthorized, "UNAUTHORIZED"},
		{"me с чужим форматом", a.withToken(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil), "secret"), http.StatusUnauthorized, "UNAUTHORIZED"},
		{"me с неизвестным пользователем", a.withToken(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil), "mock.usr_nobody"), http.StatusUnauthorized, "UNAUTHORIZED"},
		// неверный токен отклоняется на любой ручке, а не превращается в анонимный запрос
		{"регистрация с неверным токеном", a.withToken(httptest.NewRequest(http.MethodPost, "/api/v1/applicants", strings.NewReader(litvinova)), "mock.usr_nobody"),
			http.StatusUnauthorized, "UNAUTHORIZED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.w.Code != tc.want {
				t.Fatalf("status %d, want %d: %s", tc.w.Code, tc.want, tc.w.Body)
			}
			if p := decode[struct {
				Code string `json:"code"`
			}](t, tc.w.Body.Bytes()); p.Code != tc.code {
				t.Errorf("code %q, want %q", p.Code, tc.code)
			}
			if ct := tc.w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
				t.Errorf("content type %q", ct)
			}
		})
	}
}

func TestRegisterWithToken(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	register := func(token, body string) applicationBody {
		t.Helper()
		w := a.postJSON("/api/v1/applicants", body)
		if token != "" {
			w = a.withToken(httptest.NewRequest(http.MethodPost, "/api/v1/applicants", strings.NewReader(body)), token)
		}
		if w.Code != http.StatusCreated {
			t.Fatalf("register: status %d: %s", w.Code, w.Body)
		}
		return decodeApplication(t, w.Body.Bytes())
	}
	// без ИНН физлица и с чужим ФИО в теле: заявка всё равно от имени вошедшего
	body := `{"representative": {"full_name": "Кто-то Другой", "role": "DIRECTOR"}, "company": {"inn": "7799000012"}}`
	first := register("mock.usr_director_north", body)
	if first.Representative.ID != "usr_director_north" || first.Representative.FullName != "Ковалёв Игорь Сергеевич" {
		t.Errorf("representative: %+v", first.Representative)
	}
	// тестовая компания уже заведена — регистрация её переиспользует
	if first.Applicant.ID != "apl_north_wind" || first.Applicant.AlreadyKnown == nil || !*first.Applicant.AlreadyKnown {
		t.Errorf("applicant: %+v", first.Applicant)
	}
	// повторный вход в анкету — тот же черновик, а не новый
	if again := register("mock.usr_director_north", body); again.ID != first.ID {
		t.Errorf("draft is not reused: %s, then %s", first.ID, again.ID)
	}
	// другой пользователь той же компании — свой черновик
	if other := register("mock.usr_accountant_north", strings.Replace(body, "DIRECTOR", "ACCOUNTANT", 1)); other.ID == first.ID {
		t.Error("another user got the same draft")
	}
	// без токена по-прежнему анонимно: каждый раз новый пользователь
	anonymous := register("", body)
	if anonymous.Representative.ID == "usr_director_north" || anonymous.Representative.FullName != "Кто-то Другой" {
		t.Errorf("anonymous representative: %+v", anonymous.Representative)
	}
}

func TestAuthHardening(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	get := func(path, header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", header)
		return a.do(req)
	}
	// схема — без учёта регистра
	if w := get("/api/v1/me", "bearer mock.usr_director_romashka"); w.Code != http.StatusOK {
		t.Errorf("lowercase bearer: %d %s", w.Code, w.Body)
	}
	for _, h := range []string{"Bearer ", "Bearer", "Basic dXNlcjpwYXNz"} {
		if w := get("/api/v1/me", h); w.Code != http.StatusUnauthorized {
			t.Errorf("%q: %d", h, w.Code)
		}
	}
	// входу старый токен не мешает
	if w := get("/api/v1/auth/test-users", "Bearer mock.usr_gone"); w.Code != http.StatusOK {
		t.Errorf("test-users with stale token: %d", w.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/mock-login", strings.NewReader(`{"user_id":"usr_director_north"}`))
	if w := a.withToken(req, "mock.usr_gone"); w.Code != http.StatusOK {
		t.Errorf("mock-login with stale token: %d %s", w.Code, w.Body)
	}
	// пустой user_id — ошибка запроса, а не «не найден»
	if w := a.postJSON("/api/v1/auth/mock-login", `{"user_id": "  "}`); w.Code != http.StatusBadRequest {
		t.Errorf("empty user_id: %d", w.Code)
	}
}

func TestLoginOnlyTestUsers(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	// пользователь, заведённый анонимной регистрацией, — не тестовый: под ним не войти
	anon := decodeApplication(t, a.postJSON("/api/v1/applicants", litvinova).Body.Bytes())
	id := anon.Representative.ID
	if w := a.postJSON("/api/v1/auth/mock-login", `{"user_id":"`+id+`"}`); w.Code != http.StatusNotFound {
		t.Errorf("login as non-test user: %d %s", w.Code, w.Body)
	}
	if w := a.withToken(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil), "mock."+id); w.Code != http.StatusUnauthorized {
		t.Errorf("me as non-test user: %d", w.Code)
	}
}

func TestRegisterWithTokenKeepsRoleAndOrder(t *testing.T) {
	a := newTestApp(t).seedTestUsers(t)
	post := func(token, body string) *httptest.ResponseRecorder {
		return a.withToken(httptest.NewRequest(http.MethodPost, "/api/v1/applicants", strings.NewReader(body)), token)
	}
	// директор Ромашки регистрирует Ромашку как «бухгалтер» — роль в компании остаётся DIRECTOR
	w := post("mock.usr_director_romashka", `{"representative":{"full_name":"x","role":"ACCOUNTANT"},"company":{"inn":"7701234567"}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", w.Code, w.Body)
	}
	if app := decodeApplication(t, w.Body.Bytes()); app.Representative.Role != "DIRECTOR" || !app.Representative.CanSign {
		t.Errorf("role is overwritten: %+v", app.Representative)
	}
	// регистрация на другую компанию добавляет её в профиль, но основной остаётся заведённая
	if w := post("mock.usr_director_romashka", `{"representative":{"full_name":"x","role":"OWNER"},"company":{"inn":"779900010202"}}`); w.Code != http.StatusCreated {
		t.Fatalf("register other: %d %s", w.Code, w.Body)
	}
	me := decode[meBody](t, a.withToken(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil), "mock.usr_director_romashka").Body.Bytes())
	if len(me.Companies) != 2 || me.Companies[0].INN != "7701234567" || me.Companies[0].Role != "DIRECTOR" {
		t.Errorf("companies: %+v", me.Companies)
	}
	// сотрудник банка заявку не подаёт
	if w := post("mock.usr_underwriter_sokolova", litvinova); w.Code != http.StatusForbidden {
		t.Errorf("underwriter: %d %s", w.Code, w.Body)
	}
}
