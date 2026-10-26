package httptransport

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/service"
)

// CatalogService — то, что хендлеру каталога нужно от бизнес-слоя.
type CatalogService interface {
	List(purpose, applicantType string) ([]domain.Product, error)
	Get(code string) (domain.Product, error)
	Calculate(code string, amount int64, term int) (service.Calculation, error)
}

// CatalogHandler — каталог кредитных продуктов и предварительный расчёт платежа. Токен не нужен.
type CatalogHandler struct {
	svc CatalogService
}

func NewCatalogHandler(svc CatalogService) *CatalogHandler {
	return &CatalogHandler{svc: svc}
}

// WithCatalog — ручки каталога.
func WithCatalog(h *CatalogHandler) RouterOption {
	return func(_ *gin.Engine, api *gin.RouterGroup) {
		api.GET("/products", h.list)
		api.GET("/products/:code", h.get)
		api.POST("/products/:code/calculate", h.calculate)
	}
}

type rangeResponse struct {
	Min int64 `json:"min" example:"300000"`
	Max int64 `json:"max" example:"30000000"`
} //	@name	Range

type productCollateralResponse struct {
	// С какой суммы нужен залог: null — не нужен, 0 — нужен всегда.
	RequiredAboveAmount *int64   `json:"required_above_amount" extensions:"x-nullable"`
	AcceptedTypes       []string `json:"accepted_types"`
} //	@name	ProductCollateral

type productResponse struct {
	Code           string                    `json:"code" example:"WORKING_CAPITAL"`
	Name           string                    `json:"name" example:"Оборотный кредит"`
	Description    string                    `json:"description"`
	ApplicantTypes []string                  `json:"applicant_types" enums:"LEGAL_ENTITY,SOLE_PROPRIETOR"`
	Purposes       []string                  `json:"purposes"`
	Amount         rangeResponse             `json:"amount"`
	TermMonths     rangeResponse             `json:"term_months"`
	RateAnnualFrom float64                   `json:"rate_annual_from" example:"18.5"`
	Collateral     productCollateralResponse `json:"collateral"`
	// Нужно ли поручительство (обычно — собственника).
	GuaranteeRequired bool `json:"guarantee_required"`
} //	@name	Product

type productListResponse struct {
	Items []productResponse `json:"items"`
} //	@name	ProductList

type calculateRequest struct {
	Amount     *int64 `json:"amount" binding:"required" example:"5000000"`
	TermMonths *int   `json:"term_months" binding:"required" example:"24"`
} //	@name	CalculateRequest

type calculationResponse struct {
	RateAnnualFrom     float64 `json:"rate_annual_from" example:"18.5"`
	MonthlyPaymentFrom int64   `json:"monthly_payment_from" example:"251234"`
	Disclaimer         string  `json:"disclaimer"`
} //	@name	Calculation

func toProduct(p domain.Product) productResponse {
	types := make([]string, 0, len(p.ApplicantTypes))
	for _, t := range p.ApplicantTypes {
		types = append(types, string(t))
	}
	accepted := p.CollateralTypes
	if accepted == nil {
		accepted = []string{}
	}
	return productResponse{Code: p.Code, Name: p.Name, Description: p.Description, ApplicantTypes: types, Purposes: p.Purposes,
		Amount: rangeResponse{Min: p.AmountMin, Max: p.AmountMax}, TermMonths: rangeResponse{Min: int64(p.TermMin), Max: int64(p.TermMax)},
		RateAnnualFrom: p.RateFrom, Collateral: productCollateralResponse{RequiredAboveAmount: p.CollateralAbove, AcceptedTypes: accepted},
		GuaranteeRequired: p.GuaranteeRequired}
}

// list — GET /api/v1/products.
//
//	@Summary		Каталог кредитных продуктов
//	@Description	Продукты под задачу и тип заявителя; без фильтров — весь каталог. Порядок — от самого подходящего.
//	@Tags			catalog
//	@Produce		json
//	@Param			purpose			query		string	false	"Цель кредита"	Enums(WORKING_CAPITAL,EQUIPMENT,VEHICLE,REAL_ESTATE,REFINANCING,TENDER,BUSINESS_EXPANSION,OTHER)
//	@Param			applicant_type	query		string	false	"Тип заявителя"	Enums(LEGAL_ENTITY,SOLE_PROPRIETOR)
//	@Success		200				{object}	productListResponse
//	@Failure		422				{object}	problem	"неизвестная цель или тип заявителя"
//	@Router			/products [get]
func (h *CatalogHandler) list(c *gin.Context) {
	items, err := h.svc.List(c.Query("purpose"), c.Query("applicant_type"))
	var verr *service.ValidationError
	if errors.As(err, &verr) {
		writeProblem(c, fromValidationError(verr, "неверный фильтр каталога"))
		return
	}
	out := productListResponse{Items: make([]productResponse, 0, len(items))}
	for _, p := range items {
		out.Items = append(out.Items, toProduct(p))
	}
	c.JSON(http.StatusOK, out)
}

// get — GET /api/v1/products/{code}.
//
//	@Summary	Карточка продукта
//	@Tags		catalog
//	@Produce	json
//	@Param		code	path		string	true	"Код продукта"	example(WORKING_CAPITAL)
//	@Success	200		{object}	productResponse
//	@Failure	404		{object}	problem
//	@Router		/products/{code} [get]
func (h *CatalogHandler) get(c *gin.Context) {
	p, err := h.svc.Get(c.Param("code"))
	if err != nil {
		writeProblem(c, notFoundProblem("продукт не найден"))
		return
	}
	c.JSON(http.StatusOK, toProduct(p))
}

// calculate — POST /api/v1/products/{code}/calculate.
//
//	@Summary		Предварительный расчёт платежа
//	@Description	Аннуитетный платёж по ставке «от» продукта, без скоринга. Сумма и срок — в диапазоне продукта.
//	@Tags			catalog
//	@Accept			json
//	@Produce		json
//	@Param			code	path		string				true	"Код продукта"	example(WORKING_CAPITAL)
//	@Param			body	body		calculateRequest	true	"Сумма и срок"
//	@Success		200		{object}	calculationResponse
//	@Failure		400		{object}	problem	"некорректное тело"
//	@Failure		404		{object}	problem
//	@Failure		422		{object}	problem	"сумма или срок вне диапазона продукта"
//	@Router			/products/{code}/calculate [post]
func (h *CatalogHandler) calculate(c *gin.Context) {
	var req calculateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeProblem(c, bindingProblem(err))
		return
	}
	calc, err := h.svc.Calculate(c.Param("code"), *req.Amount, *req.TermMonths)
	var verr *service.ValidationError
	switch {
	case errors.As(err, &verr):
		writeProblem(c, fromValidationError(verr, "проверьте сумму и срок"))
	case err != nil:
		writeProblem(c, notFoundProblem("продукт не найден"))
	default:
		c.JSON(http.StatusOK, calculationResponse{RateAnnualFrom: calc.RateFrom, MonthlyPaymentFrom: calc.PaymentFrom, Disclaimer: service.CatalogDisclaimer})
	}
}
