package main

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

// Сквозные проверки: одни и те же сущности сходятся между моками
// (на этом держатся сценарии A–E из docs/mock-apis.md).

const alfa = `АО "АЛЬФА-БАНК"`

func testRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return newRouter(time.Second)
}

type coreClient struct {
	INN  string `json:"inn"`
	OGRN string `json:"ogrn"`
	Name string `json:"name"`
}

type coreLoan struct {
	ContractID     string   `json:"contract_id"`
	Limit          float64  `json:"limit"`
	Outstanding    float64  `json:"outstanding"`
	MonthlyPayment *float64 `json:"monthly_payment"`
	OpenedAt       string   `json:"opened_at"`
	Status         string   `json:"status"`
}

type coreTx struct {
	Direction     string  `json:"direction"`
	Amount        float64 `json:"amount"`
	OperationCode string  `json:"operation_code"`
	Counterparty  struct {
		Name string `json:"name"`
	} `json:"counterparty"`
	Purpose string `json:"purpose"`
}

type bkiAccount struct {
	CreditorName   string   `json:"creditor_name"`
	Limit          float64  `json:"limit"`
	Outstanding    float64  `json:"outstanding"`
	MonthlyPayment *float64 `json:"monthly_payment"`
	OpenedAt       string   `json:"opened_at"`
	Status         string   `json:"status"`
}

type bkiReport struct {
	Subject struct {
		INN  string `json:"inn"`
		Name string `json:"name"`
	} `json:"subject"`
	HasHistory bool         `json:"has_history"`
	Accounts   []bkiAccount `json:"accounts"`
}

type scenario struct {
	inn    string
	client coreClient
	loans  []coreLoan
	txs    []coreTx
	bki    bkiReport
}

func load(t *testing.T, r http.Handler, inn string) scenario {
	t.Helper()
	s := scenario{inn: inn}
	s.client = mt.Decode[coreClient](t, mt.Get(r, "/core-banking/v1/clients/"+inn), http.StatusOK)
	s.loans = mt.Decode[struct {
		Items []coreLoan `json:"items"`
	}](t, mt.Get(r, "/core-banking/v1/clients/"+inn+"/loans"), http.StatusOK).Items
	s.txs = mt.Decode[struct {
		Total int      `json:"total"`
		Items []coreTx `json:"items"`
	}](t, mt.Get(r, "/core-banking/v1/clients/"+inn+"/transactions?size=500"), http.StatusOK).Items
	s.bki = mt.Decode[bkiReport](t, mt.PostJSON(t, r, "/bki/v1/reports", map[string]any{
		"subject_type": "LEGAL", "inn": inn,
		"consent": map[string]any{"id": "CNS-TEST", "valid_until": "2027-04-21"},
	}), http.StatusOK)
	return s
}

func (s scenario) hasDebit(amount float64, counterparty, purposePart string) bool {
	for _, tx := range s.txs {
		if tx.Direction == "DEBIT" && tx.Amount == amount && tx.Counterparty.Name == counterparty &&
			strings.Contains(tx.Purpose, purposePart) {
			return true
		}
	}
	return false
}

func TestIdentityAcrossServices(t *testing.T) {
	r := testRouter()
	for _, inn := range mt.Companies {
		t.Run(inn, func(t *testing.T) {
			s := load(t, r, inn)
			if s.client.INN != inn {
				t.Fatalf("core banking inn %s", s.client.INN)
			}

			tok := mt.Decode[struct {
				T string `json:"t"`
			}](t, mt.PostForm(r, "/fns/egrul/", url.Values{"query": {inn}}), http.StatusOK).T
			rows := mt.Decode[struct {
				Rows []struct {
					C, I, O, G string
				} `json:"rows"`
			}](t, mt.Get(r, "/fns/egrul/search-result/"+tok), http.StatusOK).Rows
			if len(rows) != 1 || rows[0].I != inn || rows[0].O != s.client.OGRN {
				t.Fatalf("egrul: %+v vs client %+v", rows, s.client)
			}

			pb := mt.Decode[struct {
				Vyp struct {
					INN    string `json:"ИНН"`
					OGRN   string `json:"ОГРН"`
					Name   string `json:"НаимЮЛСокр"`
					Masruk []struct {
						INN  string `json:"inn"`
						Name string `json:"name"`
					} `json:"masruk"`
				} `json:"vyp"`
			}](t, mt.PostForm(r, "/fns/pb/company-proc.json", url.Values{
				"method": {"get-response"}, "token": {"MOCK-PB-" + inn}, "id": {"MOCK-PB-" + inn}}), http.StatusOK)
			if pb.Vyp.INN != inn || pb.Vyp.OGRN != s.client.OGRN || pb.Vyp.Name != s.client.Name {
				t.Errorf("pb: %+v vs client %+v", pb.Vyp, s.client)
			}
			if s.bki.Subject.INN != inn || s.bki.Subject.Name != s.client.Name {
				t.Errorf("bki subject %+v vs client %+v", s.bki.Subject, s.client)
			}

			if inn != mt.InnD {
				// ЮЛ: краткое имя одинаково в ЕГРЮЛ; директор из ПБ есть в БКИ, ФССП и Федресурсе
				if rows[0].C != s.client.Name {
					t.Errorf("egrul c %q vs client %q", rows[0].C, s.client.Name)
				}
				if len(pb.Vyp.Masruk) != 1 {
					t.Fatalf("pb masruk: %+v", pb.Vyp.Masruk)
				}
				checkDirector(t, r, pb.Vyp.Masruk[0].INN, rows[0].G)
			}
		})
	}
}

func checkDirector(t *testing.T, r http.Handler, inn, egrulHead string) {
	t.Helper()
	rep := mt.Decode[bkiReport](t, mt.PostJSON(t, r, "/bki/v1/reports", map[string]any{
		"subject_type": "PERSON", "person": map[string]any{"inn": inn},
		"consent": map[string]any{"id": "CNS-TEST", "valid_until": "2027-04-21"},
	}), http.StatusOK)
	if !rep.HasHistory || !strings.HasSuffix(egrulHead, rep.Subject.Name) {
		t.Errorf("director %s: bki %+v, egrul %q", inn, rep.Subject, egrulHead)
	}
	fr := mt.Decode[struct {
		FullName *string `json:"full_name"`
	}](t, mt.Get(r, "/fedresurs/v1/persons/"+inn+"/bankruptcy"), http.StatusOK)
	if fr.FullName == nil || *fr.FullName != rep.Subject.Name {
		t.Errorf("director %s: fedresurs full_name %v, bki %q", inn, fr.FullName, rep.Subject.Name)
	}
	fs := mt.Decode[struct {
		Debtor struct {
			INN string `json:"inn"`
		} `json:"debtor"`
	}](t, mt.Get(r, "/fssp/v1/proceedings?inn="+inn), http.StatusOK)
	if fs.Debtor.INN != inn {
		t.Errorf("director %s: fssp debtor %+v", inn, fs.Debtor)
	}
}

// Кредиты нашего банка видны в БКИ как АЛЬФА-БАНК теми же суммами (их нельзя считать дважды),
// а кредиты в других банках — в выписке платежами с той же суммой.
func TestLoansAcrossServices(t *testing.T) {
	r := testRouter()
	for _, inn := range mt.Companies {
		t.Run(inn, func(t *testing.T) {
			s := load(t, r, inn)
			var ours []bkiAccount
			for _, a := range s.bki.Accounts {
				if a.Status == "CLOSED" {
					continue
				}
				if a.CreditorName == alfa {
					ours = append(ours, a)
					continue
				}
				if a.MonthlyPayment != nil && !s.hasDebit(*a.MonthlyPayment, a.CreditorName, "") {
					t.Errorf("no payment %.2f to %s in transactions", *a.MonthlyPayment, a.CreditorName)
				}
			}
			if len(ours) != len(s.loans) {
				t.Fatalf("our loans: core %d, bki %d", len(s.loans), len(ours))
			}
			for _, l := range s.loans {
				found := false
				for _, a := range ours {
					if a.Limit == l.Limit && a.Outstanding == l.Outstanding && a.OpenedAt == l.OpenedAt &&
						fmt.Sprint(a.MonthlyPayment == nil) == fmt.Sprint(l.MonthlyPayment == nil) {
						found = true
					}
				}
				if !found {
					t.Errorf("loan %s not found in bki", l.ContractID)
				}
				if l.MonthlyPayment != nil && !s.hasDebit(*l.MonthlyPayment, alfa, l.ContractID) {
					t.Errorf("no payment for %s in transactions", l.ContractID)
				}
			}
		})
	}
}

// Лизинг из Федресурса виден в выписке: «Лизинговый платеж по договору лизинга №…».
func TestLeasingAcrossServices(t *testing.T) {
	r := testRouter()
	for _, inn := range mt.Companies {
		s := load(t, r, inn)
		fr := mt.Decode[struct {
			Messages []struct {
				Type    string `json:"type"`
				Payload struct {
					ContractNumber string `json:"contract_number"`
					LesseeINN      string `json:"lessee_inn"`
				} `json:"payload"`
			} `json:"messages"`
		}](t, mt.Get(r, "/fedresurs/v1/companies/"+inn+"/messages"), http.StatusOK)
		for _, m := range fr.Messages {
			if m.Type != "LEASING_CONTRACT" {
				continue
			}
			if m.Payload.LesseeINN != inn {
				t.Errorf("%s: lessee %s", inn, m.Payload.LesseeINN)
			}
			paid := false
			for _, tx := range s.txs {
				if tx.Direction == "DEBIT" && strings.Contains(tx.Purpose, "Лизинговый платеж по договору лизинга №"+m.Payload.ContractNumber+" ") {
					paid = true
				}
			}
			if !paid {
				t.Errorf("%s: no leasing payments for %s", inn, m.Payload.ContractNumber)
			}
		}
	}
}

// Стоп-факторы сценария C сходятся между АБС, ФНС, ФССП, КАД и Федресурсом.
func TestScenarioCStopFactors(t *testing.T) {
	r := testRouter()
	inn := mt.InnC
	rs := mt.Decode[struct {
		AccountBlocks []struct {
			DecisionNumber string `json:"decision_number"`
		} `json:"account_blocks"`
		Arrests []struct {
			Amount float64 `json:"amount"`
		} `json:"arrests"`
	}](t, mt.Get(r, "/core-banking/v1/clients/"+inn+"/restrictions"), http.StatusOK)
	bi := mt.Decode[struct {
		Decisions []struct {
			Number string `json:"number"`
		} `json:"decisions"`
	}](t, mt.PostForm(r, "/fns/bi/bi2-proc.json", url.Values{"inn": {inn}, "bik": {"044525999"}}), http.StatusOK)
	if len(rs.AccountBlocks) != 1 || len(bi.Decisions) != 1 || rs.AccountBlocks[0].DecisionNumber != bi.Decisions[0].Number {
		t.Errorf("block: core %+v, fns %+v", rs.AccountBlocks, bi.Decisions)
	}

	fssp := mt.Decode[struct {
		Items []struct {
			Status          string  `json:"status"`
			AmountRemaining float64 `json:"amount_remaining"`
		} `json:"items"`
	}](t, mt.Get(r, "/fssp/v1/proceedings?inn="+inn), http.StatusOK)
	for _, a := range rs.Arrests {
		found := false
		for _, p := range fssp.Items {
			found = found || (p.Status == "ACTIVE" && p.AmountRemaining == a.Amount)
		}
		if !found {
			t.Errorf("arrest %.2f has no active FSSP proceeding", a.Amount)
		}
	}

	kad := mt.Decode[struct {
		Items []struct {
			CaseNumber string `json:"case_number"`
			CaseType   string `json:"case_type"`
		} `json:"items"`
	}](t, mt.PostJSON(t, r, "/kad/v1/cases/search", map[string]any{"inn": inn}), http.StatusOK)
	fr := mt.Decode[struct {
		Messages []struct {
			Type    string `json:"type"`
			Payload struct {
				CaseNumber string `json:"case_number"`
			} `json:"payload"`
		} `json:"messages"`
	}](t, mt.Get(r, "/fedresurs/v1/companies/"+inn+"/messages"), http.StatusOK)
	var kadCase, frCase string
	for _, c := range kad.Items {
		if c.CaseType == "BANKRUPTCY" {
			kadCase = c.CaseNumber
		}
	}
	for _, m := range fr.Messages {
		if m.Type == "BANKRUPTCY" {
			frCase = m.Payload.CaseNumber
		}
	}
	if kadCase == "" || kadCase != frCase {
		t.Errorf("bankruptcy case: kad %q, fedresurs %q", kadCase, frCase)
	}

	s := load(t, r, inn)
	incasso := false
	for _, tx := range s.txs {
		incasso = incasso || tx.OperationCode == "06"
	}
	if !incasso {
		t.Error("no incasso (operation_code 06) in C transactions")
	}
}

// E: ЗСК MEDIUM из-за снятия наличных — снятия есть в выписке.
func TestScenarioECash(t *testing.T) {
	r := testRouter()
	s := load(t, r, mt.InnE)
	cash := 0
	for _, tx := range s.txs {
		if tx.Direction == "DEBIT" && strings.Contains(tx.Purpose, "Выдача наличных") {
			cash++
		}
	}
	if cash < 10 {
		t.Errorf("E: %d cash withdrawals, want a noticeable amount", cash)
	}
}

// X-Mock-Fault действует на новые моки и не трогает старый Bank, которым пользуется data-collector.
func TestFaultsScope(t *testing.T) {
	r := testRouter()
	paths := []string{
		"/core-banking/v1/clients/" + mt.InnA,
		"/fns/bo/advanced-search/organizations/search?query=" + mt.InnA,
		"/compliance/v1/zsk/" + mt.InnA,
		"/fssp/v1/proceedings?inn=" + mt.InnA,
		"/fedresurs/v1/persons/" + mt.DirA + "/bankruptcy",
		"/fns/puchdoc/v1/" + mt.InnA,
		"/m4d/v1/poa?principal_inn=" + mt.InnA,
		"/pledge-registry/v1/notifications?pledgor_inn=" + mt.InnB,
		"/open-banking/v1/accounts",
		"/marketplaces/v1/wildberries/sellers/" + mt.InnD,
		"/ofd/v1/organizations/" + mt.InnA + "/kkt",
		"/zakupki/v1/rnp?inn=" + mt.InnC,
		"/signals/v1/" + mt.InnC + "/news",
		"/hh/employers?text=x",
	}
	for _, p := range paths {
		if w := mt.Get(r, p, "X-Mock-Fault", "500"); w.Code != http.StatusInternalServerError {
			t.Errorf("%s: status %d", p, w.Code)
		}
	}
	for _, p := range []string{"/bki/v1/reports", "/kad/v1/cases/search", "/tax-reports/v1/requests", "/egrn/v1/extracts",
		"/gosuslugi/v1/profile-requests", "/cbr/DailyInfoWebServ/DailyInfo.asmx"} {
		if w := mt.PostJSON(t, r, p, "{}", "X-Mock-Fault", "503"); w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d", p, w.Code)
		}
	}
	if w := mt.Get(r, "/bank/v1/companies/7701234567/accounts", "X-Mock-Fault", "500"); w.Code != http.StatusOK {
		t.Errorf("bank: status %d", w.Code)
	}
	if w := mt.Get(r, "/healthz"); w.Code != http.StatusOK {
		t.Errorf("healthz: status %d", w.Code)
	}
}
