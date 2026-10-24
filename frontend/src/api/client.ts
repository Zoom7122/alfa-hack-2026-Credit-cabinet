import createClient, { type Middleware } from 'openapi-fetch';

import type { paths } from './schema';
import { session } from './session';
import type { Problem } from './types';

export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api/v1';

const authMiddleware: Middleware = {
    onRequest({ request }) {
        const token = session.getToken();
        if (token) {
            request.headers.set('Authorization', `Bearer ${token}`);
        }
        return request;
    },
};

export const api = createClient<paths>({
    baseUrl: API_BASE_URL,
    // В контракте массивы в query — `style: form, explode: false`: ?status=A,B
    querySerializer: { array: { style: 'form', explode: false } },
});
api.use(authMiddleware);

/** Ошибка REST в формате RFC 9457 (application/problem+json). */
export class ApiError extends Error {
    readonly status: number;
    readonly problem?: Problem;

    constructor(status: number, message: string, problem?: Problem) {
        super(message);
        this.name = 'ApiError';
        this.status = status;
        this.problem = problem;
    }

    get code() {
        return this.problem?.code;
    }
}

export const isUnauthorized = (error: unknown) => error instanceof ApiError && error.status === 401;

function asProblem(body: unknown): Problem | undefined {
    return body && typeof body === 'object' && 'code' in body ? (body as Problem) : undefined;
}

/** Достаёт data из ответа openapi-fetch или бросает ApiError — удобно для react-query. */
export function unwrap<T>(result: { data?: T; error?: unknown; response: Response }): T {
    const { response } = result;
    if (result.error !== undefined || !response.ok) {
        const problem = asProblem(result.error);
        throw new ApiError(response.status, problem?.detail ?? problem?.title ?? `HTTP ${response.status}`, problem);
    }
    // 204 No Content — данных нет, и это не ошибка
    return result.data as T;
}
