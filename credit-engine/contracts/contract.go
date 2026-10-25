// Package contracts defines MVP REST/JSON DTOs. Calculations belong in Go service code.
package contracts

type BorrowerType string

const (
	BorrowerTypeSoleProprietor BorrowerType = "SOLE_PROPRIETOR"
	BorrowerTypeLegalEntity    BorrowerType = "LEGAL_ENTITY"
)

type Grade string

const (
	GradeA Grade = "A"
	GradeB Grade = "B"
	GradeC Grade = "C"
	GradeD Grade = "D"
)

type DecisionStatus string

const (
	DecisionStatusApproved    DecisionStatus = "APPROVED"
	DecisionStatusNeedInfo    DecisionStatus = "NEED_INFO"
	DecisionStatusUnderReview DecisionStatus = "UNDER_REVIEW"
	DecisionStatusRejected    DecisionStatus = "REJECTED"
)

type ReviewReason string

const (
	ReviewReasonBorrower ReviewReason = "BORROWER"
	ReviewReasonBank     ReviewReason = "BANK"
)

type StatementUnit string

const (
	StatementUnitRUB         StatementUnit = "RUB"
	StatementUnitThousandRUB StatementUnit = "THOUSAND_RUB"
)

type BankVerdict string

const (
	BankVerdictIssued     BankVerdict = "ISSUED"
	BankVerdictBankReview BankVerdict = "BANK_REVIEW"
)

type NotificationStatus string

const (
	NotificationStatusDelivered NotificationStatus = "DELIVERED"
	NotificationStatusPending   NotificationStatus = "PENDING"
)

type EventType string

const (
	EventTypeLoanIssued     EventType = "LOAN_ISSUED"
	EventTypeBorrowerReview EventType = "BORROWER_REVIEW"
	EventTypeBankReview     EventType = "BANK_REVIEW"
)

type MaritalStatus string

const (
	MaritalStatusMarried          MaritalStatus = "MARRIED"
	MaritalStatusNeverMarried     MaritalStatus = "NEVER_MARRIED"
	MaritalStatusDivorcedOnce     MaritalStatus = "DIVORCED_ONCE"
	MaritalStatusDivorcedMultiple MaritalStatus = "DIVORCED_MULTIPLE"
	MaritalStatusWidowed          MaritalStatus = "WIDOWED"
)

type IncomeSource string

const (
	IncomeSourceBusiness          IncomeSource = "BUSINESS"
	IncomeSourcePublicEmployment  IncomeSource = "PUBLIC_EMPLOYMENT"
	IncomeSourcePrivateEmployment IncomeSource = "PRIVATE_EMPLOYMENT"
	IncomeSourcePension           IncomeSource = "PENSION"
	IncomeSourceSelfEmployedOrGph IncomeSource = "SELF_EMPLOYED_OR_GPH"
	IncomeSourceNone              IncomeSource = "NONE"
)

type Qualification string

const (
	QualificationNone                     Qualification = "NONE"
	QualificationSupport                  Qualification = "SUPPORT"
	QualificationSpecialist               Qualification = "SPECIALIST"
	QualificationManagerOrHighlyQualified Qualification = "MANAGER_OR_HIGHLY_QUALIFIED"
)

type CarClass string

const (
	CarClassNone    CarClass = "NONE"
	CarClassEconomy CarClass = "ECONOMY"
	CarClassComfort CarClass = "COMFORT"
	CarClassPremium CarClass = "PREMIUM"
)

type EducationLevel string

const (
	EducationLevelBelowGeneral   EducationLevel = "BELOW_GENERAL"
	EducationLevelGeneral        EducationLevel = "GENERAL"
	EducationLevelVocational     EducationLevel = "VOCATIONAL"
	EducationLevelHigher         EducationLevel = "HIGHER"
	EducationLevelAcademicDegree EducationLevel = "ACADEMIC_DEGREE"
)

type RealEstateStatus string

const (
	RealEstateStatusUnencumberedWhole RealEstateStatus = "UNENCUMBERED_WHOLE"
	RealEstateStatusUnencumberedShare RealEstateStatus = "UNENCUMBERED_SHARE"
	RealEstateStatusEncumberedOnly    RealEstateStatus = "ENCUMBERED_ONLY"
	RealEstateStatusNone              RealEstateStatus = "NONE"
)

type BusinessLocation string

const (
	BusinessLocationVerifiedPhysical       BusinessLocation = "VERIFIED_PHYSICAL"
	BusinessLocationVerifiedOnlineOrMobile BusinessLocation = "VERIFIED_ONLINE_OR_MOBILE"
	BusinessLocationUnverified             BusinessLocation = "UNVERIFIED"
)

type IncomeEvidence string

const (
	IncomeEvidenceVerified                   IncomeEvidence = "VERIFIED"
	IncomeEvidenceDocumentsPendingCrosscheck IncomeEvidence = "DOCUMENTS_PENDING_CROSSCHECK"
	IncomeEvidenceNone                       IncomeEvidence = "NONE"
)

type BuyerContracts string

const (
	BuyerContractsActiveAndPerformed BuyerContracts = "ACTIVE_AND_PERFORMED"
	BuyerContractsRepeatOrders       BuyerContracts = "REPEAT_ORDERS"
	BuyerContractsPlansOnly          BuyerContracts = "PLANS_ONLY"
)

type MilitaryStatus string

const (
	MilitaryStatusServed           MilitaryStatus = "SERVED"
	MilitaryStatusExemptOrDeferred MilitaryStatus = "EXEMPT_OR_DEFERRED"
	MilitaryStatusNotLiable        MilitaryStatus = "NOT_LIABLE"
	MilitaryStatusOtherOrUnknown   MilitaryStatus = "OTHER_OR_UNKNOWN"
)

type AuditorOpinion string

const (
	AuditorOpinionUnmodified           AuditorOpinion = "UNMODIFIED"
	AuditorOpinionNotRequired          AuditorOpinion = "NOT_REQUIRED"
	AuditorOpinionMaterialModification AuditorOpinion = "MATERIAL_MODIFICATION"
	AuditorOpinionRequiredMissing      AuditorOpinion = "REQUIRED_MISSING"
)

type NeedEvidence string

const (
	NeedEvidenceContractAndCost  NeedEvidence = "CONTRACT_AND_COST"
	NeedEvidenceVerifiedEstimate NeedEvidence = "VERIFIED_ESTIMATE"
	NeedEvidenceOralOnly         NeedEvidence = "ORAL_ONLY"
)

type Borrower struct {
	ID   string       `json:"id"`
	Type BorrowerType `json:"type"`
	INN  *string      `json:"inn"`
	Name *string      `json:"name"`
}

type CreditRequest struct {
	Purpose    *string `json:"purpose"`
	AmountRUB  *int64  `json:"amount_rub"`
	TermMonths *int    `json:"term_months"`
	// Percent, not a fraction: 23.75 means 23.75%; range 0..100, at most two decimals.
	MaxAnnualRate           *float64 `json:"max_annual_rate"`
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
	Month                    *string `json:"month"`
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

type DecisionRequest struct {
	ApplicationID      string          `json:"application_id"`
	ModelVersion       *string         `json:"model_version,omitempty"`
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
	IPEconomicData     *IpEconomicData `json:"ip_economic_data"`
}

type AnnualReport struct {
	Year          *int           `json:"year"`
	PeriodEnd     *string        `json:"period_end"`
	Balance       *BalanceShit   `json:"balance"`
	ProfitAndLoss *ProfitAndLoss `json:"profit_and_loss"`
}

type StatementsResponse struct {
	BorrowerID    string         `json:"borrower_id"`
	AnnualReports []AnnualReport `json:"annual_reports"`
}

type Reason struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Fields  []string `json:"fields"`
}

type ScoreResult struct {
	Model         string              `json:"model"`
	Questionnaire QuestionnaireResult `json:"questionnaire"`
	Economics     EconomicResult      `json:"economics"`
	RawTotal      float64             `json:"raw_total"`
	Total         int                 `json:"total"`
	Grade         Grade               `json:"grade"`
}

type LoanTerms struct {
	AmountRUB  int64 `json:"amount_rub"`
	TermMonths int   `json:"term_months"`
	// Percent, not a fraction: 24 means 24%; range 0..100, at most two decimals.
	AnnualRate float64 `json:"annual_rate"`
}

type SelectionAudit struct {
	FeasibleCandidateCount   int     `json:"feasible_candidate_count"`
	SameAmountCandidateCount int     `json:"same_amount_candidate_count"`
	MaxNPVRUB                float64 `json:"max_npv_rub"`
	SelectedNPVRUB           float64 `json:"selected_npv_rub"`
	ProfitRetention          float64 `json:"profit_retention"`
	MonthlyPaymentRUB        float64 `json:"monthly_payment_rub"`
	OverpaymentRUB           float64 `json:"overpayment_rub"`
}

type DecisionResponse struct {
	ApplicationID     string         `json:"application_id"`
	BorrowerID        string         `json:"borrower_id"`
	Status            DecisionStatus `json:"status"`
	Message           string         `json:"message"`
	Score             *ScoreResult   `json:"score"`
	Terms             *LoanTerms     `json:"terms"`
	TermsConfirmed    bool           `json:"terms_confirmed"`
	MonthlyPaymentRUB *float64       `json:"monthly_payment_rub"`
	OverpaymentRUB    *float64       `json:"overpayment_rub"`
	LoanID            *string        `json:"loan_id"`
	ReviewReason      *ReviewReason  `json:"review_reason"`
	Reasons           []Reason       `json:"reasons"`
}

type BankCheckRequest struct {
	PolicyID       string         `json:"policy_id"`
	ApplicationID  string         `json:"application_id"`
	Borrower       Borrower       `json:"borrower"`
	RequestedTerms CreditRequest  `json:"requested_terms"`
	Terms          LoanTerms      `json:"terms"`
	Score          ScoreResult    `json:"score"`
	Selection      SelectionAudit `json:"selection"`
}

type CheckResult struct {
	Code     string   `json:"code"`
	Before   *float64 `json:"before"`
	After    *float64 `json:"after"`
	Operator *string  `json:"operator"`
	Limit    *float64 `json:"limit"`
	Passed   bool     `json:"passed"`
	Detail   string   `json:"detail"`
}

type BankCheckResponse struct {
	ApplicationID      string             `json:"application_id"`
	Verdict            BankVerdict        `json:"verdict"`
	LoanID             *string            `json:"loan_id"`
	Checks             []CheckResult      `json:"checks"`
	EventID            string             `json:"event_id"`
	NotificationStatus NotificationStatus `json:"notification_status"`
}

type ManualReviewRequest struct {
	PolicyID       string        `json:"policy_id"`
	ApplicationID  string        `json:"application_id"`
	Borrower       Borrower      `json:"borrower"`
	RequestedTerms CreditRequest `json:"requested_terms"`
	Score          ScoreResult   `json:"score"`
	Reasons        []Reason      `json:"reasons"`
}

type ManualReviewResponse struct {
	ApplicationID      string             `json:"application_id"`
	EventID            string             `json:"event_id"`
	NotificationStatus NotificationStatus `json:"notification_status"`
}

type AdminEvent struct {
	EventID        string          `json:"event_id"`
	OccurredAt     string          `json:"occurred_at"`
	Kind           EventType       `json:"kind"`
	ApplicationID  string          `json:"application_id"`
	Borrower       Borrower        `json:"borrower"`
	Score          ScoreResult     `json:"score"`
	RequestedTerms CreditRequest   `json:"requested_terms"`
	LoanID         *string         `json:"loan_id"`
	Terms          *LoanTerms      `json:"terms"`
	Reasons        []Reason        `json:"reasons"`
	Problems       []CheckResult   `json:"problems"`
	Selection      *SelectionAudit `json:"selection"`
}

type EventAck struct {
	EventID  string `json:"event_id"`
	Accepted bool   `json:"accepted"`
}

type Problem struct {
	Type   string   `json:"type"`
	Title  string   `json:"title"`
	Status int      `json:"status"`
	Code   string   `json:"code"`
	Detail string   `json:"detail"`
	Fields []string `json:"fields"`
}

type BankState struct {
	K1                   float64            `json:"K1"`
	Ka                   float64            `json:"Ka"`
	K0                   float64            `json:"K0"`
	RWA11                float64            `json:"RWA11"`
	RWA12                float64            `json:"RWA12"`
	RWA10                float64            `json:"RWA10"`
	Lam                  float64            `json:"Lam"`
	Lat                  float64            `json:"Lat"`
	D2                   float64            `json:"D2"`
	D3                   float64            `json:"D3"`
	Krd                  float64            `json:"Krd"`
	Funding4             float64            `json:"Funding4"`
	Invest               float64            `json:"Invest"`
	Cash                 float64            `json:"Cash"`
	CashMin              float64            `json:"CashMin"`
	Reserves             float64            `json:"Reserves"`
	Groups               map[string]float64 `json:"groups"`
	RelatedGroups        []string           `json:"related_groups"`
	BorrowerGroups       map[string]string  `json:"borrower_groups"`
	IssuedApplicationIds []string           `json:"issued_application_ids"`
	IssuanceEnabled      bool               `json:"issuance_enabled"`
}

type BankStateResponse struct {
	PolicyID string             `json:"policy_id"`
	State    BankState          `json:"state"`
	Norms    map[string]float64 `json:"norms"`
}

type Health struct {
	Status string `json:"status"`
}

type IpScoring struct {
	OwnerAge               *int              `json:"owner_age"`
	MaritalStatus          *MaritalStatus    `json:"marital_status"`
	Dependents             *int              `json:"dependents"`
	PrimaryIncomeSource    *IncomeSource     `json:"primary_income_source"`
	Qualification          *Qualification    `json:"qualification"`
	TotalExperienceMonths  *int              `json:"total_experience_months"`
	NetMonthlyIncomeRUB    *int64            `json:"net_monthly_income_rub"`
	CarClass               *CarClass         `json:"car_class"`
	BankRelationshipMonths *int              `json:"bank_relationship_months"`
	EducationLevel         *EducationLevel   `json:"education_level"`
	RealEstateStatus       *RealEstateStatus `json:"real_estate_status"`
	CreditHistoryMonths    *int              `json:"credit_history_months"`
	WorstRepaidDPDDays24M  *int              `json:"worst_repaid_dpd_days_24m"`
	CurrentDPDDays         *int              `json:"current_dpd_days"`
	Applications90D        *int              `json:"applications_90d"`
	BusinessAgeMonths      *int              `json:"business_age_months"`
	OwnerExperienceYears   *int              `json:"owner_experience_years"`
	BusinessLocation       *BusinessLocation `json:"business_location"`
	// Required only for VERIFIED_PHYSICAL.
	LocationStabilityMonths *int            `json:"location_stability_months"`
	OverdueTaxesRUB         *int64          `json:"overdue_taxes_rub"`
	IncomeEvidence          *IncomeEvidence `json:"income_evidence"`
	BuyerContracts          *BuyerContracts `json:"buyer_contracts"`
	// Optional reference field; no points or routing effect.
	MilitaryStatus               *MilitaryStatus `json:"military_status"`
	Active                       *bool           `json:"active"`
	Bankrupt                     *bool           `json:"bankrupt"`
	MaterialForgery              *bool           `json:"material_forgery"`
	CreditProhibited             *bool           `json:"credit_prohibited"`
	IdentityAndAuthorityVerified *bool           `json:"identity_and_authority_verified"`
	ConsentsVerified             *bool           `json:"consents_verified"`
	BureauReceived               *bool           `json:"bureau_received"`
	DebtsComplete                *bool           `json:"debts_complete"`
	DocumentsComplete            *bool           `json:"documents_complete"`
	FreeCashSourceVerified       *bool           `json:"free_cash_source_verified"`
	MaterialAccountRestriction   *bool           `json:"material_account_restriction"`
	MaterialDisputeOrEnforcement *bool           `json:"material_dispute_or_enforcement"`
	KeyBuyerLoss                 *bool           `json:"key_buyer_loss"`
	StructuralBusinessChange     *bool           `json:"structural_business_change"`
}

type LegalEntityScoring struct {
	BusinessAgeMonths      *int   `json:"business_age_months"`
	CreditHistoryMonths    *int   `json:"credit_history_months"`
	WorstRepaidDPDDays24M  *int   `json:"worst_repaid_dpd_days_24m"`
	CurrentDPDDays         *int   `json:"current_dpd_days"`
	Applications90D        *int   `json:"applications_90d"`
	ManagerExperienceYears *int   `json:"manager_experience_years"`
	OverdueTaxesRUB        *int64 `json:"overdue_taxes_rub"`
	// 0 unverified; 1 client only; 2 official source; 3 official plus registers/bank.
	ReportVerificationLevel             *int            `json:"report_verification_level"`
	BankRelationshipMonths              *int            `json:"bank_relationship_months"`
	OrganizationAndRelatedDebtsComplete *bool           `json:"organization_and_related_debts_complete"`
	MaterialRegisterInaccuracy          *bool           `json:"material_register_inaccuracy"`
	AuditorOpinion                      *AuditorOpinion `json:"auditor_opinion"`
	NeedEvidence                        *NeedEvidence   `json:"need_evidence"`
	OperatingNonfinancialCompany        *bool           `json:"operating_nonfinancial_company"`
	LiquidatedOrExcluded                *bool           `json:"liquidated_or_excluded"`
	Active                              *bool           `json:"active"`
	Bankrupt                            *bool           `json:"bankrupt"`
	MaterialForgery                     *bool           `json:"material_forgery"`
	CreditProhibited                    *bool           `json:"credit_prohibited"`
	IdentityAndAuthorityVerified        *bool           `json:"identity_and_authority_verified"`
	ConsentsVerified                    *bool           `json:"consents_verified"`
	BureauReceived                      *bool           `json:"bureau_received"`
	DebtsComplete                       *bool           `json:"debts_complete"`
	DocumentsComplete                   *bool           `json:"documents_complete"`
	FreeCashSourceVerified              *bool           `json:"free_cash_source_verified"`
	MaterialAccountRestriction          *bool           `json:"material_account_restriction"`
	MaterialDisputeOrEnforcement        *bool           `json:"material_dispute_or_enforcement"`
	KeyBuyerLoss                        *bool           `json:"key_buyer_loss"`
	StructuralBusinessChange            *bool           `json:"structural_business_change"`
}

type ScoringInput struct {
	IP          *IpScoring          `json:"ip"`
	LegalEntity *LegalEntityScoring `json:"legal_entity"`
}

type IpEconomicData struct {
	PreviousComparableReceiptsRUB *int64 `json:"previous_comparable_receipts_rub"`
}

type BalanceShit struct {
	Unit *StatementUnit `json:"unit"`
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
	ShortTermFinancialInvestments *int64 `json:"short_term_financial_investments"`
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

type ProfitAndLoss struct {
	Unit *StatementUnit `json:"unit"`
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

type QuestionnaireCriterion struct {
	ID        string `json:"id"`
	Answer    string `json:"answer"`
	Points    int    `json:"points"`
	MaxPoints int    `json:"max_points"`
}

type EconomicCriterion struct {
	ID    string   `json:"id"`
	Value *float64 `json:"value"`
	// BooleanValue shares the wire key "value" with Value; at most one is set.
	BooleanValue *bool   `json:"-"`
	Points       float64 `json:"points"`
	MaxPoints    int     `json:"max_points"`
}

type QuestionnaireResult struct {
	Points    int                      `json:"points"`
	MaxPoints int                      `json:"max_points"`
	Criteria  []QuestionnaireCriterion `json:"criteria"`
}

type EconomicResult struct {
	Points    float64             `json:"points"`
	MaxPoints int                 `json:"max_points"`
	Criteria  []EconomicCriterion `json:"criteria"`
}
