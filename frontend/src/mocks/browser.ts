import { setupWorker } from 'msw/browser';

import { handlers } from './handlers';
import { workflowHandlers, wsHandlers } from './workflowHandlers';

export const worker = setupWorker(...handlers, ...workflowHandlers, ...wsHandlers);
