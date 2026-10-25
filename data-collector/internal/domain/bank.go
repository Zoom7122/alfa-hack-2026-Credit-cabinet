package domain

import "time"

// Данные внутренних систем банка (источники 1–3 в docs/data-sources.md).
//
// Структуры повторяют формат ответа банка, JSON-теги — его имена полей.
// Суммы — десятичные строки, как их отдаёт банк: на float теряются копейки.
// В признаки скоринга эти данные переводит следующий шаг.

// BankSource — система банка, из которой берутся данные.
type BankSource string

const (
	SourceAccounts      BankSource = "ACCOUNTS"       // расчётный счёт
	SourceCredits       BankSource = "CREDITS"        // кредиты
	SourceAcquiring     BankSource = "ACQUIRING"      // эквайринг
	SourceSalaryProject BankSource = "SALARY_PROJECT" // зарплатный проект
	SourceCardIndex     BankSource = "CARD_INDEX"     // картотека неоплаченных требований
)

// SourceState — итог запроса к одной системе.
type SourceState string

const (
	SourceDone   SourceState = "DONE"
	SourceFailed SourceState = "FAILED"
)

// SourceResult — как сходили в одну систему.
type SourceResult struct {
	Source   BankSource
	State    SourceState
	Error    string // пусто, если State == DONE
	Duration time.Duration
}

// InternalBankData — всё, что удалось получить из систем банка по компании.
// Поле nil, если система не ответила; причина — в Sources.
type InternalBankData struct {
	INN           string
	Accounts      *AccountsReport
	Credits       *CreditReport
	Acquiring     *AcquiringReport
	SalaryProject *SalaryProjectReport
	CardIndex     *CardIndexReport
	Sources       []SourceResult
}

// Money — сумма с валютой.
type Money struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

// ------------------------------------------------------------ расчётный счёт

// AccountsReport — счета, остатки и транзакции (формат Open Banking).
type AccountsReport struct {
	Data  AccountsData `json:"Data"`
	Links struct {
		Self string `json:"self"`
	} `json:"Links"`
	Meta struct {
		TotalPages int `json:"totalPages"`
	} `json:"Meta"`
}

type AccountsData struct {
	Account     []Account     `json:"Account"`
	Balance     []Balance     `json:"Balance"`
	Transaction []Transaction `json:"Transaction"`
}

type Account struct {
	AccountID            string           `json:"accountId"`
	Status               string           `json:"status"`
	StatusUpdateDateTime time.Time        `json:"statusUpdateDateTime"`
	Currency             string           `json:"currency"`
	AccountType          string           `json:"accountType"`
	AccountSubType       string           `json:"accountSubType"`
	AccountDescription   string           `json:"accountDescription"`
	OpeningDate          string           `json:"openingDate"`
	AccountDetails       []AccountDetails `json:"AccountDetails"`
	Servicer             Servicer         `json:"Servicer"`
}

type AccountDetails struct {
	SchemeName     string `json:"schemeName"`
	Identification string `json:"identification"` // номер счёта
	Name           string `json:"name"`
}

type Servicer struct {
	SchemeName     string `json:"schemeName"`
	Identification string `json:"identification"` // БИК
}

type Balance struct {
	AccountID            string    `json:"accountId"`
	CreditDebitIndicator string    `json:"creditDebitIndicator"`
	Type                 string    `json:"type"`
	DateTime             time.Time `json:"dateTime"`
	Amount               Money     `json:"Amount"`
}

type Transaction struct {
	AccountID              string               `json:"accountId"`
	TransactionID          string               `json:"transactionId"`
	CreditDebitIndicator   string               `json:"creditDebitIndicator"` // Credit — поступление, Debit — списание
	Status                 string               `json:"status"`
	BookingDateTime        time.Time            `json:"bookingDateTime"`
	Amount                 Money                `json:"Amount"`
	TransactionInformation string               `json:"transactionInformation"` // назначение платежа
	CounterpartyAccount    *CounterpartyAccount `json:"CounterpartyAccount,omitempty"`
	DocumentNumber         string               `json:"documentNumber"`
}

type CounterpartyAccount struct {
	Identification string `json:"identification"`
	Name           string `json:"name"`
}

// ------------------------------------------------------------ кредиты

// CreditReport — действующие кредиты и долговая нагрузка.
type CreditReport struct {
	Bureau      string           `json:"bureau"`
	ReportID    string           `json:"reportId"`
	RequestDate string           `json:"requestDate"`
	Consent     CreditConsent    `json:"consent"`
	Subject     CreditSubject    `json:"subject"`
	Summary     CreditSummary    `json:"summary"`
	Contracts   []CreditContract `json:"contracts"`
}

type CreditConsent struct {
	Date       string `json:"date"`
	ValidUntil string `json:"validUntil"`
	Purpose    string `json:"purpose"`
}

type CreditSubject struct {
	INN  string `json:"inn"`
	OGRN string `json:"ogrn"`
	Name string `json:"name"`
}

type CreditSummary struct {
	ActiveContracts       int    `json:"activeContracts"`
	TotalDebt             string `json:"totalDebt"`
	OverdueDebt           string `json:"overdueDebt"`
	MaxOverdueDaysLast12m int    `json:"maxOverdueDaysLast12m"`
	InquiriesLast30d      int    `json:"inquiriesLast30d"`
}

type CreditContract struct {
	UID              string `json:"uid"`
	Creditor         string `json:"creditor"`
	Type             string `json:"type"`
	Currency         string `json:"currency"`
	Limit            string `json:"limit"`
	OpenDate         string `json:"openDate"`
	PlannedCloseDate string `json:"plannedCloseDate,omitempty"`
	CurrentDebt      string `json:"currentDebt"`
	OverdueDebt      string `json:"overdueDebt"`
	Collateral       string `json:"collateral,omitempty"`
	PaymentHistory   string `json:"paymentHistory"` // по месяцу на символ, 0 — без просрочки
}

// ------------------------------------------------------------ эквайринг

// AcquiringReport — терминалы и оборот по картам.
type AcquiringReport struct {
	Merchant        Merchant          `json:"merchant"`
	Acquirer        string            `json:"acquirer"`
	ContractDate    string            `json:"contractDate"`
	CommissionRate  string            `json:"commissionRate"` // % от оборота
	Terminals       []Terminal        `json:"terminals"`
	MonthlyTurnover []MonthlyTurnover `json:"monthlyTurnover"`
	SBP             SBP               `json:"sbp"`
}

type Merchant struct {
	INN        string `json:"inn"`
	MerchantID string `json:"merchantId"`
	MCC        string `json:"mcc"`
}

type Terminal struct {
	TID     string `json:"tid"`
	Type    string `json:"type"` // POS, Ecom
	Address string `json:"address,omitempty"`
	Status  string `json:"status"`
}

type MonthlyTurnover struct {
	Month      string `json:"month"` // YYYY-MM
	Operations int    `json:"operations"`
	Amount     string `json:"amount"`
	Refunds    string `json:"refunds"`
}

type SBP struct {
	Connected              bool   `json:"connected"`
	MonthlyAmountLast3mAvg string `json:"monthlyAmountLast3mAvg"`
}

// ------------------------------------------------------------ зарплатный проект

// SalaryProjectReport — зарплатный проект: штат и фонд оплаты труда.
type SalaryProjectReport struct {
	Bank                 string          `json:"bank"`
	ContractNumber       string          `json:"contractNumber"`
	ContractDate         string          `json:"contractDate"`
	Status               string          `json:"status"`
	EmployeesWithCards   int             `json:"employeesWithCards"`
	Payments             []SalaryPayment `json:"payments"`
	AvgMonthlyFundLast6m string          `json:"avgMonthlyFundLast6m"`
}

type SalaryPayment struct {
	RegistryNumber string `json:"registryNumber"`
	Date           string `json:"date"`
	Employees      int    `json:"employees"`
	Amount         string `json:"amount"`
}

// ------------------------------------------------------------ картотека

// CardIndexReport — неоплаченные требования к счёту, блокировки и аресты.
type CardIndexReport struct {
	Source          string               `json:"source"`
	AsOfDate        string               `json:"asOfDate"`
	AccountNumber   string               `json:"accountNumber"`
	UnpaidDocuments []UnpaidDocument     `json:"unpaidDocuments"`
	TotalUnpaid     string               `json:"totalUnpaid"`
	Suspensions     []AccountRestriction `json:"suspensions"` // приостановления операций
	Arrests         []AccountRestriction `json:"arrests"`
}

type UnpaidDocument struct {
	Queue        int    `json:"queue"` // очерёдность списания (ст. 855 ГК)
	DocumentType string `json:"documentType"`
	Number       string `json:"number"`
	Date         string `json:"date"`
	Recipient    string `json:"recipient"`
	Amount       string `json:"amount"`
	PaidAmount   string `json:"paidAmount"`
	Basis        string `json:"basis"`
}

type AccountRestriction struct {
	Authority      string `json:"authority"`
	DecisionNumber string `json:"decisionNumber"`
	DecisionDate   string `json:"decisionDate"`
	Type           string `json:"type"`
	Amount         string `json:"amount"`
}
