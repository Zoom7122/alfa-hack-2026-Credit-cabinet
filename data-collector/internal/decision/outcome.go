package decision

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/data-collector/internal/domain"
)

// Outcome — итог оценки для заявки: статус, строки decisions/offers/info_requests.
type Outcome struct {
	Status        domain.ApplicationStatus
	AutoOutcome   string // APPROVED, REJECTED, MANUAL_REVIEW, NEED_INFO — decisions.auto_outcome
	Grade         string
	Score         *int
	Offer         *Offer // одобрено — выдаётся клиенту; на рассмотрении — предложенные движком условия
	Questions     []Question
	ClientReasons []ClientReason
	Findings      []Finding
	Message       string // для журнала клиента
}

type Offer struct {
	AmountRUB      int64   `json:"amount_rub"`
	TermMonths     int     `json:"term_months"`
	AnnualRate     float64 `json:"annual_rate"`
	MonthlyPayment int64   `json:"monthly_payment"`
	Confirmed      bool    `json:"confirmed"` // условия подтверждены движком (APPROVED)
}

// Question — вопрос клиенту по полю, которого не хватило движку.
type Question struct {
	Code        string   // тема вопроса
	Title       string   // заголовок для клиента
	Description string   // что именно нужно
	Fields      []string // поля движка
	Required    bool
	Answer      Answer // как отвечать
}

// Answer — как клиент отвечает на вопрос: значение (и в какое поле анкеты scoring оно ляжет) и/или документы.
type Answer struct {
	Type         string   `json:"type"` // NONE, TEXT, NUMBER, MONEY, CHOICE
	ScoringField string   `json:"scoring_field,omitempty"`
	Options      []Option `json:"options,omitempty"`
	DocTypes     []string `json:"-"` // какие документы приложить; есть — документ обязателен
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

func money(field string) Answer  { return Answer{Type: "MONEY", ScoringField: field} }
func number(field string) Answer { return Answer{Type: "NUMBER", ScoringField: field} }
func docs(types ...string) Answer {
	return Answer{Type: "NONE", DocTypes: types}
}
func choice(field string, options ...Option) Answer {
	return Answer{Type: "CHOICE", ScoringField: field, Options: options}
}

var (
	needEvidenceOptions = []Option{{"CONTRACT_AND_COST", "Договор или заказ и документы о стоимости"}, {"VERIFIED_ESTIMATE", "Коммерческие предложения или смета"}, {"ORAL_ONLY", "Пока только описание"}}
	incomeOptions       = []Option{{"BUSINESS", "Предпринимательская деятельность"}, {"PUBLIC_EMPLOYMENT", "Работа в бюджетной организации"}, {"PRIVATE_EMPLOYMENT", "Работа в коммерческой организации"}, {"PENSION", "Пенсия"}, {"SELF_EMPLOYED_OR_GPH", "Самозанятость или договоры ГПХ"}, {"NONE", "Регулярного дохода нет"}}
	qualificationOpts   = []Option{{"MANAGER_OR_HIGHLY_QUALIFIED", "Руководитель или специалист высшей квалификации"}, {"SPECIALIST", "Специалист или служащий"}, {"SUPPORT", "Обслуживающий персонал"}, {"NONE", "Подтверждённой квалификации нет"}}
	carOptions          = []Option{{"NONE", "Нет автомобиля"}, {"ECONOMY", "Эконом-класс"}, {"COMFORT", "Комфорт-класс"}, {"PREMIUM", "Премиум-класс"}}
	educationOptions    = []Option{{"ACADEMIC_DEGREE", "Учёная степень"}, {"HIGHER", "Высшее"}, {"VOCATIONAL", "Среднее профессиональное"}, {"GENERAL", "Среднее общее"}, {"BELOW_GENERAL", "Не завершено среднее"}}
	realEstateOptions   = []Option{{"UNENCUMBERED_WHOLE", "Есть объект без обременений"}, {"UNENCUMBERED_SHARE", "Есть только доля без обременений"}, {"ENCUMBERED_ONLY", "Есть только с обременением"}, {"NONE", "Нет"}}
	locationOptions     = []Option{{"VERIFIED_PHYSICAL", "Своё или арендованное помещение"}, {"VERIFIED_ONLINE_OR_MOBILE", "Онлайн или выездная работа"}, {"UNVERIFIED", "Постоянного места нет"}}
	buyerOptions        = []Option{{"ACTIVE_AND_PERFORMED", "Действующие договоры, они исполняются"}, {"REPEAT_ORDERS", "Повторные разовые заказы"}, {"PLANS_ONLY", "Пока только планы продаж"}}
)

type questionTopic struct {
	prefix, code, title, description string
	answer                           Answer
}

// questionTopics — как поля движка превращаются в вопросы клиенту: по первому совпавшему префиксу.
var questionTopics = []questionTopic{
	{"credit_request.max_annual_rate", "MAX_RATE", "Максимальная ставка", "Какую максимальную ставку по кредиту вы готовы рассматривать, % годовых?", number("max_annual_rate")},
	{"credit_request.min_acceptable_amount_rub", "MIN_AMOUNT", "Минимальная сумма", "Какая минимальная сумма кредита вам подойдёт?", money("min_acceptable_amount")},
	{"credit_request.max_acceptable_term_months", "MAX_TERM", "Максимальный срок", "На какой максимальный срок, в месяцах, вы готовы взять кредит?", number("max_acceptable_term_months")},
	{"need.confirmed_cost_rub", "NEED_COST", "Стоимость потребности", "Сколько стоит то, на что нужен кредит?", money("need_cost")},
	{"need.own_contribution_rub", "OWN_CONTRIBUTION", "Собственный вклад", "Сколько вы вкладываете сами? 0, если ничего.", money("own_contribution")},
	{"need", "NEED_COST", "Стоимость потребности", "Сколько стоит то, на что нужен кредит?", money("need_cost")},
	{"cash.free_after_own_contribution_rub", "FREE_CASH_AMOUNT", "Свободные деньги", "Сколько свободных денег останется на счетах после собственного вклада?", money("free_cash")},
	{"cash.minimum_required_rub", "MIN_CASH", "Неснижаемый остаток", "Какой остаток на счетах вам нужен для работы бизнеса?", money("minimum_cash")},
	{"cash", "FREE_CASH_AMOUNT", "Свободные деньги", "Сколько свободных денег останется на счетах после собственного вклада?", money("free_cash")},
	{"monthly_history", "MONTHLY_HISTORY", "Выписка по счетам за 12 месяцев", "Загрузите выписку по расчётным счетам за последние 12 полных месяцев.", docs("BANK_STATEMENT")},
	{"service_b.", "STATEMENTS", "Бухгалтерская отчётность за 2 года", "Загрузите бухгалтерский баланс и отчёт о финансовых результатах за два последних года.", docs("FINANCIAL_STATEMENTS")},
	{"evidence", "EVIDENCE", "Подтверждение выручки", "Загрузите декларации или другие документы, подтверждающие выручку за 12 месяцев.", docs("TAX_RETURN")},
	{"ip_economic_data", "PREVIOUS_RECEIPTS", "Выручка за предыдущий год", "Загрузите декларацию или выписку за предыдущие 12 месяцев.", docs("TAX_RETURN")},
	{"financial_debt_rub", "DEBTS", "Текущие долги", "Перечислите действующие кредиты, займы и лизинг: кредитор, остаток, платёж в месяц.", Answer{Type: "TEXT"}},
	{"future_debt_payments", "DEBTS", "Текущие долги", "Перечислите действующие кредиты, займы и лизинг: кредитор, остаток, платёж в месяц.", Answer{Type: "TEXT"}},
	{"facts_date", "FACTS_DATE", "Актуальность данных", "Данные устарели — подтвердите, что сведения о компании не изменились.", Answer{Type: "TEXT"}},
	{"scoring.ip.qualification", "QUALIFICATION", "Квалификация", "Какая у вас профессиональная квалификация?", choice("qualification", qualificationOpts...)},
	{"scoring.ip.primary_income_source", "INCOME_SOURCE", "Источник дохода", "Какой у вас основной источник личного дохода?", choice("primary_income_source", incomeOptions...)},
	{"scoring.ip.total_experience_months", "EXPERIENCE", "Трудовой стаж", "Каков ваш общий трудовой и предпринимательский стаж, месяцев?", number("total_experience_months")},
	{"scoring.ip.net_monthly_income_rub", "PERSONAL_INCOME", "Личный доход", "Каков ваш среднемесячный личный доход после налогов?", money("net_monthly_income")},
	{"scoring.ip.car_class", "CAR", "Автомобиль", "Есть ли у вас автомобиль и какого класса?", choice("car_class", carOptions...)},
	{"scoring.ip.education_level", "EDUCATION", "Образование", "Какое у вас образование?", choice("education_level", educationOptions...)},
	{"scoring.ip.real_estate_status", "REAL_ESTATE", "Недвижимость", "Есть ли у вас недвижимость в собственности?", choice("real_estate_status", realEstateOptions...)},
	{"scoring.ip.owner_experience_years", "BUSINESS_EXPERIENCE", "Опыт в бизнесе", "Сколько лет вы работаете в этом направлении бизнеса?", number("experience_years")},
	{"scoring.ip.business_location", "LOCATION", "Место ведения бизнеса", "Где вы ведёте бизнес?", choice("business_location", locationOptions...)},
	{"scoring.ip.location_stability_months", "LOCATION_MONTHS", "Срок на месте ведения бизнеса", "Сколько месяцев вы работаете на этом месте?", number("location_stability_months")},
	{"scoring.ip.income_evidence", "INCOME_EVIDENCE", "Подтверждение дохода", "Загрузите документы, подтверждающие личный доход.", docs("TAX_RETURN")},
	{"scoring.ip.buyer_contracts", "BUYERS", "Покупатели", "Как оформлены отношения с основными покупателями?", choice("buyer_contracts", buyerOptions...)},
	{"scoring.legal_entity.manager_experience_years", "MANAGER_EXPERIENCE", "Опыт руководителя", "Сколько лет руководитель работает в этом направлении?", number("experience_years")},
	{"scoring.legal_entity.need_evidence", "NEED_EVIDENCE", "Подтверждение потребности", "Чем подтверждена стоимость того, что финансирует кредит?", choice("need_evidence", needEvidenceOptions...)},
	{"scoring.legal_entity.report_verification_level", "STATEMENTS", "Бухгалтерская отчётность за 2 года", "Загрузите бухгалтерский баланс и отчёт о финансовых результатах за два последних года.", docs("FINANCIAL_STATEMENTS")},
	{"scoring.legal_entity.auditor_opinion", "AUDIT", "Аудиторское заключение", "Загрузите аудиторское заключение за последний год.", docs("OTHER")},
	{"scoring.ip.identity_and_authority_verified", "AUTHORITY", "Полномочия представителя", "Загрузите машиночитаемую доверенность (МЧД) на представителя или подайте заявку от руководителя.", docs("POWER_OF_ATTORNEY")},
	{"scoring.legal_entity.identity_and_authority_verified", "AUTHORITY", "Полномочия представителя", "Загрузите машиночитаемую доверенность (МЧД) на представителя или подайте заявку от руководителя.", docs("POWER_OF_ATTORNEY")},
	{"scoring.ip.free_cash_source_verified", "FREE_CASH", "Свободные деньги", "Остаток на счетах меньше указанных свободных денег — уточните сумму.", money("free_cash")},
	{"scoring.legal_entity.free_cash_source_verified", "FREE_CASH", "Свободные деньги", "Остаток на счетах меньше указанных свободных денег — уточните сумму.", money("free_cash")},
	{"scoring.ip.documents_complete", "MONTHLY_HISTORY", "Выписка по счетам за 12 месяцев", "Загрузите выписку по расчётным счетам за последние 12 полных месяцев.", docs("BANK_STATEMENT")},
	{"scoring.legal_entity.documents_complete", "STATEMENTS", "Бухгалтерская отчётность за 2 года", "Загрузите бухгалтерский баланс и отчёт о финансовых результатах за два последних года.", docs("FINANCIAL_STATEMENTS")},
	{"scoring.ip.consents_verified", "CONSENTS", "Согласия", "Без согласия на запрос кредитной истории банк не может оценить заявку. Напишите, готовы ли вы его дать.", Answer{Type: "TEXT"}},
	{"scoring.legal_entity.consents_verified", "CONSENTS", "Согласия", "Без согласия на запрос кредитной истории банк не может оценить заявку. Напишите, готовы ли вы его дать.", Answer{Type: "TEXT"}},
	{"scoring.ip.bureau_received", "CONSENTS", "Согласия", "Без согласия на запрос кредитной истории банк не может оценить заявку. Напишите, готовы ли вы его дать.", Answer{Type: "TEXT"}},
	{"scoring.legal_entity.bureau_received", "CONSENTS", "Согласия", "Без согласия на запрос кредитной истории банк не может оценить заявку. Напишите, готовы ли вы его дать.", Answer{Type: "TEXT"}},
	{"scoring.ip.structural_business_change", "BUSINESS_CHANGE", "Изменения в бизнесе", "Менялся ли за последний год бизнес: направление, место, ключевые покупатели или поставщики? Расскажите коротко.", Answer{Type: "TEXT"}},
	{"scoring.legal_entity.structural_business_change", "BUSINESS_CHANGE", "Изменения в бизнесе", "Менялся ли за последний год бизнес: направление, место, ключевые покупатели или поставщики? Расскажите коротко.", Answer{Type: "TEXT"}},
	{"scoring.ip.key_buyer_loss", "BUSINESS_CHANGE", "Изменения в бизнесе", "Менялся ли за последний год бизнес: направление, место, ключевые покупатели или поставщики? Расскажите коротко.", Answer{Type: "TEXT"}},
	{"scoring.legal_entity.key_buyer_loss", "BUSINESS_CHANGE", "Изменения в бизнесе", "Менялся ли за последний год бизнес: направление, место, ключевые покупатели или поставщики? Расскажите коротко.", Answer{Type: "TEXT"}},
	{"scoring.", "", "", "", Answer{Type: "TEXT"}}, // остальные проверяемые факты — вопрос с текстом причины
}

func topic(field string) questionTopic {
	for _, t := range questionTopics {
		if strings.HasPrefix(field, t.prefix) {
			return t
		}
	}
	return questionTopic{answer: Answer{Type: "TEXT"}}
}

type ClientReason struct {
	Code  string `json:"code"`
	Title string `json:"title"`
}

// Finding — расхождение в данных для андеррайтера.
type Finding struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Declared string `json:"declared_value,omitempty"`
	Found    string `json:"found_value,omitempty"`
	Source   string `json:"source,omitempty"`
	Material bool   `json:"material"`
}

// Interpret — итог для заявки по ответу движка.
func Interpret(resp Response, facts Facts) Outcome {
	out := Outcome{Findings: facts.findings()}
	if s := resp.Score; s != nil {
		out.Grade, out.Score = s.Grade, &s.Total
	}
	if t := resp.Terms; t != nil {
		o := &Offer{AmountRUB: t.AmountRUB, TermMonths: t.TermMonths, AnnualRate: t.AnnualRate, Confirmed: resp.TermsConfirmed}
		if resp.MonthlyPaymentRUB != nil {
			o.MonthlyPayment = int64(math.Round(*resp.MonthlyPaymentRUB))
		} else {
			o.MonthlyPayment = Annuity(t.AmountRUB, t.AnnualRate, t.TermMonths)
		}
		out.Offer = o
	}
	switch resp.Status {
	case "APPROVED":
		out.Status, out.AutoOutcome, out.Message = domain.ApplicationOffered, "APPROVED", "Банк одобрил кредит — посмотрите предложение"
	case "REJECTED":
		out.Status, out.AutoOutcome, out.Message = domain.ApplicationRejected, "REJECTED", "Банк отказал в кредите"
		for _, r := range resp.Reasons {
			out.ClientReasons = append(out.ClientReasons, ClientReason{Code: r.Code, Title: r.Message})
		}
	case "NEED_INFO":
		out.Status, out.AutoOutcome, out.Message = domain.ApplicationWaitingForInfo, "NEED_INFO", "Банку нужны дополнительные сведения"
		out.Questions = questions(resp.Reasons)
	default: // UNDER_REVIEW
		out.Status, out.AutoOutcome, out.Message = domain.ApplicationManualReview, "MANUAL_REVIEW", "Заявку рассматривает кредитный специалист"
	}
	return out
}

// questions — вопросы по полям из причин NEED_INFO, по одному на тему.
func questions(reasons []Reason) []Question {
	var out []Question
	for _, r := range reasons {
		fields := r.Fields
		if len(fields) == 0 {
			fields = []string{r.Code}
		}
		for _, f := range fields {
			t := topic(f)
			if t.code == "" {
				t.code, t.title, t.description = r.Code, "Уточните сведения", r.Message
			}
			if i := slices.IndexFunc(out, func(q Question) bool { return q.Code == t.code }); i >= 0 {
				if !slices.Contains(out[i].Fields, f) {
					out[i].Fields = append(out[i].Fields, f)
				}
				continue
			}
			out = append(out, Question{Code: t.code, Title: t.title, Description: t.description, Fields: []string{f}, Required: true, Answer: t.answer})
		}
	}
	return out
}

// WithoutAnswered убирает вопросы, на которые клиент уже ответил в прошлом раунде. Не осталось ни одного —
// движку не хватает того, что он сам не прочитает (документы клиента): заявка уходит специалисту.
func WithoutAnswered(o Outcome, answered map[string]bool) Outcome {
	if o.Status != domain.ApplicationWaitingForInfo || len(answered) == 0 {
		return o
	}
	var left []Question
	for _, q := range o.Questions {
		if !answered[q.Code] {
			left = append(left, q)
		}
	}
	o.Questions = left
	if len(left) == 0 {
		o.Status, o.AutoOutcome = domain.ApplicationManualReview, "MANUAL_REVIEW"
		o.Message = "Заявку рассматривает кредитный специалист"
	}
	return o
}

// Annuity — аннуитетный платёж, ₽.
func Annuity(amount int64, ratePct float64, months int) int64 {
	if months <= 0 {
		return 0
	}
	r := ratePct / 100 / 12
	if r == 0 {
		return int64(math.Round(float64(amount) / float64(months)))
	}
	k := r * math.Pow(1+r, float64(months)) / (math.Pow(1+r, float64(months)) - 1)
	return int64(math.Round(float64(amount) * k))
}

// findings — расхождения в данных (docs/data-collection.md): для андеррайтера, клиенту не показываются.
func (f Facts) findings() []Finding {
	var out []Finding
	if f.DeclaredRevenue > 0 && f.AnnualReceipts > 0 {
		diff := math.Abs(float64(f.AnnualReceipts-f.DeclaredRevenue)) / float64(f.DeclaredRevenue)
		if diff > 0.2 {
			out = append(out, Finding{Code: "REVENUE_MISMATCH", Material: diff > 0.3, Source: f.DeclaredSource,
				Message:  fmt.Sprintf("Поступления по счёту за 12 месяцев расходятся с выручкой в отчётности на %.0f %%", diff*100),
				Declared: rubles(f.DeclaredRevenue), Found: rubles(f.AnnualReceipts)})
		}
	}
	var undeclared []string
	for _, c := range f.DebtCounterparts {
		if c == "" || strings.Contains(strings.ToUpper(c), "АЛЬФА-БАНК") {
			continue
		}
		if !slices.ContainsFunc(f.BureauCreditors, func(b string) bool { return sameName(b, c) }) {
			undeclared = append(undeclared, c)
		}
	}
	if len(undeclared) > 0 && len(f.BureauCreditors) > 0 {
		out = append(out, Finding{Code: "UNDECLARED_DEBT", Material: true, Source: "CORE_BANKING",
			Message: "В выписке есть платежи по долгам, которых нет в кредитной истории: " + strings.Join(undeclared, ", ")})
	}
	if f.DirectorInEGRUL != nil && !*f.DirectorInEGRUL {
		out = append(out, Finding{Code: "DIRECTOR_MISMATCH", Material: true, Source: "FNS",
			Message: "Руководитель из анкеты не совпадает с руководителем по ЕГРЮЛ"})
	}
	if !f.AuthorityVerified {
		out = append(out, Finding{Code: "NO_POA", Material: true, Source: "M4D",
			Message: "Полномочия представителя не подтверждены: нет действующей МЧД или руководитель не совпал"})
	}
	return out
}

func sameName(a, b string) bool {
	norm := func(s string) string {
		s = strings.ToUpper(s)
		for _, w := range []string{"ООО", "АО", "ПАО", "\"", "«", "»"} {
			s = strings.ReplaceAll(s, w, "")
		}
		return strings.TrimSpace(s)
	}
	return norm(a) == norm(b)
}

func rubles(v int64) string { return fmt.Sprintf("%d ₽", v) }
