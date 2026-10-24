package openbanking_test

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	openbanking "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/OpenBanking"
	mt "github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/mock-api/internal/mocktest"
)

var router = mt.Router("/open-banking/v1", openbanking.Register)

const gorizont = "044525225"

func hdr(inn, bic string) []string { return []string{"X-Customer-Inn", inn, "X-Bank-Bic", bic} }

type accounts struct {
	Data struct {
		Account []struct {
			AccountID string `json:"accountId"`
		} `json:"Account"`
	} `json:"Data"`
}

type txs struct {
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
}

func TestAccounts(t *testing.T) {
	count := map[[2]string]int{{mt.InnA, gorizont}: 1, {mt.InnB, "044525777"}: 1, {mt.InnB, "044525650"}: 1,
		{mt.InnD, gorizont}: 0, {mt.InnC, "044525777"}: 0}
	for k, n := range count {
		a := mt.Decode[accounts](t, mt.Get(router, "/open-banking/v1/accounts", hdr(k[0], k[1])...), http.StatusOK)
		if len(a.Data.Account) != n || a.Data.Account == nil {
			t.Errorf("%v: %+v", k, a)
		}
	}
	bal := mt.Decode[struct {
		Data struct {
			Balance []struct {
				Amount struct {
					Amount string `json:"amount"`
				} `json:"Amount"`
			} `json:"Balance"`
		} `json:"Data"`
	}](t, mt.Get(router, "/open-banking/v1/accounts/acc-0b71/balances", hdr(mt.InnA, gorizont)...), http.StatusOK)
	if bal.Data.Balance[0].Amount.Amount != "184320.55" {
		t.Errorf("A balance: %+v", bal)
	}
	mt.ErrorCode(t, mt.Get(router, "/open-banking/v1/accounts"), http.StatusBadRequest)
	mt.ErrorCode(t, mt.Get(router, "/open-banking/v1/accounts/acc-0b71/balances", hdr(mt.InnB, gorizont)...), http.StatusNotFound)
}

func TestTransactions(t *testing.T) {
	q := url.Values{"fromBookingDateTime": {"2026-09-01T00:00:00+03:00"}, "toBookingDateTime": {"2026-09-30T23:59:59+03:00"}}
	sep := mt.Decode[txs](t, mt.Get(router, "/open-banking/v1/accounts/acc-0b71/transactions?"+q.Encode(), hdr(mt.InnA, gorizont)...),
		http.StatusOK)
	self := false
	for _, tx := range sep.Data.Transaction {
		if tx.BookingDateTime < "2026-09-01" || tx.BookingDateTime > "2026-09-31" {
			t.Errorf("out of range: %s", tx.BookingDateTime)
		}
		self = self || (tx.Info == "Перевод собственных средств" && tx.Amount.Amount == "300000.00" && tx.Indicator == "Credit")
	}
	if !self {
		t.Error("no incoming self-transfer of 300 000 in September")
	}
	all := mt.Decode[txs](t, mt.Get(router, "/open-banking/v1/accounts/acc-0b71/transactions", hdr(mt.InnA, gorizont)...), http.StatusOK)
	if len(all.Data.Transaction) <= len(sep.Data.Transaction) {
		t.Errorf("unfiltered %d <= september %d", len(all.Data.Transaction), len(sep.Data.Transaction))
	}
	mt.ErrorCode(t, mt.Get(router, "/open-banking/v1/accounts/acc-0b71/transactions?fromBookingDateTime=2026-09-01",
		hdr(mt.InnA, gorizont)...), http.StatusBadRequest)
}

func TestConsents(t *testing.T) {
	type consent struct {
		Data struct {
			ConsentID string `json:"consentId"`
			Status    string `json:"status"`
		} `json:"Data"`
	}
	body := map[string]any{"Data": map[string]any{"permissions": []string{"ReadAccountsBasic", "ReadBalances"}}}
	c := mt.Decode[consent](t, mt.PostJSON(t, router, "/open-banking/v1/account-consents", body, hdr(mt.InnA, gorizont)...),
		http.StatusCreated)
	if c.Data.Status != "AwaitingAuthorisation" || c.Data.ConsentID == "" {
		t.Fatalf("create: %+v", c)
	}
	g := mt.Decode[consent](t, mt.Get(router, "/open-banking/v1/account-consents/"+c.Data.ConsentID), http.StatusOK)
	if g.Data.Status != "Authorised" || g.Data.ConsentID != c.Data.ConsentID {
		t.Errorf("get: %+v", g)
	}
	mt.ErrorCode(t, mt.Get(router, "/open-banking/v1/account-consents/aac-nope"), http.StatusNotFound)
	mt.ErrorCode(t, mt.PostJSON(t, router, "/open-banking/v1/account-consents", map[string]any{"Data": map[string]any{}},
		hdr(mt.InnA, gorizont)...), http.StatusBadRequest)
}

// Остаток счёта = входящий остаток + обороты по выписке, и счёт ни разу не уходит в минус.
func TestSeedBalances(t *testing.T) {
	type acc struct {
		OpeningBalance float64 `json:"opening_balance"`
		Balance        struct {
			Amount struct {
				Amount string `json:"amount"`
			} `json:"Amount"`
		} `json:"balance"`
		Transactions []struct {
			Indicator string `json:"creditDebitIndicator"`
			Amount    struct {
				Amount string `json:"amount"`
			} `json:"Amount"`
		} `json:"transactions"`
	}
	for _, f := range mt.SeedFiles(t, "seed/*.json") {
		s := mt.ReadSeed[struct {
			Banks map[string][]acc `json:"banks"`
		}](t, f)
		for bic, accs := range s.Banks {
			for _, a := range accs {
				bal := a.OpeningBalance
				for i := len(a.Transactions) - 1; i >= 0; i-- { // в seed новые первыми
					v, _ := strconv.ParseFloat(a.Transactions[i].Amount.Amount, 64)
					if a.Transactions[i].Indicator == "Debit" {
						v = -v
					}
					bal += v
					if bal < -0.005 {
						t.Fatalf("%s %s: negative balance", f, bic)
					}
				}
				closing, _ := strconv.ParseFloat(a.Balance.Amount.Amount, 64)
				if d := bal - closing; d > 0.005 || d < -0.005 {
					t.Errorf("%s %s: balance %.2f, want %.2f", f, bic, closing, bal)
				}
			}
		}
	}
}
