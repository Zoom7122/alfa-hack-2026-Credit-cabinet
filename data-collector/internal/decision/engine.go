// Package decision — data-collector как «отправитель А» и «поставщик Б» для credit-engine:
// запрос на оценку из собранных данных и анкеты, бухотчётность из ГИР БО, разбор ответа движка.
//
// Типы ниже — копия нужной части credit-engine/contracts/contract.go (отдельный Go-модуль):
// JSON-теги должны совпадать с ним.
package decision

import "encoding/json"

// Request — DecisionRequest credit-engine (POST /api/v1/decisions).
type Request struct {
	ApplicationID      string          `json:"application_id"`
	Borrower           Borrower        `json:"borrower"`
	FactsDate          *string         `json:"facts_date"`
	CreditRequest      *CreditRequest  `json:"credit_request"`
	Need               *Need           `json:"need"`
	Cash               *Cash           `json:"cash"`
	MonthlyHistory     []MonthCash     `json:"monthly_history"`
	FinancialDebtRUB   *int64          `json:"financial_debt_rub"`
	FutureDebtPayments []DebtPayment   `json:"future_debt_payments"`
	Scoring            *ScoringInput   `json:"scoring"`
	Evidence           *Evidence       `json:"evidence"`
	IPEconomicData     *IPEconomicData `json:"ip_economic_data"`
}

type Borrower struct {
	ID   string  `json:"id"`
	Type string  `json:"type"` // SOLE_PROPRIETOR, LEGAL_ENTITY
	INN  *string `json:"inn"`
	Name *string `json:"name"`
}

type CreditRequest struct {
	Purpose                 *string  `json:"purpose"`
	AmountRUB               *int64   `json:"amount_rub"`
	TermMonths              *int     `json:"term_months"`
	MaxAnnualRate           *float64 `json:"max_annual_rate"` // проценты: 23.75 — это 23,75 %
	MinAcceptableAmountRUB  *int64   `json:"min_acceptable_amount_rub"`
	MaxAcceptableTermMonths *int     `json:"max_acceptable_term_months"`
}

type Need struct {
	ConfirmedCostRUB   *int64 `json:"confirmed_cost_rub"`
	OwnContributionRUB *int64 `json:"own_contribution_rub"`
}

type Cash struct {
	FreeAfterOwnContributionRUB *int64 `json:"free_after_own_contribution_rub"`
	MinimumRequiredRUB          *int64 `json:"minimum_required_rub"`
}

type MonthCash struct {
	Month                    *string `json:"month"` // YYYY-MM
	OperatingReceiptsRUB     *int64  `json:"operating_receipts_rub"`
	VariablePaymentsRUB      *int64  `json:"variable_payments_rub"`
	FixedPaymentsRUB         *int64  `json:"fixed_payments_rub"`
	TaxesPaidRUB             *int64  `json:"taxes_paid_rub"`
	MaintenanceInvestmentRUB *int64  `json:"maintenance_investment_rub"`
	OwnerRequiredPaymentsRUB *int64  `json:"owner_required_payments_rub"`
	ExistingDebtPaymentRUB   *int64  `json:"existing_debt_payment_rub"`
}

type DebtPayment struct {
	Month      *string `json:"month"`
	PaymentRUB *int64  `json:"payment_rub"`
}

type Evidence struct {
	ConfirmedReceiptsRUB         *int64 `json:"confirmed_receipts_rub"`
	UnexplainedDifferenceRUB     *int64 `json:"unexplained_difference_rub"`
	LargestBuyerGroupReceiptsRUB *int64 `json:"largest_buyer_group_receipts_rub"`
}

type IPEconomicData struct {
	PreviousComparableReceiptsRUB *int64 `json:"previous_comparable_receipts_rub"`
}

type ScoringInput struct {
	IP          *IPScoring          `json:"ip"`
	LegalEntity *LegalEntityScoring `json:"legal_entity"`
}

// riskFacts — проверяемые факты, общие для ИП и ЮЛ.
type riskFacts struct {
	Active                       *bool `json:"active"`
	Bankrupt                     *bool `json:"bankrupt"`
	MaterialForgery              *bool `json:"material_forgery"`
	CreditProhibited             *bool `json:"credit_prohibited"`
	IdentityAndAuthorityVerified *bool `json:"identity_and_authority_verified"`
	ConsentsVerified             *bool `json:"consents_verified"`
	BureauReceived               *bool `json:"bureau_received"`
	DebtsComplete                *bool `json:"debts_complete"`
	DocumentsComplete            *bool `json:"documents_complete"`
	FreeCashSourceVerified       *bool `json:"free_cash_source_verified"`
	MaterialAccountRestriction   *bool `json:"material_account_restriction"`
	MaterialDisputeOrEnforcement *bool `json:"material_dispute_or_enforcement"`
	KeyBuyerLoss                 *bool `json:"key_buyer_loss"`
	StructuralBusinessChange     *bool `json:"structural_business_change"`
}

// creditFacts — кредитная история и налоги, общие для ИП и ЮЛ.
type creditFacts struct {
	CreditHistoryMonths    *int   `json:"credit_history_months"`
	WorstRepaidDPDDays24M  *int   `json:"worst_repaid_dpd_days_24m"`
	CurrentDPDDays         *int   `json:"current_dpd_days"`
	Applications90D        *int   `json:"applications_90d"`
	BusinessAgeMonths      *int   `json:"business_age_months"`
	OverdueTaxesRUB        *int64 `json:"overdue_taxes_rub"`
	BankRelationshipMonths *int   `json:"bank_relationship_months"`
}

type IPScoring struct {
	OwnerAge                *int    `json:"owner_age"`
	MaritalStatus           *string `json:"marital_status"`
	Dependents              *int    `json:"dependents"`
	PrimaryIncomeSource     *string `json:"primary_income_source"`
	Qualification           *string `json:"qualification"`
	TotalExperienceMonths   *int    `json:"total_experience_months"`
	NetMonthlyIncomeRUB     *int64  `json:"net_monthly_income_rub"`
	CarClass                *string `json:"car_class"`
	EducationLevel          *string `json:"education_level"`
	RealEstateStatus        *string `json:"real_estate_status"`
	OwnerExperienceYears    *int    `json:"owner_experience_years"`
	BusinessLocation        *string `json:"business_location"`
	LocationStabilityMonths *int    `json:"location_stability_months"`
	IncomeEvidence          *string `json:"income_evidence"`
	BuyerContracts          *string `json:"buyer_contracts"`
	creditFacts
	riskFacts
}

type LegalEntityScoring struct {
	ManagerExperienceYears              *int    `json:"manager_experience_years"`
	ReportVerificationLevel             *int    `json:"report_verification_level"`
	OrganizationAndRelatedDebtsComplete *bool   `json:"organization_and_related_debts_complete"`
	MaterialRegisterInaccuracy          *bool   `json:"material_register_inaccuracy"`
	AuditorOpinion                      *string `json:"auditor_opinion"`
	NeedEvidence                        *string `json:"need_evidence"`
	OperatingNonfinancialCompany        *bool   `json:"operating_nonfinancial_company"`
	LiquidatedOrExcluded                *bool   `json:"liquidated_or_excluded"`
	creditFacts
	riskFacts
}

// Response — DecisionResponse credit-engine.
type Response struct {
	ModelVersion      string     `json:"model_version,omitempty"`
	ModelSHA256       string     `json:"model_sha256,omitempty"`
	ApplicationID     string     `json:"application_id"`
	Status            string     `json:"status"` // APPROVED, NEED_INFO, UNDER_REVIEW, REJECTED
	Message           string     `json:"message"`
	Score             *Score     `json:"score"`
	Terms             *LoanTerms `json:"terms"`
	TermsConfirmed    bool       `json:"terms_confirmed"`
	MonthlyPaymentRUB *float64   `json:"monthly_payment_rub"`
	OverpaymentRUB    *float64   `json:"overpayment_rub"`
	LoanID            *string    `json:"loan_id"`
	ReviewReason      *string    `json:"review_reason"` // BORROWER, BANK
	Reasons           []Reason   `json:"reasons"`
}

type Score struct {
	Model         string    `json:"model"`
	Questionnaire ScorePart `json:"questionnaire"` // анкета: максимум 750
	Economics     ScorePart `json:"economics"`     // показатели МС1: максимум 250
	RawTotal      float64   `json:"raw_total"`
	Total         int       `json:"total"`
	Grade         string    `json:"grade"` // A–D
}

// ScorePart — баллы части модели по критериям (QuestionnaireResult / EconomicResult движка).
type ScorePart struct {
	Points    float64     `json:"points"`
	MaxPoints int         `json:"max_points"`
	Criteria  []Criterion `json:"criteria"`
}

// Criterion — критерий скоринга: ответ анкеты (answer) или показатель (value) и баллы.
type Criterion struct {
	ID        string          `json:"id"`
	Answer    string          `json:"answer,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
	Points    float64         `json:"points"`
	MaxPoints int             `json:"max_points"`
}

type LoanTerms struct {
	AmountRUB  int64   `json:"amount_rub"`
	TermMonths int     `json:"term_months"`
	AnnualRate float64 `json:"annual_rate"`
}

type Reason struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Fields  []string `json:"fields"`
}

// Statements — StatementsResponse поставщика Б: два годовых комплекта бухотчётности.
type Statements struct {
	BorrowerID    string         `json:"borrower_id"`
	AnnualReports []AnnualReport `json:"annual_reports"`
}

type AnnualReport struct {
	Year          *int           `json:"year"`
	PeriodEnd     *string        `json:"period_end"`
	Balance       *Balance       `json:"balance"`
	ProfitAndLoss *ProfitAndLoss `json:"profit_and_loss"`
}

// Balance — строки баланса, нужные модели (единица — у формы).
type Balance struct {
	Unit                      *string `json:"unit"` // RUB, THOUSAND_RUB
	NonCurrentAssets          *int64  `json:"non_current_assets"`
	Inventories               *int64  `json:"inventories"`
	Receivables               *int64  `json:"receivables"`
	ShortTermFinancialInvest  *int64  `json:"short_term_financial_investments"`
	CashAndEquivalents        *int64  `json:"cash_and_equivalents"`
	CurrentAssets             *int64  `json:"current_assets"`
	TotalAssets               *int64  `json:"total_assets"`
	Equity                    *int64  `json:"equity"`
	LongTermBorrowings        *int64  `json:"long_term_borrowings"`
	LongTermLiabilities       *int64  `json:"long_term_liabilities"`
	ShortTermBorrowings       *int64  `json:"short_term_borrowings"`
	Payables                  *int64  `json:"payables"`
	CurrentLiabilities        *int64  `json:"current_liabilities"`
	TotalEquityAndLiabilities *int64  `json:"total_equity_and_liabilities"`
}

// ProfitAndLoss — строки ОФР, нужные модели.
type ProfitAndLoss struct {
	Unit                   *string `json:"unit"`
	Revenue                *int64  `json:"revenue"`
	CostOfSales            *int64  `json:"cost_of_sales"`
	SellingExpenses        *int64  `json:"selling_expenses"`
	AdministrativeExpenses *int64  `json:"administrative_expenses"`
	SalesProfit            *int64  `json:"sales_profit"`
	InterestPayable        *int64  `json:"interest_payable"`
	NetProfit              *int64  `json:"net_profit"`
}

// CreditEvent — событие МС2 → МС3 (POST /api/v1/credit-events); нужное кабинету.
type CreditEvent struct {
	EventID       string   `json:"event_id"`
	OccurredAt    string   `json:"occurred_at"`
	Kind          string   `json:"kind"` // LOAN_ISSUED, BORROWER_REVIEW, BANK_REVIEW
	ApplicationID string   `json:"application_id"`
	LoanID        *string  `json:"loan_id"`
	Reasons       []Reason `json:"reasons"`
}
