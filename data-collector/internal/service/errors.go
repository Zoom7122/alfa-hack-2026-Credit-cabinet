package service

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrNotFound — запрошенной сущности нет.
	ErrNotFound = errors.New("not found")
	// ErrInvalidTransition — действие недопустимо в текущем статусе: анкету по заявке уже отправили.
	ErrInvalidTransition = errors.New("invalid transition")
	// ErrFileTooLarge — файл больше лимита.
	ErrFileTooLarge = errors.New("file is too large")
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
