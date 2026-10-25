import createClient from 'openapi-fetch';

import type { paths } from './schema';

/** data-collector живёт под своим префиксом: пути /applications/{id} совпадают с моками остальных экранов. */
export const COLLECTOR_BASE_URL = import.meta.env.VITE_COLLECTOR_BASE_URL || '/collector/api/v1';

// авторизации у data-collector нет
export const collector = createClient<paths>({ baseUrl: COLLECTOR_BASE_URL });
