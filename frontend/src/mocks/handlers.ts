import { delay, http, HttpResponse } from 'msw';

import type {
    ApplicantType,
    ApplicationInput,
    ApplicationStatus,
    DocumentType,
    Problem,
    PurposeCode,
    RepresentativeRole,
    RequestedTerms,
} from '../api/types';
import { fieldLabel, PURPOSES } from '../shared/lib/dictionaries';
import { isInn, isValidInn } from '../shared/lib/inn';
import { mergePatch } from '../shared/lib/mergePatch';
import { products, REGISTRY_TIMEOUT_INN, registry, type StoredApplication, testUsers } from './data';
import {
    companyIds,
    currentUser,
    idempotent,
    invalidTransition,
    MOCK_TOKEN_PREFIX,
    notFound,
    ownApplication,
    problem,
    unauthorized,
    url,
    versionMismatch,
    withEtag,
} from './http';
import { annuity } from './seedWorkflow';
import { addTimeline, statusViewFor, tick, transition } from './workflow';
import {
    addDocument,
    completeness,
    findOrCreateApplicant,
    isActive,
    linkCompany,
    meFor,
    mutate,
    nextApplicationNumber,
    prefillFor,
    productByCode,
    read,
    RECOGNITION_DELAY_MS,
    toApplication,
    toDocument,
    toSummary,
} from './store';

const MAX_UPLOAD_BYTES = 20 * 1024 * 1024;
// xml — машиночитаемая доверенность (МЧД); в описании поля file в контракте её пока нет
const UPLOAD_EXTENSIONS = ['pdf', 'docx', 'xlsx', 'jpg', 'jpeg', 'png', 'sig', 'p7s', 'xml'];

const STATUSES: ApplicationStatus[] = [
    'DRAFT', 'SUBMITTED', 'ENRICHING', 'VERIFYING', 'WAITING_FOR_INFO', 'SCORING', 'DECIDING', 'MANUAL_REVIEW',
    'OFFERED', 'SIGNING', 'COMPLETED', 'REJECTED', 'DECLINED', 'CANCELLED', 'EXPIRED',
];

function validateTerms(terms: Partial<RequestedTerms> | undefined, code: string) {
    const product = productByCode(code);
    if (!product) return [{ field: 'product_code', message: 'Нет такого продукта', code: 'unknown' }];
    const errors = [];
    const { amount, term_months } = terms ?? {};
    if (typeof amount !== 'number' || amount < product.amount.min || amount > product.amount.max) {
        errors.push({ field: 'requested.amount', message: 'Сумма вне диапазона продукта', code: 'out_of_range' });
    }
    if (typeof term_months !== 'number' || term_months < product.term_months.min || term_months > product.term_months.max) {
        errors.push({ field: 'requested.term_months', message: 'Срок вне диапазона продукта', code: 'out_of_range' });
    }
    return errors;
}


type FieldError = { field: string; message: string; code?: string };

/** Проверки схемы ApplicationInput, которые сделал бы настоящий бэк на PATCH. */
function validateInput(app: StoredApplication): FieldError[] {
    const errors: FieldError[] = [];
    const missing = (field: string) => errors.push({ field, message: 'Обязательное поле', code: 'required' });
    if (app.purpose && !PURPOSES.includes(app.purpose.code)) missing('purpose.code');
    if (app.requested) errors.push(...validateTerms(app.requested, app.product_code));
    app.debts?.forEach((d, i) => {
        for (const key of ['kind', 'lender', 'outstanding', 'monthly_payment'] as const) {
            if (d[key] === undefined || d[key] === '') missing(`debts[${i}].${key}`);
        }
    });
    app.collateral?.forEach((c, i) => {
        if (!c.type) missing(`collateral[${i}].type`);
        if (typeof c.estimated_value !== 'number') missing(`collateral[${i}].estimated_value`);
    });
    app.guarantors?.forEach((g, i) => {
        if (!g.type) missing(`guarantors[${i}].type`);
        if (!g.name) missing(`guarantors[${i}].name`);
        if (!isValidInn(g.inn)) errors.push({ field: `guarantors[${i}].inn`, message: 'Неверный ИНН', code: 'checksum' });
    });
    return errors;
}

const FAIL_KEY = 'credit-cabinet:mock-fail';

/** Сбой по флагу из localStorage — для e2e: { method, path (regexp), status, code?, field?, times? }[]. */
type MockFailure = { method: string; path: string; status: number; code?: Problem['code']; field?: string; times?: number };

function mockFailure(request: Request) {
    let rules: MockFailure[];
    try {
        rules = JSON.parse(localStorage.getItem(FAIL_KEY) ?? '[]') as MockFailure[];
    } catch {
        return undefined;
    }
    const path = new URL(request.url).pathname;
    const index = rules.findIndex((r) => r.method === request.method && new RegExp(r.path).test(path));
    const rule = rules[index];
    if (!rule) return undefined;
    if (rule.times !== undefined) {
        rule.times -= 1;
        if (rule.times <= 0) rules.splice(index, 1);
        localStorage.setItem(FAIL_KEY, JSON.stringify(rules));
    }
    const code = rule.code ?? (rule.status >= 500 ? 'INTERNAL' : 'VALIDATION_FAILED');
    return problem(rule.status, code, 'Сбой (имитация)', {
        detail: 'Сбой подстроен флагом мока',
        ...(rule.field && { errors: [{ field: rule.field, message: 'Отклонено сервером' }] }),
    });
}

export const handlers = [
    // ---------------------------------------------------------------- сбои по флагу (e2e)
    http.all(url('/*'), ({ request }) => {
        tick();
        return mockFailure(request);
    }),
    // ---------------------------------------------------------------- auth
    http.get(url('/auth/test-users'), async () => {
        await delay(300);
        return HttpResponse.json({ items: testUsers });
    }),

    http.post(url('/auth/mock-login'), async ({ request }) => {
        await delay(300);
        const { user_id } = (await request.json()) as { user_id: string };
        const user = meFor(user_id);
        if (!user) return problem(404, 'NOT_FOUND', 'Пользователь не найден');
        return HttpResponse.json({ token: `${MOCK_TOKEN_PREFIX}${user_id}`, user });
    }),

    http.get(url('/me'), async ({ request }) => {
        await delay(150);
        const me = currentUser(request);
        return me ? HttpResponse.json(me) : unauthorized();
    }),

    // ---------------------------------------------------------------- catalog
    http.get(url('/products'), async ({ request }) => {
        await delay(150);
        if (!currentUser(request)) return unauthorized();
        const params = new URL(request.url).searchParams;
        const purpose = params.get('purpose') as PurposeCode | null;
        const applicantType = params.get('applicant_type') as ApplicantType | null;
        const items = products
            .filter((p) => !purpose || p.purposes?.includes(purpose))
            .filter((p) => !applicantType || p.applicant_types.includes(applicantType));
        return HttpResponse.json({ items });
    }),

    http.get(url('/products/:productCode'), async ({ request, params }) => {
        await delay(100);
        if (!currentUser(request)) return unauthorized();
        const product = productByCode(String(params.productCode));
        return product ? HttpResponse.json(product) : problem(404, 'NOT_FOUND', 'Продукт не найден');
    }),

    http.post(url('/products/:productCode/calculate'), async ({ request, params }) => {
        await delay(200);
        if (!currentUser(request)) return unauthorized();
        const code = String(params.productCode);
        const product = productByCode(code);
        if (!product) return problem(404, 'NOT_FOUND', 'Продукт не найден');
        const terms = (await request.json()) as RequestedTerms;
        const errors = validateTerms(terms, code);
        if (errors.length) return problem(422, 'VALIDATION_FAILED', 'Проверьте сумму и срок', { errors });
        return HttpResponse.json({
            rate_annual_from: product.rate_annual_from,
            monthly_payment_from: annuity(terms.amount, terms.term_months, product.rate_annual_from),
            disclaimer: 'Предварительный расчёт по условиям продукта. Точные условия — после рассмотрения заявки.',
        });
    }),

    // ---------------------------------------------------------------- applicants
    http.post(url('/counterparty/lookup'), async ({ request }) => {
        const { inn } = (await request.json()) as { inn?: unknown };
        if (!currentUser(request)) return unauthorized();
        if (!isInn(inn)) {
            return problem(422, 'VALIDATION_FAILED', 'Проверьте ИНН', {
                errors: [{ field: 'inn', message: 'ИНН — 10 цифр для юрлица или 12 для ИП', code: 'pattern' }],
            });
        }
        if (inn === REGISTRY_TIMEOUT_INN) {
            await delay(3000);
            return problem(504, 'UPSTREAM_TIMEOUT', 'Реестр не ответил вовремя');
        }
        await delay(400);
        const found = registry.find((r) => r.inn === inn) ?? read().applicants.find((a) => a.inn === inn);
        if (!found) return problem(404, 'NOT_FOUND', 'Компания с таким ИНН не найдена');
        const { id: _id, my_role: _role, can_sign: _sign, created_at: _created, ...prefill } = { ...found } as Record<string, unknown>;
        return HttpResponse.json({ ...prefill, already_known: read().applicants.some((a) => a.inn === inn) });
    }),

    http.get(url('/applicants'), async ({ request }) => {
        await delay(200);
        const me = currentUser(request);
        if (!me) return unauthorized();
        return HttpResponse.json({
            items: me.companies.flatMap((access) => {
                const applicant = read().applicants.find((a) => a.id === access.applicant_id);
                return applicant ? [{ ...applicant, my_role: access.role, can_sign: access.can_sign }] : [];
            }),
        });
    }),

    http.post(url('/applicants'), async ({ request }) => {
        await delay(300);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const { inn, representative_role } = (await request.json()) as { inn?: unknown; representative_role?: RepresentativeRole };
        if (!isInn(inn) || !representative_role) {
            return problem(422, 'VALIDATION_FAILED', 'Проверьте ИНН и роль', {
                errors: [
                    ...(!isInn(inn) ? [{ field: 'inn', message: 'Неверный ИНН' }] : []),
                    ...(!representative_role ? [{ field: 'representative_role', message: 'Укажите роль' }] : []),
                ],
            });
        }
        const { existing, remember } = idempotent(request, `applicant:${me.id}`);
        const repeated = existing && read().applicants.find((a) => a.id === existing);
        const repeatedAccess = repeated && me.companies.find((c) => c.applicant_id === repeated.id);
        if (repeated && repeatedAccess) {
            return HttpResponse.json({ ...repeated, my_role: repeatedAccess.role, can_sign: repeatedAccess.can_sign }, { status: 201 });
        }
        if (me.companies.some((c) => c.inn === inn)) return problem(409, 'CONFLICT', 'Компания уже привязана к вам');
        const applicant = findOrCreateApplicant(inn);
        if (!applicant) {
            return problem(422, 'VALIDATION_FAILED', 'Компания не найдена в реестре', {
                errors: [{ field: 'inn', message: 'Компания не найдена в реестре' }],
            });
        }
        const access = linkCompany(me, applicant, representative_role);
        remember(applicant.id);
        return HttpResponse.json({ ...applicant, my_role: access.role, can_sign: access.can_sign }, { status: 201 });
    }),

    http.get(url('/applicants/:applicantId'), async ({ request, params }) => {
        await delay(150);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const access = me.companies.find((c) => c.applicant_id === params.applicantId);
        const applicant = read().applicants.find((a) => a.id === params.applicantId);
        if (!applicant) return problem(404, 'NOT_FOUND', 'Компания не найдена');
        if (!access) return problem(403, 'FORBIDDEN', 'Нет доступа к компании');
        return HttpResponse.json({ ...applicant, my_role: access.role, can_sign: access.can_sign });
    }),

    // ---------------------------------------------------------------- applications
    http.get(url('/applications'), async ({ request }) => {
        await delay(350);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const params = new URL(request.url).searchParams;
        const applicantId = params.get('applicant_id');
        // style: form, explode: false → ?status=A,B
        const statuses = params.get('status')?.split(',').filter(Boolean) ?? [];
        const unknown = statuses.filter((s) => !STATUSES.includes(s as ApplicationStatus));
        if (unknown.length) {
            return problem(422, 'VALIDATION_FAILED', 'Неизвестный статус', {
                errors: unknown.map((s) => ({ field: 'status', message: `Нет статуса ${s}`, code: 'enum' })),
            });
        }
        const allowed = companyIds(me);
        const limit = Number(params.get('limit') ?? 20);
        const items = read()
            .applications.filter((a) => allowed.has(a.applicant.id))
            .filter((a) => !applicantId || a.applicant.id === applicantId)
            .filter((a) => !statuses.length || statuses.includes(a.status))
            .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
            .slice(0, limit)
            .map(toSummary);
        return HttpResponse.json({ items, next_cursor: null });
    }),

    http.post(url('/applications'), async ({ request }) => {
        await delay(400);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const body = (await request.json()) as ApplicationInput & { applicant_id?: string };
        const { existing, remember } = idempotent(request, `create:${me.id}`);
        const repeated = existing && read().applications.find((a) => a.id === existing);
        if (repeated) return HttpResponse.json(toApplication(repeated), { status: 201, headers: withEtag(repeated.version) });

        const access = me.companies.find((c) => c.applicant_id === body.applicant_id);
        if (!access) return problem(403, 'FORBIDDEN', 'Нет доступа к компании');
        const product = productByCode(body.product_code ?? '');
        if (!product) {
            return problem(422, 'VALIDATION_FAILED', 'Нет такого продукта', {
                errors: [{ field: 'product_code', message: 'Нет такого продукта' }],
            });
        }
        if (read().applications.some((a) => a.applicant.id === access.applicant_id && a.product_code === product.code && isActive(a))) {
            return problem(409, 'ACTIVE_APPLICATION_EXISTS', 'У компании уже есть активная заявка на этот продукт');
        }

        const { applicant_id: _applicant, ...input } = body;
        const number = nextApplicationNumber();
        const now = new Date().toISOString();
        const app: StoredApplication = {
            ...prefillFor(access.applicant_id),
            ...input,
            id: `app_${number}`,
            number: `A-2026-${number}`,
            version: 1,
            status: 'DRAFT',
            client_stage: 'FILLING',
            applicant: { id: access.applicant_id, short_name: access.short_name, inn: access.inn },
            product_code: product.code,
            created_by: { id: me.id, full_name: me.full_name },
            created_at: now,
            updated_at: now,
        };
        mutate((d) => d.applications.push(app));
        remember(app.id);
        return HttpResponse.json(toApplication(app), { status: 201, headers: withEtag(app.version) });
    }),

    http.get(url('/applications/:applicationId'), async ({ request, params }) => {
        await delay(200);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = ownApplication(me, params.applicationId);
        if (!app) return notFound();
        return HttpResponse.json(toApplication(app), { headers: withEtag(app.version) });
    }),

    http.patch(url('/applications/:applicationId'), async ({ request, params }) => {
        await delay(250);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = ownApplication(me, params.applicationId);
        if (!app) return notFound();
        if (app.status !== 'DRAFT') return invalidTransition(app);
        const conflict = versionMismatch(request, app);
        if (conflict) return conflict;

        const patch = (await request.json()) as Record<string, unknown>;
        // эти поля клиент не меняет
        for (const key of ['id', 'number', 'version', 'status', 'client_stage', 'applicant', 'created_at', 'updated_at']) delete patch[key];
        const next = mergePatch(app, patch);
        const errors = validateInput(next);
        if (errors.length) return problem(422, 'VALIDATION_FAILED', 'Проверьте данные', { errors });
        if (next.consents && (next.consents.personal_data || next.consents.credit_bureau) && !next.consents.accepted_at) {
            next.consents = { ...next.consents, accepted_at: new Date().toISOString() };
        }
        next.version = app.version + 1;
        next.updated_at = new Date().toISOString();
        const updated = mutate((d) => {
            const index = d.applications.findIndex((a) => a.id === app.id);
            d.applications[index] = next;
            return next;
        });
        return HttpResponse.json(toApplication(updated), { headers: withEtag(updated.version) });
    }),

    http.get(url('/applications/:applicationId/status'), async ({ request, params }) => {
        await delay(200);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = ownApplication(me, params.applicationId);
        if (!app) return notFound();
        return HttpResponse.json(statusViewFor(app, me.id), { headers: withEtag(app.version) });
    }),

    http.post(url('/applications/:applicationId/submit'), async ({ request, params }) => {
        await delay(500);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = ownApplication(me, params.applicationId);
        if (!app) return notFound();
        const { existing, remember } = idempotent(request, `submit:${app.id}`);
        if (existing && app.status !== 'DRAFT') return HttpResponse.json(statusViewFor(app, me.id), { status: 202 });
        if (app.status !== 'DRAFT') return invalidTransition(app);
        const conflict = versionMismatch(request, app);
        if (conflict) return conflict;

        const missing = completeness(app).missing_fields;
        const termsErrors = validateTerms(app.requested, app.product_code);
        const consentMissing = missing.filter((f) => f.startsWith('consents.'));
        const dataMissing = missing.filter((f) => !f.startsWith('consents.'));
        if (dataMissing.length || termsErrors.length) {
            return problem(422, 'DRAFT_INCOMPLETE', 'Заполните анкету до конца', {
                errors: [
                    ...dataMissing.map((field) => ({ field, message: `Не заполнено: ${fieldLabel(field)}`, code: 'required' })),
                    ...termsErrors,
                ],
            });
        }
        if (consentMissing.length) {
            return problem(422, 'CONSENT_REQUIRED', 'Нужны согласия', {
                errors: consentMissing.map((field) => ({ field, message: fieldLabel(field), code: 'required' })),
            });
        }

        mutate((d) => {
            const index = d.applications.findIndex((a) => a.id === app.id);
            d.applications[index] = { ...app, submitted_at: new Date().toISOString() };
        });
        // дальше заявка сама пойдёт по конвейеру (workflow.ts)
        const submitted = transition(app, 'SUBMITTED', {
            message: 'Заявка отправлена',
            actor: { id: me.id, full_name: me.full_name },
            next: true,
        });
        remember(app.id);
        return HttpResponse.json(statusViewFor(submitted, me.id), { status: 202 });
    }),

    // ---------------------------------------------------------------- documents
    http.get(url('/applications/:applicationId/documents'), async ({ request, params }) => {
        await delay(150);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = ownApplication(me, params.applicationId);
        if (!app) return notFound();
        return HttpResponse.json({ items: read().documents.filter((d) => d.application_id === app.id).map(toDocument) });
    }),

    http.post(url('/applications/:applicationId/documents'), async ({ request, params }) => {
        await delay(600);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = ownApplication(me, params.applicationId);
        if (!app) return notFound();
        if (!toApplication(app).allowed_actions.includes('UPLOAD_DOCUMENT')) return invalidTransition(app);

        const { existing, remember } = idempotent(request, `upload:${app.id}`);
        const repeated = existing && read().documents.find((d) => d.id === existing);
        if (repeated) return HttpResponse.json(toDocument(repeated), { status: 201 });

        const form = await request.formData();
        const file = form.get('file');
        const type = form.get('type') as DocumentType | null;
        const infoRequestId = form.get('info_request_id');
        const signedDocumentId = form.get('signed_document_id');
        if (infoRequestId && !read().infoRequests.some((r) => r.id === infoRequestId && r.application_id === app.id)) {
            return problem(422, 'VALIDATION_FAILED', 'Вопрос не найден', { errors: [{ field: 'info_request_id', message: 'Вопрос не найден' }] });
        }
        // откреплённая подпись (.sig/.p7s) — отдельный документ со ссылкой на подписанный
        if (type === 'SIGNATURE' && !read().documents.some((d) => d.id === signedDocumentId && d.application_id === app.id)) {
            return problem(422, 'VALIDATION_FAILED', 'Не указан подписанный документ', {
                errors: [{ field: 'signed_document_id', message: 'Сначала загрузите документ, к которому относится подпись' }],
            });
        }
        if (!(file instanceof File) || !type) {
            return problem(422, 'VALIDATION_FAILED', 'Нужен файл и тип документа', {
                errors: [{ field: 'file', message: 'Нужен файл и тип документа' }],
            });
        }
        if (file.size > MAX_UPLOAD_BYTES) {
            return problem(413, 'VALIDATION_FAILED', 'Файл больше 20 МБ', { errors: [{ field: 'file', message: 'Файл больше 20 МБ' }] });
        }
        const ext = file.name.split('.').pop()?.toLowerCase() ?? '';
        if (!UPLOAD_EXTENSIONS.includes(ext)) {
            return problem(415, 'VALIDATION_FAILED', 'Неподдерживаемый формат', {
                errors: [{ field: 'file', message: 'Подойдут PDF, DOCX, XLSX, JPG, PNG' }],
            });
        }
        const doc = addDocument({
            id: `doc_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`,
            application_id: app.id,
            type,
            file_name: file.name,
            mime_type: file.type || 'application/octet-stream',
            size_bytes: file.size,
            origin: 'CLIENT_UPLOAD',
            uploaded_by: { id: me.id, full_name: me.full_name },
            uploaded_at: new Date().toISOString(),
            info_request_id: typeof infoRequestId === 'string' && infoRequestId ? infoRequestId : null,
            signed_document_id: type === 'SIGNATURE' && typeof signedDocumentId === 'string' ? signedDocumentId : null,
            recognize_at: Date.now() + RECOGNITION_DELAY_MS,
            verify_at: Date.now() + RECOGNITION_DELAY_MS,
        });
        remember(doc.id);
        const current = read().applications.find((a) => a.id === app.id) ?? app;
        addTimeline(current, { type: 'DOCUMENT_UPLOADED', actor_kind: 'CLIENT', actor: { id: me.id, full_name: me.full_name }, message: `Загружен документ: ${file.name}` });
        return HttpResponse.json(doc, { status: 201 });
    }),

    http.delete(url('/applications/:applicationId/documents/:documentId'), async ({ request, params }) => {
        await delay(200);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = ownApplication(me, params.applicationId);
        if (!app) return notFound();
        if (app.status !== 'DRAFT' && app.status !== 'WAITING_FOR_INFO') return invalidTransition(app);
        const exists = read().documents.some((d) => d.id === params.documentId && d.application_id === app.id);
        if (!exists) return problem(404, 'NOT_FOUND', 'Документ не найден');
        mutate((d) => {
            d.documents = d.documents.filter((doc) => doc.id !== params.documentId);
        });
        return new HttpResponse(null, { status: 204 });
    }),
];
