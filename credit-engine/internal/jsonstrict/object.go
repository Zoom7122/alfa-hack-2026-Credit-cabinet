// Package jsonstrict supplies bounded JSON primitives for generated named
// protocol DTOs. It does not select models or interpret business requiredness.
package jsonstrict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxBytes = 1 << 20
const MaxDepth = 64

type Error struct {
	Path, Code, Message string
	Cause               error
}

func (e *Error) Error() string              { return fmt.Sprintf("JSON %s at %s: %s", e.Code, e.Path, e.Message) }
func (e *Error) Unwrap() error              { return e.Cause }
func fail(path, code, message string) error { return &Error{Path: path, Code: code, Message: message} }
func child(path, key string) string {
	return strings.TrimSuffix(path, "/") + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
func Null(raw []byte) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

func Read(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, fail("/", "INVALID_JSON", "reader is required")
	}
	data, err := io.ReadAll(io.LimitReader(reader, MaxBytes+1))
	if err != nil {
		return nil, &Error{Path: "/", Code: "READ_ERROR", Message: "cannot read document", Cause: err}
	}
	if err = Check(data); err != nil {
		return nil, err
	}
	return data, nil
}
func Check(data []byte) error {
	return CheckLimit(data, MaxBytes)
}

// CheckLimit supports bounded administrative editor documents without changing
// the default 1 MiB protocol limit used by Read, Check and generated DTOs.
func CheckLimit(data []byte, maxBytes int) error {
	if maxBytes < 1 {
		return fail("/", "INVALID_LIMIT", "a positive document limit is required")
	}
	if len(data) > maxBytes {
		message := "document exceeds the configured byte limit"
		if maxBytes == MaxBytes {
			message = "document exceeds 1 MiB"
		}
		return fail("/", "TOO_LARGE", message)
	}
	if !utf8.Valid(data) {
		return fail("/", "INVALID_JSON", "document is not valid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := tokens(d, "/", 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fail("/", "INVALID_JSON", "expected exactly one document")
	}
	return nil
}
func tokens(d *json.Decoder, path string, depth int) error {
	if depth > MaxDepth {
		return fail(path, "TOO_DEEP", "document nesting exceeds 64")
	}
	token, err := d.Token()
	if err != nil {
		return fail(path, "INVALID_JSON", "invalid JSON value")
	}
	delim, container := token.(json.Delim)
	if !container {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			keyToken, err := d.Token()
			if err != nil {
				return fail(path, "INVALID_JSON", "invalid object key")
			}
			key, ok := keyToken.(string)
			if !ok {
				return fail(path, "INVALID_JSON", "invalid object key")
			}
			if seen[key] {
				return fail(child(path, key), "DUPLICATE_KEY", "duplicate object key")
			}
			seen[key] = true
			if err = tokens(d, child(path, key), depth+1); err != nil {
				return err
			}
		}
	case '[':
		for index := 0; d.More(); index++ {
			if err = tokens(d, child(path, strconv.Itoa(index)), depth+1); err != nil {
				return err
			}
		}
	default:
		return fail(path, "INVALID_JSON", "unexpected delimiter")
	}
	end, err := d.Token()
	if err != nil || delim == '{' && end != json.Delim('}') || delim == '[' && end != json.Delim(']') {
		return fail(path, "INVALID_JSON", "unclosed container")
	}
	return nil
}

// Object requires every declared key, rejects extras and enforces nullability.
// The returned raw map is parsing scratch space; generated DTOs own named fields.
func Object(data []byte, path string, fields, nullable []string) (map[string]json.RawMessage, error) {
	if err := Check(data); err != nil {
		if format, ok := err.(*Error); ok && path != "" && path != "/" {
			copy := *format
			copy.Path = strings.TrimSuffix(path, "/") + "/" + strings.TrimPrefix(format.Path, "/")
			err = &copy
		}
		return nil, err
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return nil, fail(path, "INVALID_TYPE", "expected object")
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fail(path, "INVALID_JSON", "invalid object")
	}
	allowed, mayNull := map[string]bool{}, map[string]bool{}
	for _, key := range nullable {
		mayNull[key] = true
	}
	for _, key := range fields {
		allowed[key] = true
		raw, exists := result[key]
		if !exists {
			return nil, fail(child(path, key), "MISSING_FIELD", "required key is missing")
		}
		if Null(raw) && !mayNull[key] {
			return nil, fail(child(path, key), "NULL_FIELD", "non-null field is null")
		}
	}
	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !allowed[key] {
			return nil, fail(child(path, key), "UNKNOWN_FIELD", "unknown field; names are case-sensitive")
		}
	}
	return result, nil
}
func String(raw json.RawMessage, path string) (string, error) {
	var value string
	if Null(raw) || json.Unmarshal(raw, &value) != nil {
		return "", fail(path, "INVALID_TYPE", "expected string")
	}
	return value, nil
}
func Boolean(raw json.RawMessage, path string) (bool, error) {
	var value bool
	if Null(raw) || json.Unmarshal(raw, &value) != nil {
		return false, fail(path, "INVALID_TYPE", "expected boolean")
	}
	return value, nil
}
