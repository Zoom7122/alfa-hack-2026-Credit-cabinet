import { delay, http, HttpResponse, ws } from 'msw';

import type { DeclineReason, InfoRequestAnswerInput, Me } from '../api/types';
import type { StoredApplication } from './data';
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
} from './http';
import { buildRetentionOffer, type StoredInfoRequest } from './seedWorkflow';
import { meFor, mutate, read } from './store';
import { addTimeline, emit, eventsSince, lastEventId, statusViewFor, tick, transition } from './workflow';

/* Рассмотрение, вопросы банка, решение и предложения, отзыв заявки, WebSocket (экраны 07–10). */

const DECLINE_REASONS: DeclineReason[] = [
    'RATE_TOO_HIGH', 'AMOUNT_TOO_LOW', 'TERM_NOT_SUITABLE', 'COLLATERAL_REQUIRED', 'FOUND_ELSEWHERE', 'NO_LONGER_NEEDED', 'OTHER',
];

const strip = <T extends { application_id: string }>({ application_id: _app, ...rest }: T) => rest;
const userRef = (me: Me) => ({ id: me.id, full_name: me.full_name });

/** Общая шапка обработчика: пользователь + своя заявка. */
function resolve(request: Request, id: unknown): { me: Me; app: StoredApplication } | Response {
    const me = currentUser(request);
    if (!me) return unauthorized();
    const app = ownApplication(me, id);
    if (!app) return notFound();
    return { me, app };
}

/** Проверка ответа по типу поля вопроса. Сообщение — если ответ не подходит. */
function answerError(req: StoredInfoRequest, body: InfoRequestAnswerInput): string | undefined {
    const { value } = body;
    switch (req.input.type) {
        case 'MONEY':
        case 'NUMBER':
            if (typeof value !== 'number' || Number.isNaN(value)) return 'Укажите число';
            if (req.input.min !== undefined && value < req.input.min) return `Не меньше ${req.input.min}`;
            if (req.input.max !== undefined && value > req.input.max) return `Не больше ${req.input.max}`;
            return undefined;
        case 'TEXT':
        case 'DATE':
            return typeof value === 'string' && value.trim() ? undefined : 'Заполните ответ';
        case 'BOOLEAN':
            return typeof value === 'boolean' ? undefined : 'Выберите да или нет';
        case 'CHOICE':
            return req.input.options?.some((o) => o.value === value) ? undefined : 'Выберите вариант';
        default:
            return undefined;
    }
}

/** Записать значение ответа в поле заявки (target_field, dot-path). */
function applyToField(app: StoredApplication, path: string, value: unknown): StoredApplication {
    const next = structuredClone(app) as unknown as Record<string, unknown>;
    const keys = path.split('.');
    let node = next;
    for (const key of keys.slice(0, -1)) {
        node[key] = (node[key] as Record<string, unknown> | undefined) ?? {};
        node = node[key] as Record<string, unknown>;
    }
    node[keys.at(-1) ?? path] = value;
    return next as unknown as StoredApplication;
}

export const workflowHandlers = [
    http.get(url('/applications/:applicationId/timeline'), async ({ request, params }) => {
        await delay(150);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        const items = read()
            .timeline.filter((e) => e.application_id === ctx.app.id)
            .sort((a, b) => b.at.localeCompare(a.at) || b.id.localeCompare(a.id))
            .map(strip);
        return HttpResponse.json({ items });
    }),

    // ---------------------------------------------------------------- вопросы банка
    http.get(url('/applications/:applicationId/info-requests'), async ({ request, params }) => {
        await delay(150);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        const state = new URL(request.url).searchParams.get('state');
        const items = read()
            .infoRequests.filter((r) => r.application_id === ctx.app.id && (!state || r.state === state))
            .map(strip);
        return HttpResponse.json({ items });
    }),

    http.post(url('/applications/:applicationId/info-requests/:infoRequestId/answer'), async ({ request, params }) => {
        await delay(300);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        const { me, app } = ctx;
        const req = read().infoRequests.find((r) => r.id === params.infoRequestId && r.application_id === app.id);
        if (!req) return problem(404, 'NOT_FOUND', 'Вопрос не найден');
        const { existing, remember } = idempotent(request, `answer:${req.id}`);
        if (existing && req.state === 'ANSWERED') {
            return HttpResponse.json({ info_request: strip(req), application_status: statusViewFor(app, me.id) });
        }
        if (app.status !== 'WAITING_FOR_INFO' || req.state !== 'OPEN') return invalidTransition(app);

        const body = (await request.json()) as InfoRequestAnswerInput;
        const errors: { field: string; message: string }[] = [];
        const valueError = answerError(req, body);
        if (valueError) errors.push({ field: 'value', message: valueError });
        const docIds = body.document_ids ?? [];
        const docs = read().documents.filter((d) => d.application_id === app.id && docIds.includes(d.id));
        if (docs.length !== docIds.length) errors.push({ field: 'document_ids', message: 'Документ не найден' });
        const accepted = req.documents?.accepted_types;
        // откреплённые подписи к приложенным документам — не отдельный ответ, их не проверяем на тип
        const answerDocs = docs.filter((d) => !(d.type === 'SIGNATURE' && d.signed_document_id && docIds.includes(d.signed_document_id)));
        if (req.documents?.required && !answerDocs.length) errors.push({ field: 'document_ids', message: 'Загрузите документ' });
        if (accepted?.length && answerDocs.some((d) => !accepted.includes(d.type))) {
            errors.push({ field: 'document_ids', message: 'Этот документ не подходит к вопросу' });
        }
        if (errors.length) return problem(422, 'VALIDATION_FAILED', 'Проверьте ответ', { errors });

        const answered = mutate((d) => {
            const index = d.infoRequests.findIndex((r) => r.id === req.id);
            const next: StoredInfoRequest = {
                ...req,
                state: 'ANSWERED',
                answer: { ...body, document_ids: docIds, answered_at: new Date().toISOString(), answered_by: userRef(me) },
            };
            d.infoRequests[index] = next;
            if (req.target_field && body.value !== undefined) {
                const appIndex = d.applications.findIndex((a) => a.id === app.id);
                const current = d.applications[appIndex];
                if (current) d.applications[appIndex] = { ...applyToField(current, req.target_field, body.value), version: current.version + 1 };
            }
            return next;
        });
        remember(req.id);

        let current = read().applications.find((a) => a.id === app.id) ?? app;
        if (req.target_field && body.value !== undefined) {
            emit(app.applicant.id, 'application.updated', { application_id: app.id, version: current.version });
        }
        const openRequired = read().infoRequests.some((r) => r.application_id === app.id && r.state === 'OPEN' && r.required);
        if (openRequired) {
            addTimeline(current, { type: 'INFO_PROVIDED', actor_kind: 'CLIENT', actor: userRef(me), message: `Ответ: ${req.title}` });
        } else {
            // все обязательные отвечены — переобогащение и дальше по конвейеру (одно событие в истории, не два)
            current = transition(current, 'ENRICHING', { message: 'Вы ответили на вопросы банка', type: 'INFO_PROVIDED', actor: userRef(me), next: true });
        }
        return HttpResponse.json({ info_request: strip(answered), application_status: statusViewFor(current, me.id) });
    }),

    // ---------------------------------------------------------------- решение и предложения
    http.get(url('/applications/:applicationId/decision'), async ({ request, params }) => {
        await delay(150);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        const decision = read().decisions[ctx.app.id];
        return decision ? HttpResponse.json(decision) : problem(404, 'NOT_FOUND', 'Решения ещё нет');
    }),

    http.get(url('/applications/:applicationId/offers'), async ({ request, params }) => {
        await delay(200);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        return HttpResponse.json({ items: read().offers.filter((o) => o.application_id === ctx.app.id).map(strip) });
    }),

    http.post(url('/applications/:applicationId/offers/:offerId/accept'), async ({ request, params }) => {
        await delay(400);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        const { me, app } = ctx;
        const { existing, remember } = idempotent(request, `accept:${app.id}`);
        if (existing && app.status === 'SIGNING') return HttpResponse.json(statusViewFor(app, me.id));
        if (app.status !== 'OFFERED') return invalidTransition(app);
        const conflict = versionMismatch(request, app);
        if (conflict) return conflict;
        const offer = read().offers.find((o) => o.id === params.offerId && o.application_id === app.id);
        if (!offer) return problem(404, 'NOT_FOUND', 'Предложение не найдено');
        if (offer.state !== 'ACTIVE' || offer.valid_until < new Date().toISOString()) {
            return problem(410, 'OFFER_EXPIRED', 'Предложение больше не действует');
        }
        mutate((d) => {
            d.offers = d.offers.map((o) =>
                o.application_id !== app.id ? o : { ...o, state: o.id === offer.id ? 'ACCEPTED' : o.state === 'ACTIVE' ? 'WITHDRAWN' : o.state },
            );
        });
        const signing = transition(app, 'SIGNING', { message: 'Вы выбрали предложение', type: 'OFFER_ACCEPTED', actor: userRef(me) });
        const view = statusViewFor(signing, me.id);
        addTimeline(signing, { type: 'SIGNING_REQUESTED', actor_kind: 'SYSTEM', message: 'Договор готов к подписанию' });
        emit(app.applicant.id, 'signing.requested', { application_id: app.id, required_signer_id: view.signing?.required_signer?.id });
        remember(offer.id);
        return HttpResponse.json(view);
    }),

    http.post(url('/applications/:applicationId/offers/decline'), async ({ request, params }) => {
        await delay(400);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        const { me, app } = ctx;
        // повтор с тем же ключом (обрыв сети) — тот же ответ, хотя версия уже выросла
        const { existing, remember } = idempotent(request, `decline:${app.id}`);
        if (existing) {
            const retention = read().offers.find((o) => o.id === existing);
            return HttpResponse.json({ application_status: statusViewFor(app, me.id), retention_offers: retention ? [strip(retention)] : [] });
        }
        if (app.status !== 'OFFERED') return invalidTransition(app);
        const conflict = versionMismatch(request, app);
        if (conflict) return conflict;
        const { reason, comment } = (await request.json()) as { reason?: DeclineReason; comment?: string };
        if (!reason || !DECLINE_REASONS.includes(reason)) {
            return problem(422, 'VALIDATION_FAILED', 'Укажите причину', { errors: [{ field: 'reason', message: 'Укажите причину' }] });
        }
        if (comment && comment.length > 1000) {
            return problem(422, 'VALIDATION_FAILED', 'Комментарий длиннее 1000 символов', { errors: [{ field: 'comment', message: 'До 1000 символов' }] });
        }

        const declines = read().declines[app.id] ?? 0;
        const base = read().offers.find((o) => o.application_id === app.id && o.state === 'ACTIVE' && o.kind !== 'RETENTION');
        mutate((d) => {
            d.declines[app.id] = declines + 1;
        });
        // первый отказ, где можно помочь условиями, — удерживающее предложение, заявка остаётся в OFFERED
        if (declines === 0 && base && reason !== 'NO_LONGER_NEEDED') {
            const retention = buildRetentionOffer(app, base, Date.now());
            mutate((d) => {
                d.offers.push(retention);
            });
            const updated = transition(app, 'OFFERED', {
                message: 'Вы отказались от предложений — банк предложил другие условия',
                type: 'OFFERS_DECLINED',
                actor: userRef(me),
            });
            emit(app.applicant.id, 'offers.issued', { application_id: app.id, count: 1, kind: 'RETENTION' });
            remember(retention.id);
            return HttpResponse.json({ application_status: statusViewFor(updated, me.id), retention_offers: [strip(retention)] });
        }

        mutate((d) => {
            d.offers = d.offers.map((o) => (o.application_id === app.id && o.state === 'ACTIVE' ? { ...o, state: 'WITHDRAWN' } : o));
        });
        const declined = transition(app, 'DECLINED', { message: 'Вы отказались от предложений', type: 'OFFERS_DECLINED', actor: userRef(me) });
        remember('declined');
        return HttpResponse.json({ application_status: statusViewFor(declined, me.id), retention_offers: [] });
    }),

    // ---------------------------------------------------------------- отзыв
    http.post(url('/applications/:applicationId/cancel'), async ({ request, params }) => {
        await delay(300);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        const { me, app } = ctx;
        const { existing, remember } = idempotent(request, `cancel:${app.id}`);
        if (existing && app.status === 'CANCELLED') return HttpResponse.json(statusViewFor(app, me.id));
        if (!statusViewFor(app, me.id).allowed_actions.includes('CANCEL')) return invalidTransition(app);
        const body = ((await request.json().catch(() => ({}))) ?? {}) as { comment?: string };
        mutate((d) => {
            d.offers = d.offers.map((o) => (o.application_id === app.id && o.state === 'ACTIVE' ? { ...o, state: 'WITHDRAWN' } : o));
            d.infoRequests = d.infoRequests.map((r) => (r.application_id === app.id && r.state === 'OPEN' ? { ...r, state: 'CANCELLED' } : r));
        });
        const cancelled = transition(app, 'CANCELLED', {
            message: body.comment ? `Заявка отозвана: ${body.comment}` : 'Заявка отозвана',
            actor: userRef(me),
        });
        remember(app.id);
        return HttpResponse.json(statusViewFor(cancelled, me.id));
    }),
];

/** ws://<хост>/api/v1/ws?token=… — события по компаниям пользователя (docs/websocket.md). */
const wsUrl = new URL(url('/ws'), location.href);
wsUrl.protocol = wsUrl.protocol === 'https:' ? 'wss:' : 'ws:';
const socket = ws.link(wsUrl.toString());

export const wsHandlers = [
    socket.addEventListener('connection', ({ client }) => {
        const token = new URL(client.url).searchParams.get('token') ?? '';
        const me = token.startsWith(MOCK_TOKEN_PREFIX) ? meFor(token.slice(MOCK_TOKEN_PREFIX.length)) : undefined;
        if (!me) {
            client.close(4401, 'unauthorized');
            return;
        }
        const companies = companyIds(me);
        let last = new URL(client.url).searchParams.get('last_event_id') ?? lastEventId();
        // сокет сам «двигает» конвейер, чтобы статус менялся и без запросов со страницы
        const timer = setInterval(() => {
            tick();
            for (const event of eventsSince(last, companies)) {
                const { applicant_id: _applicant, ...envelope } = event;
                client.send(JSON.stringify(envelope));
                last = event.id;
            }
        }, 500);
        client.addEventListener('message', (e) => {
            try {
                if ((JSON.parse(String(e.data)) as { type?: string }).type === 'ping') {
                    client.send(JSON.stringify({ type: 'pong', at: new Date().toISOString(), data: {} }));
                }
            } catch {
                /* не JSON — игнорируем */
            }
        });
        client.addEventListener('close', () => clearInterval(timer));
    }),
];
