package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/storage"
)

// OutcomeService — итог для клиента.
type OutcomeService interface {
	Decision(ctx context.Context, appID string) (service.ClientDecision, error)
	Offers(ctx context.Context, appID string) ([]domain.OfferRecord, error)
	InfoRequests(ctx context.Context, appID string) ([]domain.InfoRequestRecord, error)
	StageFile(name string, r io.Reader) (storage.Staged, error)
	Discard(files []storage.Staged)
	Answer(ctx context.Context, actorID, appID, requestID string, in service.AnswerInput) (domain.ApplicationStatus, error)
}

// OutcomeHandler — что клиент видит после оценки: решение, предложения, вопросы банка и ответы на них.
type OutcomeHandler struct {
	svc     OutcomeService
	maxBody int64
	log     *slog.Logger
}

func NewOutcomeHandler(svc OutcomeService, maxBody int64, log *slog.Logger) *OutcomeHandler {
	return &OutcomeHandler{svc: svc, maxBody: maxBody, log: log}
}

// WithOutcome — ручки итога для клиента.
func WithOutcome(h *OutcomeHandler) RouterOption {
	return func(_ *gin.Engine, api *gin.RouterGroup) {
		api.GET("/applications/:id/decision", h.decision)
		api.GET("/applications/:id/offers", h.offers)
		api.GET("/applications/:id/info-requests", h.infoRequests)
		api.POST("/applications/:id/info-requests/:requestId/answer", h.answer)
	}
}

type clientDecisionResponse struct {
	Outcome string `json:"outcome" enums:"APPROVED,APPROVED_WITH_CHANGES,REJECTED,MANUAL_REVIEW,NEED_INFO"`
	// Причины отказа для клиента — без баллов и внутренних проверок.
	Reasons      []domain.ClientReason `json:"reasons"`
	ReapplyAfter *string               `json:"reapply_after"` // когда можно подать заявку снова
	DecidedAt    time.Time             `json:"decided_at"`
	DecidedBy    string                `json:"decided_by" enums:"ENGINE,EMPLOYEE"`
} //	@name	ClientDecision

type offersResponse struct {
	Items []boOfferResponse `json:"items"`
} //	@name	Offers

type infoRequestsResponse struct {
	Items []boInfoRequestResponse `json:"items"`
} //	@name	InfoRequests

type answerRequest struct {
	DocumentIDs []string `json:"document_ids,omitempty"`
	// Значение по input.type вопроса: число (NUMBER, MONEY), код варианта (CHOICE), строка (TEXT). NONE — не нужно.
	Value   json.RawMessage `json:"value" swaggertype:"string"`
	Comment string          `json:"comment"`
} //	@name	InfoRequestAnswerInput

type answerResponse struct {
	// Статус заявки после ответа: WAITING_FOR_INFO — есть ещё вопросы; VERIFYING — повторная оценка;
	// MANUAL_REVIEW — вопросы задал специалист, заявка вернулась к нему.
	Status       string                  `json:"status"`
	InfoRequests []boInfoRequestResponse `json:"info_requests"`
} //	@name	InfoRequestAnswered

// decision — GET /api/v1/applications/{id}/decision.
//
//	@Summary		Решение по заявке
//	@Description	Итог оценки (движка или специалиста) для клиента: причины отказа и когда можно подать снова.
//	@Tags			applications
//	@Produce		json
//	@Param			id	path		string	true	"id заявки (app_…)"
//	@Success		200	{object}	clientDecisionResponse
//	@Failure		404	{object}	problem	"заявки нет или её ещё не оценили"
//	@Router			/applications/{id}/decision [get]
func (h *OutcomeHandler) decision(c *gin.Context) {
	d, err := h.svc.Decision(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "decision", err)
		return
	}
	out := clientDecisionResponse{Outcome: d.Outcome, Reasons: d.Reasons, DecidedAt: d.DecidedAt, DecidedBy: "ENGINE"}
	if out.Reasons == nil {
		out.Reasons = []domain.ClientReason{}
	}
	if d.ReapplyAfter != "" {
		out.ReapplyAfter = &d.ReapplyAfter
	}
	if d.ByEmployee {
		out.DecidedBy = "EMPLOYEE"
	}
	c.JSON(http.StatusOK, out)
}

// offers — GET /api/v1/applications/{id}/offers.
//
//	@Summary	Предложения банка по заявке
//	@Tags		applications
//	@Produce	json
//	@Param		id	path		string	true	"id заявки (app_…)"
//	@Success	200	{object}	offersResponse
//	@Failure	404	{object}	problem
//	@Router		/applications/{id}/offers [get]
func (h *OutcomeHandler) offers(c *gin.Context) {
	items, err := h.svc.Offers(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "offers", err)
		return
	}
	out := offersResponse{Items: make([]boOfferResponse, 0, len(items))}
	for _, o := range items {
		out.Items = append(out.Items, toOffer(o))
	}
	c.JSON(http.StatusOK, out)
}

// infoRequests — GET /api/v1/applications/{id}/info-requests.
//
//	@Summary		Вопросы банка по заявке
//	@Description	input — как отвечать (тип значения и варианты), documents — какие документы приложить.
//	@Tags			applications
//	@Produce		json
//	@Param			id	path		string	true	"id заявки (app_…)"
//	@Success		200	{object}	infoRequestsResponse
//	@Failure		404	{object}	problem
//	@Router			/applications/{id}/info-requests [get]
func (h *OutcomeHandler) infoRequests(c *gin.Context) {
	items, err := h.svc.InfoRequests(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "info requests", err)
		return
	}
	c.JSON(http.StatusOK, infoRequestsResponse{Items: toInfoRequests(items)})
}

func toInfoRequests(items []domain.InfoRequestRecord) []boInfoRequestResponse {
	out := make([]boInfoRequestResponse, 0, len(items))
	for _, q := range items {
		out = append(out, toInfoRequest(q))
	}
	return out
}

// answer — POST /api/v1/applications/{id}/info-requests/{requestId}/answer.
//
//	@Summary		Ответ на вопрос банка
//	@Description	JSON {value, comment} или multipart/form-data: часть answer с тем же JSON и части files с документами
//	@Description	(форматы и подписи — как у анкеты). Ответили на все обязательные вопросы — заявка уходит на повторную
//	@Description	Проверенные до ответа вложения передаются в document_ids; загружать те же байты повторно не нужно.
//	@Description	оценку или к специалисту, если вопросы задал он. Отвечает только автор заявки.
//	@Tags			applications
//	@Accept			json
//	@Accept			mpfd
//	@Produce		json
//	@Security		bearer
//	@Param			id			path		string			true	"id заявки (app_…)"
//	@Param			requestId	path		string			true	"id вопроса (ir_…)"
//	@Param			body		body		answerRequest	false	"ответ (для JSON)"
//	@Success		200			{object}	answerResponse
//	@Failure		400			{object}	problem
//	@Failure		401			{object}	problem	"заявка тестового пользователя — нужен его токен"
//	@Failure		403			{object}	problem	"отвечает не автор заявки"
//	@Failure		404			{object}	problem
//	@Failure		409			{object}	problem	"банк не ждёт ответов или на вопрос уже ответили"
//	@Failure		422			{object}	problem
//	@Router			/applications/{id}/info-requests/{requestId}/answer [post]
func (h *OutcomeHandler) answer(c *gin.Context) {
	var (
		req   answerRequest
		files []storage.Staged
	)
	switch c.ContentType() {
	case binding.MIMEJSON:
		if err := c.ShouldBindJSON(&req); err != nil {
			writeProblem(c, bindingProblem(err))
			return
		}
	case binding.MIMEMultipartPOSTForm:
		var p *problem
		if files, p = h.readMultipart(c, &req); p != nil {
			h.svc.Discard(files)
			writeProblem(c, *p)
			return
		}
	default:
		writeProblem(c, unsupportedMediaProblem("ответ — application/json или multipart/form-data (часть answer и файлы files)"))
		return
	}
	actor := ""
	if user, ok := currentUser(c); ok {
		actor = user.ID
	}
	status, err := h.svc.Answer(c.Request.Context(), actor, c.Param("id"), c.Param("requestId"),
		service.AnswerInput{Value: req.Value, Comment: req.Comment, Files: files, DocumentIDs: req.DocumentIDs})
	if err != nil {
		h.fail(c, "answer info request", err)
		return
	}
	items, err := h.svc.InfoRequests(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "info requests", err)
		return
	}
	c.JSON(http.StatusOK, answerResponse{Status: string(status), InfoRequests: toInfoRequests(items)})
}

func (h *OutcomeHandler) readMultipart(c *gin.Context, req *answerRequest) ([]storage.Staged, *problem) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxBody)
	mr, err := c.Request.MultipartReader()
	if err != nil {
		p := validationProblem(http.StatusBadRequest, "некорректный multipart/form-data", nil)
		return nil, &p
	}
	var files []storage.Staged
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			p := validationProblem(http.StatusBadRequest, "некорректный multipart/form-data", nil)
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				p = tooLargeProblem(fmt.Sprintf("запрос больше %d МБ", h.maxBody>>20))
			}
			return files, &p
		}
		switch part.FormName() {
		case "answer":
			data, err := io.ReadAll(io.LimitReader(part, maxQuestionnaireBytes))
			if err == nil {
				err = json.Unmarshal(data, req)
			}
			if err != nil {
				p := validationProblem(http.StatusBadRequest, "часть answer — JSON {value, comment}", nil)
				return files, &p
			}
		case filesPart:
			if len(files) == service.MaxFiles {
				p := validationProblem(http.StatusUnprocessableEntity, "ответ не прошёл проверку", []problemField{{
					Field: "files", Code: "too_many", Message: fmt.Sprintf("не больше %d файлов", service.MaxFiles)}})
				return files, &p
			}
			st, err := h.svc.StageFile(part.FileName(), part)
			if err != nil {
				p := validationProblem(http.StatusUnprocessableEntity, "ответ не прошёл проверку", []problemField{{
					Field: fmt.Sprintf("files[%d]", len(files)), Code: "too_large", Message: "файл больше лимита"}})
				if !errors.Is(err, service.ErrFileTooLarge) {
					p = validationProblem(http.StatusBadRequest, "файл оборвался посреди загрузки", nil)
				}
				return files, &p
			}
			files = append(files, st)
		}
		_ = part.Close()
	}
	return files, nil
}

func (h *OutcomeHandler) fail(c *gin.Context, op string, err error) {
	var verr *service.ValidationError
	switch {
	case errors.As(err, &verr):
		writeProblem(c, fromValidationError(verr, "ответ не прошёл проверку"))
	case errors.Is(err, service.ErrNotFound):
		writeProblem(c, notFoundProblem("не найдено"))
	case errors.Is(err, service.ErrForbidden):
		writeProblem(c, forbiddenProblem("на вопросы банка отвечает тот, кто оформил заявку"))
	case errors.Is(err, service.ErrUnauthorized):
		writeProblem(c, unauthorizedProblem("заявка оформлена тестовым пользователем — нужен его токен"))
	case errors.Is(err, domain.ErrNotWaitingForInfo):
		writeProblem(c, problem{Type: problemTypeBase + "invalid-transition", Title: "Conflict", Status: http.StatusConflict,
			Detail: "банк сейчас не ждёт ответов по этой заявке", Code: "INVALID_TRANSITION"})
	case errors.Is(err, domain.ErrQuestionClosed):
		writeProblem(c, conflictProblem("на этот вопрос уже ответили"))
	default:
		h.log.ErrorContext(c.Request.Context(), op, "err", err)
		writeProblem(c, internalProblem())
	}
}
