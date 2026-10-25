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

// ApplicationService — то, что хендлеру заявок нужно от бизнес-слоя.
type ApplicationService interface {
	Register(ctx context.Context, in service.RegisterInput) (domain.Application, error)
	Get(ctx context.Context, id string) (domain.Application, error)
}

// ApplicationHandler регистрирует заявителя и отдаёт заявки. Маршруты — в router.go.
type ApplicationHandler struct {
	svc ApplicationService
	log *slog.Logger
}

func NewApplicationHandler(svc ApplicationService, log *slog.Logger) *ApplicationHandler {
	return &ApplicationHandler{svc: svc, log: log}
}

// register — POST /api/v1/applicants.
//
//	@Summary		Зарегистрировать заявителя: кто оформляет кредит и за какую компанию
//	@Description	Одной транзакцией создаёт пользователя (с тем же ИНН физлица — находит существующего), компанию
//	@Description	(с тем же ИНН — переиспользует вместе с собранными данными), связь с ролью в company_members
//	@Description	и черновик заявки (DRAFT). Если у этого пользователя по этой компании уже есть черновик, возвращается он.
//	@Description	Подписывать договор без доверенности может директор, у ИП — сам предприниматель (can_sign).
//	@Description	Дальше — анкета: POST /applications/{id}/questionnaire выводит заявку из DRAFT и запускает сбор данных.
//	@Description
//	@Description.markdown	applicants
//	@Tags					applicants
//	@Accept					json
//	@Produce				json
//	@Param					body	body		registerRequest	true	"Представитель и компания"
//	@Success				201		{object}	applicationResponse
//	@Header					201		{string}	Location	"/api/v1/applications/{id}"
//	@Failure				400		{object}	problem		"битый JSON, нет обязательного поля (representative.full_name, representative.role, company.inn)"
//	@Failure				422		{object}	problem		"неверный ИНН или роль"
//	@Failure				500		{object}	problem
//	@Router					/applicants [post]
func (h *ApplicationHandler) register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, bindingProblem(err))
		return
	}

	app, err := h.svc.Register(c.Request.Context(), req.toRegisterInput())
	if err != nil {
		var verr *service.ValidationError
		if errors.As(err, &verr) {
			writeProblem(c, fromValidationError(verr, "данные заявителя не прошли проверку"))
			return
		}
		h.log.ErrorContext(c.Request.Context(), "register applicant", "err", err)
		writeProblem(c, internalProblem())
		return
	}

	resp := toApplicationResponse(app)
	known := app.Company.AlreadyKnown
	resp.Applicant.AlreadyKnown = &known
	c.Header("Location", applicationsPath+"/"+app.ID)
	c.JSON(http.StatusCreated, resp)
}

// get — GET /api/v1/applications/{id}.
//
//	@Summary		Заявка
//	@Description	Статус заявки (DRAFT → SUBMITTED → ENRICHING → VERIFYING), компания (реквизиты из ЕГРЮЛ — после сбора),
//	@Description	представитель с ролью, сумма и срок, документы и последняя задача на сбор.
//	@Tags			applications
//	@Produce		json
//	@Param			id	path		string	true	"id заявки (app_…)"
//	@Success		200	{object}	applicationResponse
//	@Failure		404	{object}	problem	"заявки нет"
//	@Failure		500	{object}	problem
//	@Router			/applications/{id} [get]
func (h *ApplicationHandler) get(c *gin.Context) {
	app, err := h.svc.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			writeProblem(c, notFoundProblem("заявка не найдена"))
			return
		}
		h.log.ErrorContext(c.Request.Context(), "get application", "err", err)
		writeProblem(c, internalProblem())
		return
	}
	c.JSON(http.StatusOK, toApplicationResponse(app))
}
