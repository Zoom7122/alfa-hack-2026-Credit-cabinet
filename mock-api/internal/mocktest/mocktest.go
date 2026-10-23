// Package mocktest — помощники для тестов моков: роутер, запросы, разбор ответа
// и ИНН тестовых сценариев A–E из docs/mock-apis.md.
package mocktest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// ИНН компаний сценариев A–E.
const (
	InnA = "7799000012" // здоровый: ООО «СЕВЕРНЫЙ ВЕТЕР»
	InnB = "7799000020" // перегружен долгами: ООО «СТРОЙРЕСУРС»
	InnC = "7799000037" // стоп-фактор: ООО «АЛЬТАИР»
	InnD = "779900010202"
	InnE = "7799000044" // спорный: ООО «ТЕХНО-СЕРВИС»
)

// ИНН директоров (у ИП D — она сама).
const (
	DirA = "779900010139" // Ковалёв Игорь Сергеевич
	DirB = "779900020136" // Орлов Денис Павлович
	DirC = "779900030147" // Белов Артём Николаевич
	DirD = InnD           // Смирнова Анна Викторовна
	DirE = "779900040158" // Зайцева Ольга Михайловна
)

// Unknown — корректный ИНН, которого нет ни в одном seed.
const Unknown = "7799000999"

var (
	Companies = []string{InnA, InnB, InnC, InnD, InnE}
	Directors = []string{DirA, DirB, DirC, DirD, DirE}
)

// Router — gin в тестовом режиме, register вешает маршруты на группу prefix.
func Router(prefix string, register func(*gin.RouterGroup), middleware ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	register(r.Group(prefix, middleware...))
	return r
}

func do(h http.Handler, req *http.Request, headers []string) *httptest.ResponseRecorder {
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// Get выполняет GET; headers — пары «имя, значение».
func Get(h http.Handler, target string, headers ...string) *httptest.ResponseRecorder {
	return do(h, httptest.NewRequest(http.MethodGet, target, nil), headers)
}

// PostJSON отправляет v как JSON (строку — как есть).
func PostJSON(t testing.TB, h http.Handler, target string, v any, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if s, ok := v.(string); ok {
		body = []byte(s)
	} else {
		var err error
		if body, err = json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return do(h, req, headers)
}

// PostForm отправляет application/x-www-form-urlencoded.
func PostForm(h http.Handler, target string, form url.Values, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return do(h, req, headers)
}

// Decode проверяет статус и разбирает тело в T.
func Decode[T any](t testing.TB, w *httptest.ResponseRecorder, status int) T {
	t.Helper()
	var out T
	if w.Code != status {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, status, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v; body: %s", err, w.Body)
	}
	return out
}

// ErrorCode проверяет статус и возвращает error.code из тела.
func ErrorCode(t testing.TB, w *httptest.ResponseRecorder, status int) string {
	t.Helper()
	e := Decode[struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}](t, w, status)
	return e.Error.Code
}

// Лаптев — тестовый субъект вне сценариев: дисквалифицирован, паспорт недействителен.
const Laptev = "779900099018"

// SeedFiles — пути seed-файлов пакета по шаблону (тест запускается из папки пакета).
func SeedFiles(t testing.TB, pattern string) []string {
	t.Helper()
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		t.Fatalf("no seed files for %s", pattern)
	}
	return files
}

// ReadSeed разбирает seed-файл в T.
func ReadSeed[T any](t testing.TB, path string) T {
	t.Helper()
	var out T
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return out
}
