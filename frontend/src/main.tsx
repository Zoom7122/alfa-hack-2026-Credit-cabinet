import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './app/App';
import './styles/base.css';

/** Пока бэкенда нет, ответы API отдаёт MSW по контракту api/openapi.yaml. VITE_API_MOCK=false — ходить в настоящий API. */
async function enableMocks() {
    if (import.meta.env.VITE_API_MOCK === 'false') return;
    const { worker } = await import('./mocks/browser');
    await worker.start({ onUnhandledRequest: 'bypass', quiet: true });
}

function render() {
    createRoot(document.getElementById('root')!).render(
        <StrictMode>
            <App />
        </StrictMode>,
    );
}

// Service Worker есть только в secure context (localhost / https). Если моки не завелись — приложение всё равно стартует.
enableMocks()
    .catch((error: unknown) => console.error('[mocks] MSW не запустился, запросы пойдут в настоящий API', error))
    .finally(render);
