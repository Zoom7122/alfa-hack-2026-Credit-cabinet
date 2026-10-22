// Package mockkit — общее для моков внешних систем: загрузка seed-файлов,
// формат ошибок, проверка ИНН, пагинация и имитация сбоев (X-Mock-Fault).
package mockkit

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// AsOf — «сегодня» для моков: срок согласий, checked_at для неизвестных ИНН.
// Зафиксировано, чтобы ответы не зависели от дня запуска.
const AsOf = "2026-10-22"

// CheckedAt — время проверки в ответах, которых нет в seed (неизвестный ИНН).
const CheckedAt = AsOf + "T10:15:00Z"

const (
	CodeInvalidINN         = "INVALID_INN"
	CodeBadRequest         = "BAD_REQUEST"
	CodeNotFound           = "NOT_FOUND"
	CodeConsentRequired    = "CONSENT_REQUIRED"
	CodeConsentExpired     = "CONSENT_EXPIRED"
	CodeCaptchaRequired    = "CAPTCHA_REQUIRED"
	CodeInternalError      = "INTERNAL_ERROR"
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"
	CodeTimeout            = "TIMEOUT"
)

var innRe = regexp.MustCompile(`^(\d{10}|\d{12})$`)

// ValidINN: 10 цифр у юрлица, 12 у ИП и физлица.
func ValidINN(s string) bool { return innRe.MatchString(s) }

// Seeds читает все *.json из dir и сжимает их (в ответах без отступов).
// Ключ — имя файла без расширения (обычно ИНН).
// Битый JSON — panic: ошибка всплывает при старте, а не на запросе.
func Seeds(fsys fs.FS, dir string) map[string][]byte {
	files, err := fs.Glob(fsys, path.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		panic("mockkit: no seed files in " + dir)
	}
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		body, err := fs.ReadFile(fsys, f)
		if err != nil {
			panic("mockkit: read " + f + ": " + err.Error())
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, body); err != nil {
			panic("mockkit: invalid JSON in " + f + ": " + err.Error())
		}
		out[strings.TrimSuffix(path.Base(f), ".json")] = buf.Bytes()
	}
	return out
}

// MustDecode разбирает seed в v или паникует (вызывается только при старте).
func MustDecode(name string, body []byte, v any) {
	if err := json.Unmarshal(body, v); err != nil {
		panic("mockkit: decode " + name + ": " + err.Error())
	}
}

// Error отвечает в общем формате {"error":{"code","message"}}.
func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

// JSON отдаёт готовое тело ответа как есть.
func JSON(c *gin.Context, body []byte) {
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// INNParam достаёт ИНН из пути и отвечает 400, если он неверный.
func INNParam(c *gin.Context) (string, bool) {
	inn := c.Param("inn")
	if !ValidINN(inn) {
		Error(c, http.StatusBadRequest, CodeInvalidINN, "inn must be 10 or 12 digits")
		return "", false
	}
	return inn, true
}

// IntQuery читает целый параметр запроса в пределах [lo, hi]; пустой — def.
func IntQuery(c *gin.Context, name string, def, lo, hi int) (int, bool) {
	raw := c.Query(name)
	if raw == "" {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		Error(c, http.StatusBadRequest, CodeBadRequest,
			name+" must be an integer from "+strconv.Itoa(lo)+" to "+strconv.Itoa(hi))
		return 0, false
	}
	return n, true
}

// ValidDate: пусто или YYYY-MM-DD. Даты сравниваем строками, формат ISO это позволяет.
func ValidDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// Paginate возвращает страницу page (с 1) размера size.
func Paginate[T any](items []T, page, size int) []T {
	from := (page - 1) * size
	if from >= len(items) {
		return []T{}
	}
	return items[from:min(from+size, len(items))]
}

var nameReplacer = strings.NewReplacer("Ё", "Е", "\"", " ", "«", " ", "»", " ", "'", " ")

// NormName приводит ФИО или название к виду для сравнения:
// верхний регистр, Ё→Е, без кавычек и лишних пробелов.
func NormName(s string) string {
	return strings.Join(strings.Fields(nameReplacer.Replace(strings.ToUpper(s))), " ")
}
