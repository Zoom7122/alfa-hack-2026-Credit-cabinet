package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/bank"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/clients"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
	"github.com/gin-gonic/gin"
)

func writeError(c *gin.Context, err error) {
	status, code, fields := 500, "INTERNAL_ERROR", []string{}
	var source *scoring.Error
	var state *bank.Error
	var rest *clients.Error
	switch {
	case errors.As(err, &source):
		status, code, fields = source.Status, source.Code, source.Fields
	case errors.As(err, &state):
		status, code, fields = state.Status, state.Code, state.Fields
	case errors.As(err, &rest):
		status, code, fields = rest.Status, rest.Code, rest.Fields
	case errors.Is(err, context.DeadlineExceeded):
		status, code = 504, "REQUEST_TIMEOUT"
	case errors.Is(err, context.Canceled):
		status, code = 502, "REQUEST_CANCELED"
	}
	if status != 400 && status != 409 && status != 422 && status != 503 && status != 502 && status != 504 {
		status, code, fields = 500, "INTERNAL_ERROR", []string{}
	}
	if code == "OPERATION_IN_PROGRESS" {
		c.Header("Retry-After", "2")
	}
	writeProblem(c, status, code, fields)
}

func writeProblem(c *gin.Context, status int, code string, fields []string) {
	if fields == nil {
		fields = []string{}
	}
	title := http.StatusText(status)
	detail := title
	if status == 500 {
		detail = "Internal server error"
	}
	c.Header("Content-Type", "application/problem+json")
	c.AbortWithStatusJSON(status, contracts.Problem{Type: "about:blank", Title: title, Status: status, Code: code, Detail: detail, Fields: fields})
}
