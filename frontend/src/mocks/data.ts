import type {
    Applicant,
    ApplicationAction,
    ApplicationStatus,
    ApplicationStatusView,
    ApplicationSummary,
    ClientStage,
    Me,
    Product,
    TestUser,
} from '../api/types';

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

export function meFor(userId: string): Me | undefined {
    const u = testUsers.find((t) => t.id === userId);
    if (!u) return undefined;
    return {
        id: u.id,
        full_name: u.full_name,
        roles: u.roles,
        companies: u.companies ?? [],
        pending_signatures: u.id === 'usr_director_romashka' ? ['app_000123'] : [],
    };
}

export const applicants: Applicant[] = [
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

export const products: Product[] = [
    {
        code: 'WORKING_CAPITAL',
        name: 'Оборотный кредит',
        description: 'На закупку товара, сырья, выплату зарплат и налогов',
        applicant_types: ['LEGAL_ENTITY', 'SOLE_PROPRIETOR'],
        purposes: ['WORKING_CAPITAL', 'TENDER'],
        amount: { min: 300_000, max: 30_000_000 },
        term_months: { min: 3, max: 36 },
        rate_annual_from: 18.5,
        collateral: { required_above_amount: 10_000_000, accepted_types: ['REAL_ESTATE', 'GOODS_IN_TURNOVER'] },
        guarantee_required: false,
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
        collateral: { required_above_amount: 5_000_000, accepted_types: ['EQUIPMENT'] },
        guarantee_required: false,
    },
];

function stageOf(status: ApplicationStatus): ClientStage {
    switch (status) {
        case 'DRAFT':
            return 'FILLING';
        case 'WAITING_FOR_INFO':
            return 'ACTION_REQUIRED';
        case 'OFFERED':
            return 'OFFER';
        case 'SIGNING':
            return 'SIGNING';
        case 'COMPLETED':
            return 'DONE';
        case 'REJECTED':
        case 'DECLINED':
        case 'CANCELLED':
        case 'EXPIRED':
            return 'CLOSED';
        default:
            return 'REVIEW';
    }
}

type Seed = {
    id: string;
    number: string;
    applicant: 'romashka' | 'smirnov' | 'vector';
    status: ApplicationStatus;
    product: string;
    amount: number;
    term: number;
    created: string;
    updated: string;
};

const applicantRefs = {
    romashka: { id: 'apl_romashka', short_name: 'ООО «Ромашка»', inn: '7701234567' },
    smirnov: { id: 'apl_smirnov', short_name: 'ИП Смирнов К. А.', inn: '771234567890' },
    vector: { id: 'apl_vector', short_name: 'ООО «Вектор Строй»', inn: '7724567890' },
};

const seeds: Seed[] = [
    { id: 'app_000123', number: 'A-2026-000123', applicant: 'romashka', status: 'SIGNING', product: 'WORKING_CAPITAL', amount: 5_000_000, term: 24, created: '2026-10-20T09:10:00Z', updated: '2026-10-24T11:20:00Z' },
    { id: 'app_000131', number: 'A-2026-000131', applicant: 'romashka', status: 'WAITING_FOR_INFO', product: 'VEHICLE', amount: 3_200_000, term: 36, created: '2026-10-21T08:00:00Z', updated: '2026-10-23T15:40:00Z' },
    { id: 'app_000140', number: 'A-2026-000140', applicant: 'romashka', status: 'DRAFT', product: 'EQUIPMENT', amount: 1_800_000, term: 30, created: '2026-10-24T10:02:00Z', updated: '2026-10-24T10:15:00Z' },
    { id: 'app_000088', number: 'A-2026-000088', applicant: 'romashka', status: 'COMPLETED', product: 'WORKING_CAPITAL', amount: 1_000_000, term: 12, created: '2026-05-12T09:00:00Z', updated: '2026-05-14T13:00:00Z' },
    { id: 'app_000061', number: 'A-2026-000061', applicant: 'romashka', status: 'DECLINED', product: 'EQUIPMENT', amount: 7_500_000, term: 48, created: '2026-03-03T09:00:00Z', updated: '2026-03-06T13:00:00Z' },
    { id: 'app_000147', number: 'A-2026-000147', applicant: 'smirnov', status: 'MANUAL_REVIEW', product: 'WORKING_CAPITAL', amount: 2_500_000, term: 18, created: '2026-10-24T07:30:00Z', updated: '2026-10-24T08:05:00Z' },
    { id: 'app_000152', number: 'A-2026-000152', applicant: 'vector', status: 'REJECTED', product: 'EQUIPMENT', amount: 12_000_000, term: 48, created: '2026-10-22T07:30:00Z', updated: '2026-10-22T07:41:00Z' },
    { id: 'app_000155', number: 'A-2026-000155', applicant: 'vector', status: 'OFFERED', product: 'VEHICLE', amount: 4_000_000, term: 36, created: '2026-10-23T09:00:00Z', updated: '2026-10-24T09:12:00Z' },
];

export const applications: ApplicationSummary[] = seeds.map((s) => ({
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
}));

/** Тексты этапов — docs/fsm.md, раздел «Что видит клиент». */
const STAGE_TEXT: Record<ClientStage, { title: string; subtitle?: string }> = {
    FILLING: { title: 'Заполнение заявки', subtitle: 'Черновик сохраняется автоматически' },
    REVIEW: { title: 'Рассматриваем заявку', subtitle: 'Обычно это несколько минут' },
    ACTION_REQUIRED: { title: 'Нужно уточнить пару моментов', subtitle: 'Ответьте на вопросы банка' },
    OFFER: { title: 'Решение готово', subtitle: 'Выберите подходящее предложение' },
    SIGNING: { title: 'Подписание договора' },
    DONE: { title: 'Кредит выдан' },
    CLOSED: { title: 'Заявка закрыта' },
};

/** Доступные клиенту действия по статусу — таблица переходов docs/fsm.md. */
const ACTIONS: Partial<Record<ApplicationStatus, ApplicationAction[]>> = {
    DRAFT: ['EDIT', 'SUBMIT', 'CANCEL', 'UPLOAD_DOCUMENT'],
    WAITING_FOR_INFO: ['ANSWER_INFO_REQUEST', 'UPLOAD_DOCUMENT', 'CANCEL'],
    OFFERED: ['ACCEPT_OFFER', 'DECLINE_OFFERS'],
};

const SIGNER = { id: 'usr_director_romashka', full_name: 'Петров Пётр Петрович' };
const STAGE_ORDER: ClientStage[] = ['FILLING', 'REVIEW', 'OFFER', 'SIGNING', 'DONE'];
const STAGE_TITLES = ['Заявка', 'Рассмотрение', 'Решение', 'Подписание', 'Выдача'];

/** Сколько этапов заявка успела пройти до закрытия. */
function reachedBeforeClose(status: ApplicationStatus): number {
    if (status === 'DECLINED') return 2; // дошла до решения
    if (status === 'REJECTED' || status === 'EXPIRED') return 1;
    return 0;
}

/** ApplicationStatusView для конкретного пользователя (awaiting_me зависит от того, кто смотрит). */
export function statusViewFor(app: ApplicationSummary, viewerId: string): ApplicationStatusView {
    const closed = app.client_stage === 'CLOSED';
    const current = app.client_stage === 'ACTION_REQUIRED' ? 'REVIEW' : app.client_stage;
    const idx = closed ? reachedBeforeClose(app.status) : STAGE_ORDER.indexOf(current);
    const text = STAGE_TEXT[app.client_stage];
    const isSigning = app.status === 'SIGNING';
    const awaitingMe = isSigning && viewerId === SIGNER.id;

    const actions = [...(ACTIONS[app.status] ?? [])];
    if (isSigning) actions.push(awaitingMe ? 'SIGN' : 'START_SIGNING');

    return {
        id: app.id,
        version: app.version,
        status: app.status,
        client_stage: app.client_stage,
        title: text.title,
        subtitle: isSigning && !awaitingMe ? 'Ждём подписи директора' : text.subtitle,
        stages: STAGE_ORDER.map((code, i) => ({
            code,
            title: STAGE_TITLES[i] ?? code,
            state: closed ? (i <= idx ? 'DONE' : 'SKIPPED') : i < idx ? 'DONE' : i === idx ? 'CURRENT' : 'UPCOMING',
        })),
        allowed_actions: actions,
        open_info_requests: app.status === 'WAITING_FOR_INFO' ? 2 : 0,
        offers_available: app.status === 'OFFERED' ? 3 : 0,
        decision_outcome: app.status === 'REJECTED' ? 'REJECTED' : app.status === 'OFFERED' ? 'APPROVED' : null,
        signing: isSigning ? { required_signer: SIGNER, awaiting_me: awaitingMe } : null,
        deadline_at:
            app.status === 'WAITING_FOR_INFO' ? '2026-10-29T21:00:00Z' : app.status === 'OFFERED' ? '2026-10-31T21:00:00Z' : null,
        updated_at: app.updated_at,
    };
}
