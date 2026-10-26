package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// CabinetService — то, что хендлеру кабинета нужно от бизнес-слоя.
type CabinetService interface {
	Companies(ctx context.Context, userID string) ([]domain.CompanyProfile, error)
	Applications(ctx context.Context, f domain.ApplicationFilter) ([]domain.Application, error)
	Timeline(ctx context.Context, applicationID string) ([]domain.ApplicationEvent, error)
	Document(ctx context.Context, applicationID, documentID string) (domain.Document, io.ReadCloser, error)
	SignatureReport(ctx context.Context, applicationID, documentID string) (json.RawMessage, error)
	Lookup(ctx context.Context, inn string) (domain.CompanyCard, bool, error)
}

// CabinetHandler — кабинет клиента: компании, список и журнал заявок, документы, отчёты о подписи, подсказка по ИНН.
type CabinetHandler struct {
	svc CabinetService
	log *slog.Logger
}

func NewCabinetHandler(svc CabinetService, log *slog.Logger) *CabinetHandler {
	return &CabinetHandler{svc: svc, log: log}
}

// WithCabinet — ручки кабинета. Подключать после WithAuth: списку компаний и заявок нужен вошедший пользователь.
func WithCabinet(h *CabinetHandler) RouterOption {
	return func(_ *gin.Engine, api *gin.RouterGroup) {
		api.GET("/applicants", h.applicants)
		api.GET("/applications", h.applications)
		api.GET("/applications/:id/timeline", h.timeline)
		api.GET("/applications/:id/documents/:documentId/content", h.content)
		api.GET("/applications/:id/documents/:documentId/signature-report", h.signatureReport)
		api.POST("/counterparty/lookup", h.lookup)
	}
}

// applicants — GET /api/v1/applicants.
//
//	@Summary		Компании текущего пользователя
//	@Description	Компании, от имени которых действует вошедший пользователь, с его ролью и реквизитами из ЕГРЮЛ
//	@Description	(после сбора данных; до него — только ИНН и название). Первая — основная (заведённая тестовому пользователю).
//	@Tags			cabinet
//	@Produce		json
//	@Security		bearer
//	@Success		200	{object}	applicantsResponse
//	@Failure		401	{object}	problem	"нет или неверный токен"
//	@Failure		500	{object}	problem
//	@Router			/applicants [get]
func (h *CabinetHandler) applicants(c *gin.Context) {
	user, ok := h.requireUser(c)
	if !ok {
		return
	}
	companies, err := h.svc.Companies(c.Request.Context(), user.ID)
	if err != nil {
		h.internal(c, "list companies", err)
		return
	}
	// порядок — как в профиле (/me): основная компания первой
	order := make(map[string]int, len(user.Companies))
	for i, co := range user.Companies {
		order[co.CompanyID] = i
	}
	items := make([]applicantProfileResponse, 0, len(companies))
	for _, p := range companies {
		items = append(items, toApplicantProfile(p))
	}
	sortByOrder(items, order)
	c.JSON(http.StatusOK, applicantsResponse{Items: items})
}

// applications — GET /api/v1/applications.
//
//	@Summary		Заявки компаний пользователя
//	@Description	Заявки всех компаний вошедшего пользователя — его и коллег по компании, новые сверху (до 100).
//	@Description	Черновики — только свои: чужой черновик не виден и не отправляется.
//	@Tags			cabinet
//	@Produce		json
//	@Security		bearer
//	@Param			company_id	query		string	false	"только эта компания (apl_…)"
//	@Param			status		query		string	false	"статусы через запятую: DRAFT,SUBMITTED,ENRICHING,VERIFYING,SCORING,MANUAL_REVIEW,WAITING_FOR_INFO,OFFERED,REJECTED"
//	@Param			mine		query		bool	false	"true — только заявки, оформленные самим пользователем"
//	@Success		200			{object}	applicationsResponse
//	@Failure		401			{object}	problem	"нет или неверный токен"
//	@Failure		422			{object}	problem	"неизвестный статус"
//	@Failure		500			{object}	problem
//	@Router			/applications [get]
func (h *CabinetHandler) applications(c *gin.Context) {
	user, ok := h.requireUser(c)
	if !ok {
		return
	}
	var statuses []domain.ApplicationStatus
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
		statuses = append(statuses, st)
	}
	apps, err := h.svc.Applications(c.Request.Context(), domain.ApplicationFilter{UserID: user.ID,
		CompanyID: c.Query("company_id"), Statuses: statuses, OnlyMine: c.Query("mine") == "true"})
	if err != nil {
		h.internal(c, "list applications", err)
		return
	}
	items := make([]applicationResponse, 0, len(apps))
	for _, a := range apps {
		items = append(items, toApplicationResponse(a))
	}
	c.JSON(http.StatusOK, applicationsResponse{Items: items})
}

// timeline — GET /api/v1/applications/{id}/timeline.
//
//	@Summary		Журнал заявки для клиента
//	@Description	События заявки по времени: создание, отправка анкеты, начало и итог сбора данных, проверка подписи.
//	@Description	Служебные события без текста для клиента не отдаются.
//	@Tags			applications
//	@Produce		json
//	@Security		bearer
//	@Param			id	path		string	true	"id заявки (app_…)"
//	@Success		200	{object}	timelineResponse
//	@Failure		404	{object}	problem	"заявки нет"
//	@Failure		500	{object}	problem
//	@Router			/applications/{id}/timeline [get]
func (h *CabinetHandler) timeline(c *gin.Context) {
	events, err := h.svc.Timeline(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "timeline", err, "заявка не найдена")
		return
	}
	items := make([]timelineEventResponse, 0, len(events))
	for _, e := range events {
		if ev, ok := toTimelineEvent(e); ok {
			items = append(items, ev)
		}
	}
	c.JSON(http.StatusOK, timelineResponse{Items: items})
}

// content — GET /api/v1/applications/{id}/documents/{documentId}/content.
//
//	@Summary	Файл документа заявки
//	@Tags		applications
//	@Produce	octet-stream
//	@Security	bearer
//	@Param		id			path		string	true	"id заявки (app_…)"
//	@Param		documentId	path		string	true	"id документа (doc_…)"
//	@Success	200			{file}		file
//	@Failure	404			{object}	problem	"заявки или документа нет"
//	@Failure	500			{object}	problem
//	@Router		/applications/{id}/documents/{documentId}/content [get]
func (h *CabinetHandler) content(c *gin.Context) {
	doc, f, err := h.svc.Document(c.Request.Context(), c.Param("id"), c.Param("documentId"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			h.log.WarnContext(c.Request.Context(), "document content", "err", err)
		}
		h.fail(c, "document content", err, "заявка или документ не найдены")
		return
	}
	defer f.Close()
	mimeType := doc.MimeType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": doc.FileName}))
	c.DataFromReader(http.StatusOK, doc.SizeBytes, mimeType, f, nil)
}

// signatureReport — GET /api/v1/applications/{id}/documents/{documentId}/signature-report.
//
//	@Summary		Отчёт проверки электронной подписи
//	@Description	Последний отчёт worker'а подписи по документу или его открепленной подписи (signature_reports.report):
//	@Description	итог (decision, verdict), этапы проверки с причинами, подписант, сертификат, время подписи, политика.
//	@Tags			applications
//	@Produce		json
//	@Security		bearer
//	@Param			id			path		string	true	"id заявки (app_…)"
//	@Param			documentId	path		string	true	"id документа (doc_…)"
//	@Success		200			{object}	object	"отчёт (signature.Report)"
//	@Failure		404			{object}	problem	"заявки или документа нет (NOT_FOUND) или подпись ещё не проверяли (REPORT_NOT_READY)"
//	@Failure		500			{object}	problem
//	@Router			/applications/{id}/documents/{documentId}/signature-report [get]
func (h *CabinetHandler) signatureReport(c *gin.Context) {
	report, err := h.svc.SignatureReport(c.Request.Context(), c.Param("id"), c.Param("documentId"))
	if errors.Is(err, service.ErrReportNotReady) {
		p := notFoundProblem("подпись этого документа ещё не проверяли")
		p.Code = "REPORT_NOT_READY"
		writeProblem(c, p)
		return
	}
	if err != nil {
		h.fail(c, "signature report", err, "заявка или документ не найдены")
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", report)
}

// lookup — POST /api/v1/counterparty/lookup.
//
//	@Summary		Реквизиты компании по ИНН
//	@Description	Синхронный запрос в ЕГРЮЛ/ЕГРИП (эмулятор mock-api) — подсказка на шаге «Компания» до регистрации.
//	@Description	already_known — компания уже есть в системе (по ней подавали заявку). Нужен токен.
//	@Tags			cabinet
//	@Accept			json
//	@Produce		json
//	@Security		bearer
//	@Param			body	body		lookupRequest	true	"ИНН"
//	@Success		200		{object}	counterpartyResponse
//	@Failure		400		{object}	problem	"битый JSON, нет inn"
//	@Failure		401		{object}	problem	"нет или неверный токен"
//	@Failure		404		{object}	problem	"в реестре нет"
//	@Failure		422		{object}	problem	"неверный ИНН"
//	@Failure		504		{object}	problem	"реестр не ответил: таймаут, сбой или капча (UPSTREAM_TIMEOUT)"
//	@Router			/counterparty/lookup [post]
func (h *CabinetHandler) lookup(c *gin.Context) {
	if _, ok := h.requireUser(c); !ok {
		return
	}
	var req lookupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, bindingProblem(err))
		return
	}
	card, known, err := h.svc.Lookup(c.Request.Context(), strings.TrimSpace(*req.INN))
	var verr *service.ValidationError
	switch {
	case errors.As(err, &verr):
		writeProblem(c, fromValidationError(verr, "неверный ИНН"))
	case errors.Is(err, service.ErrNotFound):
		writeProblem(c, notFoundProblem("компания с таким ИНН не найдена в ЕГРЮЛ"))
	case errors.Is(err, service.ErrRegistryUnavailable):
		h.log.WarnContext(c.Request.Context(), "lookup", "err", err)
		writeProblem(c, problem{Type: problemTypeBase + "upstream-unavailable", Title: "Gateway timeout", Status: http.StatusGatewayTimeout,
			Detail: "ЕГРЮЛ не ответил — продолжайте, реквизиты соберём при сборе данных", Code: "UPSTREAM_TIMEOUT"})
	case errors.Is(err, context.Canceled):
		c.Status(499) // клиент ушёл, отвечать некому
	case err != nil:
		h.internal(c, "lookup", err)
	default:
		c.JSON(http.StatusOK, toCounterparty(card, known))
	}
}

func (h *CabinetHandler) requireUser(c *gin.Context) (domain.User, bool) {
	user, ok := currentUser(c)
	if !ok {
		writeProblem(c, unauthorizedProblem("нужен токен: Authorization: Bearer mock.<user_id>"))
	}
	return user, ok
}

func (h *CabinetHandler) fail(c *gin.Context, op string, err error, notFound string) {
	if errors.Is(err, service.ErrNotFound) {
		writeProblem(c, notFoundProblem(notFound))
		return
	}
	h.internal(c, op, err)
}

func (h *CabinetHandler) internal(c *gin.Context, op string, err error) {
	h.log.ErrorContext(c.Request.Context(), op, "err", err)
	writeProblem(c, internalProblem())
}

func statusList() string {
	names := make([]string, len(domain.ApplicationStatuses))
	for i, s := range domain.ApplicationStatuses {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}
