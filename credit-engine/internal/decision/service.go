// Package decision assembles the MS1 process without owning bank state.
package decision

import (
	"context"
	"fmt"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/economics"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/optimizer"
	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/scoring"
)

// Bank is the two outgoing MS2 operations; MS1 never sends admin events.
type Bank interface {
	Check(context.Context, contracts.BankCheckRequest) (contracts.BankCheckResponse, error)
	ManualReview(context.Context, contracts.ManualReviewRequest) (contracts.ManualReviewResponse, error)
}

type Service struct {
	policyID  string
	evaluator *scoring.Evaluator
	selector  *optimizer.Selector
	bank      Bank
}

func New(policyID string, evaluator *scoring.Evaluator, selector *optimizer.Selector, bank Bank) (*Service, error) {
	if strings.TrimSpace(policyID) == "" || evaluator == nil || selector == nil || bank == nil {
		return nil, fmt.Errorf("decision: policy, evaluator, selector and bank client are required")
	}
	return &Service{policyID: policyID, evaluator: evaluator, selector: selector, bank: bank}, nil
}

// Prepared is persisted before any bank call; retries never recalculate a committed operation.
type Prepared struct {
	Result contracts.DecisionResponse     `json:"result"`
	Check  *contracts.BankCheckRequest    `json:"check,omitempty"`
	Manual *contracts.ManualReviewRequest `json:"manual,omitempty"`
}

func (s *Service) Decide(ctx context.Context, r contracts.DecisionRequest) (contracts.DecisionResponse, error) {
	p, err := s.Prepare(ctx, r, nil)
	if err != nil {
		return contracts.DecisionResponse{}, err
	}
	return s.Finish(ctx, p)
}
func (s *Service) Prepare(ctx context.Context, r contracts.DecisionRequest, operationID *string) (Prepared, error) {
	if err := ctx.Err(); err != nil {
		return Prepared{}, err
	}
	a, err := s.evaluator.Evaluate(ctx, r)
	if err != nil {
		return Prepared{}, err
	}
	p := Prepared{Result: contracts.DecisionResponse{ApplicationID: r.ApplicationID, BorrowerID: r.Borrower.ID, Score: a.Score, Reasons: a.Reasons}}
	if p.Result.Reasons == nil {
		p.Result.Reasons = []contracts.Reason{}
	}
	manual := func() (Prepared, error) {
		if r.CreditRequest == nil || p.Result.Score == nil {
			return Prepared{}, fmt.Errorf("decision: incomplete manual result")
		}
		p.Manual = &contracts.ManualReviewRequest{OperationID: operationID, PolicyID: s.policyID, ApplicationID: r.ApplicationID, Borrower: r.Borrower, RequestedTerms: *r.CreditRequest, Score: *p.Result.Score, Reasons: p.Result.Reasons}
		p.Result.Status, p.Result.Message = contracts.DecisionStatusUnderReview, "Заявка на рассмотрении"
		v := contracts.ReviewReasonBorrower
		p.Result.ReviewReason = &v
		return p, nil
	}
	switch a.Route {
	case scoring.RouteNeedInfo:
		p.Result.Status, p.Result.Message = contracts.DecisionStatusNeedInfo, "Требуется дополнительная информация"
		return p, nil
	case scoring.RouteRejected:
		p.Result.Status, p.Result.Message = contracts.DecisionStatusRejected, "Отклонено"
		return p, nil
	case scoring.RouteManual:
		return manual()
	case scoring.RouteSelection:
	default:
		return Prepared{}, fmt.Errorf("decision: unexpected scoring route")
	}
	selected, err := s.selector.Select(optimizer.Input{RequestedTerms: *r.CreditRequest, Need: *r.Need, AnnualCFADSRUB: a.CashFlow.CFADS, FutureDebtPaymentsRUB: a.CashFlow.FutureDebt})
	if err != nil {
		return Prepared{}, err
	}
	if selected.Terms == nil {
		p.Result.Reasons = selected.Reasons
		return manual()
	}
	payment, err := economics.RoundToKopecks(selected.Audit.MonthlyPaymentRUB)
	if err != nil {
		return Prepared{}, err
	}
	extra, err := economics.RoundToKopecks(selected.Audit.OverpaymentRUB)
	if err != nil {
		return Prepared{}, err
	}
	monthly, over := float64(payment)/100, float64(extra)/100
	p.Result.Terms, p.Result.MonthlyPaymentRUB, p.Result.OverpaymentRUB = selected.Terms, &monthly, &over
	p.Check = &contracts.BankCheckRequest{OperationID: operationID, PolicyID: s.policyID, ApplicationID: r.ApplicationID, Borrower: r.Borrower, RequestedTerms: *r.CreditRequest, Terms: *selected.Terms, Score: *a.Score, Selection: selected.Audit}
	return p, nil
}
func (s *Service) Finish(ctx context.Context, p Prepared) (contracts.DecisionResponse, error) {
	if p.Manual != nil {
		if _, err := s.bank.ManualReview(ctx, *p.Manual); err != nil {
			return contracts.DecisionResponse{}, err
		}
	}
	if p.Check == nil {
		return p.Result, nil
	}
	checked, err := s.bank.Check(ctx, *p.Check)
	if err != nil {
		return contracts.DecisionResponse{}, err
	}
	switch checked.Verdict {
	case contracts.BankVerdictIssued:
		p.Result.Status, p.Result.Message = contracts.DecisionStatusApproved, "Одобрено"
		p.Result.TermsConfirmed, p.Result.LoanID = true, checked.LoanID
	case contracts.BankVerdictBankReview:
		p.Result.Status, p.Result.Message = contracts.DecisionStatusUnderReview, "Заявка на рассмотрении"
		review := contracts.ReviewReasonBank
		p.Result.ReviewReason = &review
		p.Result.Reasons = []contracts.Reason{{Code: "BANK_CONSTRAINT", Message: "Выбранная сделка требует рассмотрения банком.", Fields: []string{}}}
	default:
		return contracts.DecisionResponse{}, fmt.Errorf("decision: unexpected bank verdict")
	}
	return p.Result, nil
}

func (s *Service) ModelVersion(r contracts.DecisionRequest) (string, error) {
	return s.evaluator.ModelVersion(r)
}
