import type { components } from './schema';

type Schemas = components['schemas'];

export type CollectorApplication = Schemas['Application'];
export type CollectorApplicant = Schemas['Applicant'];
export type CollectorDocument = Schemas['Document'] & { info_request_id?: string; generation?: number };
export type CollectionTask = Schemas['CollectionTask'];
export type CollectionSource = Schemas['CollectionSource'];
export type CollectorSystem = NonNullable<CollectionSource['source']>;
export type CollectorStatus = NonNullable<CollectorApplication['status']>;
export type CollectorRole = Schemas['RepresentativeInput']['role'];
export type CollectorDecision = Schemas['ClientDecision'];
export type CollectorOffer = Schemas['Offer'];
export type CollectorInfoRequest = Schemas['InfoRequest'];
export type RegisterInput = Schemas['RegisterApplicantRequest'];
export type QuestionnaireSubmitted = Schemas['QuestionnaireSubmitted'];
export type CollectorProblem = Schemas['Problem'];
export type CollectorPurpose = NonNullable<Schemas['Purpose']['code']>;

export type MaritalStatus = 'SINGLE' | 'MARRIED' | 'DIVORCED' | 'DIVORCED_MULTIPLE';
export type TaxScope = 'DECLARATIONS' | 'ENS_BALANCE';
export type Marketplace = 'wildberries' | 'ozon';

/** Типы документов анкеты (DocumentSpec.type, без SIGNATURE — у подписи тип всегда SIGNATURE). */
export type CollectorDocumentType =
    | 'FINANCIAL_STATEMENTS'
    | 'TAX_RETURN'
    | 'BANK_STATEMENT'
    | 'MANAGEMENT_REPORT'
    | 'CHARTER'
    | 'POWER_OF_ATTORNEY'
    | 'DIRECTOR_PASSPORT'
    | 'COLLATERAL_TITLE'
    | 'EGRN_EXTRACT'
    | 'LEASE_AGREEMENT'
    | 'CONTRACT_WITH_CUSTOMER'
    | 'LOAN_AGREEMENT_DRAFT'
    | 'OTHER';

export type Consent = { id: string; signed_at?: string; valid_until: string; scope?: TaxScope[] };

export type Person = { inn: string; last_name: string; first_name: string; middle_name?: string; birth_date: string };

/**
 * Часть `questionnaire` в POST /applications/{id}/questionnaire. В swagger она описана строкой,
 * поэтому тип — по DTO data-collector (internal/transport/http/dto.go, questionnaireRequest).
 */
export type QuestionnaireInput = {
    age: number;
    marital_status: MaritalStatus;
    children_count: number;
    product_code?: string;
    requested?: { amount: number; term_months: number };
    purpose?: { code: CollectorPurpose; description?: string };
    documents?: { file_name: string; type: CollectorDocumentType }[];
    director?: Person;
    consents?: { bki_company?: Consent; bki_person?: Consent; tax?: Consent };
    other_bank_biks?: string[];
    marketplace_tokens?: Partial<Record<Marketplace, string>>;
    ofd_token?: string;
    collateral?: { cadastral_numbers?: string[]; vins?: string[]; serials?: string[] };
    poa_number?: string;
    representative_inn?: string;
    scoring?: ScoringAnswers;
};

/** Ответы для оценки credit-engine (блок scoring анкеты; dto.go scoringRequest). Коды — как у движка. */
export type ScoringAnswers = {
    max_annual_rate?: number;
    min_acceptable_amount?: number;
    max_acceptable_term_months?: number;
    need_cost?: number;
    own_contribution?: number;
    free_cash?: number;
    minimum_cash?: number;
    need_evidence?: 'CONTRACT_AND_COST' | 'VERIFIED_ESTIMATE' | 'ORAL_ONLY';
    experience_years?: number;
    primary_income_source?: 'BUSINESS' | 'PUBLIC_EMPLOYMENT' | 'PRIVATE_EMPLOYMENT' | 'PENSION' | 'SELF_EMPLOYED_OR_GPH' | 'NONE';
    qualification?: 'NONE' | 'SUPPORT' | 'SPECIALIST' | 'MANAGER_OR_HIGHLY_QUALIFIED';
    total_experience_months?: number;
    net_monthly_income?: number;
    car_class?: 'NONE' | 'ECONOMY' | 'COMFORT' | 'PREMIUM';
    education_level?: 'BELOW_GENERAL' | 'GENERAL' | 'VOCATIONAL' | 'HIGHER' | 'ACADEMIC_DEGREE';
    real_estate_status?: 'UNENCUMBERED_WHOLE' | 'UNENCUMBERED_SHARE' | 'ENCUMBERED_ONLY' | 'NONE';
    business_location?: 'VERIFIED_PHYSICAL' | 'VERIFIED_ONLINE_OR_MOBILE' | 'UNVERIFIED';
    location_stability_months?: number;
    buyer_contracts?: 'ACTIVE_AND_PERFORMED' | 'REPEAT_ORDERS' | 'PLANS_ONLY';
};

export const COLLECTOR_ROLES: CollectorRole[] = [
    'DIRECTOR',
    'OWNER',
    'CFO',
    'ACCOUNTANT',
    'FINANCE_EMPLOYEE',
    'BANKING_EMPLOYEE',
    'AUTHORIZED_EMPLOYEE',
];

export const COLLECTOR_ROLE_LABELS: Record<CollectorRole, string> = {
    DIRECTOR: 'Генеральный директор',
    OWNER: 'Собственник бизнеса',
    CFO: 'Финансовый директор',
    ACCOUNTANT: 'Бухгалтер',
    FINANCE_EMPLOYEE: 'Отвечаю за финансирование',
    BANKING_EMPLOYEE: 'Работаю с банковскими продуктами',
    AUTHORIZED_EMPLOYEE: 'Другой уполномоченный сотрудник',
};

/** Может подписать договор без доверенности (can_sign): директор, у ИП — сам предприниматель. */
export const signsWithoutPoa = (role: CollectorRole | undefined, soleProprietor: boolean) =>
    role === 'DIRECTOR' || (soleProprietor && role === 'OWNER');

export const MARITAL_LABELS: Record<MaritalStatus, string> = {
    SINGLE: 'Не в браке',
    MARRIED: 'В браке',
    DIVORCED: 'В разводе',
    DIVORCED_MULTIPLE: 'В разводе больше одного раза',
};

export const SYSTEM_LABELS: Record<CollectorSystem, string> = {
    CORE_BANKING: 'Счета в Альфа-Банке',
    FNS: 'ФНС: ЕГРЮЛ, отчётность, блокировки',
    BKI: 'Кредитная история (БКИ)',
    COMPLIANCE: 'Проверка ЦБ и санкции',
    FSSP: 'Исполнительные производства',
    KAD: 'Арбитражные суды',
    FEDRESURS: 'Федресурс',
    M4D: 'Реестр доверенностей',
    TAX_REPORTS: 'Налоговая отчётность',
    PLEDGE_REGISTRY: 'Реестр залогов',
    EGRN: 'ЕГРН',
    GOSUSLUGI: 'Госуслуги',
    OPEN_BANKING: 'Счета в других банках',
    MARKETPLACES: 'Маркетплейсы',
    OFD: 'Онлайн-кассы (ОФД)',
    ZAKUPKI: 'Госзакупки',
    SIGNALS: 'Проверки, товарные знаки, новости',
    CBR: 'Ключевая ставка ЦБ',
    HH: 'Вакансии на hh.ru',
};
