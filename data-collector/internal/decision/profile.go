package decision

import (
	"encoding/json"
	"time"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// CompanyFacts — сводка собранных данных о компании для досье андеррайтера.
type CompanyFacts struct {
	OKVED               string         `json:"okved,omitempty"`
	FinancialStatements []YearFigures  `json:"financial_statements"`
	CreditHistory       map[string]any `json:"credit_history,omitempty"`
	Enforcement         map[string]any `json:"enforcement,omitempty"`
	Litigation          map[string]any `json:"litigation,omitempty"`
	BankRelationship    map[string]any `json:"bank_relationship,omitempty"`
	RiskFlags           []string       `json:"risk_flags"`
}

// YearFigures — главные строки бухотчётности за год, ₽.
type YearFigures struct {
	Year        int   `json:"year"`
	Revenue     int64 `json:"revenue"`
	NetProfit   int64 `json:"net_profit"`
	TotalAssets int64 `json:"total_assets"`
	Equity      int64 `json:"equity"`
}

// Profile — сводка по сырым ответам систем последнего сбора; now — дата оценки (годы отчётности).
func Profile(systems map[domain.System]json.RawMessage, companyID string, now time.Time) CompanyFacts {
	out := CompanyFacts{FinancialStatements: []YearFigures{}, RiskFlags: []string{}}
	var (
		f    fns
		core coreBanking
		b    bki
		fs   fssp
		k    kad
		fed  fedresurs
		comp compliance
	)
	decode(systems[domain.SystemFNS], &f)
	hasCore := decode(systems[domain.SystemCoreBanking], &core)
	decode(systems[domain.SystemBKI], &b)
	decode(systems[domain.SystemFSSP], &fs)
	decode(systems[domain.SystemKAD], &k)
	decode(systems[domain.SystemFedresurs], &fed)
	decode(systems[domain.SystemCompliance], &comp)

	if f.PB != nil {
		out.OKVED = f.PB.Vyp.OKVED
		if f.PB.Vyp.Invalid != 0 {
			out.RiskFlags = append(out.RiskFlags, "Недостоверные сведения в ЕГРЮЛ")
		}
		if f.PB.Liquidated {
			out.RiskFlags = append(out.RiskFlags, "Компания ликвидирована")
		}
	}
	st, _ := StatementsFromFNS(systems[domain.SystemFNS], companyID, []int{now.Year() - 2, now.Year() - 1})
	for _, r := range st.AnnualReports {
		const thousand = 1000 // ГИР БО — в тыс. руб.
		out.FinancialStatements = append(out.FinancialStatements, YearFigures{Year: *r.Year, Revenue: *r.ProfitAndLoss.Revenue * thousand,
			NetProfit: *r.ProfitAndLoss.NetProfit * thousand, TotalAssets: *r.Balance.TotalAssets * thousand, Equity: *r.Balance.Equity * thousand})
	}

	report := b.Legal
	if report == nil {
		report = b.Person
	}
	if report != nil {
		active := 0
		for _, a := range report.Accounts {
			if a.Status == "ACTIVE" {
				active++
			}
		}
		out.CreditHistory = map[string]any{"has_history": report.HasHistory, "active_accounts": active,
			"total_outstanding": report.Summary.TotalOutstanding, "max_overdue_days_ever": report.Summary.MaxOverdueEver,
			"overdue_30p_24m": report.Summary.Overdue30P24M, "overdue_90p_24m": report.Summary.Overdue90P24M,
			"inquiries_90d": report.Summary.Inquiries90D}
		if report.Summary.Overdue90P24M > 0 {
			out.RiskFlags = append(out.RiskFlags, "Просрочка 90+ дней за 24 месяца")
		}
	}
	if fs.Company != nil {
		out.Enforcement = map[string]any{"active_amount": fs.Company.Summary.ActiveAmount}
	}
	if len(k.Cases) > 0 {
		s := k.Cases[0].Summary
		out.Litigation = map[string]any{"defendant_active_amount": s.DefendantActiveAmount, "bankruptcy_as_debtor": s.Bankruptcy}
		if s.Bankruptcy {
			out.RiskFlags = append(out.RiskFlags, "Дело о банкротстве")
		}
	}
	if fed.CompanyMessages != nil {
		if stage := fed.CompanyMessages.Summary.BankruptcyStage; stage != "" && stage != "NONE" {
			out.RiskFlags = append(out.RiskFlags, "Федресурс: банкротство ("+stage+")")
		}
	}
	if comp.Screening != nil {
		for _, r := range comp.Screening.Results {
			if r.Listed {
				out.RiskFlags = append(out.RiskFlags, "Совпадение в санкционных списках")
				break
			}
		}
	}
	if comp.ZSK != nil && comp.ZSK.RiskLevel == "HIGH" {
		out.RiskFlags = append(out.RiskFlags, "Высокий риск по ЗСК ЦБ")
	}
	if hasCore && core.Client != nil {
		rel := map[string]any{"client_since": core.Client.ClientSince}
		if core.Turnover != nil && len(core.Turnover.Months) > 0 {
			last := core.Turnover.Months[0]
			rel["last_month"] = last.Month
			rel["last_month_credit_turnover"] = last.CreditTurnover
			rel["last_month_avg_balance"] = last.AvgBalance
		}
		if r := core.Restrictions; r != nil {
			restricted := r.CardFile.HasUnpaidClaims || len(r.AccountBlocks) > 0 || len(r.Arrests) > 0
			rel["restrictions"] = restricted
			if restricted {
				out.RiskFlags = append(out.RiskFlags, "Картотека, блокировка или арест счёта")
			}
		}
		out.BankRelationship = rel
	}
	return out
}
