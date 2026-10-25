import { delay, http, HttpResponse } from 'msw';

import type { ApplicationStatus, BoDecisionInput, BoDossier, Me, OfferInput, SourceCode, TimelineEvent } from '../api/types';
import { addDaysIso } from './dates';
import {
    applicantFullFor,
    engineDecisionFor,
    findingsFor,
    listItemFor,
    scoreFor,
    sourcesFor,
    type StoredBo,
} from './backoffice';
import type { StoredApplication } from './data';
import { currentUser, idempotent, invalidTransition, problem, unauthorized, url, versionMismatch } from './http';
import { annuity, type StoredInfoRequest, type StoredOffer } from './seedWorkflow';
import { mutate, productByCode, read, toApplication, toDocument, toSummary } from './store';
import { emit, transition } from './workflow';

/* Рабочее место банка (экраны 16–18): очередь, досье, «взять в работу», решение андеррайтера, перезапрос источника. */

const userRef = (me: Me) => ({ id: me.id, full_name: me.full_name });
const strip = <T extends { application_id: string }>({ application_id: _app, ...rest }: T) => rest;

function underwriter(request: Request): Me | Response {
    const me = currentUser(request);
    if (!me) return unauthorized();
    if (!me.roles.includes('UNDERWRITER')) return problem(403, 'FORBIDDEN', 'Нужна роль андеррайтера');
    return me;
}

const appById = (id: unknown) => read().applications.find((a) => a.id === id && a.status !== 'DRAFT');
const applicantOf = (app: StoredApplication) => read().applicants.find((a) => a.id === app.applicant.id);

function updateBo(appId: string, patch: Partial<StoredBo>) {
    return mutate((d) => {
        const next: StoredBo = { assignee: null, sla_due_at: null, ...d.bo[appId], ...patch };
        d.bo[appId] = next;
        return next;
    });
}

/** Внутренние заметки андеррайтера — в истории досье, клиент их не видит. */
function internalEvents(app: StoredApplication): TimelineEvent[] {
    // в TimelineEvent.type нет отдельного типа для заметок — берём STATUS_CHANGED без from/to
    return (read().bo[app.id]?.internal_comments ?? []).map((c, i) => ({
        id: `tle_internal_${app.id}_${i}`,
        at: c.at,
        type: 'STATUS_CHANGED',
        actor_kind: 'EMPLOYEE',
        message: `Внутренний комментарий: ${c.text}`,
    }));
}

export function dossierFor(app: StoredApplication): BoDossier {
    const now = Date.now();
    const applicant = applicantOf(app);
    const product = productByCode(app.product_code);
    const bo = read().bo[app.id];
    return {
        application: toApplication(app),
        applicant: applicant
            ? applicantFullFor(applicant, app)
            : { id: app.applicant.id, inn: app.applicant.inn, applicant_type: 'LEGAL_ENTITY', full_name: app.applicant.short_name, my_role: 'DIRECTOR' },
        sources: sourcesFor(app, applicant, bo, now),
        findings: findingsFor(app),
        score: scoreFor(app),
        engine_decision: engineDecisionFor(app, product?.rate_annual_from ?? 18, now, Boolean(bo)),
        offers: read().offers.filter((o) => o.application_id === app.id).map(strip),
        documents: read().documents.filter((d) => d.application_id === app.id).map(toDocument),
        info_requests: read().infoRequests.filter((r) => r.application_id === app.id).map(strip),
        other_applications: read()
            .applications.filter((a) => a.applicant.id === app.applicant.id && a.id !== app.id && a.status !== 'DRAFT')
            .map(toSummary),
        timeline: [
            ...read()
                .timeline.filter((e) => e.application_id === app.id)
                .map(strip),
            ...internalEvents(app),
        ].sort((a, b) => b.at.localeCompare(a.at) || b.id.localeCompare(a.id)),
    };
}

/** Порядок очереди: ручное рассмотрение по сроку SLA, остальное — свежие сверху. */
function queueOrder(a: StoredApplication, b: StoredApplication) {
    const bo = read().bo;
    const slaA = a.status === 'MANUAL_REVIEW' ? (bo[a.id]?.sla_due_at ?? '9') : '9';
    const slaB = b.status === 'MANUAL_REVIEW' ? (bo[b.id]?.sla_due_at ?? '9') : '9';
    return slaA.localeCompare(slaB) || b.updated_at.localeCompare(a.updated_at);
}

const STATUSES = new Set<ApplicationStatus>([
    'SUBMITTED', 'ENRICHING', 'VERIFYING', 'WAITING_FOR_INFO', 'SCORING', 'DECIDING', 'MANUAL_REVIEW',
    'OFFERED', 'SIGNING', 'COMPLETED', 'REJECTED', 'DECLINED', 'CANCELLED', 'EXPIRED',
]);

const DECIDE_ERRORS = {
    APPROVE: 'У каждого предложения должны быть сумма, срок и ставка',
    REJECT: 'Укажите хотя бы одну причину для клиента',
    REQUEST_INFO: 'Добавьте хотя бы один вопрос; для вопроса «только документ» укажите документ',
} as const;

/** Пустой offers — по контракту берутся proposed_offers движка. */
function offersToIssue(app: StoredApplication, input: BoDecisionInput): OfferInput[] {
    if (input.offers?.length) return input.offers;
    const proposed = engineDecisionFor(app, productByCode(app.product_code)?.rate_annual_from ?? 18, Date.now(), true)?.proposed_offers ?? [];
    return proposed.map((o) => ({
        kind: o.kind === 'REQUESTED' ? 'REQUESTED' : 'ALTERNATIVE',
        product_code: o.product_code,
        amount: o.amount,
        term_months: o.term_months,
        rate_annual: o.rate_annual,
        collateral_required: o.collateral_required,
        guarantee_required: o.guarantee_required,
        conditions: o.conditions,
        valid_days: 7,
    }));
}

function approve(app: StoredApplication, me: Me, input: BoDecisionInput) {
    const now = Date.now();
    const offers = offersToIssue(app, input).map(
        (o, i): StoredOffer => {
            const monthly = annuity(o.amount, o.term_months, o.rate_annual);
            const requested = app.requested;
            const diff =
                requested && o.amount < requested.amount
                    ? `Сумма меньше запрошенной на ${(requested.amount - o.amount).toLocaleString('ru-RU')} ₽`
                    : requested && o.term_months !== requested.term_months
                      ? `Срок ${o.term_months > requested.term_months ? 'больше' : 'меньше'} запрошенного на ${Math.abs(o.term_months - requested.term_months)} мес.`
                      : null;
            return {
                id: `ofr_${app.id}_bo_${now.toString(36)}_${i + 1}`,
                application_id: app.id,
                kind: o.kind,
                state: 'ACTIVE',
                recommended: i === 0,
                product_code: o.product_code,
                product_name: productByCode(o.product_code)?.name,
                amount: o.amount,
                term_months: o.term_months,
                rate_annual: o.rate_annual,
                monthly_payment: monthly,
                total_payment: monthly * o.term_months,
                full_cost_rate: Math.round((o.rate_annual + 0.8) * 10) / 10,
                collateral_required: o.collateral_required ?? [],
                guarantee_required: o.guarantee_required ?? false,
                conditions: o.conditions ?? [],
                difference_from_requested: diff,
                valid_until: addDaysIso(now, o.valid_days ?? 7),
            };
        },
    );
    const asRequested = offers.some((o) => o.amount === app.requested?.amount && o.term_months === app.requested?.term_months);
    mutate((d) => {
        d.offers = d.offers.filter((o) => o.application_id !== app.id).concat(offers);
        d.decisions[app.id] = {
            outcome: asRequested ? 'APPROVED' : 'APPROVED_WITH_CHANGES',
            decided_at: new Date(now).toISOString(),
            decided_by: 'EMPLOYEE',
            reasons: [],
            recommendations: [],
        };
    });
    const updated = transition(app, 'OFFERED', { message: 'Банк подготовил предложения', type: 'OFFERS_ISSUED', actor: userRef(me), actorKind: 'EMPLOYEE' });
    emit(app.applicant.id, 'offers.issued', { application_id: app.id, count: offers.length, kind: 'REQUESTED' });
    return updated;
}

function reject(app: StoredApplication, me: Me, input: BoDecisionInput) {
    const now = Date.now();
    mutate((d) => {
        d.decisions[app.id] = {
            outcome: 'REJECTED',
            decided_at: new Date(now).toISOString(),
            decided_by: 'EMPLOYEE',
            reasons: input.client_reasons ?? [],
            recommendations: ['Попробуйте запросить меньшую сумму или добавить залог'],
            reapply_after: addDaysIso(now, 30).slice(0, 10),
        };
    });
    return transition(app, 'REJECTED', { message: 'Банк отказал', actor: userRef(me), actorKind: 'EMPLOYEE' });
}

function requestInfo(app: StoredApplication, me: Me, input: BoDecisionInput) {
    const now = Date.now();
    const start = read().infoRequests.filter((r) => r.application_id === app.id).length;
    const requests = (input.info_requests ?? []).map(
        (r, i): StoredInfoRequest => ({
            id: `ir_${app.id}_${start + i + 1}`,
            application_id: app.id,
            state: 'OPEN',
            required: r.required,
            title: r.title,
            description: r.description,
            target_field: r.target_field ?? null,
            input: { type: r.input.type, options: r.input.options?.map((o) => ({ value: o.value ?? '', label: o.label ?? '' })) },
            documents: r.documents,
            created_at: new Date(now).toISOString(),
            due_at: addDaysIso(now, 5),
            answer: null,
        }),
    );
    mutate((d) => {
        d.infoRequests.push(...requests);
    });
    const updated = transition(app, 'WAITING_FOR_INFO', { message: 'Банк задал вопросы', type: 'INFO_REQUESTED', actor: userRef(me), actorKind: 'EMPLOYEE' });
    for (const r of requests) emit(app.applicant.id, 'info_request.created', { application_id: app.id, info_request_id: r.id });
    return updated;
}

function adjustmentsError(input: BoDecisionInput) {
    return (input.expert_adjustments ?? []).every((a) => a.code && Number.isFinite(a.points))
        ? undefined
        : { field: 'expert_adjustments', message: 'У поправки должны быть фактор и баллы числом' };
}

function validate(input: BoDecisionInput) {
    switch (input.action) {
        case 'APPROVE':
            return (input.offers ?? []).every((o) => o.amount > 0 && o.term_months > 0 && o.rate_annual > 0)
                ? adjustmentsError(input)
                : { field: 'offers', message: DECIDE_ERRORS.APPROVE };
        case 'REJECT':
            return input.client_reasons?.length && input.client_reasons.every((r) => r.title?.trim())
                ? adjustmentsError(input)
                : { field: 'client_reasons', message: DECIDE_ERRORS.REJECT };
        case 'REQUEST_INFO':
            return input.info_requests?.length && input.info_requests.every((r) => r.title?.trim() && (r.input.type !== 'NONE' || r.documents?.accepted_types?.length))
                ? adjustmentsError(input)
                : { field: 'info_requests', message: DECIDE_ERRORS.REQUEST_INFO };
        default:
            return { field: 'action', message: 'Неизвестное действие' };
    }
}

export const backofficeHandlers = [
    http.get(url('/backoffice/applications'), async ({ request }) => {
        await delay(250);
        const me = underwriter(request);
        if (me instanceof Response) return me;
        const params = new URL(request.url).searchParams;
        const statuses = params.get('status')?.split(',').filter(Boolean) ?? [];
        if (statuses.some((s) => !STATUSES.has(s as ApplicationStatus))) {
            return problem(422, 'VALIDATION_FAILED', 'Неизвестный статус', { errors: [{ field: 'status', message: 'Неизвестный статус' }] });
        }
        const assignee = params.get('assignee');
        const limit = Number(params.get('limit') ?? 20);
        const now = Date.now();
        const items = read()
            .applications.filter((a) => a.status !== 'DRAFT')
            .filter((a) => !statuses.length || statuses.includes(a.status))
            .filter((a) => {
                const who = read().bo[a.id]?.assignee?.id ?? null;
                if (!assignee) return true;
                if (assignee === 'none') return who === null;
                return who === (assignee === 'me' ? me.id : assignee);
            })
            .sort(queueOrder)
            .slice(0, limit)
            .map((a) => listItemFor(a, applicantOf(a), read().bo[a.id], now));
        return HttpResponse.json({ items, next_cursor: null });
    }),

    http.get(url('/backoffice/applications/:applicationId'), async ({ request, params }) => {
        await delay(300);
        const me = underwriter(request);
        if (me instanceof Response) return me;
        const app = appById(params.applicationId);
        if (!app) return problem(404, 'NOT_FOUND', 'Заявка не найдена');
        return HttpResponse.json(dossierFor(app));
    }),

    http.post(url('/backoffice/applications/:applicationId/assign'), async ({ request, params }) => {
        await delay(250);
        const me = underwriter(request);
        if (me instanceof Response) return me;
        const app = appById(params.applicationId);
        if (!app) return problem(404, 'NOT_FOUND', 'Заявка не найдена');
        if (app.status !== 'MANUAL_REVIEW') return invalidTransition(app);
        const current = read().bo[app.id]?.assignee;
        if (current && current.id !== me.id) {
            return problem(409, 'CONFLICT', `Заявку уже взял(а) в работу ${current.full_name}`);
        }
        const bo = updateBo(app.id, { assignee: userRef(me) });
        emit(app.applicant.id, 'backoffice.queue_changed', { application_id: app.id, status: app.status });
        return HttpResponse.json(listItemFor(app, applicantOf(app), bo, Date.now()));
    }),

    http.post(url('/backoffice/applications/:applicationId/decision'), async ({ request, params }) => {
        await delay(500);
        const me = underwriter(request);
        if (me instanceof Response) return me;
        const app = appById(params.applicationId);
        if (!app) return problem(404, 'NOT_FOUND', 'Заявка не найдена');
        const { existing, remember } = idempotent(request, `bo-decision:${app.id}`);
        if (existing && app.status !== 'MANUAL_REVIEW') return HttpResponse.json(dossierFor(app));
        if (app.status !== 'MANUAL_REVIEW') {
            return problem(409, 'INVALID_TRANSITION', 'Решение принимают только на ручном рассмотрении', { current_status: app.status });
        }
        // в контракте нет ошибки «заявка у другого исполнителя» — мок отвечает 409 CONFLICT
        const assignee = read().bo[app.id]?.assignee;
        if (assignee && assignee.id !== me.id) {
            return problem(409, 'CONFLICT', `Заявка в работе у ${assignee.full_name}`);
        }
        const conflict = versionMismatch(request, app);
        if (conflict) return conflict;
        const input = (await request.json()) as BoDecisionInput;
        const error = validate(input);
        if (error) return problem(422, 'VALIDATION_FAILED', error.message, { errors: [error] });

        const at = new Date().toISOString();
        const comments = [
            ...(input.expert_adjustments ?? []).map((a) => `поправка ${a.code} ${a.points > 0 ? '+' : ''}${a.points}${a.comment ? ` — ${a.comment}` : ''}`),
            ...(input.internal_comment?.trim() ? [input.internal_comment.trim()] : []),
        ].map((text) => ({ at, text }));
        updateBo(app.id, {
            assignee: read().bo[app.id]?.assignee ?? userRef(me),
            internal_comments: [...(read().bo[app.id]?.internal_comments ?? []), ...comments],
        });
        const updated =
            input.action === 'APPROVE' ? approve(app, me, input) : input.action === 'REJECT' ? reject(app, me, input) : requestInfo(app, me, input);
        remember(app.id);
        return HttpResponse.json(dossierFor(updated));
    }),

    http.post(url('/backoffice/applications/:applicationId/sources/:sourceCode/refresh'), async ({ request, params }) => {
        await delay(200);
        const me = underwriter(request);
        if (me instanceof Response) return me;
        const app = appById(params.applicationId);
        if (!app) return problem(404, 'NOT_FOUND', 'Заявка не найдена');
        const code = params.sourceCode as SourceCode;
        const source = sourcesFor(app, applicantOf(app), read().bo[app.id], Date.now()).find((s) => s.code === code);
        // в контракте у refresh только 202: неприменимый источник просто возвращаем как есть
        if (!source || source.state === 'SKIPPED') return HttpResponse.json(source ?? null, { status: 202 });
        const bo = updateBo(app.id, { refresh_at: { ...read().bo[app.id]?.refresh_at, [code]: Date.now() + 2500 } });
        const running = sourcesFor(app, applicantOf(app), bo, Date.now()).find((s) => s.code === code);
        return HttpResponse.json(running, { status: 202 });
    }),
];
