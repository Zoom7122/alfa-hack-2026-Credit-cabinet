import createClient from 'openapi-fetch';

import { session } from '../session';
import type { paths } from './schema';

/** data-collector живёт под своим префиксом: пути /applications/{id} совпадают с моками остальных экранов. */
export const COLLECTOR_BASE_URL = import.meta.env.VITE_COLLECTOR_BASE_URL || '/collector/api/v1';

export const collector = createClient<paths>({ baseUrl: COLLECTOR_BASE_URL });

// токен мок-входа data-collector (POST /auth/mock-login): с ним заявка оформляется от имени пользователя
collector.use({
    onRequest({ request }) {
        const token = session.getToken();
        if (token) request.headers.set('Authorization', `Bearer ${token}`);
        return request;
    },
});
