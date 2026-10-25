package scoring

func routingMessage(code string) string {
	switch code {
	case "INACTIVE_BORROWER":
		return "Подтверждено прекращение деятельности заёмщика."
	case "BANKRUPT":
		return "Подтверждено банкротство заёмщика."
	case "MATERIAL_FORGERY":
		return "Подтверждена существенная подделка документов."
	case "CREDIT_PROHIBITED":
		return "Подтверждён запрет кредитования."
	case "LIQUIDATED_OR_EXCLUDED":
		return "Подтверждена ликвидация или исключение организации из реестра."
	case "CURRENT_DPD_30_PLUS":
		return "Текущая просрочка составляет не менее 30 дней."
	case "WORST_REPAID_DPD_90_PLUS":
		return "Погашенная просрочка за последние 24 месяца достигала 90 дней."
	case "UNEXPLAINED_DIFFERENCE":
		return "Необъяснённое расхождение источников превышает 5% поступлений."
	case "NONSTANDARD_PURPOSE":
		return "Заявлена цель, отличная от развития бизнеса."
	case "NO_UNCOVERED_NEED":
		return "Подтверждённая потребность полностью покрыта собственным вкладом."
	case "INSUFFICIENT_FREE_CASH":
		return "Свободные деньги ниже необходимого минимального остатка."
	case "NO_CREDIT_HISTORY":
		return "Нет подтверждённой кредитной истории."
	case "BUSINESS_AGE_UNDER_12_MONTHS":
		return "Бизнес действует менее 12 месяцев."
	case "CURRENT_DPD_1_29":
		return "Текущая просрочка составляет от 1 до 29 дней."
	case "MANY_CREDIT_APPLICATIONS":
		return "За 90 дней было не менее восьми кредитных обращений."
	case "MATERIAL_ACCOUNT_RESTRICTION":
		return "Подтверждено существенное ограничение банковского счёта."
	case "MATERIAL_DISPUTE_OR_ENFORCEMENT":
		return "Подтверждён существенный спор или исполнительное взыскание."
	case "KEY_BUYER_LOSS":
		return "Подтверждена потеря ключевого покупателя."
	case "STRUCTURAL_BUSINESS_CHANGE":
		return "Подтверждено существенное структурное изменение бизнеса."
	case "OVERDUE_TAXES_OVER_10_PERCENT_CFADS":
		return "Просроченные налоги превышают 10% годового CFADS."
	case "FEWER_THAN_8_POSITIVE_CFADS_MONTHS":
		return "Положительный CFADS получен менее чем в восьми месяцах."
	case "MATERIAL_REGISTER_INACCURACY":
		return "Подтверждена существенная недостоверность сведений ЕГРЮЛ."
	case "MATERIAL_AUDITOR_MODIFICATION":
		return "Аудиторское заключение содержит существенную модификацию."
	case "NON_OPERATING_NONFINANCIAL_SEGMENT":
		return "Организация не относится к операционным нефинансовым компаниям."
	case "SPECIAL_IP_E07", "SPECIAL_IP_E08", "SPECIAL_LE_E15":
		return "Подтверждённая база сравнения поступлений или выручки равна нулю."
	case "SPECIAL_IP_E12":
		return "Подтверждённая стоимость потребности равна нулю."
	case "SPECIAL_IP_E13":
		return "Поступления не превышают переменные выплаты; запас прочности не рассчитывается."
	case "SPECIAL_LE_E04", "SPECIAL_LE_E05", "SPECIAL_LE_E13", "SPECIAL_LE_E16":
		return "Активы или средние активы равны нулю; показатель не рассчитывается."
	case "SPECIAL_LE_E06":
		return "Оборотные активы равны нулю; показатель не рассчитывается."
	case "SPECIAL_LE_E07", "SPECIAL_LE_E08":
		return "Прибыль от продаж неположительна; долговое покрытие не рассчитывается."
	case "SPECIAL_LE_E10", "SPECIAL_LE_E11", "SPECIAL_LE_E12", "SPECIAL_LE_E17":
		return "Годовая выручка равна нулю; показатель не рассчитывается."
	case "SPECIAL_LE_E14":
		return "Текущий или средний капитал неположителен; ROE не интерпретируется."
	case "SPECIAL_LE_E18":
		return "Себестоимость равна нулю при положительных средних запасах."
	default:
		return "Показатель требует проверки сотрудником."
	}
}
