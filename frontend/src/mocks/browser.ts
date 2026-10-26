import type { RequestHandler, WebSocketHandler } from 'msw';
import { setupWorker } from 'msw/browser';

import { collectorBackofficeHandlers } from './collector/backofficeHandlers';
import { collectorCatalogHandlers } from './collector/catalogHandlers';
import { collectorHandlers } from './collector/handlers';
import { handlers } from './handlers';
import { signingHandlers } from './signingHandlers';
import { workflowHandlers, wsHandlers } from './workflowHandlers';

/** api — экраны без своего бэкенда (по openapi.yaml), collector — ответы data-collector (по его swagger). */
export function createWorker({ api, collector }: { api: boolean; collector: boolean }) {
    const list: (RequestHandler | WebSocketHandler)[] = [
        ...(api ? [...handlers, ...workflowHandlers, ...signingHandlers, ...wsHandlers] : []),
        ...(collector ? [...collectorHandlers, ...collectorBackofficeHandlers, ...collectorCatalogHandlers] : []),
    ];
    return setupWorker(...list);
}
