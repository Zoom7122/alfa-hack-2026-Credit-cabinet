package service

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// CatalogDisclaimer — оговорка к предварительному расчёту.
const CatalogDisclaimer = "Предварительный расчёт по условиям продукта. Точные условия — после рассмотрения заявки."

// Calculation — предварительный платёж по ставке «от», без скоринга.
type Calculation struct {
	RateFrom    float64
	PaymentFrom int64
}

// CatalogService — каталог продуктов и калькулятор платежа. Хранилища нет: каталог — в коде (domain.Products).
type CatalogService struct{}

func NewCatalogService() *CatalogService { return &CatalogService{} }

// List — продукты под задачу и тип заявителя; пустой фильтр — без ограничения.
func (s *CatalogService) List(purpose, applicantType string) ([]domain.Product, error) {
	var fields []FieldError
	if purpose != "" && !slices.Contains(domain.PurposeCodes, purpose) {
		fields = append(fields, FieldError{Field: "purpose", Code: "invalid_value", Message: "допустимые значения: " + strings.Join(domain.PurposeCodes, ", ")})
	}
	types := []string{string(domain.CompanyLegalEntity), string(domain.CompanySoleProprietor)}
	if applicantType != "" && !slices.Contains(types, applicantType) {
		fields = append(fields, FieldError{Field: "applicant_type", Code: "invalid_value", Message: "допустимые значения: " + strings.Join(types, ", ")})
	}
	if len(fields) > 0 {
		return nil, &ValidationError{Fields: fields}
	}
	out := []domain.Product{}
	for _, p := range domain.Products {
		if purpose != "" && !slices.Contains(p.Purposes, purpose) {
			continue
		}
		if applicantType != "" && !slices.Contains(p.ApplicantTypes, domain.CompanyType(applicantType)) {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *CatalogService) Get(code string) (domain.Product, error) {
	p, ok := domain.ProductByCode(code)
	if !ok {
		return domain.Product{}, ErrNotFound
	}
	return p, nil
}

// Calculate — аннуитетный платёж по ставке «от» продукта; сумма и срок — в его диапазоне.
func (s *CatalogService) Calculate(code string, amount int64, term int) (Calculation, error) {
	p, ok := domain.ProductByCode(code)
	if !ok {
		return Calculation{}, ErrNotFound
	}
	if fields := termsErrors(p, amount, term); len(fields) > 0 {
		return Calculation{}, &ValidationError{Fields: fields}
	}
	return Calculation{RateFrom: p.RateFrom, PaymentFrom: decision.Annuity(amount, p.RateFrom, term)}, nil
}

// termsErrors — сумма и срок вне диапазона продукта (поля — как в анкете: requested.*).
func termsErrors(p domain.Product, amount int64, term int) []FieldError {
	var fields []FieldError
	if amount < p.AmountMin || amount > p.AmountMax {
		fields = append(fields, FieldError{Field: "requested.amount", Code: "out_of_range",
			Message: fmt.Sprintf("сумма по продукту «%s» — от %d до %d ₽", p.Name, p.AmountMin, p.AmountMax)})
	}
	if term < p.TermMin || term > p.TermMax {
		fields = append(fields, FieldError{Field: "requested.term_months", Code: "out_of_range",
			Message: fmt.Sprintf("срок по продукту «%s» — от %d до %d месяцев", p.Name, p.TermMin, p.TermMax)})
	}
	return fields
}
