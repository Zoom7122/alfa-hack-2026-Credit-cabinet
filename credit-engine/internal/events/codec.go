// Package events validates the existing MS2 notification contract. It never
// scores a borrower, issues a loan, or sends an acknowledgement itself.
package events

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/jsonstrict"
)

type Error struct {
	Status int
	Code   string
	Fields []string
}

func (e *Error) Error() string { return fmt.Sprintf("credit event %s", e.Code) }
func invalid(status int, code, path string) error {
	return &Error{Status: status, Code: code, Fields: []string{path}}
}

func Decode(reader io.Reader) (contracts.AdminEvent, error) {
	var event contracts.AdminEvent
	data, err := jsonstrict.Read(reader)
	if err != nil {
		var status = 400
		if format, ok := err.(*jsonstrict.Error); ok && format.Code == "TOO_LARGE" {
			status = 413
		}
		return event, invalid(status, "INVALID_EVENT_JSON", "/")
	}
	if err := shape(data, reflect.TypeOf(event), ""); err != nil {
		return event, err
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return event, invalid(400, "INVALID_EVENT_STRUCTURE", "/")
	}
	if err := Validate(event); err != nil {
		return contracts.AdminEvent{}, err
	}
	return event, nil
}

func Canonical(event contracts.AdminEvent) ([]byte, error) {
	if err := Validate(event); err != nil {
		return nil, err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return nil, invalid(422, "INVALID_EVENT_VALUE", "/")
	}
	if len(data) > jsonstrict.MaxBytes {
		return nil, invalid(413, "EVENT_TOO_LARGE", "/")
	}
	if err := shape(data, reflect.TypeOf(event), ""); err != nil {
		return nil, err
	}
	return data, nil
}

func pointer(path, key string) string {
	return path + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

// Reflection checks named DTO members and presence only. Numeric/boolean union
// values are decoded by EconomicCriterion.UnmarshalJSON, never a financial map.
func shape(raw json.RawMessage, typ reflect.Type, path string) error {
	if jsonstrict.Null(raw) {
		if typ.Kind() == reflect.Pointer {
			return nil
		}
		return invalid(400, "INVALID_EVENT_STRUCTURE", path)
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.Kind() {
	case reflect.Struct:
		var members map[string]json.RawMessage
		if err := json.Unmarshal(raw, &members); err != nil || members == nil {
			return invalid(400, "INVALID_EVENT_STRUCTURE", path)
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
			value, exists := members[name]
			optional := false
			for _, part := range parts[1:] {
				optional = optional || part == "omitempty"
			}
			if !exists {
				if optional {
					continue
				}
				return invalid(400, "MISSING_EVENT_FIELD", pointer(path, name))
			}
			if err := shape(value, field.Type, pointer(path, name)); err != nil {
				return err
			}
		}
		for name := range members {
			if !allowed[name] {
				return invalid(400, "UNKNOWN_EVENT_FIELD", pointer(path, name))
			}
		}
	case reflect.Slice:
		var elements []json.RawMessage
		if json.Unmarshal(raw, &elements) != nil {
			return invalid(400, "INVALID_EVENT_STRUCTURE", path)
		}
		for index, element := range elements {
			if err := shape(element, typ.Elem(), pointer(path, strconv.Itoa(index))); err != nil {
				return err
			}
		}
	}
	return nil
}
