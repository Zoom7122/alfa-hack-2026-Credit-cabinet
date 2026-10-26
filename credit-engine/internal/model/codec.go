package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

const MaxDefinitionBytes = 4 << 20
const MaxDefinitionDepth = 64

type FormatError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *FormatError) Error() string {
	return fmt.Sprintf("model %s at %s: %s", e.Code, e.Path, e.Message)
}

func formatError(path, code, message string) error {
	return &FormatError{Path: path, Code: code, Message: message}
}

// Decode checks the document format, not model business validity. Drafts with
// invalid weights/rules can be read and saved; release validation is separate.
func Decode(r io.Reader) (ModelDefinition, error) {
	var result ModelDefinition
	if r == nil {
		return result, formatError("/", "INVALID_JSON", "reader is required")
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxDefinitionBytes+1))
	if err != nil {
		return result, fmt.Errorf("read model definition: %w", err)
	}
	if len(data) > MaxDefinitionBytes {
		return result, formatError("/", "DOCUMENT_TOO_LARGE", "definition exceeds 4 MiB")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return result, formatError("/", "INVALID_JSON", "expected one JSON object")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err = checkTokens(d, "/", 0); err != nil {
		return result, err
	}
	if _, err = d.Token(); err != io.EOF {
		return result, formatError("/", "INVALID_JSON", "expected exactly one JSON document")
	}
	if err = checkPresence(data, reflect.TypeOf(result), ""); err != nil {
		return result, err
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err = d.Decode(&result); err != nil {
		return ModelDefinition{}, fmt.Errorf("decode model definition: %w", err)
	}
	if result.FormatVersion != FormatVersion {
		return ModelDefinition{}, formatError("/format_version", "UNSUPPORTED_FORMAT", "expected format_version=1")
	}
	return result, nil
}

// Marshal is deterministic for an unchanged definition and preserves ordered
// criteria/rules. It performs no model normalization or weight corrections.
func Marshal(d ModelDefinition) ([]byte, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode model definition: %w", err)
	}
	if len(data)+1 > MaxDefinitionBytes {
		return nil, formatError("/", "DOCUMENT_TOO_LARGE", "definition exceeds 4 MiB")
	}
	if _, err = Decode(bytes.NewReader(data)); err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func pointer(path, key string) string {
	return strings.TrimSuffix(path, "/") + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func checkTokens(d *json.Decoder, path string, depth int) error {
	if depth > MaxDefinitionDepth {
		return formatError(path, "DOCUMENT_TOO_DEEP", "definition nesting exceeds 64")
	}
	token, err := d.Token()
	if err != nil {
		return formatError(path, "INVALID_JSON", "invalid JSON value")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			token, err = d.Token()
			if err != nil {
				return formatError(path, "INVALID_JSON", "invalid object key")
			}
			key, ok := token.(string)
			if !ok {
				return formatError(path, "INVALID_JSON", "object key is not a string")
			}
			if seen[key] {
				return formatError(pointer(path, key), "DUPLICATE_KEY", "duplicate object key")
			}
			seen[key] = true
			if err = checkTokens(d, pointer(path, key), depth+1); err != nil {
				return err
			}
		}
	case '[':
		for i := 0; d.More(); i++ {
			if err = checkTokens(d, pointer(path, strconv.Itoa(i)), depth+1); err != nil {
				return err
			}
		}
	default:
		return formatError(path, "INVALID_JSON", "unexpected delimiter")
	}
	end, err := d.Token()
	if err != nil || delim == '{' && end != json.Delim('}') || delim == '[' && end != json.Delim(']') {
		return formatError(path, "INVALID_JSON", "unclosed container")
	}
	return nil
}

func checkPresence(raw json.RawMessage, typ reflect.Type, path string) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		if typ.Kind() == reflect.Pointer {
			return nil
		}
		return formatError(path, "NULL_FIELD", "non-null field is null")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(NumberLiteral("")) {
		token := NumberLiteral(string(bytes.TrimSpace(raw)))
		if !validNumber(token) {
			return formatError(path, "INVALID_NUMBER", "Expected a finite JSON number token within the numeric resource limits")
		}
		return nil
	}
	switch typ.Kind() {
	case reflect.Struct:
		var obj map[string]json.RawMessage
		if json.Unmarshal(raw, &obj) != nil || obj == nil {
			return formatError(path, "INVALID_TYPE", "expected object")
		}
		allowed := map[string]bool{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			parts := strings.Split(field.Tag.Get("json"), ",")
			name := parts[0]
			if name == "" || name == "-" {
				continue
			}
			allowed[name] = true
			value, present := obj[name]
			optional := false
			for _, option := range parts[1:] {
				optional = optional || option == "omitempty"
			}
			if !present {
				if !optional {
					return formatError(pointer(path, name), "MISSING_FIELD", "required document field is absent")
				}
				continue
			}
			if err := checkPresence(value, field.Type, pointer(path, name)); err != nil {
				return err
			}
		}
		keys := make([]string, 0, len(obj))
		for key := range obj {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if !allowed[key] {
				return formatError(pointer(path, key), "UNKNOWN_FIELD", "unknown document field; JSON names are case-sensitive")
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return formatError(path, "INVALID_TYPE", "expected array")
		}
		for i, value := range values {
			if err := checkPresence(value, typ.Elem(), pointer(path, strconv.Itoa(i))); err != nil {
				return err
			}
		}
	}
	return nil
}
