import { setupWorker } from 'msw/browser';

import { backofficeHandlers } from './backofficeHandlers';
import { handlers } from './handlers';
import { signingHandlers } from './signingHandlers';
import { workflowHandlers, wsHandlers } from './workflowHandlers';

export const worker = setupWorker(...handlers, ...workflowHandlers, ...signingHandlers, ...backofficeHandlers, ...wsHandlers);
