package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

const maxAdminBody = 4<<20 + 32<<10

// Only the envelope is decoded here. A model definition is separately decoded
// by model.Decode with its own complete format/presence/duplicate checks.
func adminObject(data []byte, required []string) (map[string]json.RawMessage, error) {
	return adminObjectNullable(data, required, nil)
}
func adminObjectNullable(data []byte, required, nullable []string) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("expected object")
	}
	allowed := map[string]bool{}
	for _, key := range required {
		allowed[key] = true
	}
	mayNull := map[string]bool{}
	for _, key := range nullable {
		mayNull[key] = true
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		keyToken, err := d.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyToken.(string)
		if !ok || !allowed[key] {
			return nil, errors.New("unknown field")
		}
		if _, exists := fields[key]; exists {
			return nil, errors.New("duplicate field")
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, err
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !mayNull[key] {
			return nil, errors.New("null field")
		}
		fields[key] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("trailing document")
	}
	if len(fields) != len(required) {
		return nil, errors.New("missing field")
	}
	return fields, nil
}
func adminBody(c *gin.Context, fields ...string) (map[string]json.RawMessage, bool) {
	media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || media != "application/json" {
		writeProblem(c, 415, "JSON_REQUIRED", nil)
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, maxAdminBody+1))
	if err != nil {
		writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
		return nil, false
	}
	if len(data) > maxAdminBody {
		writeProblem(c, 413, "ADMIN_REQUEST_TOO_LARGE", nil)
		return nil, false
	}
	if !utf8.Valid(data) {
		writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
		return nil, false
	}
	result, err := adminObject(data, fields)
	if err != nil {
		writeProblem(c, 400, "INVALID_ADMIN_REQUEST", nil)
		return nil, false
	}
	return result, true
}
func adminString(raw json.RawMessage) (string, error) {
	var value string
	err := json.Unmarshal(raw, &value)
	if err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || strings.ContainsRune(value, 0) {
		return "", errors.New("invalid string")
	}
	return value, nil
}
func adminRevision(raw json.RawMessage) (int64, error) {
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil || value < 1 {
		return 0, errors.New("expected positive revision")
	}
	return value, nil
}

func adminPage(c *gin.Context) (limit, offset int, ok bool) {
	limit = 50
	offset = 0
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		writeProblem(c, 400, "INVALID_PAGINATION", nil)
		return 0, 0, false
	}
	for key, values := range query {
		if key != "limit" && key != "offset" || len(values) != 1 {
			writeProblem(c, 400, "INVALID_PAGINATION", nil)
			return 0, 0, false
		}
		value, err := strconv.Atoi(values[0])
		if err != nil || value < 0 || key == "limit" && (value < 1 || value > 200) {
			writeProblem(c, 400, "INVALID_PAGINATION", nil)
			return 0, 0, false
		}
		if key == "limit" {
			limit = value
		} else {
			offset = value
		}
	}
	return limit, offset, true
}
