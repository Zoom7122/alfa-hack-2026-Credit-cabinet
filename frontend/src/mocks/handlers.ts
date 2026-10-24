import { delay, http, HttpResponse } from 'msw';

import { API_BASE_URL } from '../api/client';
import type { ApplicationStatus, Me, Problem } from '../api/types';
import { applicants, applications, meFor, products, statusViewFor, testUsers } from './data';

const url = (path: string) => `${API_BASE_URL}${path}`;
const TOKEN_PREFIX = 'mock.';

const STATUSES: ApplicationStatus[] = [
    'DRAFT', 'SUBMITTED', 'ENRICHING', 'VERIFYING', 'WAITING_FOR_INFO', 'SCORING', 'DECIDING', 'MANUAL_REVIEW',
    'OFFERED', 'SIGNING', 'COMPLETED', 'REJECTED', 'DECLINED', 'CANCELLED', 'EXPIRED',
];

function problem(status: number, code: Problem['code'], title: string, extra?: Partial<Problem>) {
    return HttpResponse.json<Problem>(
        { type: `https://errors.credit-cabinet.local/${code.toLowerCase().replace(/_/g, '-')}`, title, status, code, ...extra },
        { status, headers: { 'Content-Type': 'application/problem+json' } },
    );
}

/** Пользователь по Bearer-токену. Токен мока — `mock.<user_id>`. */
function currentUser(request: Request): Me | undefined {
    const token = (request.headers.get('Authorization') ?? '').replace(/^Bearer\s+/i, '');
    if (!token.startsWith(TOKEN_PREFIX)) return undefined;
    return meFor(token.slice(TOKEN_PREFIX.length));
}

const unauthorized = () => problem(401, 'UNAUTHORIZED', 'Нужно войти');
const companyIds = (me: Me) => new Set(me.companies.map((c) => c.applicant_id));

export const handlers = [
    http.get(url('/auth/test-users'), async () => {
        await delay(300);
        return HttpResponse.json({ items: testUsers });
    }),

    http.post(url('/auth/mock-login'), async ({ request }) => {
        await delay(300);
        const { user_id } = (await request.json()) as { user_id: string };
        const user = meFor(user_id);
        if (!user) return problem(404, 'NOT_FOUND', 'Пользователь не найден');
        return HttpResponse.json({ token: `${TOKEN_PREFIX}${user_id}`, user });
    }),

    http.get(url('/me'), async ({ request }) => {
        await delay(150);
        const me = currentUser(request);
        return me ? HttpResponse.json(me) : unauthorized();
    }),

    http.get(url('/products'), async ({ request }) => {
        await delay(150);
        if (!currentUser(request)) return unauthorized();
        return HttpResponse.json({ items: products });
    }),

    http.get(url('/applicants'), async ({ request }) => {
        await delay(200);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const ids = companyIds(me);
        return HttpResponse.json({
            items: applicants
                .filter((a) => ids.has(a.id))
                .map((a) => {
                    const access = me.companies.find((c) => c.applicant_id === a.id);
                    return { ...a, my_role: access?.role ?? a.my_role, can_sign: access?.can_sign ?? false };
                }),
        });
    }),

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
        const items = applications
            .filter((a) => allowed.has(a.applicant.id))
            .filter((a) => !applicantId || a.applicant.id === applicantId)
            .filter((a) => !statuses.length || statuses.includes(a.status))
            .sort((a, b) => b.updated_at.localeCompare(a.updated_at))
            .slice(0, limit);
        return HttpResponse.json({ items, next_cursor: null });
    }),

    http.get(url('/applications/:applicationId/status'), async ({ request, params }) => {
        await delay(200);
        const me = currentUser(request);
        if (!me) return unauthorized();
        const app = applications.find((a) => a.id === params.applicationId);
        // чужие заявки не раскрываем — как будто их нет
        if (!app || !companyIds(me).has(app.applicant.id)) return problem(404, 'NOT_FOUND', 'Заявка не найдена');
        return HttpResponse.json(statusViewFor(app, me.id));
    }),
];
