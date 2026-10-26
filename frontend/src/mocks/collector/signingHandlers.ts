import { delay, http, HttpResponse } from 'msw';

import type { CollectorSigningSession } from '../../api/collector/types';
import { authenticate, notFound, problem, scoredAt, statusOf, unauthorized, url } from './handlers';
import { offersOf } from './outcome';
import { mutateCollector, newId, readCollector, type StoredCollectorApp } from './store';
import { COLLECTOR_TEST_USERS } from './users';

function resolve(request: Request, id: unknown) {
    const auth = authenticate(request);
    if (auth.error) return auth.error;
    if (!auth.user) return unauthorized('войдите в кабинет');
    const user = auth.user;
    if (!user.roles.includes('CLIENT')) return problem(403, 'FORBIDDEN', 'Договор подписывает представитель компании');
    const app = readCollector().apps.find((a) => a.id === id);
    if (!app) return notFound('заявки нет');
    if (app.representative.id !== user.id && !user.companies.some((c) => c.inn === app.company.inn)) return problem(403, 'FORBIDDEN', 'Заявка другой компании');
    return { app, user };
}

function response(s: CollectorSigningSession, actor: string) {
    const awaiting = s.state === 'AWAITING_SIGNER' && s.required_signer.id === actor;
    return HttpResponse.json({ ...s, awaiting_me: awaiting, demo_code: awaiting ? s.demo_code : undefined });
}

function event(app: StoredCollectorApp, type: string, message: string, actor: 'CLIENT' | 'SYSTEM') {
    (app.signing_events ??= []).push({ id: newId('evt'), at: new Date().toISOString(), type, message, actor_kind: actor });
    app.updated_at = new Date().toISOString();
}

export const collectorSigningHandlers = [
    http.post(url('/applications/:id/offers/:offerId/accept'), async ({ request, params }) => {
        await delay(150);
        const ctx = resolve(request, params.id);
        if (ctx instanceof Response) return ctx;
        const { app, user } = ctx;
        if (app.signing && ['SIGNING', 'COMPLETED'].includes(statusOf(app)) && app.signing.offer_id === params.offerId) return response(app.signing, user.id);
        if (statusOf(app) !== 'OFFERED') return problem(409, 'INVALID_TRANSITION', 'Заявка не ждёт выбора предложения');
        const offer = offersOf(app, scoredAt(app)).find((o) => o.id === params.offerId);
        if (!offer) return notFound('предложения нет');
        if (offer.state !== 'ACTIVE' || Date.parse(offer.valid_until ?? '') <= Date.now()) return problem(410, 'OFFER_EXPIRED', 'Предложение истекло');
        const signer = app.representative.can_sign ? app.representative : COLLECTOR_TEST_USERS.find((u) => u.companies.some((c) => c.inn === app.company.inn && c.can_sign));
        if (!signer) return problem(403, 'NOT_AUTHORIZED_SIGNER', 'Подписант должен быть зарегистрирован в кабинете');
        const content = `ДЕМОНСТРАЦИОННЫЙ КРЕДИТНЫЙ ДОГОВОР\nЗаявка: ${app.number}\nИНН: ${app.company.inn}\nСумма: ${offer.amount} RUB\nСрок: ${offer.term_months} мес.\nСтавка: ${offer.rate_annual} %\nПлатёж: ${offer.monthly_payment} RUB\nУчебный документ. Реальные выплаты не выполняются.\n`;
        const bytes = new TextEncoder().encode(content);
        const digest = await crypto.subtle.digest('SHA-256', bytes);
        const hash = [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('');
        const now = new Date().toISOString();
        const doc = { id: newId('doc'), type: 'LOAN_AGREEMENT_DRAFT' as const, file_name: `Кредитный договор ${app.number}.txt`, mime_type: 'text/plain; charset=utf-8', size_bytes: bytes.length, sha256: hash, signature_state: 'NOT_SIGNED' as const, recognition_state: 'NOT_APPLICABLE' as const, uploaded_at: now };
        const code = String((crypto.getRandomValues(new Uint32Array(1))[0] ?? 0) % 1_000_000).padStart(6, '0');
        const session: CollectorSigningSession = { id: newId('sgn'), state: 'AWAITING_SIGNER', method: 'SIMPLE_CODE', profile: 'MOCK', offer_id: offer.id ?? '', required_signer: { id: signer.id, full_name: signer.full_name }, awaiting_me: user.id === signer.id, demo_code: code, code_expires_at: new Date(Date.now() + 600_000).toISOString(), attempts_left: 3, documents: [doc], document_sha256: hash };
        mutateCollector(() => { app.signing = session; app.signing_contract = content; app.documents.push(doc); event(app, 'offer.accepted', 'Предложение выбрано, договор готов к подписанию', 'CLIENT'); });
        return response(session, user.id);
    }),
    http.get(url('/applications/:id/signing'), async ({ request, params }) => {
        const ctx = resolve(request, params.id);
        if (ctx instanceof Response) return ctx;
        return ctx.app.signing ? response(ctx.app.signing, ctx.user.id) : notFound('подписание ещё не началось');
    }),
    http.post(url('/applications/:id/signing/confirm'), async ({ request, params }) => {
        await delay(150);
        const ctx = resolve(request, params.id);
        if (ctx instanceof Response) return ctx;
        const { app, user } = ctx;
        const s = app.signing;
        if (!s) return notFound('подписание ещё не началось');
        if (s.required_signer.id !== user.id) return problem(403, 'NOT_AUTHORIZED_SIGNER', 'Договор подписывает директор компании или сам ИП');
        const body = (await request.json()) as { code?: string; method?: string };
        if (!/^\d{6}$/.test(body.code ?? '') || (body.method && body.method !== 'SIMPLE_CODE')) return problem(422, 'VALIDATION_FAILED', 'Введите 6 цифр');
        if (s.state === 'SIGNED') return response(s, user.id);
        if (s.state !== 'AWAITING_SIGNER') return problem(409, 'INVALID_TRANSITION', 'Выберите предложение заново');
        const expired = Date.parse(s.code_expires_at) <= Date.now();
        if (expired || body.code !== s.demo_code) {
            mutateCollector(() => {
                s.attempts_left = expired ? 0 : s.attempts_left - 1;
                if (s.attempts_left === 0) { s.state = 'FAILED'; delete s.demo_code; event(app, 'sign.failed', 'Подписание не завершено — выберите предложение заново', 'CLIENT'); }
            });
            return problem(expired ? 410 : 422, expired ? 'CODE_EXPIRED' : 'SIGNATURE_INVALID', 'Не удалось подписать', s.attempts_left ? `Неверный код. Осталось попыток: ${s.attempts_left}` : 'Выберите предложение заново, чтобы получить новый код');
        }
        const offer = offersOf(app, scoredAt(app)).find((o) => o.id === s.offer_id);
        mutateCollector(() => {
            s.state = 'SIGNED'; s.signed_at = new Date().toISOString(); delete s.demo_code;
            s.disbursement = { amount: offer?.amount, account_masked: 'демонстрационный счёт', executed_at: s.signed_at };
            event(app, 'sign.confirmed', 'Договор подписан демонстрационным кодом', 'CLIENT');
            event(app, 'disbursement.simulated', 'Демонстрационная выдача кредита завершена', 'SYSTEM');
        });
        return response(s, user.id);
    }),
    http.get(url('/applications/:id/documents/:documentId/content'), ({ params }) => {
        const app = readCollector().apps.find((a) => a.id === params.id);
        const doc = app?.signing?.documents.find((d) => d.id === params.documentId);
        if (!doc || !app?.signing_contract) return;
        return new HttpResponse(app.signing_contract, { headers: { 'Content-Type': 'text/plain; charset=utf-8', 'Content-Disposition': `attachment; filename*=UTF-8''${encodeURIComponent(doc.file_name ?? 'contract.txt')}` } });
    }),
];
