// Package decision — data-collector как «отправитель А» и «поставщик Б» для credit-engine:
// запрос на оценку из собранных данных и анкеты, бухотчётность из ГИР БО, разбор ответа движка.
//
// Типы ниже — копия нужной части credit-engine/contracts/contract.go (отдельный Go-модуль):
// JSON-теги должны совпадать с ним.
package decision

import "encoding/json"

// Request — DecisionRequest credit-engine (POST /api/v1/decisions).
type Request struct {
	InputSnapshot      *InputSnapshot  `json:"input_snapshot,omitempty"`
	ModelVersion       *string         `json:"model_version,omitempty"`
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
	MilitaryStatus          *string `json:"military_status"`
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
	Unit *string `json:"unit"`
	// 1110 — Нематериальные активы
	IntangibleAssets *int64 `json:"intangible_assets"`
	// 1120 — Результаты исследований и разработок
	ResearchAndDevelopment *int64 `json:"research_and_development"`
	// 1130 — Нематериальные поисковые активы
	IntangibleExplorationAssets *int64 `json:"intangible_exploration_assets"`
	// 1140 — Материальные поисковые активы
	TangibleExplorationAssets *int64 `json:"tangible_exploration_assets"`
	// 1150 — Основные средства
	FixedAssets *int64 `json:"fixed_assets"`
	// 1160 — Доходные вложения в материальные ценности
	IncomeBearingTangibleInvestments *int64 `json:"income_bearing_tangible_investments"`
	// 1170 — Долгосрочные финансовые вложения
	LongTermFinancialInvestments *int64 `json:"long_term_financial_investments"`
	// 1180 — Отложенные налоговые активы
	DeferredTaxAssets *int64 `json:"deferred_tax_assets"`
	// 1190 — Прочие внеоборотные активы
	OtherNonCurrentAssets *int64 `json:"other_non_current_assets"`
	// 1100 — Итого внеоборотные активы
	NonCurrentAssets *int64 `json:"non_current_assets"`
	// 1210 — Запасы
	Inventories *int64 `json:"inventories"`
	// 1220 — НДС по приобретённым ценностям
	VATOnAcquiredAssets *int64 `json:"vat_on_acquired_assets"`
	// 1230 — Дебиторская задолженность
	Receivables *int64 `json:"receivables"`
	// 1240 — Краткосрочные финансовые вложения
	ShortTermFinancialInvest *int64 `json:"short_term_financial_investments"`
	// 1250 — Деньги и денежные эквиваленты
	CashAndEquivalents *int64 `json:"cash_and_equivalents"`
	// 1260 — Прочие оборотные активы
	OtherCurrentAssets *int64 `json:"other_current_assets"`
	// 1200 — Итого оборотные активы
	CurrentAssets *int64 `json:"current_assets"`
	// 1600 — Баланс: актив
	TotalAssets *int64 `json:"total_assets"`
	// 1310 — Уставный капитал
	CharterCapital *int64 `json:"charter_capital"`
	// 1320 — Собственные выкупленные акции: вычитаемая величина
	TreasuryShares *int64 `json:"treasury_shares"`
	// 1340 — Переоценка внеоборотных активов
	RevaluationReserve *int64 `json:"revaluation_reserve"`
	// 1350 — Добавочный капитал
	AdditionalCapital *int64 `json:"additional_capital"`
	// 1360 — Резервный капитал
	ReserveCapital *int64 `json:"reserve_capital"`
	// 1370 — Нераспределённая прибыль / непокрытый убыток
	RetainedEarnings *int64 `json:"retained_earnings"`
	// 1300 — Итого капитал и резервы
	Equity *int64 `json:"equity"`
	// 1410 — Долгосрочные заёмные средства
	LongTermBorrowings *int64 `json:"long_term_borrowings"`
	// 1420 — Отложенные налоговые обязательства
	DeferredTaxLiabilities *int64 `json:"deferred_tax_liabilities"`
	// 1430 — Долгосрочные оценочные обязательства
	LongTermProvisions *int64 `json:"long_term_provisions"`
	// 1450 — Прочие долгосрочные обязательства
	OtherLongTermLiabilities *int64 `json:"other_long_term_liabilities"`
	// 1400 — Итого долгосрочные обязательства
	LongTermLiabilities *int64 `json:"long_term_liabilities"`
	// 1510 — Краткосрочные заёмные средства
	ShortTermBorrowings *int64 `json:"short_term_borrowings"`
	// 1520 — Кредиторская задолженность
	Payables *int64 `json:"payables"`
	// 1530 — Доходы будущих периодов
	DeferredIncome *int64 `json:"deferred_income"`
	// 1540 — Краткосрочные оценочные обязательства
	ShortTermProvisions *int64 `json:"short_term_provisions"`
	// 1550 — Прочие краткосрочные обязательства
	OtherShortTermLiabilities *int64 `json:"other_short_term_liabilities"`
	// 1500 — Итого краткосрочные обязательства
	CurrentLiabilities *int64 `json:"current_liabilities"`
	// 1700 — Баланс: пассив
	TotalEquityAndLiabilities *int64 `json:"total_equity_and_liabilities"`
}

// ProfitAndLoss — строки ОФР, нужные модели.
type ProfitAndLoss struct {
	Unit *string `json:"unit"`
	// 2110 — Выручка без НДС и акцизов
	Revenue *int64 `json:"revenue"`
	// 2120 — Себестоимость продаж
	CostOfSales *int64 `json:"cost_of_sales"`
	// 2100 — Валовая прибыль / убыток
	GrossProfit *int64 `json:"gross_profit"`
	// 2210 — Коммерческие расходы
	SellingExpenses *int64 `json:"selling_expenses"`
	// 2220 — Управленческие расходы
	AdministrativeExpenses *int64 `json:"administrative_expenses"`
	// 2200 — Прибыль / убыток от продаж
	SalesProfit *int64 `json:"sales_profit"`
	// 2310 — Доходы от участия в других организациях
	ParticipationIncome *int64 `json:"participation_income"`
	// 2320 — Проценты к получению
	InterestReceivable *int64 `json:"interest_receivable"`
	// 2330 — Проценты к уплате
	InterestPayable *int64 `json:"interest_payable"`
	// 2340 — Прочие доходы
	OtherIncome *int64 `json:"other_income"`
	// 2350 — Прочие расходы
	OtherExpenses *int64 `json:"other_expenses"`
	// 2300 — Прибыль / убыток до налогообложения
	ProfitBeforeTax *int64 `json:"profit_before_tax"`
	// 2410 — Налог на прибыль: расход положительный, доход отрицательный
	IncomeTax *int64 `json:"income_tax"`
	// 2400 — Чистая прибыль / убыток
	NetProfit *int64 `json:"net_profit"`
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
