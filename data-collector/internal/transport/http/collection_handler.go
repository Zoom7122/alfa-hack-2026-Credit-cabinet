package httptransport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// CollectionService — то, что хендлеру задач на сбор нужно от бизнес-слоя.
type CollectionService interface {
	Get(ctx context.Context, id string) (domain.CollectionTask, error)
}

// CollectionHandler отдаёт состояние задач на сбор данных. Маршруты — в router.go.
type CollectionHandler struct {
	svc CollectionService
	log *slog.Logger
}

func NewCollectionHandler(svc CollectionService, log *slog.Logger) *CollectionHandler {
	return &CollectionHandler{svc: svc, log: log}
}

// get — GET /api/v1/collections/{id}.
//
//	@Summary		Состояние сбора данных по анкете
//	@Description	Задача на сбор, созданная POST /questionnaires. PENDING — ждёт worker, RUNNING — системы опрашиваются,
//	@Description	COMPLETED — сбор сохранён в raw_collections (строка raw_collection_id), FAILED — не выполнена, причина в error.
//	@Description	У COMPLETED в sources — состояние по каждой системе: DONE, PARTIAL, FAILED, SKIPPED.
//	@Tags			collections
//	@Produce		json
//	@Param			id	path		string	true	"id задачи (tsk_…)"
//	@Success		200	{object}	collectionTaskResponse
//	@Failure		404	{object}	problem	"задачи нет"
//	@Failure		500	{object}	problem
//	@Security		bearer
//	@Router			/collections/{id} [get]
func (h *CollectionHandler) get(c *gin.Context) {
	task, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeProblem(c, notFoundProblem("задача на сбор не найдена"))
			return
		}
		h.log.ErrorContext(c.Request.Context(), "get collection task", "err", err)
		writeProblem(c, internalProblem())
		return
	}
	c.JSON(http.StatusOK, toCollectionTaskResponse(task))
}
