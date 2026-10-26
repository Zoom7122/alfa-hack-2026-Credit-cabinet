package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
)

// DecisionRepository — заявки на оценку и запись итога (repository/postgres/decision.go).
type DecisionRepository interface {
	CompanyFNS(ctx context.Context, companyID string) (json.RawMessage, bool, error)
	SaveCreditEvent(ctx context.Context, ev decision.CreditEvent) (bool, error)
}

// DecisionConfig — сроки конвейера оценки.
type DecisionConfig struct {
	EvaluationDate time.Time     // дата оценки — та же, что у движка (DEMO_EVALUATION_DATE); пусто — сегодня
	ReviewSLA      time.Duration // срок ручного рассмотрения
	OfferValidity  time.Duration // срок действия предложения
}

// DecisionService provides the legacy statements supplier and durable incoming credit events.
// Dispatch execution is owned by internal/dispatch and scoring_jobs.
type DecisionService struct{ repo DecisionRepository }

func NewDecisionService(repo DecisionRepository) *DecisionService {
	return &DecisionService{repo: repo}
}

// Statements — ответ поставщика Б по компании (borrower_id = id компании). ErrNotFound — сборов по ней нет.
func (s *DecisionService) Statements(ctx context.Context, borrowerID string, years []int) (decision.Statements, error) {
	raw, ok, err := s.repo.CompanyFNS(ctx, borrowerID)
	if err != nil {
		return decision.Statements{}, err
	}
	if !ok {
		return decision.Statements{}, ErrNotFound
	}
	return decision.StatementsFromFNS(raw, borrowerID, years)
}

// CreditEvent — событие МС2 (выдача, ручное рассмотрение, проблема банка) в журнал заявки.
func (s *DecisionService) CreditEvent(ctx context.Context, ev decision.CreditEvent) error {
	_, err := s.repo.SaveCreditEvent(ctx, ev)
	return err
}
