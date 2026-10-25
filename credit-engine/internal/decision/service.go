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

func (s *Service) Decide(ctx context.Context, r contracts.DecisionRequest) (contracts.DecisionResponse, error) {
	if err := ctx.Err(); err != nil {
		return contracts.DecisionResponse{}, err
	}
	a, err := s.evaluator.Evaluate(ctx, r)
	if err != nil {
		return contracts.DecisionResponse{}, err
	}
	result := contracts.DecisionResponse{ApplicationID: r.ApplicationID, BorrowerID: r.Borrower.ID, Score: a.Score, Reasons: a.Reasons}
	if result.Reasons == nil {
		result.Reasons = []contracts.Reason{}
	}
	switch a.Route {
	case scoring.RouteNeedInfo:
		result.Status, result.Message = contracts.DecisionStatusNeedInfo, "Требуется дополнительная информация"
		return result, nil
	case scoring.RouteRejected:
		result.Status, result.Message = contracts.DecisionStatusRejected, "Отклонено"
		return result, nil
	case scoring.RouteManual:
		return s.manual(ctx, r, result)
	case scoring.RouteSelection:
	default:
		return contracts.DecisionResponse{}, fmt.Errorf("decision: unexpected scoring route")
	}
	selected, err := s.selector.Select(optimizer.Input{RequestedTerms: *r.CreditRequest, Need: *r.Need, AnnualCFADSRUB: a.CashFlow.CFADS, FutureDebtPaymentsRUB: a.CashFlow.FutureDebt})
	if err != nil {
		return contracts.DecisionResponse{}, err
	}
	if selected.Terms == nil {
		result.Reasons = selected.Reasons
		return s.manual(ctx, r, result)
	}
	// Format borrower amounts before calling MS2: a local arithmetic error must
	// not happen after MS2 has already committed the issue.
	payment, err := economics.RoundToKopecks(selected.Audit.MonthlyPaymentRUB)
	if err != nil {
		return contracts.DecisionResponse{}, err
	}
	overpayment, err := economics.RoundToKopecks(selected.Audit.OverpaymentRUB)
	if err != nil {
		return contracts.DecisionResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return contracts.DecisionResponse{}, err
	}
	checked, err := s.bank.Check(ctx, contracts.BankCheckRequest{PolicyID: s.policyID, ApplicationID: r.ApplicationID, Borrower: r.Borrower, RequestedTerms: *r.CreditRequest, Terms: *selected.Terms, Score: *a.Score, Selection: selected.Audit})
	if err != nil {
		return contracts.DecisionResponse{}, err
	}
	result.Terms = selected.Terms
	monthly, extra := float64(payment)/100, float64(overpayment)/100
	result.MonthlyPaymentRUB, result.OverpaymentRUB = &monthly, &extra
	switch checked.Verdict {
	case contracts.BankVerdictIssued:
		result.Status, result.Message = contracts.DecisionStatusApproved, "Одобрено"
		result.TermsConfirmed, result.LoanID = true, checked.LoanID
	case contracts.BankVerdictBankReview:
		result.Status, result.Message = contracts.DecisionStatusUnderReview, "Заявка на рассмотрении"
		review := contracts.ReviewReasonBank
		result.ReviewReason = &review
		// Private checks and bank values belong only to MS2/MS3.
		result.Reasons = []contracts.Reason{{Code: "BANK_CONSTRAINT", Message: "Выбранная сделка требует рассмотрения банком.", Fields: []string{}}}
	default:
		return contracts.DecisionResponse{}, fmt.Errorf("decision: unexpected bank verdict")
	}
	return result, nil
}

func (s *Service) ModelVersion(r contracts.DecisionRequest) (string, error) {
	return s.evaluator.ModelVersion(r)
}

func (s *Service) manual(ctx context.Context, r contracts.DecisionRequest, result contracts.DecisionResponse) (contracts.DecisionResponse, error) {
	if err := ctx.Err(); err != nil {
		return contracts.DecisionResponse{}, err
	}
	_, err := s.bank.ManualReview(ctx, contracts.ManualReviewRequest{PolicyID: s.policyID, ApplicationID: r.ApplicationID, Borrower: r.Borrower, RequestedTerms: *r.CreditRequest, Score: *result.Score, Reasons: result.Reasons})
	if err != nil {
		return contracts.DecisionResponse{}, err
	}
	result.Status, result.Message = contracts.DecisionStatusUnderReview, "Заявка на рассмотрении"
	review := contracts.ReviewReasonBorrower
	result.ReviewReason = &review
	return result, nil
}
