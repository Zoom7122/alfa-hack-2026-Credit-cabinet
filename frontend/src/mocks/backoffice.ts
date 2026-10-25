import type {
    Applicant,
    ApplicantFull,
    BoApplicationListItem,
    EngineDecision,
    Finding,
    Offer,
    ScoreFactor,
    ScoreResult,
    SourceCode,
    SourceResult,
    UserRef,
} from '../api/types';
import type { StoredApplication } from './data';
import { annuity } from './seedWorkflow';

/*
 * Данные рабочего места банка (docs/data-collection.md): источники, расхождения, скоринг, решение движка.
 * Для заявок из макетов 16–17 — заданные профили, для остальных — детерминированно из номера заявки. Чистые функции.
 */

const HOUR = 3_600_000;
const DAY = 24 * HOUR;

export const ORLOV: UserRef = { id: 'usr_underwriter_orlov', full_name: 'Орлов Дмитрий Андреевич' };
export const SOKOLOVA: UserRef = { id: 'usr_underwriter_sokolova', full_name: 'Соколова Мария Игоревна' };

/** Что лежит в хранилище мока по заявке для бэк-офиса. */
export type StoredBo = {
    assignee: UserRef | null;
    sla_due_at: string | null;
    /** Источник перезапрошен — до этого момента RUNNING, потом DONE. */
    refresh_at?: Partial<Record<SourceCode, number>>;
    internal_comments?: { at: string; text: string }[];
};

type Grade = ScoreResult['grade'];

type Profile = {
    grade: Grade;
    total: number;
    reasons: string[];
    rule: { code: string; detail: string };
    failed?: SourceCode[];
    findings?: Finding[];
    factors?: ScoreFactor[];
    maxAmount?: { value: number; limited_by: NonNullable<ScoreResult['max_amount']>['limited_by'] };
    /** Предложения движка: доля от запрошенной суммы и надбавка к ставке. */
    proposals?: { share: number; extraRate: number; kind: Offer['kind']; guarantee?: boolean; conditions?: string[] }[];
    assignee?: UserRef | null;
    slaHours?: number;
};

const PROFILES: Record<string, Profile> = {
    app_000147: {
        grade: 'C',
        total: 412,
        reasons: ['Серая зона скоринга', 'Расхождение по выручке'],
        rule: { code: 'GRADE_C_GREY_ZONE', detail: 'Серая зона скоринга: 412 баллов (порог автоодобрения — 450)' },
        failed: ['KAD'],
        findings: [
            {
                code: 'REVENUE_MISMATCH',
                severity: 'WARN',
                field: 'financials.annual_revenue',
                declared_value: 18_400_000,
                found_value: 12_900_000,
                source: 'GIRBO',
                material: true,
                message: 'Выручка по отчётности ниже заявленной на 30%',
            },
            {
                code: 'EMPLOYEES_MISMATCH',
                severity: 'INFO',
                field: 'financials.employees_count',
                declared_value: 9,
                found_value: 7,
                source: 'PB_NALOG',
                material: false,
                message: 'Сотрудников по данным ФНС меньше',
            },
            {
                code: 'ENFORCEMENT_PROCEEDINGS',
                severity: 'INFO',
                field: null,
                declared_value: null,
                found_value: { count: 1, amount: 42_300 },
                source: 'FSSP',
                material: false,
                message: 'Одно исполнительное производство на 42 300 ₽',
            },
        ],
        factors: [
            { code: 'COMPANY_AGE', title: 'Возраст бизнеса', value: 1.5, bucket: '1,5 года', points: 40, source: 'EGRIP' },
            { code: 'CREDIT_HISTORY', title: 'Кредитная история', bucket: 'без просрочек', points: 120, source: 'BKI' },
            { code: 'DEBT_LOAD', title: 'Долговая нагрузка', value: 0.22, bucket: '22%', points: 95, source: 'DERIVED' },
            { code: 'AMOUNT_TO_REVENUE', title: 'Сумма / выручка', value: 0.19, bucket: '19%', points: 60, source: 'DERIVED' },
            { code: 'PROFITABILITY', title: 'Рентабельность', value: 0.06, bucket: '6%', points: 55, source: 'GIRBO' },
            { code: 'ENFORCEMENT', title: 'Исп. производства', value: 1, bucket: '1', points: -18, source: 'FSSP' },
            { code: 'BANK_TURNOVER', title: 'Обороты в Альфа-Банке', bucket: 'есть', points: 60, source: 'BANK_INTERNAL' },
        ],
        maxAmount: { value: 2_100_000, limited_by: 'REVENUE' },
        proposals: [
            { share: 0.84, extraRate: 3, kind: 'ALTERNATIVE' },
            { share: 1, extraRate: 4.4, kind: 'REQUESTED', guarantee: true, conditions: ['Обороты по счёту в Альфа-Банке — от 30% выручки'] },
        ],
        assignee: null,
        slaHours: 1 + 20 / 60,
    },
    app_000145: {
        grade: 'B',
        total: 468,
        reasons: ['БКИ недоступно, сумма выше лимита без БКИ'],
        rule: { code: 'CRITICAL_SOURCE_MISSING', detail: 'Источник BKI недоступен, запрошено больше лимита без БКИ (5 000 000 ₽)' },
        failed: ['BKI'],
        assignee: null,
        slaHours: 3 + 5 / 60,
    },
    app_000142: {
        grade: 'D',
        total: 318,
        reasons: ['Грейд D, сумма больше 3 млн'],
        rule: { code: 'GRADE_D_OVER_3M', detail: 'Грейд D: автоодобрение только до 3 000 000 ₽' },
        assignee: SOKOLOVA,
        slaHours: 6,
    },
    app_000139: {
        grade: 'C',
        total: 405,
        reasons: ['Отчётность недоступна, сумма выше порога'],
        rule: { code: 'CRITICAL_SOURCE_MISSING', detail: 'Источник GIRBO недоступен, сумма выше порога без отчётности' },
        failed: ['GIRBO'],
        assignee: ORLOV,
        slaHours: 24,
    },
    app_000136: {
        grade: 'C',
        total: 398,
        reasons: ['Бизнесу меньше 1 года'],
        rule: { code: 'COMPANY_AGE_UNDER_1Y', detail: 'Возраст бизнеса меньше 12 месяцев' },
        assignee: SOKOLOVA,
        slaHours: 28,
    },
};

/** Профиль по умолчанию: грейд и баллы из номера заявки — стабильно между перезагрузками. */
function profileOf(app: StoredApplication): Profile {
    const preset = PROFILES[app.id];
    if (preset) return preset;
    const n = Number(app.number.slice(-3));
    const grades: Grade[] = ['A', 'B', 'B', 'C'];
    const grade = grades[n % grades.length] ?? 'B';
    const total = { A: 560, B: 480, C: 420, D: 330, E: 250 }[grade] + (n % 17);
    return {
        grade,
        total,
        reasons: ['Серая зона скоринга'],
        rule: { code: 'GRADE_GREY_ZONE', detail: `Скоринг ${total} баллов` },
    };
}

/** Начальное состояние бэк-офиса для сидов (исполнитель и SLA из профилей). */
export function seedBackoffice(apps: StoredApplication[], now: number): Record<string, StoredBo> {
    const bo: Record<string, StoredBo> = {};
    for (const app of apps) {
        const p = PROFILES[app.id];
        if (!p || app.status !== 'MANUAL_REVIEW') continue;
        bo[app.id] = { assignee: p.assignee ?? null, sla_due_at: new Date(now + (p.slaHours ?? 4) * HOUR).toISOString() };
    }
    return bo;
}

const ORDER = ['SUBMITTED', 'ENRICHING', 'VERIFYING', 'SCORING', 'DECIDING'];
/** Насколько далеко зашла заявка по конвейеру: 0 — ещё ничего не собрано. */
function stage(app: StoredApplication) {
    const i = ORDER.indexOf(app.status);
    if (app.status === 'DRAFT') return -1;
    return i === -1 ? ORDER.length : i;
}

const SOURCE_ORDER: { code: SourceCode; criticality: SourceResult['criticality'] }[] = [
    { code: 'EGRUL', criticality: 'CRITICAL' },
    { code: 'EGRIP', criticality: 'CRITICAL' },
    { code: 'BKI', criticality: 'CRITICAL' },
    { code: 'GIRBO', criticality: 'CRITICAL' },
    { code: 'PB_NALOG', criticality: 'NORMAL' },
    { code: 'FSSP', criticality: 'NORMAL' },
    { code: 'FEDRESURS', criticality: 'NORMAL' },
    { code: 'KAD', criticality: 'NORMAL' },
    { code: 'RNP', criticality: 'NORMAL' },
    { code: 'EGRN', criticality: 'NORMAL' },
    { code: 'BANK_INTERNAL', criticality: 'NORMAL' },
];

const FAIL_ERRORS: Partial<Record<SourceCode, string>> = {
    BKI: 'Таймаут ответа БКИ (3 попытки)',
    GIRBO: 'ГИР БО вернул 503',
    KAD: 'Картотека арбитражных дел не ответила',
};

export function sourcesFor(app: StoredApplication, applicant: Applicant | undefined, bo: StoredBo | undefined, now: number): SourceResult[] {
    const reached = stage(app);
    const soleProprietor = applicant?.applicant_type === 'SOLE_PROPRIETOR';
    const failed = new Set(profileOf(app).failed ?? []);
    const fetchedAt = new Date(new Date(app.created_at).getTime() + 2 * 60_000).toISOString();
    const hasRealEstate = app.collateral?.some((c) => c.type === 'REAL_ESTATE');
    return SOURCE_ORDER.filter(({ code }) => (soleProprietor ? code !== 'EGRUL' : code !== 'EGRIP')).map(({ code, criticality }) => {
        const refreshAt = bo?.refresh_at?.[code];
        if (refreshAt && now < refreshAt) return { code, criticality, state: 'RUNNING', mode: 'LIVE', attempts: 1, fetched_at: null };
        if (code === 'EGRN' && !hasRealEstate) return { code, criticality, state: 'SKIPPED', skip_reason: 'Нет залога недвижимости', fetched_at: null };
        if (reached < 1) return { code, criticality, state: 'PENDING', fetched_at: null };
        if (reached === 1 && !refreshAt) return { code, criticality, state: 'RUNNING', mode: 'MOCK', attempts: 1, fetched_at: null };
        if (failed.has(code) && !refreshAt) {
            return { code, criticality, state: 'FAILED', mode: 'LIVE', attempts: 3, fetched_at: null, error: FAIL_ERRORS[code] ?? 'Источник не ответил' };
        }
        const fromCache = code === 'EGRIP' || code === 'EGRUL';
        return {
            code,
            criticality,
            state: 'DONE',
            mode: refreshAt ? 'LIVE' : fromCache ? 'CACHE' : 'MOCK',
            attempts: 1,
            fetched_at: refreshAt ? new Date(refreshAt).toISOString() : fromCache ? new Date(new Date(fetchedAt).getTime() - 2 * DAY).toISOString() : fetchedAt,
            snapshot_id: `snap_${app.id}_${code.toLowerCase()}`,
            error: null,
        };
    });
}

export const findingsFor = (app: StoredApplication): Finding[] => (stage(app) >= 3 ? (profileOf(app).findings ?? []) : []);

const DEFAULT_FACTORS = (app: StoredApplication): ScoreFactor[] => [
    { code: 'COMPANY_AGE', title: 'Возраст бизнеса', bucket: '3–5 лет', points: 80, source: 'EGRUL' },
    { code: 'CREDIT_HISTORY', title: 'Кредитная история', bucket: 'без просрочек', points: 120, source: 'BKI' },
    { code: 'DEBT_LOAD', title: 'Долговая нагрузка', bucket: '30%', points: 70, source: 'DERIVED' },
    { code: 'AMOUNT_TO_REVENUE', title: 'Сумма / выручка', bucket: app.requested && app.requested.amount > 5_000_000 ? '35%' : '15%', points: 50, source: 'DERIVED' },
    { code: 'PROFITABILITY', title: 'Рентабельность', bucket: '8%', points: 60, source: 'GIRBO' },
];

export function scoreFor(app: StoredApplication): ScoreResult | null {
    if (stage(app) < 4) return null;
    const p = profileOf(app);
    const factors = p.factors ?? DEFAULT_FACTORS(app);
    const requested = app.requested?.amount ?? 0;
    return {
        model_version: 'scorecard-sme-v1',
        engine: 'SCORECARD',
        total: p.total,
        grade: p.grade,
        pd: null,
        factors,
        stop_factors: [],
        max_amount: p.maxAmount ?? { value: requested, limited_by: 'PRODUCT_LIMIT' },
        computed_at: new Date(new Date(app.created_at).getTime() + 4 * 60_000).toISOString(),
    };
}

/** Предложения движка для ручного рассмотрения — их андеррайтер правит в панели решения. */
function proposedOffers(app: StoredApplication, rateFrom: number, now: number): Offer[] {
    const amount = app.requested?.amount ?? 0;
    const term = app.requested?.term_months ?? 12;
    const proposals = profileOf(app).proposals ?? [{ share: 1, extraRate: 1, kind: 'REQUESTED' as const }];
    return proposals.map((p, i) => {
        const value = Math.round((amount * p.share) / 100_000) * 100_000;
        const rate = Math.round((rateFrom + p.extraRate) * 10) / 10;
        const monthly = annuity(value, term, rate);
        return {
            id: `prop_${app.id}_${i + 1}`,
            kind: p.kind,
            state: 'ACTIVE',
            product_code: app.product_code,
            amount: value,
            term_months: term,
            rate_annual: rate,
            monthly_payment: monthly,
            total_payment: monthly * term,
            guarantee_required: Boolean(p.guarantee),
            conditions: p.conditions ?? [],
            valid_until: new Date(now + 7 * DAY).toISOString(),
        };
    });
}

/** manualReview — заявка побывала на ручном рассмотрении: решение движка остаётся «на ручное», даже когда андеррайтер уже решил. */
export function engineDecisionFor(app: StoredApplication, rateFrom: number, now: number, manualReview = false): EngineDecision | null {
    if (stage(app) < 4 && app.status !== 'WAITING_FOR_INFO') return null;
    const p = profileOf(app);
    switch (manualReview ? 'MANUAL_REVIEW' : app.status) {
        case 'MANUAL_REVIEW':
        case 'DECIDING':
            return {
                outcome: 'MANUAL_REVIEW',
                rules_fired: [{ code: p.rule.code, effect: 'MANUAL_REVIEW', detail: p.rule.detail }],
                manual_review_reasons: p.reasons,
                proposed_offers: proposedOffers(app, rateFrom, now),
            };
        case 'REJECTED':
            return { outcome: 'REJECTED', rules_fired: [{ code: 'HIGH_DEBT_LOAD', effect: 'REJECT' }] };
        case 'WAITING_FOR_INFO':
            return { outcome: 'NEED_INFO', rules_fired: [{ code: 'MANDATORY_DATA_MISSING', effect: 'WAITING_FOR_INFO' }] };
        default:
            return { outcome: 'APPROVED', rules_fired: [{ code: `GRADE_${p.grade}_AUTO`, effect: 'APPROVE' }] };
    }
}

export function listItemFor(app: StoredApplication, applicant: Applicant | undefined, bo: StoredBo | undefined, now: number): BoApplicationListItem {
    const sources = sourcesFor(app, applicant, bo, now);
    const scored = stage(app) >= 4;
    return {
        id: app.id,
        number: app.number,
        status: app.status,
        applicant: { id: app.applicant.id, short_name: app.applicant.short_name, inn: app.applicant.inn, type: applicant?.applicant_type },
        product_code: app.product_code,
        requested: app.requested ?? { amount: 0, term_months: 0 },
        grade: scored ? profileOf(app).grade : null,
        manual_review_reasons: app.status === 'MANUAL_REVIEW' ? profileOf(app).reasons : [],
        degraded_sources: sources.filter((s) => s.state === 'FAILED').map((s) => s.code),
        assignee: bo?.assignee ?? null,
        submitted_at: app.submitted_at ?? app.created_at,
        sla_due_at: app.status === 'MANUAL_REVIEW' ? (bo?.sla_due_at ?? null) : null,
    };
}

/** Полный профиль компании: что собрали из реестра, отчётности, БКИ, ФССП, КАД. */
export function applicantFullFor(applicant: Applicant, app: StoredApplication): ApplicantFull {
    const revenueFinding = findingsFor(app).find((f) => f.code === 'REVENUE_MISMATCH');
    const declared = app.financials?.annual_revenue;
    const revenue = typeof revenueFinding?.found_value === 'number' ? revenueFinding.found_value : (declared ?? 10_000_000);
    return {
        ...applicant,
        registry: {
            status: applicant.registry_status ?? 'ACTIVE',
            registration_date: applicant.registration_date,
            okved: applicant.okved_main ? `${applicant.okved_main.code} ${applicant.okved_main.title ?? ''}`.trim() : undefined,
            director: applicant.director_name,
            address: applicant.legal_address?.full,
        },
        financial_statements: [
            { year: 2025, revenue, net_profit: Math.round(revenue * 0.06), total_assets: Math.round(revenue * 0.8), equity: Math.round(revenue * 0.3) },
            { year: 2024, revenue: Math.round(revenue * 0.85), net_profit: Math.round(revenue * 0.04), total_assets: Math.round(revenue * 0.7), equity: Math.round(revenue * 0.25) },
        ],
        risk_flags: findingsFor(app).some((f) => f.code === 'ENFORCEMENT_PROCEEDINGS') ? ['ENFORCEMENT_PROCEEDINGS'] : [],
        credit_history: { active_loans: app.debts?.length ?? 0, overdue_90d: 0, last_request: '2026-05-12' },
        enforcement: { count: app.id === 'app_000147' ? 1 : 0, amount: app.id === 'app_000147' ? 42_300 : 0 },
        litigation: { as_defendant: 0, as_plaintiff: 0 },
        bank_relationship: { client_since: applicant.registration_date?.slice(0, 4), turnover_share: '35%' },
    };
}
