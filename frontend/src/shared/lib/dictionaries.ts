import type {
    ApplicationStatus,
    ClientStage,
    CollateralItem,
    CollateralType,
    DeclineReason,
    DocumentType,
    ExistingDebt,
    Guarantor,
    PurposeCode,
    RepresentativeRole,
} from '../../api/types';

export const ROLE_LABELS: Record<RepresentativeRole, string> = {
    DIRECTOR: 'Директор',
    OWNER: 'Собственник',
    CFO: 'Финансовый директор',
    ACCOUNTANT: 'Бухгалтер',
    AUTHORIZED_EMPLOYEE: 'Сотрудник',
};

type StatusColor = 'green' | 'orange' | 'red' | 'blue' | 'grey';

/** Клиентские этапы из docs/fsm.md: клиент не видит внутренние статусы конвейера. */
export const STAGE_BADGES: Record<ClientStage, { label: string; color: StatusColor; contrast?: boolean }> = {
    FILLING: { label: 'Черновик', color: 'grey' },
    REVIEW: { label: 'Рассматриваем', color: 'blue' },
    ACTION_REQUIRED: { label: 'Нужно уточнить', color: 'orange' },
    OFFER: { label: 'Решение готово', color: 'green' },
    SIGNING: { label: 'Подписание договора', color: 'grey', contrast: true },
    DONE: { label: 'Кредит выдан', color: 'green' },
    CLOSED: { label: 'Заявка закрыта', color: 'grey' },
};

/** Уточнение для закрытых заявок — почему закрыта. */
export const CLOSED_REASONS: Partial<Record<ApplicationStatus, string>> = {
    REJECTED: 'Банк отказал',
    DECLINED: 'Вы отказались от предложения',
    CANCELLED: 'Заявка отозвана',
    EXPIRED: 'Истёк срок',
};

export const ACTIVE_STAGES: ClientStage[] = ['FILLING', 'REVIEW', 'ACTION_REQUIRED', 'OFFER', 'SIGNING'];

/** Этапы, по которым на главной нужен лёгкий /status (вопросы банка, кто подписывает, решение). */
export const STAGES_WITH_TASKS: ClientStage[] = ['ACTION_REQUIRED', 'OFFER', 'SIGNING'];

/** Формулировки задач клиента — как в чипсах экрана подбора. */
export const PURPOSE_LABELS: Record<PurposeCode, string> = {
    WORKING_CAPITAL: 'Пополнить оборот',
    EQUIPMENT: 'Купить оборудование',
    VEHICLE: 'Купить транспорт',
    REAL_ESTATE: 'Недвижимость',
    REFINANCING: 'Рефинансировать кредит',
    BUSINESS_EXPANSION: 'Расширить бизнес',
    TENDER: 'Исполнить госконтракт',
    OTHER: 'Другое',
};

export const PURPOSES = Object.keys(PURPOSE_LABELS) as PurposeCode[];

export const COLLATERAL_LABELS: Record<CollateralType, string> = {
    REAL_ESTATE: 'Недвижимость',
    VEHICLE: 'Транспорт',
    EQUIPMENT: 'Оборудование',
    GOODS_IN_TURNOVER: 'Товары в обороте',
    DEPOSIT: 'Депозит',
    RECEIVABLES: 'Дебиторская задолженность',
};

export const COLLATERAL_OWNER_LABELS: Record<NonNullable<CollateralItem['owner']>, string> = {
    APPLICANT: 'Компания',
    FOUNDER: 'Учредитель',
    THIRD_PARTY: 'Третье лицо',
};

export const DEBT_KIND_LABELS: Record<ExistingDebt['kind'], string> = {
    LOAN: 'Кредит',
    CREDIT_LINE: 'Кредитная линия',
    OVERDRAFT: 'Овердрафт',
    LEASING: 'Лизинг',
    GUARANTEE: 'Банковская гарантия',
    OTHER: 'Другое',
};

export const GUARANTOR_TYPE_LABELS: Record<Guarantor['type'], string> = {
    INDIVIDUAL: 'Физическое лицо',
    LEGAL_ENTITY: 'Компания',
};

export const GUARANTOR_RELATION_LABELS: Record<NonNullable<Guarantor['relation']>, string> = {
    FOUNDER: 'Учредитель',
    DIRECTOR: 'Директор',
    SPOUSE_OF_OWNER: 'Супруг(а) владельца',
    AFFILIATED_COMPANY: 'Связанная компания',
    OTHER: 'Другое',
};

export const DOCUMENT_LABELS: Record<DocumentType, string> = {
    FINANCIAL_STATEMENTS: 'Бухгалтерская отчётность',
    TAX_RETURN: 'Налоговая декларация',
    BANK_STATEMENT: 'Выписка по счёту из другого банка',
    MANAGEMENT_REPORT: 'Управленческая отчётность',
    CHARTER: 'Устав',
    POWER_OF_ATTORNEY: 'Доверенность',
    DIRECTOR_PASSPORT: 'Паспорт директора',
    COLLATERAL_TITLE: 'Документы на залог',
    EGRN_EXTRACT: 'Выписка из ЕГРН',
    LEASE_AGREEMENT: 'Договор аренды',
    CONTRACT_WITH_CUSTOMER: 'Договор с поставщиком или покупателем',
    LOAN_AGREEMENT_DRAFT: 'Кредитный договор',
    SIGNATURE: 'Электронная подпись',
    OTHER: 'Другой документ',
};

export const BANKS = ['Альфа-Банк', 'Сбербанк', 'ВТБ', 'Т-Банк', 'Газпромбанк', 'Другой банк'];

/** Названия полей из completeness.missing_fields и Problem.errors[].field. */
export const FIELD_LABELS: Record<string, string> = {
    'requested.amount': 'сумма кредита',
    'requested.term_months': 'срок кредита',
    'purpose.code': 'цель кредита',
    'financials.annual_revenue': 'выручка за год',
    'financials.net_profit': 'чистая прибыль',
    'financials.avg_monthly_turnover': 'поступления на счета',
    'financials.avg_monthly_expenses': 'регулярные расходы',
    'financials.employees_count': 'число сотрудников',
    debts: 'действующие кредиты — или отметка, что их нет',
    collateral: 'залог',
    guarantors: 'поручитель',
    'consents.personal_data': 'согласие на обработку персональных данных',
    'consents.credit_bureau': 'согласие на запрос в бюро кредитных историй',
};

/** «guarantors[0].inn» → «поручитель»: для элементов массивов берём название списка. */
export const fieldLabel = (field: string) => FIELD_LABELS[field] ?? FIELD_LABELS[field.replace(/\[\d+\].*$/, '')] ?? field;

export const DECLINE_REASON_LABELS: Record<DeclineReason, string> = {
    RATE_TOO_HIGH: 'Высокая ставка',
    AMOUNT_TOO_LOW: 'Нужна сумма больше',
    TERM_NOT_SUITABLE: 'Не подходит срок',
    COLLATERAL_REQUIRED: 'Не хочу оформлять залог или поручительство',
    FOUND_ELSEWHERE: 'Нашёл кредит в другом банке',
    NO_LONGER_NEEDED: 'Кредит больше не нужен',
    OTHER: 'Другое',
};
