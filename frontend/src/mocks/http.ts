import { HttpResponse } from 'msw';

import { API_BASE_URL } from '../api/client';
import type { Me, Problem } from '../api/types';
import type { StoredApplication } from './data';
import { meFor, mutate, read, toApplication } from './store';

/* Общее для MSW-обработчиков: пользователь по токену, Problem, проверки версии и идемпотентность. */

export const url = (path: string) => `${API_BASE_URL}${path}`;
export const MOCK_TOKEN_PREFIX = 'mock.';

export function problem(status: number, code: Problem['code'], title: string, extra?: Partial<Problem>) {
    return HttpResponse.json<Problem>(
        { type: `https://errors.credit-cabinet.local/${code.toLowerCase().replace(/_/g, '-')}`, title, status, code, ...extra },
        { status, headers: { 'Content-Type': 'application/problem+json' } },
    );
}

/** Пользователь по Bearer-токену. Токен мока — `mock.<user_id>`. */
export function currentUser(request: Request): Me | undefined {
    const token = (request.headers.get('Authorization') ?? '').replace(/^Bearer\s+/i, '');
    if (!token.startsWith(MOCK_TOKEN_PREFIX)) return undefined;
    return meFor(token.slice(MOCK_TOKEN_PREFIX.length));
}

export const unauthorized = () => problem(401, 'UNAUTHORIZED', 'Нужно войти');
export const notFound = () => problem(404, 'NOT_FOUND', 'Заявка не найдена');
export const companyIds = (me: Me) => new Set(me.companies.map((c) => c.applicant_id));
export const withEtag = (version: number) => ({ ETag: String(version) });

/** Заявка, если она принадлежит компании пользователя. Чужие не раскрываем — как будто их нет. */
export function ownApplication(me: Me, id: unknown): StoredApplication | undefined {
    const app = read().applications.find((a) => a.id === id);
    return app && companyIds(me).has(app.applicant.id) ? app : undefined;
}

export function invalidTransition(app: StoredApplication) {
    return problem(409, 'INVALID_TRANSITION', 'Действие недоступно в текущем статусе', {
        current_status: app.status,
        allowed_actions: toApplication(app).allowed_actions,
    });
}

export function versionMismatch(request: Request, app: StoredApplication) {
    const ifMatch = request.headers.get('If-Match');
    if (ifMatch === null) return problem(422, 'VALIDATION_FAILED', 'Нужен заголовок If-Match');
    if (ifMatch !== String(app.version)) return problem(412, 'VERSION_CONFLICT', 'Заявку уже изменили — обновите данные');
    return undefined;
}

/** Повтор запроса с тем же Idempotency-Key возвращает уже созданный ресурс. */
export function idempotent(request: Request, scope: string) {
    const key = request.headers.get('Idempotency-Key');
    const full = key ? `${scope}:${key}` : undefined;
    return {
        existing: full ? read().idempotency[full] : undefined,
        remember: (id: string) => full && mutate((d) => (d.idempotency[full] = id)),
    };
}
