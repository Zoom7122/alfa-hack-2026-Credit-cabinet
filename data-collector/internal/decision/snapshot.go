package decision

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

type SnapshotDocument struct {
	ID               string          `json:"id"`
	SHA256           string          `json:"sha256"`
	Generation       int             `json:"generation"`
	SignatureState   string          `json:"signature_state"`
	SignatureVerdict *string         `json:"signature_verdict"`
	IntegrityStatus  *string         `json:"integrity_status"`
	SignatureReport  json.RawMessage `json:"signature_report"`
	FinancialStatus  *string         `json:"financial_status"`
	FinancialResult  *struct {
		Diagnostics       json.RawMessage `json:"diagnostics"`
		Signature         json.RawMessage `json:"signature"`
		DocumentSHA256    string          `json:"document_sha256"`
		ScoringIndicators []struct {
			Name       string          `json:"name"`
			Period     string          `json:"period"`
			PeriodKind string          `json:"period_kind"`
			Amount     string          `json:"amount"`
			Basis      string          `json:"basis"`
			Sources    json.RawMessage `json:"sources"`
			Eligible   bool            `json:"eligible"`
		} `json:"scoring_indicators"`
	} `json:"financial_result"`
}

// ExactStatements is the snapshot adapter. Unlike the legacy supplier it never invents zero lines.
func ExactStatements(raw json.RawMessage, borrower string, years []int) (Statements, error) {
	out := Statements{BorrowerID: borrower, AnnualReports: []AnnualReport{}}
	var data struct {
		Forms []struct {
			Period      string `json:"period"`
			Corrections []struct {
				Correction struct {
					Balance map[string]json.Number `json:"balance"`
					Profit  map[string]json.Number `json:"financialResult"`
				} `json:"correction"`
			} `json:"typeCorrections"`
		} `json:"bo_bfo"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &data); err != nil {
			return out, err
		}
	}
	forms := map[int]map[string]json.Number{}
	for _, form := range data.Forms {
		year, err := strconv.Atoi(form.Period)
		if err != nil || len(form.Corrections) == 0 {
			continue
		}
		c := form.Corrections[len(form.Corrections)-1].Correction
		lines := map[string]json.Number{}
		for k, v := range c.Balance {
			lines[k] = v
		}
		for k, v := range c.Profit {
			lines[k] = v
		}
		forms[year] = lines
	}
	for _, year := range years {
		lines, prefix := forms[year], "current"
		if lines == nil {
			lines, prefix = forms[year+1], "previous"
		}
		if lines == nil {
			continue
		}
		hasPeriod := false
		for key := range lines {
			if strings.HasPrefix(key, prefix) {
				hasPeriod = true
				break
			}
		}
		if !hasPeriod {
			continue
		}
		report := emptyReport(year)
		for code, target := range statementTargets(&report) {
			n, exists := lines[prefix+code]
			if !exists {
				continue
			}
			rat, ok := new(big.Rat).SetString(string(n))
			if !ok {
				continue
			}
			rat.Mul(rat, big.NewRat(1000, 1))
			if rat.IsInt() && rat.Num().IsInt64() {
				*target = ptr(rat.Num().Int64())
			}
		}
		out.AnnualReports = append(out.AnnualReports, report)
	}
	return out, nil
}
func emptyReport(year int) AnnualReport {
	return AnnualReport{Year: ptr(year), PeriodEnd: ptr(fmt.Sprintf("%04d-12-31", year)), Balance: &Balance{Unit: ptr("RUB")}, ProfitAndLoss: &ProfitAndLoss{Unit: ptr("RUB")}}
}
func statementTargets(r *AnnualReport) map[string]**int64 {
	b, p := r.Balance, r.ProfitAndLoss
	return map[string]**int64{"1110": &b.IntangibleAssets, "1120": &b.ResearchAndDevelopment, "1130": &b.IntangibleExplorationAssets, "1140": &b.TangibleExplorationAssets, "1150": &b.FixedAssets, "1160": &b.IncomeBearingTangibleInvestments, "1170": &b.LongTermFinancialInvestments, "1180": &b.DeferredTaxAssets, "1190": &b.OtherNonCurrentAssets, "1100": &b.NonCurrentAssets, "1210": &b.Inventories, "1220": &b.VATOnAcquiredAssets, "1230": &b.Receivables, "1240": &b.ShortTermFinancialInvest, "1250": &b.CashAndEquivalents, "1260": &b.OtherCurrentAssets, "1200": &b.CurrentAssets, "1600": &b.TotalAssets, "1310": &b.CharterCapital, "1320": &b.TreasuryShares, "1340": &b.RevaluationReserve, "1350": &b.AdditionalCapital, "1360": &b.ReserveCapital, "1370": &b.RetainedEarnings, "1300": &b.Equity, "1410": &b.LongTermBorrowings, "1420": &b.DeferredTaxLiabilities, "1430": &b.LongTermProvisions, "1450": &b.OtherLongTermLiabilities, "1400": &b.LongTermLiabilities, "1510": &b.ShortTermBorrowings, "1520": &b.Payables, "1530": &b.DeferredIncome, "1540": &b.ShortTermProvisions, "1550": &b.OtherShortTermLiabilities, "1500": &b.CurrentLiabilities, "1700": &b.TotalEquityAndLiabilities, "2110": &p.Revenue, "2120": &p.CostOfSales, "2100": &p.GrossProfit, "2210": &p.SellingExpenses, "2220": &p.AdministrativeExpenses, "2200": &p.SalesProfit, "2310": &p.ParticipationIncome, "2320": &p.InterestReceivable, "2330": &p.InterestPayable, "2340": &p.OtherIncome, "2350": &p.OtherExpenses, "2300": &p.ProfitBeforeTax, "2410": &p.IncomeTax, "2400": &p.NetProfit}
}

var indicatorCodes = map[string]string{"accounting_revenue": "2110", "net_profit": "2400", "total_assets": "1600", "equity": "1300", "long_term_liabilities": "1400", "short_term_liabilities": "1500"}

// BuildSnapshot uses only admitted, hash-bound indicators of the current document generation.
func BuildSnapshot(in Input, id, collection string, docs []SnapshotDocument) (Request, Facts, error) {
	statements, err := ExactStatements(in.Systems["FNS"], in.Application.Company.ID, []int{in.Now.Year() - 2, in.Now.Year() - 1})
	if err != nil {
		return Request{}, Facts{}, err
	}
	snapshot := &InputSnapshot{ID: id, CollectionID: collection, EvaluationDate: in.Now.Format("2006-01-02"), Statements: &statements, Indicators: []FinancialIndicator{}, Diagnostics: []InputDiagnostic{}}
	snapshot.Documents, err = json.Marshal(docs)
	if err != nil {
		return Request{}, Facts{}, err
	}
	seen := map[string]string{}
	conflicts := map[string]bool{}
	in.ForgedDocument = false
	for _, doc := range docs {
		if documentForged(doc) {
			in.ForgedDocument = true
		}
		if doc.FinancialStatus != nil && *doc.FinancialStatus == "FAILED" || doc.SignatureState == "FAILED" || doc.SignatureVerdict != nil && (*doc.SignatureVerdict == "INVALID" || *doc.SignatureVerdict == "INDETERMINATE") {
			snapshot.Diagnostics = append(snapshot.Diagnostics, InputDiagnostic{Code: "DOCUMENT_UNAVAILABLE", Message: "Документ не допущен к скорингу", Fields: []string{"documents." + doc.ID}})
		}
		result := doc.FinancialResult
		if result == nil || result.DocumentSHA256 != doc.SHA256 {
			continue
		}
		for _, item := range result.ScoringIndicators {
			if !item.Eligible {
				continue
			}
			value := FinancialIndicator{Name: item.Name, Period: item.Period, PeriodKind: item.PeriodKind, Amount: item.Amount, Basis: item.Basis, DocumentID: doc.ID, Generation: doc.Generation, Sources: item.Sources}
			snapshot.Indicators = append(snapshot.Indicators, value)
			key := item.Name + ":" + item.Period
			amount, ok := new(big.Rat).SetString(item.Amount)
			if !ok {
				return Request{}, Facts{}, fmt.Errorf("invalid admitted indicator amount")
			}
			code, mapped := indicatorCodes[item.Name]
			var target **int64
			if mapped && item.PeriodKind == "YEAR" {
				year, err := strconv.Atoi(item.Period)
				if err != nil {
					continue
				}
				if year < in.Now.Year()-2 || year > in.Now.Year()-1 {
					continue
				}
				idx := -1
				for i, r := range statements.AnnualReports {
					if r.Year != nil && *r.Year == year {
						idx = i
						break
					}
				}
				if idx < 0 {
					statements.AnnualReports = append(statements.AnnualReports, emptyReport(year))
					idx = len(statements.AnnualReports) - 1
				}
				target = statementTargets(&statements.AnnualReports[idx])[code]
			}
			prior, exists := seen[key]
			if !exists && target != nil && *target != nil {
				prior = strconv.FormatInt(**target, 10)
				exists = true
			}
			if exists {
				old, _ := new(big.Rat).SetString(prior)
				if old.Cmp(amount) != 0 {
					vals, _ := json.Marshal([]string{prior, item.Amount})
					snapshot.Diagnostics = append(snapshot.Diagnostics, InputDiagnostic{Code: "FINANCIAL_DATA_CONFLICT", Message: "Уточните расхождение финансовых данных за " + item.Period, Fields: []string{"service_b.annual_reports." + item.Period, "documents." + doc.ID}, Conflict: true, Values: vals})
					conflicts[key] = true
					if target != nil {
						*target = nil
					}
				}
			}
			if !exists {
				seen[key] = item.Amount
			} else {
				seen[key] = prior
			}
			if target != nil && !conflicts[key] {
				if amount.IsInt() && amount.Num().IsInt64() {
					*target = ptr(amount.Num().Int64())
				} else {
					*target = nil
					snapshot.Diagnostics = append(snapshot.Diagnostics, InputDiagnostic{Code: "UNREPRESENTABLE_AMOUNT", Message: "Сумма требует уточнения для контракта в целых рублях", Fields: []string{"service_b.annual_reports." + item.Period}, Conflict: true})
				}
			}
		}
	}
	in.Statements = &statements
	req, facts := Build(in)
	req.InputSnapshot = snapshot
	version := "legal-v2"
	if req.Borrower.Type == "SOLE_PROPRIETOR" {
		version = "ip-v2"
	}
	req.ModelVersion = &version
	return req, facts, nil
}

// The document list is read in one statement and pins current financial reports.
// Never combine a historical integrity flag with the current generation's values.
func documentForged(d SnapshotDocument) bool {
	if d.IntegrityStatus != nil && *d.IntegrityStatus == "INVALID" {
		return true
	}
	if d.FinancialResult == nil {
		return false
	}
	var diagnostics []struct {
		Code string `json:"code"`
	}
	json.Unmarshal(d.FinancialResult.Diagnostics, &diagnostics)
	for _, item := range diagnostics {
		if item.Code == "STORED_HASH_MISMATCH" {
			return true
		}
	}
	var report struct {
		DocumentSHA256 string `json:"document_sha256"`
		Signatures     []struct {
			Checks []struct {
				Code   string `json:"code"`
				Status string `json:"status"`
			} `json:"checks"`
		} `json:"signatures"`
	}
	if json.Unmarshal(d.FinancialResult.Signature, &report) != nil || report.DocumentSHA256 != d.SHA256 {
		return false
	}
	for _, sig := range report.Signatures {
		for _, check := range sig.Checks {
			if (check.Code == "INTEGRITY" || check.Code == "DOCUMENT_NOT_MODIFIED") && check.Status == "FAILED" {
				return true
			}
		}
	}
	return false
}
