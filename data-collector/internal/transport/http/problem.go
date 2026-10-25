package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// problem — ответ об ошибке по RFC 9457 (схема Problem в api/openapi.yaml).
type problem struct {
	Type   string         `json:"type"`
	Title  string         `json:"title"`
	Status int            `json:"status"`
	Detail string         `json:"detail,omitempty"`
	Code   string         `json:"code"`
	Errors []problemField `json:"errors,omitempty"`
} //	@name	Problem

type problemField struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
} //	@name	ProblemField

const (
	codeValidationFailed  = "VALIDATION_FAILED"
	codeNotFound          = "NOT_FOUND"
	codeInvalidTransition = "INVALID_TRANSITION"
	codePayloadTooLarge   = "PAYLOAD_TOO_LARGE"
	codeUnsupportedMedia  = "UNSUPPORTED_MEDIA_TYPE"
	codeInternal          = "INTERNAL"
	codeUnauthorized      = "UNAUTHORIZED"
	codeForbidden         = "FORBIDDEN"
	problemTypeBase       = "https://errors.credit-cabinet.local/"
)

func writeProblem(c *gin.Context, p problem) {
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(p.Status, p)
}

func validationProblem(status int, detail string, fields []problemField) problem {
	return problem{
		Type:   problemTypeBase + "validation-failed",
		Title:  "Validation failed",
		Status: status,
		Detail: detail,
		Code:   codeValidationFailed,
		Errors: fields,
	}
}

// fromValidationError переводит нарушения бизнес-правил из сервиса в Problem 422.
func fromValidationError(verr *service.ValidationError, detail string) problem {
	fields := make([]problemField, 0, len(verr.Fields))
	for _, f := range verr.Fields {
		fields = append(fields, problemField{Field: f.Field, Message: f.Message, Code: f.Code})
	}
	return validationProblem(http.StatusUnprocessableEntity, detail, fields)
}

func unauthorizedProblem(detail string) problem {
	return problem{
		Type:   problemTypeBase + "unauthorized",
		Title:  "Unauthorized",
		Status: http.StatusUnauthorized,
		Detail: detail,
		Code:   codeUnauthorized,
	}
}

func forbiddenProblem(detail string) problem {
	return problem{
		Type:   problemTypeBase + "forbidden",
		Title:  "Forbidden",
		Status: http.StatusForbidden,
		Detail: detail,
		Code:   codeForbidden,
	}
}

func notFoundProblem(detail string) problem {
	return problem{
		Type:   problemTypeBase + "not-found",
		Title:  "Not found",
		Status: http.StatusNotFound,
		Detail: detail,
		Code:   codeNotFound,
	}
}

func conflictProblem(detail string) problem {
	return problem{
		Type:   problemTypeBase + "invalid-transition",
		Title:  "Invalid transition",
		Status: http.StatusConflict,
		Detail: detail,
		Code:   codeInvalidTransition,
	}
}

func tooLargeProblem(detail string) problem {
	return problem{
		Type:   problemTypeBase + "payload-too-large",
		Title:  "Payload too large",
		Status: http.StatusRequestEntityTooLarge,
		Detail: detail,
		Code:   codePayloadTooLarge,
	}
}

func unsupportedMediaProblem(detail string) problem {
	return problem{
		Type:   problemTypeBase + "unsupported-media-type",
		Title:  "Unsupported media type",
		Status: http.StatusUnsupportedMediaType,
		Detail: detail,
		Code:   codeUnsupportedMedia,
	}
}

func internalProblem() problem {
	return problem{
		Type:   problemTypeBase + "internal",
		Title:  "Internal server error",
		Status: http.StatusInternalServerError,
		Code:   codeInternal,
	}
}

// bindingProblem переводит ошибку разбора тела запроса в Problem 400.
func bindingProblem(err error) problem {
	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		fields := make([]problemField, 0, len(verrs))
		for _, fe := range verrs {
			fields = append(fields, problemField{
				Field:   fieldPath(fe),
				Message: "обязательное поле",
				Code:    fe.Tag(),
			})
		}
		return validationProblem(http.StatusBadRequest, "тело запроса не прошло проверку", fields)
	}
	var terr *json.UnmarshalTypeError
	if errors.As(err, &terr) {
		return validationProblem(http.StatusBadRequest, "неверный тип поля", []problemField{{
			Field:   terr.Field,
			Message: "ожидается " + terr.Type.String(),
			Code:    "invalid_type",
		}})
	}
	return validationProblem(http.StatusBadRequest, "некорректный JSON в теле запроса", nil)
}

// fieldPath — путь поля как в JSON (representative.full_name): корень — тип запроса — отбрасывается.
func fieldPath(fe validator.FieldError) string {
	if _, path, ok := strings.Cut(fe.Namespace(), "."); ok {
		return path
	}
	return jsonFieldName(fe.Field())
}

// jsonFieldName превращает имя Go-поля DTO в имя JSON-поля (ChildrenCount → children_count).
func jsonFieldName(goName string) string {
	var b strings.Builder
	for i, r := range goName {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
