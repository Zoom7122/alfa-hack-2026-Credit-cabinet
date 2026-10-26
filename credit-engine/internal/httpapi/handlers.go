package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
	"github.com/gin-gonic/gin"
)

type Decisions interface {
	Decide(context.Context, contracts.DecisionRequest) (contracts.DecisionResponse, error)
}
type ModelResolver interface {
	ModelVersion(contracts.DecisionRequest) (string, error)
}
type KeyedModelDecisions interface {
	DecideWithKeyModel(context.Context, contracts.DecisionRequest, string) (contracts.DecisionResponse, modelbindings.Metadata, error)
}
type ModelSnapshotResolver interface {
	ModelSnapshot(context.Context, contracts.DecisionRequest) (context.Context, modelbindings.Metadata, error)
}
type BankOperations interface {
	Check(context.Context, contracts.BankCheckRequest) (contracts.BankCheckResponse, error)
	ManualReview(context.Context, contracts.ManualReviewRequest) (contracts.ManualReviewResponse, error)
	Snapshot() contracts.BankStateResponse
}

func NewScoringRouter(service Decisions) *gin.Engine {
	r := NewRouter()
	r.POST("/api/v1/decisions", func(c *gin.Context) {
		request, err := scoring.Decode(c.Request.Body)
		if err != nil {
			writeError(c, err)
			return
		}
		key := c.GetHeader("Idempotency-Key")
		if len(key) > 200 {
			writeProblem(c, 400, "INVALID_IDEMPOTENCY_KEY", nil)
			return
		}
		if request.InputSnapshot != nil && key == "" {
			writeProblem(c, 400, "IDEMPOTENCY_KEY_REQUIRED", nil)
			return
		}
		if keyed, ok := service.(KeyedModelDecisions); ok && key != "" {
			result, metadata, err := keyed.DecideWithKeyModel(c.Request.Context(), request, key)
			if metadata.Version != "" {
				c.Header("X-Scoring-Model-Version", metadata.Version)
			}
			if metadata.DefinitionSHA256 != "" {
				c.Header("X-Scoring-Model-SHA256", metadata.DefinitionSHA256)
			}
			if err != nil {
				writeError(c, err)
				return
			}
			writeJSON(c, http.StatusOK, result)
			return
		}
		ctx := c.Request.Context()
		if resolver, ok := service.(ModelSnapshotResolver); ok {
			pinned, metadata, err := resolver.ModelSnapshot(ctx, request)
			if err != nil {
				writeError(c, err)
				return
			}
			ctx = pinned
			c.Header("X-Scoring-Model-Version", metadata.Version)
			c.Header("X-Scoring-Model-SHA256", metadata.DefinitionSHA256)
		} else if resolver, ok := service.(ModelResolver); ok {
			version, err := resolver.ModelVersion(request)
			if err != nil {
				writeError(c, err)
				return
			}
			c.Header("X-Scoring-Model-Version", version)
		}
		var result contracts.DecisionResponse
		if key != "" {
			keyed, ok := service.(interface {
				DecideWithKey(context.Context, contracts.DecisionRequest, string) (contracts.DecisionResponse, error)
			})
			if !ok {
				writeProblem(c, 503, "PERSISTENCE_REQUIRED", nil)
				return
			}
			result, err = keyed.DecideWithKey(ctx, request, key)
		} else {
			result, err = service.Decide(ctx, request)
		}
		if err != nil {
			writeError(c, err)
			return
		}
		writeJSON(c, http.StatusOK, result)
	})
	return r
}

func NewBankRouter(service BankOperations) *gin.Engine {
	r := NewRouter()
	r.POST("/api/v1/bank/check-and-issue", func(c *gin.Context) {
		var request contracts.BankCheckRequest
		if err := decodeBank(c.Request.Body, &request); err != nil {
			writeError(c, err)
			return
		}
		result, err := service.Check(c.Request.Context(), request)
		if err != nil {
			writeError(c, err)
			return
		}
		writeJSON(c, http.StatusOK, result)
	})
	r.POST("/api/v1/bank/manual-reviews", func(c *gin.Context) {
		var request contracts.ManualReviewRequest
		if err := decodeBank(c.Request.Body, &request); err != nil {
			writeError(c, err)
			return
		}
		result, err := service.ManualReview(c.Request.Context(), request)
		if err != nil {
			writeError(c, err)
			return
		}
		writeJSON(c, http.StatusAccepted, result)
	})
	r.GET("/api/v1/bank/state", func(c *gin.Context) {
		if durable, ok := service.(interface {
			SnapshotContext(context.Context) (contracts.BankStateResponse, error)
		}); ok {
			out, err := durable.SnapshotContext(c.Request.Context())
			if err != nil {
				writeError(c, err)
				return
			}
			writeJSON(c, 200, out)
			return
		}
		writeJSON(c, 200, service.Snapshot())
	})
	return r
}

func writeJSON(c *gin.Context, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Data(status, "application/json; charset=utf-8", data)
}

func decodeBank(reader io.Reader, out any) error {
	var data json.RawMessage
	d := json.NewDecoder(reader)
	bad := &scoring.Error{Status: 400, Code: "INVALID_JSON"}
	if err := d.Decode(&data); err != nil {
		return bad
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return bad
	}
	if err := json.Unmarshal(data, out); err != nil {
		return bad
	}
	return requiredFields(data, out)
}
