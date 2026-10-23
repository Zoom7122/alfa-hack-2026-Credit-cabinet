package main

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

// Сквозные проверки моков второй и третьей очереди со старыми (docs/mock-apis-wave-2-3.md).

func taxDocs(t *testing.T, r *gin.Engine, inn string, docs ...string) []struct {
	Type   string             `json:"type"`
	Period string             `json:"period"`
	Data   map[string]float64 `json:"data"`
} {
	t.Helper()
	created := mt.Decode[struct {
		RequestID string `json:"request_id"`
	}](t, mt.PostJSON(t, r, "/tax-reports/v1/requests", map[string]any{"inn": inn, "documents": docs,
		"consent": map[string]any{"id": "c", "valid_until": "2027-04-21", "scope": []string{"DECLARATIONS", "ENS_BALANCE"}}}),
		http.StatusAccepted)
	return mt.Decode[struct {
		Documents []struct {
			Type   string             `json:"type"`
			Period string             `json:"period"`
			Data   map[string]float64 `json:"data"`
		} `json:"documents"`
	}](t, mt.Get(r, "/tax-reports/v1/requests/"+created.RequestID), http.StatusOK).Documents
}

type girbo []struct {
	Period          string `json:"period"`
	Actives         int    `json:"actives"`
	GainSum         int    `json:"gainSum"`
	TypeCorrections []struct {
		Correction struct {
			FinancialResult map[string]int `json:"financialResult"`
		} `json:"correction"`
	} `json:"typeCorrections"`
}

func girboReports(t *testing.T, r *gin.Engine, inn string) girbo {
	t.Helper()
	s := mt.Decode[struct {
		Content []struct {
			ID int64 `json:"id"`
		} `json:"content"`
	}](t, mt.Get(r, "/fns/bo/advanced-search/organizations/search?query="+inn), http.StatusOK)
	return mt.Decode[girbo](t, mt.Get(r, "/fns/bo/nbo/organizations/"+strconv.FormatInt(s.Content[0].ID, 10)+"/bfo/"),
		http.StatusOK)
}

// Налоговая отчётность сходится с ГИР БО (выручка, налог) и с картотекой C (долг ЕНС = инкассо).
func TestTaxReportsAcrossServices(t *testing.T) {
	r := testRouter()
	for _, inn := range []string{mt.InnA, mt.InnB, mt.InnC, mt.InnE} {
		reports := girboReports(t, r, inn)
		for _, d := range taxDocs(t, r, inn, "PROFIT") {
			for _, rep := range reports {
				if rep.Period != d.Period {
					continue
				}
				if d.Data["revenue_from_sales"] != float64(rep.GainSum)*1000 {
					t.Errorf("%s %s: revenue %.0f vs GIR BO %d тыс", inn, d.Period, d.Data["revenue_from_sales"], rep.GainSum)
				}
				fin := rep.TypeCorrections[0].Correction.FinancialResult
				if fin["current2300"] > 0 && d.Data["tax_amount"] != float64(fin["current2410"])*1000 {
					t.Errorf("%s %s: tax %.0f vs 2410 %d тыс", inn, d.Period, d.Data["tax_amount"], fin["current2410"])
				}
			}
		}
	}
	card := mt.Decode[struct {
		CardFile struct {
			TotalAmount float64 `json:"total_amount"`
		} `json:"card_file"`
	}](t, mt.Get(r, "/core-banking/v1/clients/"+mt.InnC+"/restrictions"), http.StatusOK)
	ens := taxDocs(t, r, mt.InnC, "ENS_BALANCE")[0].Data
	if -ens["balance"] != card.CardFile.TotalAmount || ens["debt"]+ens["penalties"] != card.CardFile.TotalAmount {
		t.Errorf("C: ENS %+v vs card file %.2f", ens, card.CardFile.TotalAmount)
	}
}

// D: выплаты WB на счёт = поступления в Core Banking; доход НПД = продажи на площадках = доход в Госуслугах.
func TestMarketplaceAcrossServices(t *testing.T) {
	r := testRouter()
	tok := []string{"X-Seller-Token", "t"}
	payouts := mt.Decode[struct {
		Items []struct {
			Date        string  `json:"date"`
			Amount      float64 `json:"amount"`
			Destination struct {
				Type    string `json:"type"`
				Account string `json:"account"`
			} `json:"destination"`
		} `json:"items"`
	}](t, mt.Get(r, "/marketplaces/v1/wildberries/sellers/"+mt.InnD+"/payouts", tok...), http.StatusOK).Items
	client := mt.Decode[struct {
		Accounts []struct {
			Number string `json:"account_number"`
		} `json:"accounts"`
	}](t, mt.Get(r, "/core-banking/v1/clients/"+mt.InnD), http.StatusOK)
	s := load(t, r, mt.InnD)
	toAccount := 0
	for _, p := range payouts {
		if p.Destination.Type != "ACCOUNT" {
			continue
		}
		toAccount++
		if p.Destination.Account != client.Accounts[0].Number {
			t.Errorf("payout %s to %s, not to the client account", p.Date, p.Destination.Account)
		}
		found := false
		for _, tx := range s.txs {
			found = found || (tx.Direction == "CREDIT" && tx.Amount == p.Amount && tx.Counterparty.Name == `ООО "ВАЙЛДБЕРРИЗ"`)
		}
		if !found {
			t.Errorf("payout %s %.2f not in core banking", p.Date, p.Amount)
		}
	}
	credits := 0
	for _, tx := range s.txs {
		if tx.Direction == "CREDIT" {
			credits++
		}
	}
	if toAccount == 0 || toAccount != credits {
		t.Errorf("payouts to account %d, credits in core banking %d", toAccount, credits)
	}

	// доход НПД по годам
	net := map[string]float64{}
	for _, mp := range []string{"wildberries", "ozon"} {
		months := mt.Decode[struct {
			Months []struct {
				Month         string  `json:"month"`
				GrossSales    float64 `json:"gross_sales"`
				ReturnsAmount float64 `json:"returns_amount"`
			} `json:"months"`
		}](t, mt.Get(r, "/marketplaces/v1/"+mp+"/sellers/"+mt.InnD+"/sales/monthly?months=36", tok...), http.StatusOK).Months
		for _, m := range months {
			net[m.Month[:4]] += m.GrossSales - m.ReturnsAmount
		}
	}
	npd := map[string]float64{}
	for _, d := range taxDocs(t, r, mt.InnD, "NPD_INCOME") {
		npd[d.Period] = d.Data["income"]
	}
	gu := gosuslugiIncome(t, r, mt.InnD)
	for _, year := range []string{"2024", "2025"} {
		if d := net[year] - npd[year]; d > 0.01 || d < -0.01 || gu[year] != npd[year] {
			t.Errorf("%s: marketplaces %.2f, NPD %.2f, Gosuslugi %.2f", year, net[year], npd[year], gu[year])
		}
	}
}

func gosuslugiIncome(t *testing.T, r *gin.Engine, inn string) map[string]float64 {
	t.Helper()
	created := mt.Decode[struct {
		RequestID string `json:"request_id"`
	}](t, mt.PostJSON(t, r, "/gosuslugi/v1/profile-requests", map[string]any{"person": map[string]any{"inn": inn},
		"scopes": []string{"INCOME"}}), http.StatusAccepted)
	p := mt.Decode[struct {
		Data struct {
			Income []struct {
				Year    string `json:"year"`
				Sources []struct {
					Amount float64 `json:"amount"`
				} `json:"sources"`
			} `json:"income"`
		} `json:"data"`
	}](t, mt.Get(r, "/gosuslugi/v1/profile-requests/"+created.RequestID), http.StatusOK)
	out := map[string]float64{}
	for _, y := range p.Data.Income {
		for _, s := range y.Sources {
			out[y.Year] += s.Amount
		}
	}
	return out
}

// A: безнал по чекам ОФД = эквайринг в Core Banking.
func TestOFDAcrossServices(t *testing.T) {
	r := testRouter()
	ofd := mt.Decode[struct {
		Months []struct {
			Month       string  `json:"month"`
			RevenueCard float64 `json:"revenue_card"`
		} `json:"months"`
	}](t, mt.Get(r, "/ofd/v1/organizations/"+mt.InnA+"/receipts/monthly", "X-OFD-Token", "t"), http.StatusOK).Months
	turn := mt.Decode[struct {
		Months []struct {
			Month     string  `json:"month"`
			Acquiring float64 `json:"acquiring_turnover"`
		} `json:"months"`
	}](t, mt.Get(r, "/core-banking/v1/clients/"+mt.InnA+"/turnover"), http.StatusOK).Months
	if len(ofd) != len(turn) {
		t.Fatalf("months: ofd %d, core banking %d", len(ofd), len(turn))
	}
	for i := range ofd {
		if ofd[i].Month != turn[i].Month || ofd[i].RevenueCard != turn[i].Acquiring {
			t.Errorf("%s: ofd card %.2f, acquiring %.2f", ofd[i].Month, ofd[i].RevenueCard, turn[i].Acquiring)
		}
	}
}

// Залогодержатели и ипотека — кредиторы из БКИ; у кредитов стоит признак залога.
func TestCollateralAcrossServices(t *testing.T) {
	r := testRouter()
	for _, inn := range []string{mt.InnB, mt.InnC} {
		s := load(t, r, inn)
		secured := map[string]bool{}
		for _, a := range s.bki.Accounts {
			if a.Status != "CLOSED" {
				secured[a.CreditorName] = true
			}
		}
		pl := mt.Decode[struct {
			Items []struct {
				Pledgees []struct {
					Name string `json:"name"`
				} `json:"pledgees"`
			} `json:"items"`
		}](t, mt.Get(r, "/pledge-registry/v1/notifications?pledgor_inn="+inn), http.StatusOK)
		if len(pl.Items) == 0 {
			t.Errorf("%s: no pledges", inn)
		}
		for _, n := range pl.Items {
			if !secured[n.Pledgees[0].Name] {
				t.Errorf("%s: pledgee %s has no active loan in BKI", inn, n.Pledgees[0].Name)
			}
		}
	}
	ex := mt.Decode[struct {
		Encumbrances []struct {
			InFavorOf struct {
				Name string `json:"name"`
			} `json:"in_favor_of"`
		} `json:"encumbrances"`
	}](t, mt.PostJSON(t, r, "/egrn/v1/extracts", map[string]any{"cadastral_number": "50:21:0110203:845"}), http.StatusOK)
	b := load(t, r, mt.InnB)
	found := false
	for _, a := range b.bki.Accounts {
		found = found || (a.Status != "CLOSED" && a.CreditorName == ex.Encumbrances[0].InFavorOf.Name)
	}
	if !found {
		t.Errorf("mortgagee %s has no active loan of B in BKI", ex.Encumbrances[0].InFavorOf.Name)
	}
}

// A: «переводы собственных средств» из Альфы приходят на её счёт в Банке Горизонт.
func TestOpenBankingAcrossServices(t *testing.T) {
	r := testRouter()
	hdr := []string{"X-Customer-Inn", mt.InnA, "X-Bank-Bic", "044525225"}
	acc := mt.Decode[struct {
		Data struct {
			Account []struct {
				AccountID      string `json:"accountId"`
				AccountDetails []struct {
					Identification string `json:"identification"`
				} `json:"AccountDetails"`
			} `json:"Account"`
		} `json:"Data"`
	}](t, mt.Get(r, "/open-banking/v1/accounts", hdr...), http.StatusOK).Data.Account[0]
	ob := mt.Decode[struct {
		Data struct {
			Transaction []struct {
				BookingDateTime string `json:"bookingDateTime"`
				Indicator       string `json:"creditDebitIndicator"`
				Amount          struct {
					Amount string `json:"amount"`
				} `json:"Amount"`
				Info string `json:"transactionInformation"`
			} `json:"Transaction"`
		} `json:"Data"`
	}](t, mt.Get(r, "/open-banking/v1/accounts/"+acc.AccountID+"/transactions", hdr...), http.StatusOK).Data.Transaction

	core := mt.Decode[struct {
		Items []struct {
			Date         string  `json:"date"`
			Direction    string  `json:"direction"`
			Amount       float64 `json:"amount"`
			Purpose      string  `json:"purpose"`
			Counterparty struct {
				Account string `json:"account"`
			} `json:"counterparty"`
		} `json:"items"`
	}](t, mt.Get(r, "/core-banking/v1/clients/"+mt.InnA+"/transactions?size=500"), http.StatusOK).Items
	n := 0
	for _, tx := range core {
		if tx.Direction != "DEBIT" || tx.Purpose != "Перевод собственных средств" {
			continue
		}
		n++
		if tx.Counterparty.Account != acc.AccountDetails[0].Identification {
			t.Errorf("%s: self-transfer to %s, open banking account %s", tx.Date, tx.Counterparty.Account,
				acc.AccountDetails[0].Identification)
		}
		found := false
		for _, o := range ob {
			found = found || (o.Indicator == "Credit" && o.BookingDateTime[:10] == tx.Date &&
				o.Amount.Amount == strconv.FormatFloat(tx.Amount, 'f', 2, 64) && o.Info == tx.Purpose)
		}
		if !found {
			t.Errorf("%s: self-transfer %.2f not in open banking", tx.Date, tx.Amount)
		}
	}
	if n == 0 {
		t.Error("A has no self-transfers in core banking")
	}
}

// Госконтракты: оплаты заказчиков есть в выписке; контракт из РНП — расторгнутый контракт C.
func TestZakupkiAcrossServices(t *testing.T) {
	r := testRouter()
	type contracts struct {
		Items []struct {
			RegNumber  string  `json:"reg_number"`
			Status     string  `json:"status"`
			PaidAmount float64 `json:"paid_amount"`
			Customer   struct {
				Name string `json:"name"`
			} `json:"customer"`
			SignedAt string `json:"signed_at"`
		} `json:"items"`
	}
	for _, inn := range []string{mt.InnC, mt.InnE} {
		cs := mt.Decode[contracts](t, mt.Get(r, "/zakupki/v1/contracts?supplier_inn="+inn), http.StatusOK)
		s := load(t, r, inn)
		for _, c := range cs.Items {
			if c.SignedAt < "2025-10-01" { // раньше окна выписки
				continue
			}
			var paid float64
			for _, tx := range s.txs {
				if tx.Direction == "CREDIT" && tx.Counterparty.Name == c.Customer.Name {
					paid += tx.Amount
				}
			}
			if paid != c.PaidAmount {
				t.Errorf("%s %s: paid %.2f, in core banking %.2f", inn, c.RegNumber, c.PaidAmount, paid)
			}
		}
	}
	rnp := mt.Decode[struct {
		Entries []struct {
			ContractRegNumber string `json:"contract_reg_number"`
		} `json:"entries"`
	}](t, mt.Get(r, "/zakupki/v1/rnp?inn="+mt.InnC), http.StatusOK)
	cs := mt.Decode[contracts](t, mt.Get(r, "/zakupki/v1/contracts?supplier_inn="+mt.InnC), http.StatusOK)
	if len(rnp.Entries) != 1 || len(cs.Items) != 1 || cs.Items[0].RegNumber != rnp.Entries[0].ContractRegNumber ||
		cs.Items[0].Status != "TERMINATED" {
		t.Errorf("C: rnp %+v, contracts %+v", rnp, cs)
	}
}

// Устав, реестр МСП, МЧД и Госуслуги сходятся с ГИР БО, АБС и ЕГРЮЛ.
func TestRegistriesAcrossServices(t *testing.T) {
	r := testRouter()
	for _, inn := range mt.Companies {
		client := mt.Decode[struct {
			Segment  string `json:"segment"`
			Products struct {
				Employees int `json:"payroll_employees"`
			} `json:"products"`
		}](t, mt.Get(r, "/core-banking/v1/clients/"+inn), http.StatusOK)
		rmsp := mt.Decode[struct {
			Category  string `json:"category"`
			Employees int    `json:"employees"`
		}](t, mt.Get(r, "/fns/rmsp/v1/"+inn), http.StatusOK)
		if rmsp.Category != client.Segment || rmsp.Employees != client.Products.Employees {
			t.Errorf("%s: rmsp %+v, core banking %+v", inn, rmsp, client)
		}
		if inn == mt.InnD {
			continue
		}
		ch := mt.Decode[struct {
			Major struct {
				Assets float64 `json:"assets_at_last_reporting_date"`
			} `json:"major_transaction"`
			Participants []struct {
				INN string `json:"inn"`
			} `json:"participants"`
		}](t, mt.Get(r, "/fns/puchdoc/v1/"+inn), http.StatusOK)
		if bo := girboReports(t, r, inn); ch.Major.Assets != float64(bo[0].Actives)*1000 {
			t.Errorf("%s: charter assets %.0f, GIR BO %d тыс", inn, ch.Major.Assets, bo[0].Actives)
		}
		pb := mt.Decode[struct {
			Vyp struct {
				Masruk []struct {
					INN string `json:"inn"`
				} `json:"masruk"`
			} `json:"vyp"`
		}](t, mt.PostForm(r, "/fns/pb/company-proc.json", url.Values{"method": {"get-response"},
			"token": {"MOCK-PB-" + inn}, "id": {"MOCK-PB-" + inn}}), http.StatusOK)
		director := pb.Vyp.Masruk[0].INN
		if ch.Participants[0].INN != director {
			t.Errorf("%s: first participant %s, director %s", inn, ch.Participants[0].INN, director)
		}
		created := mt.Decode[struct {
			RequestID string `json:"request_id"`
		}](t, mt.PostJSON(t, r, "/gosuslugi/v1/profile-requests", map[string]any{"person": map[string]any{"inn": director},
			"scopes": []string{"EMPLOYMENT"}}), http.StatusAccepted)
		emp := mt.Decode[struct {
			Data struct {
				Employment []struct {
					EmployerINN string `json:"employer_inn"`
				} `json:"employment"`
			} `json:"data"`
		}](t, mt.Get(r, "/gosuslugi/v1/profile-requests/"+created.RequestID), http.StatusOK)
		if len(emp.Data.Employment) != 1 || emp.Data.Employment[0].EmployerINN != inn {
			t.Errorf("%s: director employment %+v", inn, emp.Data)
		}
	}

	poa := mt.Decode[struct {
		Principal struct {
			INN      string `json:"inn"`
			SignedBy struct {
				INN string `json:"inn"`
			} `json:"signed_by"`
		} `json:"principal"`
	}](t, mt.Get(r, "/m4d/v1/poa/0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37"), http.StatusOK)
	if poa.Principal.INN != mt.InnA || poa.Principal.SignedBy.INN != mt.DirA {
		t.Errorf("POA: %+v", poa.Principal)
	}
}
