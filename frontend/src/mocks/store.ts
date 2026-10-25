import type {
    Applicant,
    ClientDecision,
    Application,
    ApplicationSummary,
    CompanyAccess,
    Document,
    Me,
    Product,
    RepresentativeRole,
} from '../api/types';
import { applicantSeeds, products, REGISTRY_TIMEOUT_INN, registry, seedApplications, type StoredApplication, testUsers } from './data';
import { ACTIONS } from './fsm';
import { signatureSummary } from './signatures';
import {
    seedWorkflow,
    type StoredDocument,
    type StoredInfoRequest,
    type StoredOffer,
    type StoredSigning,
    type StoredTimelineEvent,
} from './seedWorkflow';

/**
 * Изменяемая «база» мока. Живёт в localStorage, чтобы черновики переживали перезагрузку.
 * Номер версии в ключе — сменить, если поменялись сиды.
 */
const DB_KEY = 'credit-cabinet:mock-db:v8';

/** Документ + время, когда мок «распознает» его (в контракт не попадает). */
export type { StoredDocument } from './seedWorkflow';

/** Событие для WebSocket (docs/websocket.md) + чьей компании оно касается. */
export type StoredWsEvent = { id: string; type: string; at: string; data: Record<string, unknown>; applicant_id: string };

type Db = {
    applications: StoredApplication[];
    applicants: Applicant[];
    /** Компании, которые пользователь привязал сам (POST /applicants). */
    links: Record<string, CompanyAccess[]>;
    documents: StoredDocument[];
    /** Idempotency-Key → id созданного ресурса. */
    idempotency: Record<string, string>;
    timeline: StoredTimelineEvent[];
    infoRequests: StoredInfoRequest[];
    offers: StoredOffer[];
    decisions: Record<string, ClientDecision>;
    /** Когда конвейер сделает следующий шаг по заявке (мс). */
    pipeline: Record<string, number>;
    /** Сколько раз клиент отказывался от предложений — удерживающее даём только на первый. */
    declines: Record<string, number>;
    events: StoredWsEvent[];
    seq: number;
    signing: Record<string, StoredSigning>;
};

function seed(): Db {
    const applications = structuredClone(seedApplications);
    return {
        applications,
        applicants: structuredClone(applicantSeeds),
        links: {},
        idempotency: {},
        ...seedWorkflow(applications),
        pipeline: {},
        declines: {},
        events: [],
        seq: 0,
    };
}

function load(): Db {
    try {
        const raw = localStorage.getItem(DB_KEY);
        if (raw) return JSON.parse(raw) as Db;
    } catch {
        /* битые данные или нет доступа — начинаем с сидов */
    }
    return seed();
}

let db = load();

// вторая вкладка поменяла базу — берём её версию, чтобы вкладки не затирали друг друга
if (typeof window !== 'undefined') {
    window.addEventListener('storage', (e) => {
        if (e.key === DB_KEY && e.newValue) db = JSON.parse(e.newValue) as Db;
    });
}

function persist() {
    try {
        localStorage.setItem(DB_KEY, JSON.stringify(db));
    } catch {
        /* приватный режим — живём в памяти */
    }
}

/** Изменить базу и сохранить. */
export function mutate<T>(fn: (data: Db) => T): T {
    const result = fn(db);
    persist();
    return result;
}

export const read = () => db;

export function resetDb() {
    db = seed();
    persist();
}

// ---------------------------------------------------------------- пользователи

export function meFor(userId: string): Me | undefined {
    const u = testUsers.find((t) => t.id === userId);
    if (!u) return undefined;
    return {
        id: u.id,
        full_name: u.full_name,
        roles: u.roles,
        companies: [...(u.companies ?? []), ...(db.links[u.id] ?? [])],
        // договоры в подписании по компаниям, где пользователь — директор
        pending_signatures: db.applications
            .filter(
                (a) =>
                    a.status === 'SIGNING' &&
                    [...(u.companies ?? []), ...(db.links[u.id] ?? [])].some((c) => c.applicant_id === a.applicant.id && c.role === 'DIRECTOR'),
            )
            .map((a) => a.id),
    };
}

export function linkCompany(me: Me, applicant: Applicant, role: RepresentativeRole): CompanyAccess {
    const access: CompanyAccess = {
        applicant_id: applicant.id,
        short_name: applicant.short_name ?? applicant.full_name,
        inn: applicant.inn,
        role,
        can_sign: role === 'DIRECTOR',
    };
    mutate((d) => {
        d.links[me.id] = [...(d.links[me.id] ?? []), access];
    });
    return access;
}

/** Компания из реестра заводится в системе один раз — повторные привязки её переиспользуют. */
export function findOrCreateApplicant(inn: string): Applicant | undefined {
    const known = db.applicants.find((a) => a.inn === inn);
    if (known) return known;
    // реестр не ответил при проверке — заводим компанию по одному ИНН, реквизиты «соберёт обогащение»
    const timeoutStub = inn === REGISTRY_TIMEOUT_INN && {
        inn,
        applicant_type: inn.length === 12 ? ('SOLE_PROPRIETOR' as const) : ('LEGAL_ENTITY' as const),
        full_name: `Компания, ИНН ${inn}`,
        short_name: `ИНН ${inn}`,
    };
    const prefill = registry.find((r) => r.inn === inn) ?? timeoutStub;
    if (!prefill) return undefined;
    const { already_known: _known, ...rest } = { already_known: false, ...prefill };
    const created: Applicant = {
        ...rest,
        id: `apl_${inn}`,
        my_role: 'DIRECTOR',
        created_at: new Date().toISOString(),
    };
    mutate((d) => d.applicants.push(created));
    return created;
}

// ---------------------------------------------------------------- заявки

export const productByCode = (code: string) => products.find((p) => p.code === code);

const CLOSED_STATUSES = new Set(['COMPLETED', 'REJECTED', 'DECLINED', 'CANCELLED', 'EXPIRED']);
export const isActive = (app: ApplicationSummary) => !CLOSED_STATUSES.has(app.status);

function valueAt(obj: unknown, path: string): unknown {
    return path.split('.').reduce<unknown>((acc, key) => (acc && typeof acc === 'object' ? (acc as Record<string, unknown>)[key] : undefined), obj);
}

function isFilled(app: StoredApplication, field: string): boolean {
    if (field === 'debts') return app.has_no_debts === true || Boolean(app.debts?.length);
    if (field === 'collateral') return Boolean(app.collateral?.length);
    if (field === 'guarantors') return Boolean(app.guarantors?.length);
    if (field.startsWith('consents.')) return valueAt(app, field) === true;
    const value = valueAt(app, field);
    return value !== undefined && value !== null && value !== '';
}

/** Обязательные поля: из продукта + залог/поручители по его условиям. */
export function requiredFields(app: StoredApplication, product?: Product): string[] {
    const fields = [...(product?.required_fields ?? [])];
    const threshold = product?.collateral?.required_above_amount;
    if (threshold !== null && threshold !== undefined && (app.requested?.amount ?? 0) > threshold) fields.push('collateral');
    if (product?.guarantee_required) fields.push('guarantors');
    return fields;
}

export function completeness(app: StoredApplication) {
    const fields = requiredFields(app, productByCode(app.product_code));
    const missing = fields.filter((f) => !isFilled(app, f));
    const percent = fields.length ? Math.round(((fields.length - missing.length) / fields.length) * 100) : 100;
    return { percent, missing_fields: missing };
}

export function toSummary(app: StoredApplication): ApplicationSummary {
    const { id, number, version, status, client_stage, applicant, product_code, requested, created_at, updated_at } = app;
    return { id, number, version, status, client_stage, applicant, product_code, requested, created_at, updated_at };
}

export function toApplication(app: StoredApplication): Application {
    return {
        ...app,
        allowed_actions: [...(ACTIONS[app.status] ?? [])],
        completeness: completeness(app),
    };
}

export function nextApplicationNumber() {
    const max = Math.max(...db.applications.map((a) => Number(a.number.slice(-6))));
    return String(max + 1).padStart(6, '0');
}

/** Данные с прошлой отправленной заявки компании — чтобы не вводить финансы и долги заново. */
export function prefillFor(applicantId: string): Partial<StoredApplication> {
    const previous = db.applications
        .filter((a) => a.applicant.id === applicantId && a.status !== 'DRAFT' && a.financials)
        .sort((a, b) => b.created_at.localeCompare(a.created_at))[0];
    if (!previous) return { prefilled_from: null };
    const { financials, debts, has_no_debts, collateral, guarantors } = structuredClone(previous);
    return { financials, debts, has_no_debts, collateral, guarantors, prefilled_from: previous.id };
}

// ---------------------------------------------------------------- документы

const RECOGNIZABLE = new Set(['FINANCIAL_STATEMENTS', 'TAX_RETURN', 'BANK_STATEMENT']);

/** Распознавание и проверка подписи «завершаются» через пару секунд после загрузки. */
export function toDocument(doc: StoredDocument): Document {
    const { application_id: _app, recognize_at, verify_at: _verify, mock_signature: _sig, mock_recognition, ...rest } = doc;
    const now = Date.now();
    const app = db.applications.find((x) => x.id === doc.application_id);
    const applicant = app && db.applicants.find((x) => x.id === app.applicant.id);
    const signatures = signatureSummary(doc, app, { documents: db.documents, applicant }, now);
    if (mock_recognition === 'FAILED') return { ...rest, signatures, recognition: { state: 'FAILED' } };
    if (!RECOGNIZABLE.has(doc.type)) return { ...rest, signatures, recognition: { state: 'NOT_APPLICABLE' } };
    if (recognize_at && now < recognize_at) return { ...rest, signatures, recognition: { state: 'PROCESSING' } };
    return {
        ...rest,
        signatures,
        recognition:
            doc.type === 'FINANCIAL_STATEMENTS'
                ? {
                      state: 'DONE',
                      detected_type: 'FINANCIAL_STATEMENTS',
                      extracted: { revenue_2025: 41_600_000, net_profit_2025: 2_700_000 },
                      confidence: 0.93,
                  }
                : { state: 'DONE', detected_type: doc.type, extracted: {}, confidence: 0.88 },
    };
}

export function addDocument(doc: StoredDocument) {
    mutate((d) => d.documents.push(doc));
    return toDocument(doc);
}

export const RECOGNITION_DELAY_MS = 2500;
