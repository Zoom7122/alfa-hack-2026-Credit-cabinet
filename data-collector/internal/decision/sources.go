package decision

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// Разбор сырых ответов систем из raw_collections — только поля, нужные оценке.
// Формы — как их кладёт mockapi.Client (ключи put в internal/client/mockapi/systems.go).

type coreBanking struct {
	Client *struct {
		ClientSince string `json:"client_since"`
	} `json:"client"`
	Turnover *struct {
		Months []turnoverMonth `json:"months"`
	} `json:"turnover"`
	Loans *struct {
		Items []struct {
			CurrentOverdueDays int `json:"current_overdue_days"`
			MaxOverdueDays24M  int `json:"max_overdue_days_24m"`
		} `json:"items"`
	} `json:"loans"`
	Restrictions *struct {
		CardFile struct {
			HasUnpaidClaims bool `json:"has_unpaid_claims"`
		} `json:"card_file"`
		AccountBlocks []json.RawMessage `json:"account_blocks"`
		Arrests       []json.RawMessage `json:"arrests"`
	} `json:"restrictions"`
	Transactions []struct {
		Items []transaction `json:"items"`
	} `json:"transactions"`
}

type turnoverMonth struct {
	Month          string  `json:"month"`
	CreditTurnover float64 `json:"credit_turnover"`
	DebitTurnover  float64 `json:"debit_turnover"`
	AvgBalance     float64 `json:"avg_balance"`
}

type transaction struct {
	Date         string  `json:"date"`
	Direction    string  `json:"direction"` // CREDIT, DEBIT
	Amount       float64 `json:"amount"`
	Purpose      string  `json:"purpose"`
	Counterparty struct {
		INN  string `json:"inn"`
		Name string `json:"name"`
	} `json:"counterparty"`
}

type bkiReport struct {
	HasHistory bool `json:"has_history"`
	Summary    struct {
		TotalOutstanding float64 `json:"total_outstanding"`
		MaxOverdueEver   int     `json:"max_overdue_days_ever"`
		Overdue30P24M    int     `json:"overdue_30p_24m"`
		Overdue60P24M    int     `json:"overdue_60p_24m"`
		Overdue90P24M    int     `json:"overdue_90p_24m"`
		Inquiries90D     int     `json:"inquiries_90d"`
	} `json:"summary"`
	Accounts []struct {
		CreditorName       string   `json:"creditor_name"`
		ProductType        string   `json:"product_type"`
		OpenedAt           string   `json:"opened_at"`
		PlannedCloseAt     string   `json:"planned_close_at"`
		MonthlyPayment     *float64 `json:"monthly_payment"`
		Status             string   `json:"status"`
		CurrentOverdueDays int      `json:"current_overdue_days"`
	} `json:"accounts"`
}

type bki struct {
	Legal  *bkiReport `json:"legal"`
	Person *bkiReport `json:"person"`
}

type egrulRow struct {
	RegDate string `json:"r"` // ДД.ММ.ГГГГ
	EndDate string `json:"e"` // дата прекращения
	Head    string `json:"g"`
}

type fns struct {
	EGRUL *struct {
		Rows []egrulRow `json:"rows"`
	} `json:"egrul"`
	PB *struct {
		Vyp struct {
			OKVED   string   `json:"КодОКВЭД"`
			Status  int      `json:"sulst_ex"` // 10 — действующая
			Invalid int      `json:"invalid"`  // недостоверность сведений
			TaxDebt int      `json:"pr_zd"`    // задолженность по налогам
			Heads   []pbHead `json:"masruk"`
		} `json:"vyp"`
		Liquidated bool `json:"liquidated"`
	} `json:"pb"`
	BOBFO         []bfoReport `json:"bo_bfo"`
	AccountBlocks map[string]struct {
		Decisions []json.RawMessage `json:"decisions"`
	} `json:"account_blocks"`
}

type pbHead struct {
	INN string `json:"inn"`
}

type taxReports struct {
	Report *struct {
		TaxRegime struct {
			Current string `json:"current"`
		} `json:"tax_regime"`
		Documents []struct {
			Type   string             `json:"type"`
			Period string             `json:"period"`
			Data   map[string]float64 `json:"data"`
		} `json:"documents"`
	} `json:"report"`
}

type fssp struct {
	Company *struct {
		Summary struct {
			ActiveAmount float64 `json:"active_amount"`
		} `json:"summary"`
	} `json:"company"`
}

type kad struct {
	Cases []struct {
		Summary struct {
			DefendantActiveAmount float64 `json:"as_defendant_active_amount"`
			Bankruptcy            bool    `json:"bankruptcy_as_debtor"`
		} `json:"summary"`
	} `json:"cases"`
}

type fedresurs struct {
	CompanyMessages *struct {
		Summary struct {
			BankruptcyStage string `json:"bankruptcy_stage"`
			Liquidation     bool   `json:"liquidation"`
		} `json:"summary"`
	} `json:"company_messages"`
}

type compliance struct {
	ZSK *struct {
		RiskLevel string `json:"risk_level"`
	} `json:"zsk"`
	Screening *struct {
		Results []struct {
			Listed bool `json:"listed"`
		} `json:"results"`
	} `json:"screening"`
}

type m4d struct {
	Search *struct {
		Items []poa `json:"items"`
	} `json:"search"`
	POA *poa `json:"poa"`
}

type poa struct {
	Number         string `json:"number"`
	Status         string `json:"status"`
	Representative struct {
		INN string `json:"inn"`
	} `json:"representative"`
}

type signals struct {
	News *struct {
		Items []struct {
			Tags []string `json:"tags"`
		} `json:"items"`
	} `json:"news"`
}

// decode — разбор сырого ответа системы; пусто или null — false.
func decode(raw json.RawMessage, v any) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// Классы операций по назначению платежа (как в выписках сценариев mock-api).
var (
	reNonOperatingCredit = regexp.MustCompile(`займ|заём|кредит|собственных средств`)
	reDebt               = regexp.MustCompile(`погашени|процент|лизинг|кредитн`)
	reTaxes              = regexp.MustCompile(`налог|енп`)
	reFixed              = regexp.MustCompile(`заработн|аренд|страхов`)
	reOwner              = regexp.MustCompile(`дивиденд|учредител`)
	reOwnTransfer        = regexp.MustCompile(`собственных средств`)
)

func lower(s string) string { return strings.ToLower(s) }

// monthsBetween — полных месяцев от from до to.
func monthsBetween(from, to time.Time) int {
	m := (to.Year()-from.Year())*12 + int(to.Month()-from.Month())
	if to.Day() < from.Day() {
		m--
	}
	return max(m, 0)
}

func parseDate(s string) (time.Time, bool) {
	for _, layout := range []string{time.DateOnly, "02.01.2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
