import { delay, http, HttpResponse } from 'msw';

import { COLLECTOR_BASE_URL } from '../../api/collector/client';
import {
    COLLECTOR_ROLES,
    type CollectionSource,
    type CollectionTask,
    type CollectorApplication,
    type CollectorDocument,
    type CollectorRole,
    type CollectorStatus,
    type CollectorSystem,
    type QuestionnaireInput,
} from '../../api/collector/types';
import { mockFailure } from '../handlers';
import { stepMs } from '../workflow';
import { FAILING_COLLECTION_INN, scenarioByInn } from './scenarios';
import { mutateCollector, newId, readCollector, type StoredCollectorApp } from './store';
import { collectorUser, COLLECTOR_TEST_USERS } from './users';

/* Мок data-collector по его swagger и README: регистрация, анкета с файлами, статус заявки, итог сбора. */

const url = (path: string) => `${COLLECTOR_BASE_URL}${path}`;

type Field = { field: string; code: string; message: string };

function problem(status: number, code: string, title: string, detail?: string, errors?: Field[]) {
    return HttpResponse.json(
        { type: `https://errors.credit-cabinet.local/${code.toLowerCase().replace(/_/g, '-')}`, title, status, code, detail, errors },
        { status, headers: { 'Content-Type': 'application/problem+json' } },
    );
}

const validation = (status: 400 | 422, detail: string, errors: Field[]) => problem(status, 'VALIDATION_FAILED', 'Ошибка в данных', detail, errors);
const notFound = (detail: string) => problem(404, 'NOT_FOUND', 'Не найдено', detail);

const INN = /^(\d{10}|\d{12})$/;
const PERSON_INN = /^\d{12}$/;
const BIK = /^\d{9}$/;
const CADASTRAL = /^\d{2}:\d{2}:\d{6,7}:\d+$/;
const VIN = /^[A-HJ-NPR-Z0-9]{17}$/;
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const DATE = /^\d{4}-\d{2}-\d{2}$/;
const MAX_FILES = 10;
const MAX_FILE_BYTES = 20 * 1024 * 1024;
const PURPOSES = ['WORKING_CAPITAL', 'EQUIPMENT', 'VEHICLE', 'REAL_ESTATE', 'REFINANCING', 'BUSINESS_EXPANSION', 'TENDER', 'OTHER'];
const MARITAL = ['SINGLE', 'MARRIED', 'DIVORCED', 'DIVORCED_MULTIPLE'];
const DOC_TYPES = [
    'FINANCIAL_STATEMENTS', 'TAX_RETURN', 'BANK_STATEMENT', 'MANAGEMENT_REPORT', 'CHARTER', 'POWER_OF_ATTORNEY', 'DIRECTOR_PASSPORT',
    'COLLATERAL_TITLE', 'EGRN_EXTRACT', 'LEASE_AGREEMENT', 'CONTRACT_WITH_CUSTOMER', 'LOAN_AGREEMENT_DRAFT', 'OTHER',
];
const MIME: Record<string, string> = {
    pdf: 'application/pdf',
    docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
    jpg: 'image/jpeg',
    jpeg: 'image/jpeg',
    png: 'image/png',
    xml: 'application/xml',
    sig: 'application/pkcs7-signature',
    p7s: 'application/pkcs7-signature',
};
const RECOGNIZABLE = new Set(['pdf', 'docx', 'xlsx', 'jpg', 'jpeg', 'png']);
const SYSTEMS: CollectorSystem[] = [
    'CORE_BANKING', 'FNS', 'BKI', 'COMPLIANCE', 'FSSP', 'KAD', 'FEDRESURS', 'M4D', 'TAX_REPORTS', 'PLEDGE_REGISTRY', 'EGRN',
    'GOSUSLUGI', 'OPEN_BANKING', 'MARKETPLACES', 'OFD', 'ZAKUPKI', 'SIGNALS', 'CBR', 'HH',
];

const extOf = (name: string) => name.split('.').pop()?.toLowerCase() ?? '';
const isSignature = (name: string) => /\.(sig|p7s)$/i.test(name);
const iso = (ms: number) => new Date(ms).toISOString();
const canSign = (role: CollectorRole, soleProprietor: boolean) => role === 'DIRECTOR' || (soleProprietor && role === 'OWNER');

/** Статус по времени отправки: шаг конвейера — как у остальных моков (флаг credit-cabinet:mock-pipeline-ms). */
function statusOf(app: StoredCollectorApp, now = Date.now()): CollectorStatus {
    if (app.submitted_ms === undefined) return 'DRAFT';
    const elapsed = now - app.submitted_ms;
    if (elapsed < stepMs()) return 'SUBMITTED';
    // сбор не удался — заявка так и остаётся в ENRICHING, в журнале enrich.failed
    if (elapsed < 2 * stepMs() || app.company.inn === FAILING_COLLECTION_INN) return 'ENRICHING';
    return 'VERIFYING';
}

function toApplication(app: StoredCollectorApp, withKnown = false): CollectorApplication {
    const status = statusOf(app);
    const scenario = scenarioByInn(app.company.inn);
    // после сбора имя и ОГРН компании — из ЕГРЮЛ, и для всех её заявок (компания одна на ИНН)
    const collected = scenario && readCollector().apps.some((a) => a.company.inn === app.company.inn && statusOf(a) === 'VERIFYING');
    const q = app.questionnaire;
    return {
        id: app.id,
        number: app.number,
        status,
        applicant: {
            id: app.company.id,
            inn: app.company.inn,
            type: app.company.inn.length === 12 ? 'SOLE_PROPRIETOR' : 'LEGAL_ENTITY',
            name: collected ? scenario.egrulName : (app.company.name ?? `ИНН ${app.company.inn}`),
            ogrn: collected ? scenario.ogrn : undefined,
            already_known: withKnown ? app.already_known : undefined,
        },
        representative: app.representative,
        product_code: q?.product_code,
        requested: q?.requested,
        purpose: q?.purpose,
        documents: app.documents,
        collection_task_id: app.task_id,
        created_at: app.created_at,
        submitted_at: app.submitted_ms === undefined ? undefined : iso(app.submitted_ms),
        updated_at: app.updated_at,
    };
}

/** Системы без входных данных (domain.SkippedSystems) — с причиной. */
function skippedSystems(q: NonNullable<StoredCollectorApp['questionnaire']>): Partial<Record<CollectorSystem, string>> {
    const skipped: Partial<Record<CollectorSystem, string>> = {};
    if (!q.consents?.bki_company) {
        if (!q.director) skipped.BKI = 'отчёт по компании: нет согласия consents.bki_company; отчёт по руководителю: нет данных руководителя';
        else if (!q.consents?.bki_person) skipped.BKI = 'отчёт по компании: нет согласия consents.bki_company; отчёт по руководителю: нет согласия consents.bki_person';
    }
    if (!q.consents?.tax) skipped.TAX_REPORTS = 'нет согласия на налоговую отчётность consents.tax';
    if (!q.collateral?.cadastral_numbers?.length) skipped.EGRN = 'нет недвижимости в залог collateral.cadastral_numbers';
    if (!q.director) skipped.GOSUSLUGI = 'нет данных руководителя';
    if (!q.other_bank_biks?.length) skipped.OPEN_BANKING = 'нет счетов в других банках other_bank_biks';
    if (!q.has_marketplace_tokens) skipped.MARKETPLACES = 'нет токенов продавца marketplace_tokens';
    if (!q.has_ofd_token) skipped.OFD = 'нет токена ОФД ofd_token';
    return skipped;
}

function toTask(app: StoredCollectorApp, now = Date.now()): CollectionTask {
    const q = app.questionnaire;
    const start = app.submitted_ms ?? now;
    const skipped = q ? skippedSystems(q) : {};
    const elapsed = now - start;
    const failing = app.company.inn === FAILING_COLLECTION_INN;
    const status = elapsed < stepMs() ? 'PENDING' : elapsed < 2 * stepMs() ? 'RUNNING' : failing ? 'FAILED' : 'COMPLETED';
    const base: CollectionTask = {
        id: app.task_id,
        application_id: app.id,
        questionnaire_id: q?.id,
        inn: app.company.inn,
        status,
        attempts: status === 'PENDING' ? 0 : 1,
        skipped_sources: Object.keys(skipped),
        document_ids: app.documents.map((d) => d.id ?? ''),
        created_at: iso(start),
        started_at: status === 'PENDING' ? undefined : iso(start + stepMs()),
    };
    if (status === 'FAILED') return { ...base, attempts: 3, error: 'collect: превышено число попыток (3)', finished_at: iso(start + 2 * stepMs()) };
    if (status !== 'COMPLETED') return base;
    const scenario = scenarioByInn(app.company.inn);
    const sources: CollectionSource[] = SYSTEMS.map((source, i) => {
        const reason = skipped[source];
        if (reason) return { source, state: 'SKIPPED', reason, calls: 0, failed_calls: 0, duration_ms: 0 };
        // не наш клиент: в АБС его нет
        if (!scenario && source === 'CORE_BANKING') return { source, state: 'FAILED', calls: 5, failed_calls: 5, duration_ms: 12 };
        if (scenario?.partial?.includes(source)) return { source, state: 'PARTIAL', calls: 4, failed_calls: 1, duration_ms: 140 };
        return { source, state: 'DONE', calls: 1 + (i % 4), failed_calls: 0, duration_ms: 6 + i * 7 };
    });
    return {
        ...base,
        finished_at: iso(start + 2 * stepMs()),
        raw_collection_id: `col_${app.task_id?.slice(4)}`,
        sources,
    };
}

// ---------------------------------------------------------------- проверки анкеты

function validateQuestionnaire(q: QuestionnaireInput, inn: string): Field[] {
    const errors: Field[] = [];
    const add = (field: string, code: string, message: string) => errors.push({ field, code, message });
    if (q.age < 18 || q.age > 100) add('age', 'out_of_range', 'возраст должен быть от 18 до 100 лет');
    if (!MARITAL.includes(q.marital_status)) add('marital_status', 'invalid_value', 'допустимые значения: SINGLE, MARRIED, DIVORCED, DIVORCED_MULTIPLE');
    if (q.children_count < 0 || q.children_count > 20) add('children_count', 'out_of_range', 'количество детей и иждивенцев должно быть от 0 до 20');
    if ((q.product_code?.length ?? 0) > 64) add('product_code', 'too_long', 'не длиннее 64 символов');
    if (q.requested) {
        if (!(q.requested.amount > 0)) add('requested.amount', 'out_of_range', 'сумма должна быть больше нуля');
        if (!(q.requested.term_months > 0 && q.requested.term_months <= 600)) add('requested.term_months', 'out_of_range', 'срок от 1 до 600 месяцев');
    }
    if (q.purpose) {
        if (!PURPOSES.includes(q.purpose.code)) add('purpose.code', 'invalid_value', `допустимые значения: ${PURPOSES.join(', ')}`);
        if ((q.purpose.description?.length ?? 0) > 2000) add('purpose.description', 'too_long', 'не длиннее 2000 символов');
    }
    if (!INN.test(inn)) add('inn', 'invalid_format', 'ИНН должен состоять из 10 (ЮЛ) или 12 (ИП) цифр');
    const d = q.director;
    if (d) {
        if (!PERSON_INN.test(d.inn)) add('director.inn', 'invalid_format', 'ИНН физлица — 12 цифр');
        if (!d.last_name) add('director.last_name', 'required', 'обязательное поле');
        if (!d.first_name) add('director.first_name', 'required', 'обязательное поле');
        if (!DATE.test(d.birth_date)) add('director.birth_date', 'invalid_format', 'дата рождения в формате ГГГГ-ММ-ДД');
    }
    for (const [field, c] of Object.entries(q.consents ?? {})) {
        if (!c) continue;
        if (!c.id?.trim()) add(`consents.${field}.id`, 'required', 'обязательное поле');
        if (!DATE.test(c.valid_until)) add(`consents.${field}.valid_until`, 'invalid_format', 'дата в формате ГГГГ-ММ-ДД');
    }
    if (q.consents?.bki_person && !d) add('consents.bki_person', 'invalid_value', 'согласие руководителя без данных руководителя (director)');
    if (q.consents?.tax && !q.consents.tax.scope?.length) add('consents.tax.scope', 'required', 'нужно хотя бы одно право: DECLARATIONS, ENS_BALANCE');
    if ((q.other_bank_biks?.length ?? 0) > 10) add('other_bank_biks', 'too_many', 'не больше 10 банков');
    q.other_bank_biks?.forEach((b, i) => !BIK.test(b) && add(`other_bank_biks[${i}]`, 'invalid_format', 'БИК — 9 цифр'));
    for (const [mp, token] of Object.entries(q.marketplace_tokens ?? {})) {
        if (mp !== 'wildberries' && mp !== 'ozon') add(`marketplace_tokens.${mp}`, 'invalid_value', 'допустимые площадки: wildberries, ozon');
        else if (!token) add(`marketplace_tokens.${mp}`, 'required', 'пустой токен продавца');
    }
    const col = q.collateral ?? {};
    if ((col.cadastral_numbers?.length ?? 0) + (col.vins?.length ?? 0) + (col.serials?.length ?? 0) > 20) add('collateral', 'too_many', 'не больше 20 объектов залога');
    col.cadastral_numbers?.forEach((n, i) => !CADASTRAL.test(n) && add(`collateral.cadastral_numbers[${i}]`, 'invalid_format', 'кадастровый номер вида 50:21:0110203:845'));
    col.vins?.forEach((v, i) => !VIN.test(v) && add(`collateral.vins[${i}]`, 'invalid_format', 'VIN — 17 символов, без I, O, Q'));
    col.serials?.forEach((s, i) => (!s || s.length > 64) && add(`collateral.serials[${i}]`, 'invalid_format', 'заводской номер — от 1 до 64 символов'));
    if (q.poa_number && !UUID.test(q.poa_number)) add('poa_number', 'invalid_format', 'номер МЧД — UUID');
    if (q.representative_inn && !PERSON_INN.test(q.representative_inn)) add('representative_inn', 'invalid_format', 'ИНН физлица — 12 цифр');
    return errors;
}

/** Файлы и пары «документ + открепленная подпись» (service/documents.go). */
function validateFiles(files: File[], specs: QuestionnaireInput['documents'] = []) {
    const errors: Field[] = [];
    const add = (field: string, code: string, message: string) => errors.push({ field, code, message });
    if (files.length > MAX_FILES) add('files', 'too_many', 'не больше 10 файлов, включая подписи');
    const seen = new Map<string, number>();
    files.forEach((f, i) => {
        const field = `files[${i}]`;
        if (!f.name || f.name.length > 255) add(field, 'invalid_name', 'имя файла пустое, длиннее 255 байт или с управляющими символами');
        else if (!MIME[extOf(f.name)]) add(field, 'invalid_type', 'допустимые форматы: pdf, docx, xlsx, jpg, png, xml; подписи — sig, p7s');
        else if (f.size === 0) add(field, 'empty', 'пустой файл');
        else if (f.size > MAX_FILE_BYTES) add(field, 'too_large', 'файл больше 20 МБ');
        const lower = f.name.toLowerCase();
        const prev = seen.get(lower);
        if (prev !== undefined) add(field, 'duplicate', `файл с таким именем уже есть: files[${prev}]`);
        else seen.set(lower, i);
    });
    const signedBy = new Map<number, number>();
    const targets = new Map<number, number>();
    files.forEach((f, i) => {
        if (!isSignature(f.name)) return;
        // имена — без учёта регистра, как strings.ToLower в data-collector
        const base = f.name.replace(/\.(sig|p7s)$/i, '').toLowerCase();
        let target = files.findIndex((x) => x.name.toLowerCase() === base);
        if (target >= 0 && isSignature(files[target]?.name ?? '')) {
            add(`files[${i}]`, 'signature_of_signature', 'подпись не может подписывать другую подпись');
            return;
        }
        if (target < 0 && !base.includes('.')) {
            const same = files.map((x, j) => ({ x, j })).filter(({ x }) => !isSignature(x.name) && x.name.replace(/\.[^.]+$/, '').toLowerCase() === base);
            if (same.length > 1) {
                add(`files[${i}]`, 'ambiguous_signature', `неясно, какой документ подписан: ${same.map((s) => `files[${s.j}]`).join(', ')}`);
                return;
            }
            target = same[0]?.j ?? -1;
        }
        if (target < 0) return; // присоединённая подпись
        const prev = signedBy.get(target);
        if (prev !== undefined) add(`files[${i}]`, 'second_signature', `у ${files[target]?.name} уже есть подпись files[${prev}]`);
        else {
            signedBy.set(target, i);
            targets.set(i, target);
        }
    });
    const specified = new Set<string>();
    specs.forEach((s, i) => {
        const field = `documents[${i}]`;
        const index = files.findIndex((f) => f.name.toLowerCase() === s.file_name.trim().toLowerCase());
        if (index < 0) add(`${field}.file_name`, 'unknown_file', 'такого файла нет среди files');
        // тип нельзя только у открепленной подписи; присоединённая — обычный документ
        else if (targets.has(index)) add(`${field}.file_name`, 'signature_file', 'это открепленная подпись: её тип всегда SIGNATURE');
        if (!DOC_TYPES.includes(s.type)) add(`${field}.type`, 'invalid_value', 'тип документа из списка DocumentType, кроме SIGNATURE');
        if (specified.has(s.file_name)) add(`${field}.file_name`, 'duplicate', 'тип для этого файла уже указан');
        specified.add(s.file_name);
    });
    return { errors, signedBy, targets };
}

function buildDocuments(files: File[], specs: QuestionnaireInput['documents'], targets: Map<number, number>, signedBy: Map<number, number>) {
    const ids = files.map(() => newId('doc'));
    const now = new Date().toISOString();
    const built = files.map((f, i): CollectorDocument => {
        const ext = extOf(f.name);
        const sig = isSignature(f.name);
        const target = targets.get(i);
        return {
            id: ids[i],
            type: target !== undefined ? 'SIGNATURE' : (specs?.find((s) => s.file_name.toLowerCase() === f.name.toLowerCase())?.type ?? 'OTHER'),
            file_name: f.name,
            mime_type: MIME[ext],
            size_bytes: f.size,
            sha256: `${f.size.toString(16)}${i}`.padEnd(64, '0'),
            signed_document_id: target === undefined ? undefined : ids[target],
            signature_state: sig || signedBy.has(i) ? 'PENDING' : 'NOT_SIGNED',
            recognition_state: RECOGNIZABLE.has(ext) ? 'PENDING' : 'NOT_APPLICABLE',
            uploaded_at: now,
        };
    });
    // как data-collector: сначала документы, потом открепленные подписи
    return [...built.filter((_, i) => !targets.has(i)), ...built.filter((_, i) => targets.has(i))];
}

// ---------------------------------------------------------------- вход (мок-логин, как у data-collector)

const unauthorized = (detail: string) => problem(401, 'UNAUTHORIZED', 'Unauthorized', detail);

/** Пользователь по `Authorization: Bearer mock.<user_id>`. Без заголовка — анонимно; неверный токен — 401. */
function authenticate(request: Request) {
    const header = request.headers.get('Authorization');
    if (!header) return { user: undefined };
    // схема — без учёта регистра, как у data-collector
    const token = /^bearer\s+(\S+)\s*$/i.exec(header.trim())?.[1];
    const id = token?.match(/^mock\.(.+)$/)?.[1];
    const user = id ? collectorUser(id) : undefined;
    return user ? { user } : { error: unauthorized('токен недействителен') };
}

/** Вход — без проверки токена: по контракту она ему не нужна, и старый токен не мешает перелогиниться. */
const AUTH_PATHS = ['/auth/test-users', '/auth/mock-login'];

/** Роль пользователя в компании, если уже записана: заведённая или с прошлой регистрации. Не переписывается. */
function memberRole(userId: string, inn: string): { role: CollectorRole; can_sign: boolean } | undefined {
    const seeded = collectorUser(userId)?.companies.find((c) => c.inn === inn);
    if (seeded) return { role: seeded.role, can_sign: seeded.can_sign };
    const app = readCollector().apps.find((a) => a.representative.id === userId && a.company.inn === inn);
    return app && { role: app.representative.role, can_sign: app.representative.can_sign };
}

/** Профиль: заведённые компании и те, по которым пользователь подавал заявки (company_members у сервиса). */
function meOf(user: (typeof COLLECTOR_TEST_USERS)[number]) {
    // сначала заведённые компании, потом добавленные регистрацией — по имени (как у сервиса)
    const added: typeof user.companies = [];
    for (const app of readCollector().apps) {
        if (app.representative.id !== user.id || [...user.companies, ...added].some((c) => c.inn === app.company.inn)) continue;
        added.push({ applicant_id: app.company.id, short_name: app.company.name ?? `ИНН ${app.company.inn}`, inn: app.company.inn, role: app.representative.role, can_sign: app.representative.can_sign });
    }
    added.sort((a, b) => a.short_name.localeCompare(b.short_name) || a.applicant_id.localeCompare(b.applicant_id));
    return { id: user.id, full_name: user.full_name, roles: user.roles, companies: [...user.companies, ...added], pending_signatures: [] };
}

// ---------------------------------------------------------------- обработчики

type RegisterBody = {
    representative?: { full_name?: string; inn?: string; role?: string };
    company?: { inn?: string; name?: string };
};

export const collectorHandlers = [
    http.all(url('/*'), ({ request }) => {
        const failure = mockFailure(request);
        if (failure) return failure;
        if (AUTH_PATHS.some((p) => new URL(request.url).pathname.endsWith(p))) return undefined;
        return authenticate(request).error;
    }),

    http.get(url('/auth/test-users'), async () => {
        await delay(250);
        return HttpResponse.json({ items: COLLECTOR_TEST_USERS });
    }),

    http.post(url('/auth/mock-login'), async ({ request }) => {
        await delay(250);
        const { user_id } = (await request.json().catch(() => ({}))) as { user_id?: string };
        if (!user_id) return validation(400, 'тело запроса не прошло проверку', [{ field: 'user_id', code: 'required', message: 'обязательное поле' }]);
        const user = collectorUser(user_id);
        if (!user) return notFound('пользователь не найден');
        return HttpResponse.json({ token: `mock.${user.id}`, user: meOf(user) });
    }),

    http.get(url('/me'), async ({ request }) => {
        await delay(150);
        const { user } = authenticate(request);
        return user ? HttpResponse.json(meOf(user)) : unauthorized('нужен токен: Authorization: Bearer mock.<user_id>');
    }),

    http.post(url('/applicants'), async ({ request }) => {
        await delay(400);
        let body: RegisterBody;
        try {
            body = (await request.json()) as RegisterBody;
        } catch {
            return validation(400, 'тело запроса — не JSON', []);
        }
        const rep = body.representative;
        const company = body.company;
        const missing: Field[] = [];
        if (!rep) missing.push({ field: 'representative', code: 'required', message: 'обязательное поле' });
        if (rep && !rep.full_name) missing.push({ field: 'representative.full_name', code: 'required', message: 'обязательное поле' });
        if (rep && !rep.role) missing.push({ field: 'representative.role', code: 'required', message: 'обязательное поле' });
        if (!company?.inn) missing.push({ field: 'company.inn', code: 'required', message: 'обязательное поле' });
        if (missing.length || !rep || !company?.inn) return validation(400, 'не заполнены обязательные поля', missing);

        const errors: Field[] = [];
        if (!INN.test(company.inn)) errors.push({ field: 'company.inn', code: 'invalid_format', message: 'ИНН компании — 10 или 12 цифр' });
        if (rep.inn && !PERSON_INN.test(rep.inn)) errors.push({ field: 'representative.inn', code: 'invalid_format', message: 'ИНН физлица — 12 цифр' });
        if (!COLLECTOR_ROLES.includes(rep.role as CollectorRole)) errors.push({ field: 'representative.role', code: 'invalid_value', message: 'неизвестная роль' });
        if (errors.length) return validation(422, 'ошибка в данных заявителя', errors);

        const inn = company.inn;
        const role = rep.role as CollectorRole;
        // вошёл по токену — заявка от его имени: ФИО из профиля, черновик по компании — его
        const { user } = authenticate(request);
        if (user && !user.roles.includes('CLIENT')) return problem(403, 'FORBIDDEN', 'Forbidden', 'заявку на кредит подаёт клиент, а не сотрудник банка');
        const fullName = user?.full_name ?? rep.full_name ?? '';
        // вошедший: уже записанная роль в компании не переписывается
        const kept = user ? memberRole(user.id, inn) : undefined;
        const { apps } = readCollector();
        const existingDraft = apps.find(
            (a) =>
                a.company.inn === inn &&
                a.submitted_ms === undefined &&
                (user ? a.representative.id === user.id : Boolean(rep.inn) && a.representative.inn === rep.inn),
        );
        if (existingDraft) {
            const updated = mutateCollector((d) => {
                const app = d.apps.find((a) => a.id === existingDraft.id) ?? existingDraft;
                app.representative = kept
                    ? { ...app.representative, full_name: fullName, ...kept }
                    : { ...app.representative, full_name: fullName, role, can_sign: canSign(role, inn.length === 12) };
                return app;
            });
            return HttpResponse.json(toApplication(updated, true), { status: 201, headers: { Location: `/api/v1/applications/${updated.id}` } });
        }
        const known = apps.find((a) => a.company.inn === inn);
        // компании тестовых пользователей заведены заранее — со своими id
        const seeded = COLLECTOR_TEST_USERS.flatMap((u) => u.companies).find((c) => c.inn === inn);
        const now = new Date().toISOString();
        const app = mutateCollector((d) => {
            d.seq += 1;
            const created: StoredCollectorApp = {
                id: newId('app'),
                number: `A-2026-${String(d.seq).padStart(6, '0')}`,
                company: known ? known.company : seeded ? { id: seeded.applicant_id, inn, name: seeded.short_name } : { id: newId('apl'), inn, name: company.name || undefined },
                representative: {
                    id: user?.id ?? newId('usr'),
                    full_name: fullName,
                    inn: user ? undefined : rep.inn || undefined,
                    role: kept?.role ?? role,
                    can_sign: kept?.can_sign ?? canSign(role, inn.length === 12),
                },
                already_known: Boolean(known ?? seeded),
                created_at: now,
                updated_at: now,
                documents: [],
            };
            d.apps.push(created);
            return created;
        });
        return HttpResponse.json(toApplication(app, true), { status: 201, headers: { Location: `/api/v1/applications/${app.id}` } });
    }),

    http.get(url('/applications/:id'), async ({ params }) => {
        await delay(200);
        const app = readCollector().apps.find((a) => a.id === params.id);
        if (!app) return notFound('заявка не найдена');
        return HttpResponse.json(toApplication(app));
    }),

    http.post(url('/applications/:id/questionnaire'), async ({ request, params }) => {
        await delay(700);
        const type = request.headers.get('Content-Type') ?? '';
        if (!type.startsWith('multipart/form-data') && !type.startsWith('application/json')) {
            return problem(415, 'UNSUPPORTED_MEDIA_TYPE', 'Неподдерживаемый формат', 'анкета — multipart/form-data или application/json');
        }
        const app = readCollector().apps.find((a) => a.id === params.id);
        if (!app) return notFound('заявка не найдена');

        let raw: unknown;
        let files: File[] = [];
        try {
            if (type.startsWith('multipart/form-data')) {
                const form = await request.formData();
                const part = form.get('questionnaire');
                if (typeof part !== 'string') return validation(400, 'нет части questionnaire', [{ field: 'questionnaire', code: 'required', message: 'обязательное поле' }]);
                raw = JSON.parse(part);
                files = form.getAll('files').filter((f): f is File => f instanceof File);
            } else {
                raw = await request.json();
            }
        } catch {
            return validation(400, 'анкета — не JSON', []);
        }
        const q = raw as Partial<QuestionnaireInput>;
        const required = (['age', 'marital_status', 'children_count'] as const).filter((f) => q[f] === undefined || q[f] === null);
        if (required.length) {
            return validation(400, 'не заполнены обязательные поля', required.map((field) => ({ field, code: 'required', message: 'обязательное поле' })));
        }
        const input = q as QuestionnaireInput;
        // как у сервиса: повторную анкету отклоняем уже после разбора тела
        if (app.submitted_ms !== undefined) return problem(409, 'INVALID_TRANSITION', 'Анкету уже отправили', 'анкету по заявке принимают один раз');
        const fileCheck = validateFiles(files, input.documents);
        const errors = [...validateQuestionnaire(input, app.company.inn), ...fileCheck.errors];
        if (errors.length) return validation(422, 'анкета не прошла проверку', errors);

        const now = Date.now();
        const documents = buildDocuments(files, input.documents, fileCheck.targets, fileCheck.signedBy);
        const { marketplace_tokens, ofd_token, ...rest } = input;
        const updated = mutateCollector((d) => {
            const stored = d.apps.find((a) => a.id === app.id) ?? app;
            stored.submitted_ms = now;
            stored.updated_at = iso(now);
            stored.documents = documents;
            stored.task_id = newId('tsk');
            // токены в анкету не попадают — только признак, что они были
            stored.questionnaire = {
                ...rest,
                id: newId('qst'),
                created_at: iso(now),
                has_marketplace_tokens: Object.values(marketplace_tokens ?? {}).some(Boolean),
                has_ofd_token: Boolean(ofd_token),
            };
            return stored;
        });
        const q2 = updated.questionnaire;
        return HttpResponse.json(
            {
                application: toApplication(updated),
                questionnaire: { id: q2?.id, age: input.age, marital_status: input.marital_status, children_count: input.children_count, created_at: q2?.created_at },
                collection: toTask(updated, now),
            },
            { status: 202, headers: { Location: `/api/v1/collections/${updated.task_id}` } },
        );
    }),

    http.get(url('/collections/:id'), async ({ params }) => {
        await delay(200);
        const app = readCollector().apps.find((a) => a.task_id === params.id);
        if (!app) return notFound('задача не найдена');
        return HttpResponse.json(toTask(app));
    }),
];
