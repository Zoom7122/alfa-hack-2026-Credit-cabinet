import { delay, http, HttpResponse } from 'msw';

import type { Me, SignatureReport, SigningMethod, SigningSession } from '../api/types';
import { buildReport, signatureSourceOf } from './signatures';
import type { StoredApplication } from './data';
import { currentUser, idempotent, invalidTransition, notFound, ownApplication, problem, unauthorized, url } from './http';
import { accountMasked, buildSigning, SIGNING_ATTEMPTS, type StoredOffer, type StoredSigning } from './seedWorkflow';
import { mutate, read, toDocument } from './store';
import { addTimeline, emit, signerOf, transition } from './workflow';

/* Подписание договора и выдача (экраны 11–13): signing, signing/start, signing/confirm, скачивание документов. */

const userRef = (me: Me) => ({ id: me.id, full_name: me.full_name });

const reportContext = (app: StoredApplication) => ({
    documents: read().documents,
    applicant: read().applicants.find((a) => a.id === app.applicant.id),
});

/** Сформировать договор и график при выборе предложения — сессия ждёт подписи директора. */
export function createSigning(app: StoredApplication, offer: StoredOffer, withdrawnOfferIds: string[] = []) {
    const { documents, session } = buildSigning(app, offer, Date.now());
    mutate((d) => {
        const old = new Set(d.documents.filter((doc) => doc.application_id === app.id && doc.origin === 'GENERATED').map((doc) => doc.id));
        // прошлый комплект и подписи к нему больше не действуют
        d.documents = d.documents
            .filter((doc) => !old.has(doc.id) && !(doc.type === 'SIGNATURE' && doc.signed_document_id && old.has(doc.signed_document_id)))
            .concat(documents);
        d.signing[app.id] = { ...session, withdrawn_offer_ids: withdrawnOfferIds };
    });
}

/** SigningSession для того, кто смотрит. Демо-код видит только подписант (SMS по ТЗ не используем). */
export function signingView(app: StoredApplication, session: StoredSigning, viewerId: string): SigningSession {
    const signer = signerOf(app);
    const { document_ids, withdrawn_offer_ids: _withdrawn, ...rest } = session;
    const awaitingMe = session.state === 'AWAITING_SIGNER' && signer.id === viewerId;
    return {
        ...rest,
        required_signer: signer,
        awaiting_me: awaitingMe,
        demo_code: awaitingMe ? (session.demo_code ?? null) : null,
        documents: read()
            .documents.filter((doc) => document_ids.includes(doc.id))
            .map(toDocument),
    };
}

function resolve(request: Request, id: unknown): { me: Me; app: StoredApplication; session?: StoredSigning } | Response {
    const me = currentUser(request);
    if (!me) return unauthorized();
    const app = ownApplication(me, id);
    if (!app) return notFound();
    return { me, app, session: read().signing[app.id] };
}

const newCode = () => String(Math.floor(100_000 + Math.random() * 900_000));

function updateSession(appId: string, patch: Partial<StoredSigning>) {
    return mutate((d) => {
        const next = { ...(d.signing[appId] as StoredSigning), ...patch };
        d.signing[appId] = next;
        return next;
    });
}

/** Подписано: выдача имитируется сразу (SIGNING → COMPLETED). */
function complete(app: StoredApplication, me: Me, method: SigningMethod, report: SignatureReport | null) {
    const offer = read().offers.find((o) => o.id === read().signing[app.id]?.offer_id);
    const now = new Date().toISOString();
    updateSession(app.id, {
        state: 'SIGNED',
        method,
        signed_at: now,
        demo_code: null,
        signature_report: report,
        disbursement: { amount: offer?.amount ?? app.requested?.amount, account_masked: accountMasked(app.applicant.inn), executed_at: now },
    });
    addTimeline(app, { type: 'SIGNED', actor_kind: 'CLIENT', actor: userRef(me), message: 'Договор подписан' });
    const done = transition(app, 'COMPLETED', { message: 'Кредит выдан', type: 'DISBURSED' });
    emit(app.applicant.id, 'signing.completed', { application_id: app.id });
    return done;
}

/** Попытки кончились или подпись недействительна — SIGNING → OFFERED (sign.failed), предложения снова доступны. */
function fail(app: StoredApplication, me: Me) {
    const session = updateSession(app.id, { state: 'FAILED', demo_code: null });
    // возвращаем выбранное и то, что отозвал сам выбор; отозванное раньше (отказ, истечение) не трогаем
    const back = new Set([session.offer_id, ...(session.withdrawn_offer_ids ?? [])]);
    mutate((d) => {
        const now = new Date().toISOString();
        d.offers = d.offers.map((o) => (o.application_id === app.id && back.has(o.id) && o.valid_until > now ? { ...o, state: 'ACTIVE' } : o));
    });
    transition(app, 'OFFERED', { message: 'Подписать не удалось — выберите предложение ещё раз', actor: userRef(me) });
}

export const signingHandlers = [
    http.get(url('/applications/:applicationId/signing'), async ({ request, params }) => {
        await delay(150);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        if (!ctx.session) return problem(404, 'NOT_FOUND', 'Подписание ещё не начато');
        return HttpResponse.json(signingView(ctx.app, ctx.session, ctx.me.id));
    }),

    http.post(url('/applications/:applicationId/signing/start'), async ({ request, params }) => {
        await delay(250);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        const { me, app } = ctx;
        if (app.status !== 'SIGNING') return invalidTransition(app);
        const { existing, remember } = idempotent(request, `signing-start:${app.id}`);
        if (existing && ctx.session) return HttpResponse.json(signingView(app, ctx.session, me.id));
        // по контракту start «формирует договор»: если комплекта ещё нет — собираем по принятому предложению
        if (!ctx.session || ctx.session.state !== 'AWAITING_SIGNER') {
            const accepted = read().offers.find((o) => o.application_id === app.id && o.state === 'ACCEPTED');
            if (!accepted) return invalidTransition(app);
            createSigning(app, accepted);
        }
        const current = read().signing[app.id] as StoredSigning;
        const body = ((await request.json().catch(() => ({}))) ?? {}) as { method?: SigningMethod };
        const signer = signerOf(app);
        const method = body.method ?? current.method;
        // код выдаём один раз на сессию, пока его не использовали
        const session = updateSession(app.id, {
            method,
            demo_code: method === 'SIMPLE_CODE' ? (current.demo_code ?? newCode()) : current.demo_code,
        });
        if (signer.id !== me.id) {
            addTimeline(app, { type: 'SIGNING_REQUESTED', actor_kind: 'CLIENT', actor: userRef(me), message: 'Напомнили директору о подписании' });
            emit(app.applicant.id, 'signing.requested', { application_id: app.id, required_signer_id: signer.id });
        }
        remember(app.id);
        return HttpResponse.json(signingView(app, session, me.id));
    }),

    http.post(url('/applications/:applicationId/signing/confirm'), async ({ request, params }) => {
        await delay(500);
        const ctx = resolve(request, params.applicationId);
        if (ctx instanceof Response) return ctx;
        const { me, app, session } = ctx;
        const { existing, remember } = idempotent(request, `signing-confirm:${app.id}`);
        if (existing && session?.state === 'SIGNED') return HttpResponse.json(signingView(app, session, me.id));
        if (app.status !== 'SIGNING' || !session || session.state !== 'AWAITING_SIGNER') return invalidTransition(app);
        if (signerOf(app).id !== me.id) return problem(403, 'NOT_AUTHORIZED_SIGNER', 'Подписать договор может только директор');

        const body = (await request.json()) as { method?: SigningMethod; code?: string; signature_document_id?: string };
        if (body.method === 'SIMPLE_CODE') {
            if (!body.code || !/^\d{6}$/.test(body.code)) {
                return problem(422, 'VALIDATION_FAILED', 'Код — 6 цифр', { errors: [{ field: 'code', message: 'Код — 6 цифр' }] });
            }
            if (body.code !== session.demo_code) {
                const left = (session.attempts_left ?? SIGNING_ATTEMPTS) - 1;
                updateSession(app.id, { attempts_left: left });
                if (left <= 0) fail(app, me);
                return problem(422, 'SIGNATURE_INVALID', left > 0 ? 'Неверный код' : 'Попытки закончились', {
                    detail: left > 0 ? `Неверный код. Осталось попыток: ${left}` : 'Попытки закончились — выберите предложение ещё раз',
                    errors: [{ field: 'code', message: 'Неверный код', code: 'invalid_code' }],
                });
            }
            const done = complete(app, me, 'SIMPLE_CODE', null);
            remember(app.id);
            return HttpResponse.json(signingView(done, read().signing[app.id] as StoredSigning, me.id));
        }
        if (body.method === 'QUALIFIED_SIGNATURE') {
            const contractId =
                read().documents.find((d) => session.document_ids.includes(d.id) && d.type === 'LOAN_AGREEMENT_DRAFT')?.id ?? '';
            const signature = read().documents.find((d) => d.id === body.signature_document_id && d.application_id === app.id);
            if (!signature || signature.type !== 'SIGNATURE' || signature.signed_document_id !== contractId) {
                return problem(422, 'SIGNATURE_INVALID', 'Подпись не относится к договору', {
                    errors: [{ field: 'signature_document_id', message: 'Загрузите подпись (.sig) к сгенерированному договору' }],
                });
            }
            const done = complete(app, me, 'QUALIFIED_SIGNATURE', buildReport(app, signature, reportContext(app)));
            remember(app.id);
            return HttpResponse.json(signingView(done, read().signing[app.id] as StoredSigning, me.id));
        }
        return problem(422, 'VALIDATION_FAILED', 'Выберите способ подписи', { errors: [{ field: 'method', message: 'Выберите способ подписи' }] });
    }),

    http.get(url('/applications/:applicationId/documents/:documentId/signature-report'), async ({ request, params }) => {
        await delay(200);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = ownApplication(me, params.applicationId);
        const doc = app && read().documents.find((d) => d.id === params.documentId && d.application_id === app.id);
        if (!app || !doc) return problem(404, 'NOT_FOUND', 'Документ не найден');
        // отчёт строим по самой подписи: откреплённой (.sig) или встроенной в файл
        const signature = signatureSourceOf(doc, read().documents);
        if (!signature || toDocument(doc).signatures?.state !== 'DONE') {
            return problem(404, 'NOT_FOUND', 'Документ не подписан или проверка ещё идёт');
        }
        return HttpResponse.json(buildReport(app, signature, reportContext(app)));
    }),

    http.get(url('/applications/:applicationId/documents/:documentId/content'), async ({ request, params }) => {
        await delay(150);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = ownApplication(me, params.applicationId);
        const doc = app && read().documents.find((d) => d.id === params.documentId && d.application_id === app.id);
        if (!doc) return problem(404, 'NOT_FOUND', 'Документ не найден');
        // в моке содержимого файлов нет — отдаём заглушку с названием
        const body = `%PDF-1.4\n% ${doc.file_name}\n% Демо-документ кредитного кабинета\n`;
        return new HttpResponse(body, {
            headers: {
                'Content-Type': doc.mime_type || 'application/octet-stream',
                'Content-Disposition': `attachment; filename*=UTF-8''${encodeURIComponent(doc.file_name)}`,
            },
        });
    }),
];
