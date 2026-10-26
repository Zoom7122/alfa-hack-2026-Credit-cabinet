package decision

import (
	"encoding/json"
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Input — всё, из чего собирается запрос на оценку.
type Input struct {
	Statements         *Statements
	Application        domain.Application
	Age                int
	MaritalStatus      domain.MaritalStatus
	Children           int
	Scoring            *domain.ScoringAnswers
	Collect            domain.CollectInput // руководитель, согласия, МЧД
	Systems            map[domain.System]json.RawMessage
	FactsDate          time.Time           // когда закончился сбор
	Now                time.Time           // дата оценки: от неё движок отсчитывает 12 месяцев истории и 36 — графика
	ForgedDocument     bool                // подпись документа не прошла проверку целостности
	DocumentIndicators []DocumentIndicator // current admitted document values
}

// Facts — что посчитано из данных: для находок (Findings) и досье андеррайтера.
type Facts struct {
	AnnualReceipts     int64  // операционные поступления за 12 месяцев, ₽
	DeclaredRevenue    int64  // выручка последнего года из деклараций или БФО, ₽ без НДС
	DeclaredSource     string // TAX_REPORTS, FNS_BFO
	HistoryMonths      int
	StatementYears     int
	DebtCounterparts   []string // кредиторы из выписки (погашения, лизинг)
	BureauCreditors    []string // кредиторы из БКИ
	DirectorInEGRUL    *bool    // ИНН руководителя из анкеты есть среди руководителей ЕГРЮЛ
	AuthorityVerified  bool
	DocumentIndicators []DocumentIndicator `json:"document_indicators,omitempty"`
}

// purposes — цели кабинета, которые движок считает развитием бизнеса; остальные идут к сотруднику.
var businessPurposes = []string{"WORKING_CAPITAL", "EQUIPMENT", "VEHICLE", "REAL_ESTATE", "BUSINESS_EXPANSION", "TENDER"}

var maritalCodes = map[domain.MaritalStatus]string{
	"SINGLE": "NEVER_MARRIED", "MARRIED": "MARRIED", "DIVORCED": "DIVORCED_ONCE", "DIVORCED_MULTIPLE": "DIVORCED_MULTIPLE",
}

func ptr[T any](v T) *T { return &v }

func str(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func rub(v float64) int64 { return int64(math.Round(v)) }

// Build — DecisionRequest из анкеты и собранных данных. Чего в данных нет, остаётся null:
// движок ответит NEED_INFO с перечнем полей, и банк спросит клиента.
func Build(in Input) (Request, Facts) {
	app := in.Application
	app.Documents = slices.DeleteFunc(slices.Clone(app.Documents), func(d domain.Document) bool { return d.IsTest() })
	var (
		core  coreBanking
		b     bki
		f     fns
		tax   taxReports
		fs    fssp
		k     kad
		fed   fedresurs
		comp  compliance
		power m4d
		sig   signals
	)
	hasCore := decode(in.Systems[domain.SystemCoreBanking], &core)
	hasBKI := decode(in.Systems[domain.SystemBKI], &b)
	hasFNS := decode(in.Systems[domain.SystemFNS], &f)
	hasTax := decode(in.Systems[domain.SystemTaxReports], &tax) && tax.Report != nil
	decode(in.Systems[domain.SystemFSSP], &fs)
	hasKAD := decode(in.Systems[domain.SystemKAD], &k)
	hasFed := decode(in.Systems[domain.SystemFedresurs], &fed)
	hasComp := decode(in.Systems[domain.SystemCompliance], &comp)
	decode(in.Systems[domain.SystemM4D], &power)
	hasSignals := decode(in.Systems[domain.SystemSignals], &sig) && sig.News != nil

	ip := app.Company.Type == domain.CompanySoleProprietor
	sc := in.Scoring
	if sc == nil {
		sc = &domain.ScoringAnswers{}
	}
	var facts Facts
	lastYear := in.Now.Year() - 1

	req := Request{
		ApplicationID: app.ID,
		Borrower:      Borrower{ID: app.Company.ID, Type: "LEGAL_ENTITY", INN: str(app.Company.INN), Name: str(app.Company.Name)},
		FactsDate:     ptr(in.FactsDate.Format(time.DateOnly)),
	}
	if ip {
		req.Borrower.Type = "SOLE_PROPRIETOR"
	}

	// заявка: сумма и срок из анкеты, границы — из ответов для оценки
	cr := &CreditRequest{MaxAnnualRate: sc.MaxAnnualRate, MinAcceptableAmountRUB: sc.MinAcceptableAmount,
		MaxAcceptableTermMonths: sc.MaxAcceptableTermMonths}
	if r := app.Requested; r != nil {
		cr.AmountRUB, cr.TermMonths = ptr(r.Amount), ptr(r.TermMonths)
	}
	if p := app.Purpose; p != nil {
		code := p.Code
		if slices.Contains(businessPurposes, code) {
			code = "BUSINESS_DEVELOPMENT"
		}
		cr.Purpose = &code
	}
	req.CreditRequest = cr
	req.Need = &Need{ConfirmedCostRUB: sc.NeedCost, OwnContributionRUB: sc.OwnContribution}
	req.Cash = &Cash{FreeAfterOwnContributionRUB: sc.FreeCash, MinimumRequiredRUB: sc.MinimumCash}

	// денежный поток по месяцам: обороты АБС, разложенные по назначениям платежей
	history, receipts, avgBalance := monthlyHistory(core, in.Now)
	req.MonthlyHistory = history
	facts.AnnualReceipts, facts.HistoryMonths = receipts, len(history)

	// долги по БКИ: у ИП — отчёт по ИНН предпринимателя, иначе — как физлица
	report := b.Legal
	if report == nil && ip {
		report = b.Person
	}
	if hasBKI && report != nil {
		req.FinancialDebtRUB = ptr(rub(report.Summary.TotalOutstanding))
		req.FutureDebtPayments = futurePayments(report, in.Now)
		for _, a := range report.Accounts {
			facts.BureauCreditors = append(facts.BureauCreditors, a.CreditorName)
		}
	}

	// подтверждение поступлений: выручка из деклараций, иначе — из бухотчётности
	declared, source, prevDeclared := declaredRevenue(tax, f, lastYear)
	statements, _ := StatementsFromFNS(in.Systems[domain.SystemFNS], app.Company.ID, []int{lastYear - 1, lastYear})
	documentRevenue := false
	if in.Statements != nil {
		statements = *in.Statements
		if !ip {
			req.FinancialStatements = &statements
		}
	} else if ip {
		if income, doc := documentIncome(in.DocumentIndicators, lastYear); doc != nil {
			declared, source, documentRevenue = income, "DOCUMENT_KUDIR", true
			facts.DocumentIndicators = append(facts.DocumentIndicators, *doc)
		}
		if income, doc := documentIncome(in.DocumentIndicators, lastYear-1); doc != nil {
			prevDeclared = income
			facts.DocumentIndicators = append(facts.DocumentIndicators, *doc)
		}
	} else {
		statements, facts.DocumentIndicators = applyDocumentStatements(statements, in.DocumentIndicators, lastYear)
		if len(facts.DocumentIndicators) > 0 {
			req.FinancialStatements = &statements
		}
		for _, doc := range facts.DocumentIndicators {
			if doc.Name == "accounting_revenue" && doc.Period == strconv.Itoa(lastYear) {
				declared, source, documentRevenue = doc.AmountRUB, "DOCUMENT_GIRBO", true
			}
		}
	}
	facts.DeclaredRevenue, facts.DeclaredSource = declared, source
	vat := !ip && (source == "FNS_BFO" || source == "DOCUMENT_GIRBO" || hasTax && tax.Report.TaxRegime.Current == "OSN")
	gross := func(v int64) int64 {
		if vat {
			n := new(big.Int).Mul(big.NewInt(v), big.NewInt(120))
			n.Quo(n, big.NewInt(100))
			if !n.IsInt64() {
				// Only compared with observed receipts, which fit in int64.
				return math.MaxInt64
			}
			return n.Int64()
		}
		return v
	}
	if len(history) == 12 && (declared > 0 || documentRevenue) {
		confirmed := min(receipts, gross(declared))
		req.Evidence = &Evidence{ConfirmedReceiptsRUB: ptr(confirmed), UnexplainedDifferenceRUB: ptr(receipts - confirmed)}
		if ip {
			req.Evidence.LargestBuyerGroupReceiptsRUB = ptr(min(largestBuyer(core), receipts))
		}
	}
	if ip {
		req.IPEconomicData = &IPEconomicData{}
		if prevDeclared > 0 {
			req.IPEconomicData.PreviousComparableReceiptsRUB = ptr(gross(prevDeclared))
		}
	}
	for _, t := range transactions(core) {
		if t.Direction == "DEBIT" && reDebt.MatchString(lower(t.Purpose)) && !slices.Contains(facts.DebtCounterparts, t.Counterparty.Name) {
			facts.DebtCounterparts = append(facts.DebtCounterparts, t.Counterparty.Name)
		}
	}

	// факты по кредитной истории и налогам — общие для ИП и ЮЛ
	credit := creditFacts{}
	if report != nil {
		credit.CreditHistoryMonths = ptr(creditHistoryMonths(report, in.FactsDate))
		credit.WorstRepaidDPDDays24M = ptr(worstDPD(report, core))
		credit.CurrentDPDDays = ptr(currentDPD(report, core))
		credit.Applications90D = ptr(report.Summary.Inquiries90D)
	}
	var egrul *egrulRow
	if hasFNS && f.EGRUL != nil && len(f.EGRUL.Rows) > 0 {
		egrul = &f.EGRUL.Rows[0]
		if t, ok := parseDate(egrul.RegDate); ok {
			credit.BusinessAgeMonths = ptr(monthsBetween(t, in.FactsDate))
		}
	}
	credit.OverdueTaxesRUB = overdueTaxes(tax, hasTax, f)
	if hasCore && core.Client != nil {
		if t, ok := parseDate(core.Client.ClientSince); ok {
			credit.BankRelationshipMonths = ptr(monthsBetween(t, in.FactsDate))
		}
	} else if in.Systems[domain.SystemCoreBanking] != nil {
		credit.BankRelationshipMonths = ptr(0) // в АБС клиента нет — не наш клиент
	}

	// проверяемые факты: стопы, полномочия, ограничения
	risk := riskFacts{}
	if egrul != nil || hasFNS && f.PB != nil {
		active := egrul == nil || egrul.EndDate == ""
		if f.PB != nil && f.PB.Vyp.Status != 0 {
			active = active && f.PB.Vyp.Status == 10
		}
		risk.Active = ptr(active)
	}
	if hasFed && fed.CompanyMessages != nil {
		bankrupt := fed.CompanyMessages.Summary.BankruptcyStage != "" && fed.CompanyMessages.Summary.BankruptcyStage != "NONE"
		for _, c := range k.Cases {
			bankrupt = bankrupt || c.Summary.Bankruptcy
		}
		risk.Bankrupt = ptr(bankrupt)
	}
	risk.MaterialForgery = ptr(in.ForgedDocument)
	if hasComp {
		listed := comp.ZSK != nil && comp.ZSK.RiskLevel == "HIGH"
		if comp.Screening != nil {
			for _, r := range comp.Screening.Results {
				listed = listed || r.Listed
			}
		}
		risk.CreditProhibited = ptr(listed)
	}
	facts.AuthorityVerified, facts.DirectorInEGRUL = authority(app, in.Collect, f, power)
	risk.IdentityAndAuthorityVerified = ptr(facts.AuthorityVerified)
	consents := in.Collect.BKICompanyConsent != nil && (in.Collect.Director == nil || in.Collect.BKIPersonConsent != nil)
	risk.ConsentsVerified = ptr(consents)
	risk.BureauReceived = ptr(report != nil)
	risk.DebtsComplete = ptr(report != nil && (core.Loans != nil || !hasCore))
	for _, report := range statements.AnnualReports {
		if completeStatement(report) {
			facts.StatementYears++
		}
	}
	docsComplete := len(history) == 12 && (ip || facts.StatementYears == 2)
	risk.DocumentsComplete = ptr(docsComplete)
	if sc.FreeCash != nil && avgBalance >= 0 {
		risk.FreeCashSourceVerified = ptr(hasCore && rub(avgBalance) >= *sc.FreeCash)
	}
	if hasCore || hasFNS {
		restricted := core.Restrictions != nil && (core.Restrictions.CardFile.HasUnpaidClaims ||
			len(core.Restrictions.AccountBlocks) > 0 || len(core.Restrictions.Arrests) > 0)
		for _, bl := range f.AccountBlocks {
			restricted = restricted || len(bl.Decisions) > 0
		}
		risk.MaterialAccountRestriction = ptr(restricted)
	}
	if hasKAD || fs.Company != nil {
		claims := 0.0
		if fs.Company != nil {
			claims += fs.Company.Summary.ActiveAmount
		}
		if len(k.Cases) > 0 {
			claims += k.Cases[0].Summary.DefendantActiveAmount
		}
		// существенно — больше 10 % среднемесячных поступлений
		risk.MaterialDisputeOrEnforcement = ptr(receipts > 0 && claims > float64(receipts)/12/10 || receipts == 0 && claims > 0)
	}
	if hasSignals {
		lost := false
		for _, n := range sig.News.Items {
			lost = lost || slices.ContainsFunc(n.Tags, func(t string) bool { return strings.Contains(t, "CLIENT_LOSS") || strings.Contains(t, "BUYER_LOSS") })
		}
		risk.KeyBuyerLoss = ptr(lost)
	}
	if len(history) == 12 {
		risk.StructuralBusinessChange = ptr(receiptsDropped(history))
	}

	if ip {
		docs := slices.ContainsFunc(app.Documents, func(d domain.Document) bool {
			return d.Type == domain.DocFinancialStatements || d.Type == domain.DocTaxReturn || d.Type == domain.DocBankStatement
		})
		evidence := "NONE"
		switch {
		case source == "TAX_REPORTS" || source == "DOCUMENT_KUDIR":
			evidence = "VERIFIED"
		case docs:
			evidence = "DOCUMENTS_PENDING_CROSSCHECK"
		}
		req.Scoring = &ScoringInput{IP: &IPScoring{
			OwnerAge: ptr(in.Age), MaritalStatus: str(maritalCodes[in.MaritalStatus]), Dependents: ptr(in.Children),
			PrimaryIncomeSource: str(sc.PrimaryIncomeSource), Qualification: str(sc.Qualification),
			TotalExperienceMonths: sc.TotalExperienceMonths, NetMonthlyIncomeRUB: sc.NetMonthlyIncome,
			CarClass: str(sc.CarClass), EducationLevel: str(sc.EducationLevel), RealEstateStatus: str(sc.RealEstateStatus),
			OwnerExperienceYears: sc.ExperienceYears, BusinessLocation: str(sc.BusinessLocation),
			LocationStabilityMonths: sc.LocationStabilityMonths, IncomeEvidence: &evidence, BuyerContracts: str(sc.BuyerContracts),
			creditFacts: credit, riskFacts: risk,
		}}
		return req, facts
	}

	le := &LegalEntityScoring{ManagerExperienceYears: sc.ExperienceYears, NeedEvidence: str(sc.NeedEvidence),
		creditFacts: credit, riskFacts: risk}
	if le.NeedEvidence == nil && slices.ContainsFunc(app.Documents, func(d domain.Document) bool { return d.Type == domain.DocContractWithCustomer }) {
		le.NeedEvidence = ptr("CONTRACT_AND_COST")
	}
	level := 0
	if facts.StatementYears == 2 {
		level = 2
		if source == "TAX_REPORTS" && len(history) == 12 {
			level = 3 // сверено с декларациями и выпиской
		}
	}
	le.ReportVerificationLevel = ptr(level)
	le.OrganizationAndRelatedDebtsComplete = ptr(report != nil && (in.Collect.Director == nil || b.Person != nil))
	if f.PB != nil {
		le.MaterialRegisterInaccuracy = ptr(f.PB.Vyp.Invalid != 0)
		okved := f.PB.Vyp.OKVED
		le.OperatingNonfinancialCompany = ptr(okved != "" && !strings.HasPrefix(okved, "64") && !strings.HasPrefix(okved, "65") && !strings.HasPrefix(okved, "66"))
	}
	if risk.Active != nil {
		le.LiquidatedOrExcluded = ptr(!*risk.Active || f.PB != nil && f.PB.Liquidated || hasFed && fed.CompanyMessages != nil && fed.CompanyMessages.Summary.Liquidation)
	}
	if facts.StatementYears > 0 {
		// Documents are in RUB; the supplier may return THOUSAND_RUB.
		last := rubReport(statements.AnnualReports[len(statements.AnnualReports)-1])
		opinion := "NOT_REQUIRED"
		if last.ProfitAndLoss.Revenue == nil || last.Balance.TotalAssets == nil {
			// Partial documents cannot establish whether an audit is required.
			le.AuditorOpinion = nil
		} else if *last.ProfitAndLoss.Revenue > 800_000_000 || *last.Balance.TotalAssets > 400_000_000 {
			opinion = "REQUIRED_MISSING"
			le.AuditorOpinion = &opinion
		} else {
			le.AuditorOpinion = &opinion
		}
	}
	req.Scoring = &ScoringInput{LegalEntity: le}
	return req, facts
}

func transactions(core coreBanking) []transaction {
	var out []transaction
	for _, p := range core.Transactions {
		out = append(out, p.Items...)
	}
	return out
}

// monthlyHistory — 12 завершённых месяцев до даты оценки: поступления без займов и своих переводов,
// выплаты по классам назначений; переменные — остаток списаний. Возвращает и годовые поступления,
// и средний остаток последнего месяца.
func monthlyHistory(core coreBanking, now time.Time) ([]MonthCash, int64, float64) {
	if core.Turnover == nil {
		return nil, 0, -1
	}
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -12, 0)
	window := map[string]bool{}
	for i := range 12 {
		window[start.AddDate(0, i, 0).Format("2006-01")] = true
	}
	type sums struct{ nonOperating, debt, taxes, fixed, owner, own float64 }
	byMonth := map[string]*sums{}
	for _, t := range transactions(core) {
		if len(t.Date) < 7 {
			continue
		}
		m := t.Date[:7]
		s := byMonth[m]
		if s == nil {
			s = &sums{}
			byMonth[m] = s
		}
		p := lower(t.Purpose)
		if t.Direction == "CREDIT" {
			if reNonOperatingCredit.MatchString(p) {
				s.nonOperating += t.Amount
			}
			continue
		}
		switch {
		case reOwnTransfer.MatchString(p):
			s.own += t.Amount
		case reDebt.MatchString(p):
			s.debt += t.Amount
		case reTaxes.MatchString(p):
			s.taxes += t.Amount
		case reFixed.MatchString(p):
			s.fixed += t.Amount
		case reOwner.MatchString(p):
			s.owner += t.Amount
		}
	}
	months := slices.Clone(core.Turnover.Months)
	slices.SortFunc(months, func(a, b turnoverMonth) int { return strings.Compare(a.Month, b.Month) })
	var (
		out      []MonthCash
		receipts int64
		avg      = -1.0
	)
	for _, tm := range months {
		if !window[tm.Month] {
			continue
		}
		s := byMonth[tm.Month]
		if s == nil {
			s = &sums{}
		}
		r := max(rub(tm.CreditTurnover-s.nonOperating), 0)
		v := max(rub(tm.DebitTurnover-s.debt-s.taxes-s.fixed-s.owner-s.own), 0)
		out = append(out, MonthCash{Month: ptr(tm.Month), OperatingReceiptsRUB: ptr(r), VariablePaymentsRUB: ptr(v),
			FixedPaymentsRUB: ptr(rub(s.fixed)), TaxesPaidRUB: ptr(rub(s.taxes)), MaintenanceInvestmentRUB: ptr(int64(0)),
			OwnerRequiredPaymentsRUB: ptr(rub(s.owner)), ExistingDebtPaymentRUB: ptr(rub(s.debt))})
		receipts += r
		avg = tm.AvgBalance
	}
	return out, receipts, avg
}

// futurePayments — график платежей по действующим кредитам БКИ на 36 месяцев после даты оценки.
func futurePayments(r *bkiReport, now time.Time) []DebtPayment {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	out := make([]DebtPayment, 36)
	for i := range out {
		month := start.AddDate(0, i, 0)
		var sum float64
		for _, a := range r.Accounts {
			if a.Status != "ACTIVE" || a.MonthlyPayment == nil {
				continue
			}
			if end, ok := parseDate(a.PlannedCloseAt); ok && end.Before(month) {
				continue
			}
			sum += *a.MonthlyPayment
		}
		out[i] = DebtPayment{Month: ptr(month.Format("2006-01")), PaymentRUB: ptr(rub(sum))}
	}
	return out
}

func creditHistoryMonths(r *bkiReport, at time.Time) int {
	if !r.HasHistory {
		return 0
	}
	first := at
	for _, a := range r.Accounts {
		if t, ok := parseDate(a.OpenedAt); ok && t.Before(first) {
			first = t
		}
	}
	return monthsBetween(first, at)
}

// worstDPD — худшая погашенная просрочка за 24 месяца: по сводке БКИ (30/60/90+) и нашим кредитам.
func worstDPD(r *bkiReport, core coreBanking) int {
	s := r.Summary
	switch {
	case s.Overdue90P24M > 0:
		return 90
	case s.Overdue60P24M > 0:
		return 60
	case s.Overdue30P24M > 0:
		return 30
	}
	worst := min(s.MaxOverdueEver, 29)
	if core.Loans != nil {
		for _, l := range core.Loans.Items {
			worst = max(worst, l.MaxOverdueDays24M)
		}
	}
	return worst
}

func currentDPD(r *bkiReport, core coreBanking) int {
	cur := 0
	for _, a := range r.Accounts {
		cur = max(cur, a.CurrentOverdueDays)
	}
	if core.Loans != nil {
		for _, l := range core.Loans.Items {
			cur = max(cur, l.CurrentOverdueDays)
		}
	}
	return cur
}

// declaredRevenue — выручка года year (₽, без НДС) и предыдущего: декларации, иначе БФО.
func declaredRevenue(tax taxReports, f fns, year int) (int64, string, int64) {
	if tax.Report != nil {
		byYear := map[int]float64{}
		for _, d := range tax.Report.Documents {
			y, err := strconv.Atoi(d.Period)
			if err != nil {
				continue
			}
			switch d.Type {
			case "PROFIT":
				byYear[y] += d.Data["revenue_from_sales"]
			case "USN":
				byYear[y] += d.Data["income"]
			case "NPD_INCOME":
				byYear[y] += d.Data["income"]
			}
		}
		if byYear[year] > 0 {
			return rub(byYear[year]), "TAX_REPORTS", rub(byYear[year-1])
		}
	}
	for _, r := range f.BOBFO {
		if r.Period != strconv.Itoa(year) || len(r.TypeCorrections) == 0 {
			continue
		}
		fr := r.TypeCorrections[len(r.TypeCorrections)-1].Correction.FinancialResult
		return rub(fr["current2110"] * 1000), "FNS_BFO", rub(fr["previous2110"] * 1000)
	}
	return 0, "", 0
}

// overdueTaxes — просрочка по налогам: долг на ЕНС, а без согласия — признак задолженности ФНС.
func overdueTaxes(tax taxReports, hasTax bool, f fns) *int64 {
	if hasTax {
		for _, d := range tax.Report.Documents {
			if d.Type == "ENS_BALANCE" {
				return ptr(rub(d.Data["debt"] + d.Data["penalties"] + d.Data["fines"]))
			}
		}
	}
	if f.PB != nil && f.PB.Vyp.TaxDebt == 0 {
		return ptr(int64(0))
	}
	return nil
}

func largestBuyer(core coreBanking) int64 {
	by := map[string]float64{}
	for _, t := range transactions(core) {
		if t.Direction == "CREDIT" && t.Counterparty.INN != "" && !reNonOperatingCredit.MatchString(lower(t.Purpose)) {
			by[t.Counterparty.INN] += t.Amount
		}
	}
	var best float64
	for _, v := range by {
		best = max(best, v)
	}
	return rub(best)
}

// receiptsDropped — поступления последних трёх месяцев меньше половины первых трёх.
func receiptsDropped(h []MonthCash) bool {
	sum := func(ms []MonthCash) int64 {
		var s int64
		for _, m := range ms {
			s += *m.OperatingReceiptsRUB
		}
		return s
	}
	first, last := sum(h[:3]), sum(h[len(h)-3:])
	return first > 0 && last*2 < first
}

// authority — полномочия представителя: руководитель по ЕГРЮЛ («Прозрачный бизнес»), сам ИП
// или действующая МЧД на представителя.
func authority(app domain.Application, collect domain.CollectInput, f fns, power m4d) (bool, *bool) {
	var inEGRUL *bool
	if d := collect.Director; d != nil && f.PB != nil && len(f.PB.Vyp.Heads) > 0 { // у ИП руководителей в выписке нет
		inEGRUL = ptr(slices.ContainsFunc(f.PB.Vyp.Heads, func(h pbHead) bool { return h.INN == d.INN }))
	}
	switch app.Representative.Role {
	case domain.RoleDirector:
		return inEGRUL != nil && *inEGRUL, inEGRUL
	case domain.RoleOwner:
		if app.Company.Type == domain.CompanySoleProprietor {
			return true, inEGRUL
		}
	}
	rep := collect.RepresentativeINN
	if p := power.POA; p != nil && p.Status == "ACTIVE" && (rep == "" || p.Representative.INN == rep) {
		return true, inEGRUL
	}
	return false, inEGRUL
}
