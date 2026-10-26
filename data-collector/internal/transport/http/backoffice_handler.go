package httptransport

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// BackofficeService — то, что рабочему месту андеррайтера нужно от бизнес-слоя.
type BackofficeService interface {
	Queue(ctx context.Context, userID string, f domain.QueueFilter) ([]domain.QueueItem, domain.QueueCounters, int, error)
	Dossier(ctx context.Context, appID string) (domain.Dossier, error)
	Assign(ctx context.Context, userID, appID string) (domain.QueueItem, error)
	Decide(ctx context.Context, userID, appID string, in service.DecisionInput) (domain.Dossier, error)
}

// BackofficeHandler — рабочее место андеррайтера (экраны 16–18): очередь, досье, взять в работу, решение.
type BackofficeHandler struct {
	svc BackofficeService
	log *slog.Logger
	now func() time.Time
}

func NewBackofficeHandler(svc BackofficeService, log *slog.Logger) *BackofficeHandler {
	return &BackofficeHandler{svc: svc, log: log, now: time.Now}
}

// WithBackoffice — ручки рабочего места; только для андеррайтера. Подключать после WithAuth.
func WithBackoffice(h *BackofficeHandler) RouterOption {
	return func(_ *gin.Engine, api *gin.RouterGroup) {
		bo := api.Group("/backoffice", requireUnderwriter)
		bo.GET("/applications", h.queue)
		bo.GET("/applications/:id", h.dossier)
		bo.POST("/applications/:id/assign", h.assign)
		bo.POST("/applications/:id/decision", h.decide)
	}
}

func requireUnderwriter(c *gin.Context) {
	user, ok := currentUser(c)
	switch {
	case !ok:
		writeProblem(c, unauthorizedProblem("нужен токен: Authorization: Bearer mock.<user_id>"))
	case user.Role != domain.UserUnderwriter:
		writeProblem(c, forbiddenProblem("рабочее место банка — только для андеррайтера"))
	default:
		c.Next()
		return
	}
	c.Abort()
}

// queue — GET /api/v1/backoffice/applications.
//
//	@Summary		Очередь андеррайтера
//	@Description	Заявки после отправки анкеты: сначала ручное рассмотрение по сроку, затем остальные — свежие сверху.
//	@Description	counters — счётчики над очередью (без учёта фильтров). Курсор — смещение следующей страницы.
//	@Tags			backoffice
//	@Produce		json
//	@Security		bearer
//	@Param			status		query		string	false	"статусы через запятую, например MANUAL_REVIEW,WAITING_FOR_INFO"
//	@Param			assignee	query		string	false	"me — мои, none — без исполнителя, или id андеррайтера"
//	@Param			limit		query		int		false	"до 100, по умолчанию 50"
//	@Param			cursor		query		string	false	"next_cursor прошлой страницы"
//	@Success		200			{object}	boQueueResponse
//	@Failure		401			{object}	problem
//	@Failure		403			{object}	problem	"не андеррайтер"
//	@Failure		422			{object}	problem	"неизвестный статус"
//	@Router			/backoffice/applications [get]
func (h *BackofficeHandler) queue(c *gin.Context) {
	user, _ := currentUser(c)
	f := domain.QueueFilter{UserID: user.ID, Assignee: strings.TrimSpace(c.Query("assignee"))}
	for _, s := range strings.Split(c.Query("status"), ",") {
		s = strings.TrimSpace(strings.ToUpper(s))
		if s == "" {
			continue
		}
		st := domain.ApplicationStatus(s)
		if !st.Valid() {
			writeProblem(c, validationProblem(http.StatusUnprocessableEntity, "неизвестный статус",
				[]problemField{{Field: "status", Code: "invalid_value", Message: "допустимые значения: " + statusList()}}))
			return
		}
		f.Statuses = append(f.Statuses, st)
	}
	f.Limit, _ = strconv.Atoi(c.Query("limit"))
	f.Offset, _ = strconv.Atoi(c.Query("cursor"))
	f.Offset = max(f.Offset, 0)
	items, counters, next, err := h.svc.Queue(c.Request.Context(), user.ID, f)
	if err != nil {
		h.internal(c, "backoffice queue", err)
		return
	}
	out := boQueueResponse{Items: make([]boQueueItemResponse, 0, len(items)), Counters: boCountersResponse{ManualReview: counters.ManualReview,
		Mine: counters.Mine, DueToday: counters.DueToday, WaitingForInfo: counters.WaitingForInfo}}
	for _, it := range items {
		out.Items = append(out.Items, toQueueItem(it))
	}
	if next > 0 {
		cur := strconv.Itoa(next)
		out.NextCursor = &cur
	}
	c.JSON(http.StatusOK, out)
}

// dossier — GET /api/v1/backoffice/applications/{id}.
//
//	@Summary		Досье заявки
//	@Description	Заявка с версией (для If-Match решения), исполнителем и сроком, компания по собранным данным,
//	@Description	итог сбора по системам, находки, скоринг и решение credit-engine, предложения, вопросы клиенту,
//	@Description	другие заявки компании и журнал со служебными событиями. ETag — версия заявки.
//	@Tags			backoffice
//	@Produce		json
//	@Security		bearer
//	@Param			id	path		string	true	"id заявки (app_…)"
//	@Success		200	{object}	boDossierResponse
//	@Failure		401	{object}	problem
//	@Failure		403	{object}	problem
//	@Failure		404	{object}	problem
//	@Router			/backoffice/applications/{id} [get]
func (h *BackofficeHandler) dossier(c *gin.Context) {
	d, err := h.svc.Dossier(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "backoffice dossier", err)
		return
	}
	c.Header("ETag", strconv.Quote(strconv.Itoa(d.Version)))
	c.JSON(http.StatusOK, toDossier(d, h.now()))
}

// assign — POST /api/v1/backoffice/applications/{id}/assign.
//
//	@Summary	Взять заявку в работу
//	@Tags		backoffice
//	@Produce	json
//	@Security	bearer
//	@Param		id	path		string	true	"id заявки (app_…)"
//	@Success	200	{object}	boQueueItemResponse
//	@Failure	401	{object}	problem
//	@Failure	403	{object}	problem
//	@Failure	404	{object}	problem
//	@Failure	409	{object}	problem	"не на ручном рассмотрении (INVALID_TRANSITION) или уже у другого (CONFLICT)"
//	@Router		/backoffice/applications/{id}/assign [post]
func (h *BackofficeHandler) assign(c *gin.Context) {
	user, _ := currentUser(c)
	item, err := h.svc.Assign(c.Request.Context(), user.ID, c.Param("id"))
	if err != nil {
		h.fail(c, "backoffice assign", err)
		return
	}
	c.JSON(http.StatusOK, toQueueItem(item))
}

// decide — POST /api/v1/backoffice/applications/{id}/decision.
//
//	@Summary		Решение андеррайтера
//	@Description	APPROVE — предложения клиенту (offers; пусто — условия, предложенные credit-engine), статус OFFERED.
//	@Description	REJECT — отказ с причинами для клиента (client_reasons), повторно — через 30 дней, статус REJECTED.
//	@Description	REQUEST_INFO — вопросы клиенту (info_requests), срок ответа 5 дней, статус WAITING_FOR_INFO.
//	@Description	expert_adjustments и internal_comment — в журнал, клиенту не видны. Заявка без исполнителя
//	@Description	закрепляется за андеррайтером. Повтор с тем же Idempotency-Key ничего не меняет.
//	@Tags			backoffice
//	@Accept			json
//	@Produce		json
//	@Security		bearer
//	@Param			id				path		string			true	"id заявки (app_…)"
//	@Param			If-Match		header		string			false	"версия заявки из досье"
//	@Param			Idempotency-Key	header		string			false	"ключ повтора"
//	@Param			body			body		decisionRequest	true	"решение"
//	@Success		200				{object}	boDossierResponse
//	@Failure		400				{object}	problem
//	@Failure		401				{object}	problem
//	@Failure		403				{object}	problem
//	@Failure		404				{object}	problem
//	@Failure		409				{object}	problem	"не на ручном рассмотрении или у другого андеррайтера"
//	@Failure		412				{object}	problem	"заявка изменилась (If-Match)"
//	@Failure		422				{object}	problem
//	@Router			/backoffice/applications/{id}/decision [post]
func (h *BackofficeHandler) decide(c *gin.Context) {
	user, _ := currentUser(c)
	var req decisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, bindingProblem(err))
		return
	}
	in := service.DecisionInput{Action: strings.ToUpper(strings.TrimSpace(req.Action)), IdempotencyKey: c.GetHeader("Idempotency-Key"),
		InternalComment: req.InternalComment}
	if m := strings.Trim(strings.TrimPrefix(c.GetHeader("If-Match"), "W/"), `" `); m != "" {
		v, err := strconv.Atoi(m)
		if err != nil {
			writeProblem(c, validationProblem(http.StatusBadRequest, "If-Match — версия заявки числом", nil))
			return
		}
		in.IfMatch = &v
	}
	for _, o := range req.Offers {
		in.Offers = append(in.Offers, domain.OfferInput{Kind: o.Kind, ProductCode: o.ProductCode, Amount: o.Amount, TermMonths: o.TermMonths,
			Rate: o.RateAnnual, CollateralRequired: o.CollateralRequired, GuaranteeRequired: o.GuaranteeRequired, Conditions: o.Conditions,
			ValidDays: o.ValidDays})
	}
	for _, r := range req.ClientReasons {
		in.ClientReasons = append(in.ClientReasons, domain.ClientReason{Code: r.Code, Title: r.Title, Description: r.Description})
	}
	for _, q := range req.InfoRequests {
		in.InfoRequests = append(in.InfoRequests, domain.InfoRequestInput{Title: q.Title, Description: q.Description,
			TargetField: q.TargetField, Required: q.Required, Input: q.Input, Documents: q.Documents})
	}
	for _, a := range req.Adjustments {
		in.Adjustments = append(in.Adjustments, domain.Adjustment{Code: a.Code, Points: a.Points, Comment: a.Comment})
	}
	d, err := h.svc.Decide(c.Request.Context(), user.ID, c.Param("id"), in)
	if err != nil {
		h.fail(c, "backoffice decision", err)
		return
	}
	c.Header("ETag", strconv.Quote(strconv.Itoa(d.Version)))
	c.JSON(http.StatusOK, toDossier(d, h.now()))
}

func (h *BackofficeHandler) fail(c *gin.Context, op string, err error) {
	var verr *service.ValidationError
	switch {
	case errors.As(err, &verr):
		writeProblem(c, fromValidationError(verr, "решение не прошло проверку"))
	case errors.Is(err, service.ErrNotFound):
		writeProblem(c, notFoundProblem("заявка не найдена"))
	case errors.Is(err, domain.ErrNotInReview):
		writeProblem(c, problem{Type: problemTypeBase + "invalid-transition", Title: "Conflict", Status: http.StatusConflict,
			Detail: "заявка не на ручном рассмотрении", Code: "INVALID_TRANSITION"})
	case errors.Is(err, domain.ErrAssignedToOther):
		writeProblem(c, problem{Type: problemTypeBase + "conflict", Title: "Conflict", Status: http.StatusConflict,
			Detail: "заявку уже взял другой андеррайтер", Code: "CONFLICT"})
	case errors.Is(err, domain.ErrVersionConflict):
		writeProblem(c, problem{Type: problemTypeBase + "version-conflict", Title: "Precondition failed", Status: http.StatusPreconditionFailed,
			Detail: "заявка изменилась — обновите досье", Code: "VERSION_CONFLICT"})
	default:
		h.log.ErrorContext(c.Request.Context(), op, "err", err)
		writeProblem(c, internalProblem())
	}
}

func (h *BackofficeHandler) internal(c *gin.Context, op string, err error) {
	h.log.ErrorContext(c.Request.Context(), op, "err", err)
	writeProblem(c, internalProblem())
}
