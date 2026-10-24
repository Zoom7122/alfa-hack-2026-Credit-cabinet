import type {
    Applicant,
    ApplicationInput,
    ApplicationStatus,
    ApplicationSummary,
    CounterpartyPrefill,
    Product,
    TestUser,
    UserRef,
} from '../api/types';
import { stageOf } from './fsm';

type RequiredDocument = NonNullable<Product['required_documents']>[number];

/* Синтетические данные для демо. Совпадают с персонажами из макетов. */

const romashka = { applicant_id: 'apl_romashka', short_name: 'ООО «Ромашка»', inn: '7701234567' };
const smirnov = { applicant_id: 'apl_smirnov', short_name: 'ИП Смирнов К. А.', inn: '771234567890' };
const vector = { applicant_id: 'apl_vector', short_name: 'ООО «Вектор Строй»', inn: '7724567890' };

export const testUsers: TestUser[] = [
    {
        id: 'usr_director_romashka',
        full_name: 'Петров Пётр Петрович',
        roles: ['CLIENT'],
        persona: 'Директор ООО «Ромашка»: хорошая история, авто-одобрение, сам подписывает договор',
        companies: [{ ...romashka, role: 'DIRECTOR', can_sign: true }],
    },
    {
        id: 'usr_accountant_romashka',
        full_name: 'Иванова Анна Сергеевна',
        roles: ['CLIENT'],
        persona: 'Бухгалтер ООО «Ромашка»: подаёт заявку, банк запросит доверенность, подписывает директор',
        companies: [{ ...romashka, role: 'ACCOUNTANT', can_sign: false }],
    },
    {
        id: 'usr_ip_smirnov',
        full_name: 'Смирнов Кирилл Андреевич',
        roles: ['CLIENT'],
        persona: 'ИП, кофейня 1,5 года: расхождение по выручке, ручное рассмотрение',
        companies: [{ ...smirnov, role: 'DIRECTOR', can_sign: true }],
    },
    {
        id: 'usr_director_vector',
        full_name: 'Лебедева Вера Олеговна',
        roles: ['CLIENT'],
        persona: 'Директор ООО «Вектор Строй»: отказ по оборудованию с объяснением, готовое предложение по транспорту',
        companies: [{ ...vector, role: 'DIRECTOR', can_sign: true }],
    },
    {
        id: 'usr_underwriter_sokolova',
        full_name: 'Соколова Мария Игоревна',
        roles: ['UNDERWRITER'],
        persona: 'Андеррайтер: очередь ручного рассмотрения, досье и решение по заявке',
        companies: [],
    },
];

export const applicantSeeds: Applicant[] = [
    {
        id: 'apl_romashka',
        inn: '7701234567',
        applicant_type: 'LEGAL_ENTITY',
        ogrn: '1027700000000',
        kpp: '770101001',
        full_name: 'Общество с ограниченной ответственностью «Ромашка»',
        short_name: 'ООО «Ромашка»',
        registration_date: '2019-03-14',
        legal_address: { full: 'г. Москва, ул. Тверская, д. 7, оф. 12', region_code: '77' },
        okved_main: { code: '47.11', title: 'Розничная торговля' },
        director_name: 'Петров Пётр Петрович',
        registry_status: 'ACTIVE',
        my_role: 'DIRECTOR',
        can_sign: true,
    },
    {
        id: 'apl_smirnov',
        inn: '771234567890',
        applicant_type: 'SOLE_PROPRIETOR',
        ogrn: '323774600012345',
        full_name: 'Индивидуальный предприниматель Смирнов Кирилл Андреевич',
        short_name: 'ИП Смирнов К. А.',
        registration_date: '2025-04-02',
        okved_main: { code: '56.30', title: 'Подача напитков' },
        director_name: 'Смирнов Кирилл Андреевич',
        registry_status: 'ACTIVE',
        my_role: 'DIRECTOR',
        can_sign: true,
    },
    {
        id: 'apl_vector',
        inn: '7724567890',
        applicant_type: 'LEGAL_ENTITY',
        ogrn: '1157746000000',
        full_name: 'Общество с ограниченной ответственностью «Вектор Строй»',
        short_name: 'ООО «Вектор Строй»',
        registration_date: '2015-06-20',
        okved_main: { code: '41.20', title: 'Строительство зданий' },
        director_name: 'Лебедева Вера Олеговна',
        registry_status: 'ACTIVE',
        my_role: 'DIRECTOR',
        can_sign: true,
    },
];

/** Поля, без которых заявку не отправить (dot-path ApplicationInput). Залог и поручители — по условиям продукта. */
const REQUIRED_FIELDS = [
    'requested.amount',
    'requested.term_months',
    'purpose.code',
    'financials.annual_revenue',
    'financials.net_profit',
    'financials.avg_monthly_turnover',
    'financials.avg_monthly_expenses',
    'financials.employees_count',
    'debts',
    'consents.personal_data',
    'consents.credit_bureau',
];

const BASE_DOCUMENTS: RequiredDocument[] = [
    { type: 'FINANCIAL_STATEMENTS' },
    { type: 'BANK_STATEMENT' },
];

export const products: Product[] = [
    {
        code: 'WORKING_CAPITAL',
        name: 'Оборотный кредит',
        description: 'На закупку товара, сырья, выплату зарплат и налогов',
        applicant_types: ['LEGAL_ENTITY', 'SOLE_PROPRIETOR'],
        purposes: ['WORKING_CAPITAL', 'TENDER', 'BUSINESS_EXPANSION', 'OTHER'],
        amount: { min: 300_000, max: 30_000_000 },
        term_months: { min: 3, max: 36 },
        rate_annual_from: 18.5,
        collateral: { required_above_amount: 10_000_000, accepted_types: ['REAL_ESTATE', 'GOODS_IN_TURNOVER', 'DEPOSIT'] },
        guarantee_required: false,
        required_fields: REQUIRED_FIELDS,
        required_documents: [...BASE_DOCUMENTS, { type: 'COLLATERAL_TITLE', condition: 'amount > 10000000' }],
    },
    {
        code: 'CREDIT_LINE',
        name: 'Кредитная линия',
        description: 'Берёте деньги частями, когда нужно, проценты — только на использованную сумму',
        applicant_types: ['LEGAL_ENTITY'],
        purposes: ['WORKING_CAPITAL', 'TENDER', 'OTHER'],
        amount: { min: 1_000_000, max: 50_000_000 },
        term_months: { min: 6, max: 24 },
        rate_annual_from: 19.9,
        collateral: { required_above_amount: 15_000_000, accepted_types: ['REAL_ESTATE', 'RECEIVABLES', 'DEPOSIT'] },
        guarantee_required: true,
        required_fields: REQUIRED_FIELDS,
        required_documents: [...BASE_DOCUMENTS, { type: 'COLLATERAL_TITLE', condition: 'amount > 15000000' }],
    },
    {
        code: 'EXPRESS',
        name: 'Экспресс-кредит без залога',
        description: 'Решение за 5 минут по данным о компании, без документов',
        applicant_types: ['LEGAL_ENTITY', 'SOLE_PROPRIETOR'],
        purposes: ['WORKING_CAPITAL', 'EQUIPMENT', 'BUSINESS_EXPANSION', 'OTHER'],
        amount: { min: 100_000, max: 5_000_000 },
        term_months: { min: 3, max: 24 },
        rate_annual_from: 23,
        collateral: { required_above_amount: null, accepted_types: [] },
        guarantee_required: false,
        required_fields: REQUIRED_FIELDS,
        required_documents: [],
    },
    {
        code: 'VEHICLE',
        name: 'Кредит на транспорт',
        description: 'Покупка грузового и легкового транспорта для бизнеса',
        applicant_types: ['LEGAL_ENTITY', 'SOLE_PROPRIETOR'],
        purposes: ['VEHICLE'],
        amount: { min: 500_000, max: 20_000_000 },
        term_months: { min: 12, max: 60 },
        rate_annual_from: 17.9,
        collateral: { required_above_amount: 0, accepted_types: ['VEHICLE'] },
        guarantee_required: false,
        required_fields: REQUIRED_FIELDS,
        required_documents: [...BASE_DOCUMENTS, { type: 'CONTRACT_WITH_CUSTOMER' }],
    },
    {
        code: 'EQUIPMENT',
        name: 'Кредит на оборудование',
        description: 'Покупка и модернизация оборудования',
        applicant_types: ['LEGAL_ENTITY', 'SOLE_PROPRIETOR'],
        purposes: ['EQUIPMENT'],
        amount: { min: 500_000, max: 50_000_000 },
        term_months: { min: 12, max: 60 },
        rate_annual_from: 18.9,
        collateral: { required_above_amount: 5_000_000, accepted_types: ['EQUIPMENT', 'REAL_ESTATE'] },
        guarantee_required: false,
        required_fields: REQUIRED_FIELDS,
        required_documents: [...BASE_DOCUMENTS, { type: 'CONTRACT_WITH_CUSTOMER' }],
    },
    {
        code: 'REAL_ESTATE',
        name: 'Коммерческая ипотека',
        description: 'Покупка офиса, склада или торгового помещения',
        applicant_types: ['LEGAL_ENTITY', 'SOLE_PROPRIETOR'],
        purposes: ['REAL_ESTATE', 'BUSINESS_EXPANSION'],
        amount: { min: 1_000_000, max: 100_000_000 },
        term_months: { min: 12, max: 120 },
        rate_annual_from: 16.9,
        collateral: { required_above_amount: 0, accepted_types: ['REAL_ESTATE'] },
        guarantee_required: true,
        required_fields: REQUIRED_FIELDS,
        required_documents: [...BASE_DOCUMENTS, { type: 'EGRN_EXTRACT' }],
    },
    {
        code: 'REFINANCING',
        name: 'Рефинансирование',
        description: 'Перенесём кредиты других банков и снизим платёж',
        applicant_types: ['LEGAL_ENTITY', 'SOLE_PROPRIETOR'],
        purposes: ['REFINANCING'],
        amount: { min: 500_000, max: 50_000_000 },
        term_months: { min: 12, max: 60 },
        rate_annual_from: 17.5,
        collateral: { required_above_amount: 10_000_000, accepted_types: ['REAL_ESTATE', 'EQUIPMENT', 'VEHICLE'] },
        guarantee_required: false,
        required_fields: REQUIRED_FIELDS,
        required_documents: [...BASE_DOCUMENTS, { type: 'LOAN_AGREEMENT_DRAFT' }],
    },
];

/** Реестр для POST /counterparty/lookup. ИНН 7700000000 имитирует таймаут реестра. */
export const registry: CounterpartyPrefill[] = [
    ...applicantSeeds.map(({ id: _id, my_role: _role, can_sign: _sign, ...prefill }) => ({ ...prefill, already_known: true })),
    {
        inn: '7707083893',
        applicant_type: 'LEGAL_ENTITY',
        ogrn: '1027700132195',
        kpp: '770701001',
        full_name: 'Общество с ограниченной ответственностью «Северный ветер»',
        short_name: 'ООО «Северный ветер»',
        registration_date: '2012-09-03',
        legal_address: { full: 'г. Москва, Ленинский пр-т, д. 30', region_code: '77' },
        okved_main: { code: '46.90', title: 'Неспециализированная оптовая торговля' },
        director_name: 'Орлов Игорь Николаевич',
        registry_status: 'ACTIVE',
        already_known: false,
    },
    {
        inn: '7709876543',
        applicant_type: 'LEGAL_ENTITY',
        ogrn: '1107746000111',
        full_name: 'Общество с ограниченной ответственностью «Закат»',
        short_name: 'ООО «Закат»',
        registration_date: '2010-02-11',
        legal_address: { full: 'г. Москва, ул. Летниковская, д. 2', region_code: '77' },
        okved_main: { code: '68.20', title: 'Аренда недвижимости' },
        director_name: 'Зайцев Олег Петрович',
        registry_status: 'LIQUIDATING',
        already_known: false,
    },
];

export const REGISTRY_TIMEOUT_INN = '7700000000';


type Seed = {
    id: string;
    number: string;
    applicant: keyof typeof applicantRefs;
    status: ApplicationStatus;
    product: string;
    amount: number;
    term: number;
    created: string;
    updated: string;
    input?: Omit<ApplicationInput, 'requested' | 'product_code'>;
    prefilledFrom?: string;
    /** Кто подал; по умолчанию — директор компании. */
    createdBy?: string;
};

const applicantRefs = {
    romashka: { id: 'apl_romashka', short_name: 'ООО «Ромашка»', inn: '7701234567' },
    smirnov: { id: 'apl_smirnov', short_name: 'ИП Смирнов К. А.', inn: '771234567890' },
    vector: { id: 'apl_vector', short_name: 'ООО «Вектор Строй»', inn: '7724567890' },
};

/** Анкета «Ромашки» из выданного в мае кредита — из неё предзаполняются новые черновики. */
const romashkaProfile = {
    financials: {
        annual_revenue: 48_200_000,
        annual_revenue_year: 2025,
        net_profit: 3_100_000,
        avg_monthly_turnover: 4_100_000,
        employees_count: 24,
        main_bank: 'Альфа-Банк',
    },
    debts: [
        {
            kind: 'LOAN',
            lender: 'Сбербанк',
            original_amount: 2_000_000,
            outstanding: 1_150_000,
            monthly_payment: 85_000,
            maturity_date: '2027-08-01',
            has_overdue: false,
        },
    ],
    has_no_debts: false,
    collateral: [],
    guarantors: [{ type: 'INDIVIDUAL', inn: '770112345695', name: 'Петров Пётр Петрович', relation: 'FOUNDER' }],
} satisfies ApplicationInput;

const seeds: Seed[] = [
    { id: 'app_000123', number: 'A-2026-000123', applicant: 'romashka', status: 'SIGNING', product: 'WORKING_CAPITAL', amount: 5_000_000, term: 24, created: '2026-10-20T09:10:00Z', updated: '2026-10-24T11:20:00Z' },
    { id: 'app_000131', number: 'A-2026-000131', applicant: 'romashka', status: 'WAITING_FOR_INFO', product: 'VEHICLE', amount: 3_200_000, term: 36, created: '2026-10-21T08:00:00Z', updated: '2026-10-23T15:40:00Z', createdBy: 'usr_accountant_romashka' },
    {
        id: 'app_000140',
        number: 'A-2026-000140',
        applicant: 'romashka',
        status: 'DRAFT',
        product: 'EQUIPMENT',
        amount: 1_800_000,
        term: 30,
        created: '2026-10-24T10:02:00Z',
        updated: '2026-10-24T10:15:00Z',
        // предзаполнено из прошлой заявки, расходы в мае не спрашивали
        input: { purpose: { code: 'EQUIPMENT', description: 'Холодильные витрины для двух магазинов' }, ...romashkaProfile },
        prefilledFrom: 'app_000088',
    },
    {
        id: 'app_000088',
        number: 'A-2026-000088',
        applicant: 'romashka',
        status: 'COMPLETED',
        product: 'WORKING_CAPITAL',
        amount: 1_000_000,
        term: 12,
        created: '2026-05-12T09:00:00Z',
        updated: '2026-05-14T13:00:00Z',
        input: { purpose: { code: 'WORKING_CAPITAL', description: 'Закупка товара под сезон' }, ...romashkaProfile },
    },
    { id: 'app_000061', number: 'A-2026-000061', applicant: 'romashka', status: 'DECLINED', product: 'EQUIPMENT', amount: 7_500_000, term: 48, created: '2026-03-03T09:00:00Z', updated: '2026-03-06T13:00:00Z' },
    { id: 'app_000147', number: 'A-2026-000147', applicant: 'smirnov', status: 'MANUAL_REVIEW', product: 'WORKING_CAPITAL', amount: 2_500_000, term: 18, created: '2026-10-24T07:30:00Z', updated: '2026-10-24T08:05:00Z' },
    { id: 'app_000152', number: 'A-2026-000152', applicant: 'vector', status: 'REJECTED', product: 'EQUIPMENT', amount: 12_000_000, term: 48, created: '2026-10-22T07:30:00Z', updated: '2026-10-22T07:41:00Z' },
    { id: 'app_000155', number: 'A-2026-000155', applicant: 'vector', status: 'OFFERED', product: 'VEHICLE', amount: 4_000_000, term: 36, created: '2026-10-23T09:00:00Z', updated: '2026-10-24T09:12:00Z' },
];

/** Заявка в хранилище мока: краткая карточка + то, что ввёл клиент. */
export type StoredApplication = ApplicationSummary &
    ApplicationInput & {
        created_by?: UserRef;
        submitted_at?: string | null;
        prefilled_from?: string | null;
    };

/** Директор компании — он подписывает договор. */
export function directorOf(applicantId: string): UserRef | undefined {
    const user = testUsers.find((u) => u.companies?.some((c) => c.applicant_id === applicantId && c.role === 'DIRECTOR'));
    return user && { id: user.id, full_name: user.full_name };
}

const userRef = (id: string): UserRef | undefined => {
    const user = testUsers.find((u) => u.id === id);
    return user && { id: user.id, full_name: user.full_name };
};

export const seedApplications: StoredApplication[] = seeds.map((s) => ({
    created_by: s.createdBy ? userRef(s.createdBy) : directorOf(applicantRefs[s.applicant].id),
    submitted_at: s.status === 'DRAFT' ? null : s.created,
    purpose: { code: products.find((p) => p.code === s.product)?.purposes?.[0] ?? 'OTHER' },
    id: s.id,
    number: s.number,
    version: 3,
    status: s.status,
    client_stage: stageOf(s.status),
    applicant: applicantRefs[s.applicant],
    product_code: s.product,
    requested: { amount: s.amount, term_months: s.term },
    created_at: s.created,
    updated_at: s.updated,
    ...s.input,
    prefilled_from: s.prefilledFrom ?? null,
}));
