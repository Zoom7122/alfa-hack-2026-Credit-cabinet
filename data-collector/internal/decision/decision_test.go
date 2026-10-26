package decision

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

var evaluation = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)

// coreJSON — АБС: 12 месяцев оборотов 2025-10…2026-09 и выписка с классами платежей.
func coreJSON() string {
	var months, txs []string
	for i := range 12 {
		m := time.Date(2025, 10+time.Month(i), 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
		months = append(months, fmt.Sprintf(`{"month":"%s","credit_turnover":1100000,"debit_turnover":900000,"avg_balance":200000}`, m))
		for _, t := range []string{
			`{"date":"%s-05","direction":"CREDIT","amount":100000,"purpose":"Предоставление процентного займа учредителем","counterparty":{"inn":"1"}}`,
			`{"date":"%s-06","direction":"CREDIT","amount":600000,"purpose":"Оплата по счету за товар","counterparty":{"inn":"7700000001","name":"ООО \"ПОКУПАТЕЛЬ\""}}`,
			`{"date":"%s-10","direction":"DEBIT","amount":100000,"purpose":"Погашение основного долга и процентов по кредитному договору","counterparty":{"name":"ПАО \"БАНК ВОСТОК\""}}`,
			`{"date":"%s-11","direction":"DEBIT","amount":50000,"purpose":"Единый налоговый платеж","counterparty":{}}`,
			`{"date":"%s-12","direction":"DEBIT","amount":150000,"purpose":"Перечисление заработной платы","counterparty":{}}`,
			`{"date":"%s-13","direction":"DEBIT","amount":30000,"purpose":"Лизинговый платеж по договору","counterparty":{"name":"АО \"ТЕХЛИЗИНГ\""}}`,
		} {
			txs = append(txs, fmt.Sprintf(t, m))
		}
	}
	return `{"client":{"client_since":"2022-03-14"},"turnover":{"months":[` + strings.Join(months, ",") + `]},
		"loans":{"items":[{"current_overdue_days":0,"max_overdue_days_24m":3}]},
		"restrictions":{"card_file":{"has_unpaid_claims":false},"account_blocks":[],"arrests":[]},
		"transactions":[{"items":[` + strings.Join(txs, ",") + `]}]}`
}

const (
	bkiJSON = `{"legal":{"has_history":true,"summary":{"total_outstanding":1200000,"max_overdue_days_ever":17,"inquiries_90d":2},
		"accounts":[{"creditor_name":"ПАО \"БАНК ВОСТОК\"","opened_at":"2023-01-10","planned_close_at":"2027-03-12","monthly_payment":100000,"status":"ACTIVE","current_overdue_days":0},
		            {"creditor_name":"АО \"АЛЬФА-БАНК\"","opened_at":"2024-05-01","planned_close_at":"2026-12-01","monthly_payment":null,"status":"ACTIVE"}]},
		"person":{"has_history":true}}`
	fnsJSON = `{"egrul":{"rows":[{"r":"14.03.2021","g":"ГЕНЕРАЛЬНЫЙ ДИРЕКТОР: Ковалёв Игорь Сергеевич"}]},
		"pb":{"vyp":{"КодОКВЭД":"46.39","sulst_ex":10,"invalid":0,"pr_zd":0,"masruk":[{"inn":"779900010139"}]},"liquidated":false},
		"bo_bfo":[{"period":"2025","typeCorrections":[{"correction":{
			"balance":{"current1100":3120,"current1200":18310,"current1600":21430,"current1300":7650,"previous1600":17260,"previous1300":4920},
			"financialResult":{"current2110":10000,"current2400":2730,"previous2110":9000}}}]}],
		"account_blocks":{"044525999":{"decisions":[]}}}`
	taxJSON = `{"report":{"tax_regime":{"current":"OSN"},"documents":[
		{"type":"PROFIT","period":"2025","data":{"revenue_from_sales":10000000}},
		{"type":"PROFIT","period":"2024","data":{"revenue_from_sales":9000000}},
		{"type":"ENS_BALANCE","data":{"debt":0,"penalties":0,"fines":0}}]}}`
)

func legalInput() Input {
	return Input{
		Application: domain.Application{ID: "app_1",
			Company:        domain.Company{ID: "apl_1", INN: "7799000012", Type: domain.CompanyLegalEntity, Name: `ООО "СЕВЕРНЫЙ ВЕТЕР"`},
			Representative: domain.Representative{Role: domain.RoleDirector},
			Requested:      &domain.RequestedTerms{Amount: 5_000_000, TermMonths: 24},
			Purpose:        &domain.Purpose{Code: "WORKING_CAPITAL"}},
		Age: 42, MaritalStatus: "MARRIED", Children: 2,
		Scoring: &domain.ScoringAnswers{MaxAnnualRate: ptr(28.0), MinAcceptableAmount: ptr(int64(1_500_000)),
			MaxAcceptableTermMonths: ptr(36), NeedCost: ptr(int64(5_500_000)), OwnContribution: ptr(int64(500_000)),
			FreeCash: ptr(int64(100_000)), MinimumCash: ptr(int64(20_000)), NeedEvidence: "VERIFIED_ESTIMATE", ExperienceYears: ptr(8)},
		Collect: domain.CollectInput{Director: &domain.Person{INN: "779900010139"},
			BKICompanyConsent: &domain.Consent{ID: "1"}, BKIPersonConsent: &domain.Consent{ID: "2"}},
		Systems: map[domain.System]json.RawMessage{
			domain.SystemCoreBanking: json.RawMessage(coreJSON()), domain.SystemBKI: json.RawMessage(bkiJSON),
			domain.SystemFNS: json.RawMessage(fnsJSON), domain.SystemTaxReports: json.RawMessage(taxJSON),
			domain.SystemFSSP:       json.RawMessage(`{"company":{"summary":{"active_amount":48250}}}`),
			domain.SystemKAD:        json.RawMessage(`{"cases":[{"summary":{"as_defendant_active_amount":10000,"bankruptcy_as_debtor":false}}]}`),
			domain.SystemFedresurs:  json.RawMessage(`{"company_messages":{"summary":{"bankruptcy_stage":"NONE","liquidation":false}}}`),
			domain.SystemCompliance: json.RawMessage(`{"zsk":{"risk_level":"LOW"},"screening":{"results":[{"listed":false}]}}`),
			domain.SystemSignals:    json.RawMessage(`{"news":{"items":[{"tags":["EXPANSION"]}]}}`),
		},
		FactsDate: evaluation, Now: evaluation,
	}
}

func TestBuildLegalEntity(t *testing.T) {
	req, facts := Build(legalInput())

	if req.Borrower.Type != "LEGAL_ENTITY" || *req.Borrower.INN != "7799000012" || *req.FactsDate != "2026-10-22" {
		t.Fatalf("borrower: %+v %v", req.Borrower, *req.FactsDate)
	}
	if *req.CreditRequest.Purpose != "BUSINESS_DEVELOPMENT" || *req.CreditRequest.MaxAnnualRate != 28 {
		t.Errorf("credit request: %+v", req.CreditRequest)
	}

	// 12 месяцев: поступления без займа учредителя, долги — погашения и лизинг, налоги, ФОТ, остальное — переменные
	if len(req.MonthlyHistory) != 12 || *req.MonthlyHistory[0].Month != "2025-10" || *req.MonthlyHistory[11].Month != "2026-09" {
		t.Fatalf("history months: %d", len(req.MonthlyHistory))
	}
	m := req.MonthlyHistory[0]
	if *m.OperatingReceiptsRUB != 1_000_000 || *m.ExistingDebtPaymentRUB != 130_000 || *m.TaxesPaidRUB != 50_000 ||
		*m.FixedPaymentsRUB != 150_000 || *m.VariablePaymentsRUB != 570_000 || *m.MaintenanceInvestmentRUB != 0 {
		t.Errorf("month: R %d debt %d tax %d fixed %d var %d", *m.OperatingReceiptsRUB, *m.ExistingDebtPaymentRUB,
			*m.TaxesPaidRUB, *m.FixedPaymentsRUB, *m.VariablePaymentsRUB)
	}
	if facts.AnnualReceipts != 12_000_000 {
		t.Errorf("annual receipts: %d", facts.AnnualReceipts)
	}

	// долги: остаток из БКИ, график на 36 месяцев с ноября 2026, кредит — до марта 2027
	if *req.FinancialDebtRUB != 1_200_000 || len(req.FutureDebtPayments) != 36 || *req.FutureDebtPayments[0].Month != "2026-11" {
		t.Fatalf("debts: %v %d", *req.FinancialDebtRUB, len(req.FutureDebtPayments))
	}
	if *req.FutureDebtPayments[4].PaymentRUB != 100_000 || *req.FutureDebtPayments[5].PaymentRUB != 0 {
		t.Errorf("schedule: 2027-03 %d, 2027-04 %d", *req.FutureDebtPayments[4].PaymentRUB, *req.FutureDebtPayments[5].PaymentRUB)
	}

	// подтверждение: декларация 10 млн × НДС = 12 млн ≥ поступлений — всё подтверждено
	if *req.Evidence.ConfirmedReceiptsRUB != 12_000_000 || *req.Evidence.UnexplainedDifferenceRUB != 0 || req.Evidence.LargestBuyerGroupReceiptsRUB != nil {
		t.Errorf("evidence: %+v", req.Evidence)
	}
	le := req.Scoring.LegalEntity
	if req.Scoring.IP != nil || le == nil {
		t.Fatal("legal entity branch only")
	}
	checks := map[string]bool{
		"business age 67":        *le.BusinessAgeMonths == 67,
		"credit history 45":      *le.CreditHistoryMonths == 45,
		"worst dpd 17":           *le.WorstRepaidDPDDays24M == 17,
		"applications 2":         *le.Applications90D == 2,
		"taxes 0":                *le.OverdueTaxesRUB == 0,
		"bank 55 months":         *le.BankRelationshipMonths == 55,
		"verification 3":         *le.ReportVerificationLevel == 3,
		"active":                 *le.Active && !*le.LiquidatedOrExcluded && !*le.Bankrupt,
		"authority":              *le.IdentityAndAuthorityVerified && *le.ConsentsVerified,
		"bureau":                 *le.BureauReceived && *le.DebtsComplete && *le.OrganizationAndRelatedDebtsComplete,
		"documents":              *le.DocumentsComplete,
		"free cash":              *le.FreeCashSourceVerified,
		"no restriction":         !*le.MaterialAccountRestriction,
		"no material dispute":    !*le.MaterialDisputeOrEnforcement,
		"no buyer loss":          !*le.KeyBuyerLoss && !*le.StructuralBusinessChange,
		"auditor not required":   *le.AuditorOpinion == "NOT_REQUIRED",
		"operating":              *le.OperatingNonfinancialCompany && !*le.MaterialRegisterInaccuracy,
		"need evidence answered": *le.NeedEvidence == "VERIFIED_ESTIMATE",
	}
	for name, ok := range checks {
		if !ok {
			t.Errorf("legal entity: %s", name)
		}
	}
	if facts.StatementYears != 2 || facts.DirectorInEGRUL == nil || !*facts.DirectorInEGRUL {
		t.Errorf("facts: %+v", facts)
	}

	// JSON — поля credit-engine, факты вложенных структур на верхнем уровне ветки
	raw, _ := json.Marshal(req.Scoring)
	for _, key := range []string{`"business_age_months":67`, `"bankrupt":false`, `"manager_experience_years":8`, `"ip":null`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("scoring json misses %s: %s", key, raw)
		}
	}
}

func TestBuildMissingData(t *testing.T) {
	in := legalInput()
	in.Scoring = nil
	in.Collect.BKICompanyConsent = nil
	delete(in.Systems, domain.SystemBKI)
	in.Application.Representative.Role = domain.RoleAccountant // без МЧД
	req, facts := Build(in)
	if req.FinancialDebtRUB != nil || req.FutureDebtPayments != nil || req.CreditRequest.MaxAnnualRate != nil {
		t.Errorf("unknown facts must be null: %+v", req)
	}
	le := req.Scoring.LegalEntity
	if le.CreditHistoryMonths != nil || *le.BureauReceived || *le.ConsentsVerified || *le.IdentityAndAuthorityVerified {
		t.Errorf("bureau and authority: %+v", le)
	}
	if facts.AuthorityVerified {
		t.Error("accountant without POA must not be verified")
	}
	// МЧД на представителя — полномочия подтверждены
	in.Systems[domain.SystemM4D] = json.RawMessage(`{"poa":{"status":"ACTIVE","representative":{"inn":"779900010333"}}}`)
	in.Collect.RepresentativeINN = "779900010333"
	if _, facts := Build(in); !facts.AuthorityVerified {
		t.Error("active POA must verify authority")
	}
}

func TestBuildSoleProprietor(t *testing.T) {
	in := legalInput()
	in.Application.Company = domain.Company{ID: "apl_ip", INN: "779900010202", Type: domain.CompanySoleProprietor, Name: "ИП Смирнова"}
	in.Application.Representative.Role = domain.RoleOwner
	in.MaritalStatus = "SINGLE"
	in.Scoring.PrimaryIncomeSource, in.Scoring.BusinessLocation = "BUSINESS", "VERIFIED_ONLINE_OR_MOBILE"
	in.Systems[domain.SystemTaxReports] = json.RawMessage(`{"report":{"tax_regime":{"current":"NPD"},"documents":[
		{"type":"NPD_INCOME","period":"2025","data":{"income":13000000}},{"type":"NPD_INCOME","period":"2024","data":{"income":2000000}}]}}`)
	req, _ := Build(in)
	ip := req.Scoring.IP
	if req.Borrower.Type != "SOLE_PROPRIETOR" || ip == nil || req.Scoring.LegalEntity != nil {
		t.Fatalf("ip branch: %+v", req.Scoring)
	}
	if *ip.OwnerAge != 42 || *ip.MaritalStatus != "NEVER_MARRIED" || *ip.Dependents != 2 || *ip.IncomeEvidence != "VERIFIED" ||
		*ip.PrimaryIncomeSource != "BUSINESS" || ip.Qualification != nil {
		t.Errorf("ip answers: %+v", ip)
	}
	// у ИП без НДС: поступления 12 млн ≤ доход 13 млн; крупнейший покупатель — по выписке
	if *req.Evidence.ConfirmedReceiptsRUB != 12_000_000 || *req.Evidence.LargestBuyerGroupReceiptsRUB != 7_200_000 {
		t.Errorf("evidence: %+v", req.Evidence)
	}
	if req.IPEconomicData == nil || *req.IPEconomicData.PreviousComparableReceiptsRUB != 2_000_000 {
		t.Errorf("previous receipts: %+v", req.IPEconomicData)
	}
}

func TestStatementsFromFNS(t *testing.T) {
	st, err := StatementsFromFNS(json.RawMessage(fnsJSON), "apl_1", []int{2024, 2025})
	if err != nil || st.BorrowerID != "apl_1" || len(st.AnnualReports) != 2 {
		t.Fatalf("statements: %+v %v", st, err)
	}
	y2024, y2025 := st.AnnualReports[0], st.AnnualReports[1]
	// 2025 — «текущий» период формы 2025, 2024 — её «предыдущий»; отсутствующие строки — 0, пассив = актив
	if *y2025.Year != 2025 || *y2025.PeriodEnd != "2025-12-31" || *y2025.Balance.TotalAssets != 21430 ||
		*y2025.Balance.TotalEquityAndLiabilities != 21430 || *y2025.Balance.ShortTermFinancialInvest != 0 ||
		*y2025.ProfitAndLoss.Revenue != 10000 || *y2025.Balance.Unit != "THOUSAND_RUB" {
		t.Errorf("2025: %+v %+v", y2025.Balance, y2025.ProfitAndLoss)
	}
	if *y2024.Year != 2024 || *y2024.Balance.TotalAssets != 17260 || *y2024.ProfitAndLoss.Revenue != 9000 {
		t.Errorf("2024: %+v", y2024.Balance)
	}
	if st, _ := StatementsFromFNS(nil, "apl_ip", []int{2024, 2025}); len(st.AnnualReports) != 0 {
		t.Errorf("no fns: %+v", st)
	}
}

func TestInterpret(t *testing.T) {
	pay := 80000.4
	approved := Interpret(Response{Status: "APPROVED", TermsConfirmed: true, Score: &Score{Total: 873, Grade: "A"},
		Terms: &LoanTerms{AmountRUB: 2_030_000, TermMonths: 36, AnnualRate: 20.75}, MonthlyPaymentRUB: &pay}, Facts{AuthorityVerified: true})
	if approved.Status != domain.ApplicationOffered || approved.AutoOutcome != "APPROVED" || approved.Grade != "A" ||
		approved.Offer == nil || approved.Offer.MonthlyPayment != 80000 || !approved.Offer.Confirmed {
		t.Errorf("approved: %+v", approved)
	}

	rejected := Interpret(Response{Status: "REJECTED", Reasons: []Reason{{Code: "BANKRUPT", Message: "Банкротство"}}}, Facts{AuthorityVerified: true})
	if rejected.Status != domain.ApplicationRejected || len(rejected.ClientReasons) != 1 || rejected.ClientReasons[0].Title != "Банкротство" {
		t.Errorf("rejected: %+v", rejected)
	}

	review := Interpret(Response{Status: "UNDER_REVIEW", ReviewReason: ptr("BANK"), Terms: &LoanTerms{AmountRUB: 1_000_000, TermMonths: 12, AnnualRate: 24}},
		Facts{AuthorityVerified: true})
	if review.Status != domain.ApplicationManualReview || review.Offer == nil || review.Offer.MonthlyPayment != Annuity(1_000_000, 24, 12) {
		t.Errorf("review: %+v", review)
	}

	// вопросы — по темам: стоимость и вклад — два вопроса с полем анкеты, выписка — документ, неизвестный факт — текст причины
	need := Interpret(Response{Status: "NEED_INFO", Reasons: []Reason{
		{Code: "MISSING_DATA", Message: "Нужны данные", Fields: []string{"need.confirmed_cost_rub", "need.own_contribution_rub", "monthly_history"}},
		{Code: "MISSING_DATA", Message: "Нужны данные", Fields: []string{"scoring.legal_entity.material_forgery", "credit_request.max_annual_rate"}},
	}}, Facts{AuthorityVerified: true})
	if need.Status != domain.ApplicationWaitingForInfo || len(need.Questions) != 5 {
		t.Fatalf("need info: %+v", need.Questions)
	}
	byCode := map[string]Question{}
	for _, q := range need.Questions {
		byCode[q.Code] = q
	}
	if q := byCode["NEED_COST"]; q.Answer.Type != "MONEY" || q.Answer.ScoringField != "need_cost" || !q.Required {
		t.Errorf("need cost: %+v", q)
	}
	if q := byCode["MONTHLY_HISTORY"]; q.Answer.Type != "NONE" || q.Answer.DocTypes[0] != "BANK_STATEMENT" {
		t.Errorf("history: %+v", q)
	}
	if q := byCode["MISSING_DATA"]; q.Description != "Нужны данные" || q.Answer.Type != "TEXT" {
		t.Errorf("generic question: %+v", q)
	}
	if q := byCode["MAX_RATE"]; q.Answer.Type != "NUMBER" || q.Answer.ScoringField != "max_annual_rate" {
		t.Errorf("rate: %+v", q)
	}

	// повторная оценка: отвеченные темы не спрашиваем; не осталось ничего — к специалисту
	partly := WithoutAnswered(need, map[string]bool{"NEED_COST": true, "MONTHLY_HISTORY": true})
	if partly.Status != domain.ApplicationWaitingForInfo || len(partly.Questions) != 3 {
		t.Errorf("partly answered: %+v", partly.Questions)
	}
	all := map[string]bool{}
	for _, q := range need.Questions {
		all[q.Code] = true
	}
	if done := WithoutAnswered(need, all); done.Status != domain.ApplicationManualReview || done.AutoOutcome != "MANUAL_REVIEW" || len(done.Questions) != 0 {
		t.Errorf("all answered: %+v", done)
	}
}

func TestFindings(t *testing.T) {
	f := Facts{AnnualReceipts: 12_900_000, DeclaredRevenue: 18_400_000, DeclaredSource: "TAX_REPORTS",
		DebtCounterparts: []string{`АО "АЛЬФА-БАНК"`, `ООО "ЛИЗИНГ-ПРО"`, `АО "ТЕХЛИЗИНГ"`},
		BureauCreditors:  []string{`ООО «ЛИЗИНГ-ПРО»`}, DirectorInEGRUL: ptr(false)}
	codes := map[string]bool{}
	for _, x := range f.findings() {
		codes[x.Code] = x.Material
	}
	for _, code := range []string{"REVENUE_MISMATCH", "UNDECLARED_DEBT", "DIRECTOR_MISMATCH", "NO_POA"} {
		if _, ok := codes[code]; !ok {
			t.Errorf("findings: no %s in %v", code, codes)
		}
	}
	if codes["REVENUE_MISMATCH"] { // 29,9 % — не больше 30 %
		t.Error("revenue mismatch under 30% is not material")
	}
	ok := Facts{AnnualReceipts: 10_000_000, DeclaredRevenue: 10_500_000, AuthorityVerified: true}
	if got := ok.findings(); len(got) != 0 {
		t.Errorf("clean facts: %+v", got)
	}
}

func TestAnnuity(t *testing.T) {
	if got := Annuity(1_200_000, 0, 12); got != 100_000 {
		t.Errorf("zero rate: %d", got)
	}
	if got := Annuity(1_000_000, 24, 12); got < 94_500 || got > 94_600 {
		t.Errorf("24%% / 12m: %d", got)
	}
}
