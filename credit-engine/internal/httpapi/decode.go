package httpapi

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

// MS2 DTOs use non-pointer fields for mandatory wire values. Reject missing/null
// values before decoding could silently turn an absent annual_rate into 0%.
// Pointer fields remain nullable; their business meaning is checked by bank.
// Maps below contain JSON presence metadata only, not financial calculations.
func requiredFields(data []byte, out any) error {
	var fields []string
	var inspect func(json.RawMessage, reflect.Type, string)
	inspect = func(raw json.RawMessage, typ reflect.Type, path string) {
		if typ.Kind() == reflect.Pointer {
			return
		}
		if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			fields = append(fields, path)
			return
		}
		switch typ.Kind() {
		case reflect.Struct:
			var members map[string]json.RawMessage
			if json.Unmarshal(raw, &members) != nil {
				fields = append(fields, path)
				return
			}
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				if name == "" || name == "-" {
					continue
				}
				child := name
				if path != "" {
					child = path + "." + name
				}
				inspect(members[name], field.Type, child)
			}
		case reflect.Slice:
			var elements []json.RawMessage
			if json.Unmarshal(raw, &elements) != nil {
				fields = append(fields, path)
				return
			}
			for _, element := range elements {
				inspect(element, typ.Elem(), path)
			}
		}
	}
	inspect(data, reflect.TypeOf(out).Elem(), "")
	if len(fields) > 0 {
		return &scoring.Error{Status: 400, Code: "MISSING_REQUIRED_FIELD", Fields: fields}
	}
	return nil
}
