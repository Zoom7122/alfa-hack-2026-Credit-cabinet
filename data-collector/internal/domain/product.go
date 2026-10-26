package domain

// Product — кредитный продукт каталога: для кого, на что, диапазоны суммы и срока, ставка «от».
type Product struct {
	Code           string
	Name           string
	Description    string
	ApplicantTypes []CompanyType
	Purposes       []string
	AmountMin      int64
	AmountMax      int64
	TermMin        int
	TermMax        int
	RateFrom       float64 // годовых, в процентах
	// С какой суммы нужен залог: nil — залог не нужен, 0 — нужен всегда.
	CollateralAbove   *int64
	CollateralTypes   []string
	GuaranteeRequired bool
}

func rub(v int64) *int64 { return &v }

var both = []CompanyType{CompanyLegalEntity, CompanySoleProprietor}

// Products — каталог банка, порядок — от самого ходового. Ставки «от» ниже ставок credit-engine: точная — после оценки.
var Products = []Product{
	{Code: "WORKING_CAPITAL", Name: "Оборотный кредит", Description: "На закупку товара, сырья, выплату зарплат и налогов",
		ApplicantTypes: both, Purposes: []string{"WORKING_CAPITAL", "TENDER", "BUSINESS_EXPANSION", "OTHER"},
		AmountMin: 300_000, AmountMax: 30_000_000, TermMin: 3, TermMax: 36, RateFrom: 18.5,
		CollateralAbove: rub(10_000_000), CollateralTypes: []string{"REAL_ESTATE", "GOODS_IN_TURNOVER", "DEPOSIT"}},
	{Code: "CREDIT_LINE", Name: "Кредитная линия", Description: "Берёте деньги частями, когда нужно, проценты — только на использованную сумму",
		ApplicantTypes: []CompanyType{CompanyLegalEntity}, Purposes: []string{"WORKING_CAPITAL", "TENDER", "OTHER"},
		AmountMin: 1_000_000, AmountMax: 50_000_000, TermMin: 6, TermMax: 24, RateFrom: 19.9,
		CollateralAbove: rub(15_000_000), CollateralTypes: []string{"REAL_ESTATE", "RECEIVABLES", "DEPOSIT"}, GuaranteeRequired: true},
	{Code: "EXPRESS", Name: "Экспресс-кредит без залога", Description: "Решение за 5 минут по данным о компании, без документов",
		ApplicantTypes: both, Purposes: []string{"WORKING_CAPITAL", "EQUIPMENT", "BUSINESS_EXPANSION", "OTHER"},
		AmountMin: 100_000, AmountMax: 5_000_000, TermMin: 3, TermMax: 24, RateFrom: 23},
	{Code: "VEHICLE", Name: "Кредит на транспорт", Description: "Покупка грузового и легкового транспорта для бизнеса",
		ApplicantTypes: both, Purposes: []string{"VEHICLE"},
		AmountMin: 500_000, AmountMax: 20_000_000, TermMin: 12, TermMax: 60, RateFrom: 17.9,
		CollateralAbove: rub(0), CollateralTypes: []string{"VEHICLE"}},
	{Code: "EQUIPMENT", Name: "Кредит на оборудование", Description: "Покупка и модернизация оборудования",
		ApplicantTypes: both, Purposes: []string{"EQUIPMENT"},
		AmountMin: 500_000, AmountMax: 50_000_000, TermMin: 12, TermMax: 60, RateFrom: 18.9,
		CollateralAbove: rub(5_000_000), CollateralTypes: []string{"EQUIPMENT", "REAL_ESTATE"}},
	{Code: "REAL_ESTATE", Name: "Коммерческая ипотека", Description: "Покупка офиса, склада или торгового помещения",
		ApplicantTypes: both, Purposes: []string{"REAL_ESTATE", "BUSINESS_EXPANSION"},
		AmountMin: 1_000_000, AmountMax: 100_000_000, TermMin: 12, TermMax: 120, RateFrom: 16.9,
		CollateralAbove: rub(0), CollateralTypes: []string{"REAL_ESTATE"}, GuaranteeRequired: true},
	{Code: "REFINANCING", Name: "Рефинансирование", Description: "Перенесём кредиты других банков и снизим платёж",
		ApplicantTypes: both, Purposes: []string{"REFINANCING"},
		AmountMin: 500_000, AmountMax: 50_000_000, TermMin: 12, TermMax: 60, RateFrom: 17.5,
		CollateralAbove: rub(10_000_000), CollateralTypes: []string{"REAL_ESTATE", "EQUIPMENT", "VEHICLE"}},
}

// ProductByCode — продукт каталога по коду.
func ProductByCode(code string) (Product, bool) {
	for _, p := range Products {
		if p.Code == code {
			return p, true
		}
	}
	return Product{}, false
}
