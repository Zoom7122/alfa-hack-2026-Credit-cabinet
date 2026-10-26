package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/decision"
)

// DecisionRepository — заявки на оценку и запись итога (repository/postgres/decision.go).
type DecisionRepository interface {
	ClaimNext(ctx context.Context, signatureWait, stale time.Duration) (string, bool, error)
	Input(ctx context.Context, appID string, now time.Time) (decision.Input, error)
	SaveOutcome(ctx context.Context, appID string, o decision.Outcome, details any, deadline *time.Time, offerValidUntil time.Time) error
	CompanyFNS(ctx context.Context, companyID string) (json.RawMessage, bool, error)
	SaveCreditEvent(ctx context.Context, ev decision.CreditEvent) (bool, error)
	AnsweredCodes(ctx context.Context, appID string) (map[string]bool, error)
}

// Engine — МС1 credit-engine.
type Engine interface {
	Decide(ctx context.Context, req decision.Request) (decision.Response, error)
}

// DecisionConfig — сроки конвейера оценки.
type DecisionConfig struct {
	EvaluationDate time.Time     // дата оценки — та же, что у движка (DEMO_EVALUATION_DATE); пусто — сегодня
	SignatureWait  time.Duration // сколько ждать проверку подписей перед оценкой
	Stale          time.Duration // когда оценка считается брошенной
	ReviewSLA      time.Duration // срок ручного рассмотрения
	OfferValidity  time.Duration // срок действия предложения
}

// DecisionService — конвейер после сбора данных: заявка → запрос в credit-engine → итог в базе.
// Он же отвечает за роли «поставщик Б» (бухотчётность) и «МС3» (события движка).
type DecisionService struct {
	repo   DecisionRepository
	engine Engine
	cfg    DecisionConfig
	now    func() time.Time
}

func NewDecisionService(repo DecisionRepository, engine Engine, cfg DecisionConfig) *DecisionService {
	return &DecisionService{repo: repo, engine: engine, cfg: cfg, now: time.Now}
}

func (s *DecisionService) evaluationDate() time.Time {
	if !s.cfg.EvaluationDate.IsZero() {
		return s.cfg.EvaluationDate
	}
	return s.now().UTC()
}

// ProcessNext оценивает одну заявку. false — оценивать нечего.
func (s *DecisionService) ProcessNext(ctx context.Context) (string, bool, error) {
	id, ok, err := s.repo.ClaimNext(ctx, s.cfg.SignatureWait, s.cfg.Stale)
	if err != nil || !ok {
		return "", false, err
	}
	now := s.evaluationDate()
	in, err := s.repo.Input(ctx, id, now)
	if err != nil {
		return id, true, err
	}
	// дата фактов не позже даты оценки: движок не принимает «будущие» факты
	if in.FactsDate.After(now) {
		in.FactsDate = now
	}
	req, facts := decision.Build(in)
	resp, err := s.engine.Decide(ctx, req)
	if err != nil {
		return id, true, err // заявка остаётся в SCORING и будет взята снова через Stale
	}
	outcome := decision.Interpret(resp, facts)
	answered, err := s.repo.AnsweredCodes(ctx, id)
	if err != nil {
		return id, true, err
	}
	outcome = decision.WithoutAnswered(outcome, answered)
	var deadline *time.Time
	if outcome.Status == "MANUAL_REVIEW" {
		d := s.now().Add(s.cfg.ReviewSLA)
		deadline = &d
	}
	details := map[string]any{"engine": resp, "request": req, "findings": outcome.Findings, "facts": facts,
		"proposed_offer": outcome.Offer, "evaluated_at": now.Format(time.DateOnly)}
	if err := s.repo.SaveOutcome(ctx, id, outcome, details, deadline, s.now().Add(s.cfg.OfferValidity)); err != nil {
		return id, true, fmt.Errorf("save outcome: %w", err)
	}
	return id, true, nil
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
