package domain

// ScoringAnswers — ответы клиента для credit-engine, которых нет в собранных данных (блок scoring анкеты).
// Коды — как в контракте credit-engine (credit-engine/contracts/contract.go). Всё необязательно:
// чего нет, движок запросит (NEED_INFO), и банк задаст клиенту вопрос.
type ScoringAnswers struct {
	MaxAnnualRate           *float64 // максимальная ставка, % годовых
	MinAcceptableAmount     *int64   // минимальная сумма, ₽
	MaxAcceptableTermMonths *int     // максимальный срок, мес.
	NeedCost                *int64   // стоимость того, на что нужен кредит, ₽
	OwnContribution         *int64   // собственный вклад, ₽
	FreeCash                *int64   // свободные деньги после собственного вклада, ₽
	MinimumCash             *int64   // неснижаемый остаток на счетах, ₽
	NeedEvidence            string   // CONTRACT_AND_COST, VERIFIED_ESTIMATE, ORAL_ONLY
	ExperienceYears         *int     // опыт руководителя (у ИП — владельца) в этом бизнесе, лет

	// Только ИП.
	PrimaryIncomeSource     string // BUSINESS, PUBLIC_EMPLOYMENT, PRIVATE_EMPLOYMENT, PENSION, SELF_EMPLOYED_OR_GPH, NONE
	Qualification           string // NONE, SUPPORT, SPECIALIST, MANAGER_OR_HIGHLY_QUALIFIED
	TotalExperienceMonths   *int
	NetMonthlyIncome        *int64 // личный доход после налогов, ₽ в месяц
	CarClass                string // NONE, ECONOMY, COMFORT, PREMIUM
	EducationLevel          string // BELOW_GENERAL, GENERAL, VOCATIONAL, HIGHER, ACADEMIC_DEGREE
	RealEstateStatus        string // UNENCUMBERED_WHOLE, UNENCUMBERED_SHARE, ENCUMBERED_ONLY, NONE
	BusinessLocation        string // VERIFIED_PHYSICAL, VERIFIED_ONLINE_OR_MOBILE, UNVERIFIED
	LocationStabilityMonths *int   // нужно при VERIFIED_PHYSICAL
	BuyerContracts          string // ACTIVE_AND_PERFORMED, REPEAT_ORDERS, PLANS_ONLY
}

// Допустимые коды ответов (как у credit-engine).
var ScoringEnums = map[string][]string{
	"need_evidence":         {"CONTRACT_AND_COST", "VERIFIED_ESTIMATE", "ORAL_ONLY"},
	"primary_income_source": {"BUSINESS", "PUBLIC_EMPLOYMENT", "PRIVATE_EMPLOYMENT", "PENSION", "SELF_EMPLOYED_OR_GPH", "NONE"},
	"qualification":         {"NONE", "SUPPORT", "SPECIALIST", "MANAGER_OR_HIGHLY_QUALIFIED"},
	"car_class":             {"NONE", "ECONOMY", "COMFORT", "PREMIUM"},
	"education_level":       {"BELOW_GENERAL", "GENERAL", "VOCATIONAL", "HIGHER", "ACADEMIC_DEGREE"},
	"real_estate_status":    {"UNENCUMBERED_WHOLE", "UNENCUMBERED_SHARE", "ENCUMBERED_ONLY", "NONE"},
	"business_location":     {"VERIFIED_PHYSICAL", "VERIFIED_ONLINE_OR_MOBILE", "UNVERIFIED"},
	"buyer_contracts":       {"ACTIVE_AND_PERFORMED", "REPEAT_ORDERS", "PLANS_ONLY"},
}
