package modelgen

import (
	"fmt"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/model"
)

// ValidationError exposes the shared validator result to CLI/HTTP callers.
// It carries no connection details or dependency response bodies.
type ValidationError struct {
	Version string
	Result  model.ValidationResult
}

func (e *ValidationError) Error() string {
	if len(e.Result.Issues) == 0 {
		return fmt.Sprintf("modelgen: invalid model %s", e.Version)
	}
	first := e.Result.Issues[0]
	return fmt.Sprintf("modelgen: invalid model %s: %s at %s", e.Version, first.Code, first.Path)
}
