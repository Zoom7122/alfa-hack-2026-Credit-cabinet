package service

import (
	"fmt"
	"strings"
)

// FieldError — нарушение правила для одного поля анкеты.
type FieldError struct {
	Field   string
	Code    string
	Message string
}

// ValidationError собирает все нарушения бизнес-правил сразу,
// чтобы клиент исправил форму за один заход.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		parts = append(parts, fmt.Sprintf("%s: %s", f.Field, f.Message))
	}
	return "validation failed: " + strings.Join(parts, "; ")
}
