import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './app/App';
import './styles/base.css';

/**
 * Моки MSW: VITE_API_MOCK — экраны без своего бэкенда (по api/openapi.yaml), VITE_COLLECTOR_MOCK — ответы data-collector.
 * 'false' — ходить в настоящий сервис через прокси.
 */
async function enableMocks() {
    const api = import.meta.env.VITE_API_MOCK !== 'false';
    const collector = import.meta.env.VITE_COLLECTOR_MOCK !== 'false';
    if (!api && !collector) return;
    const { createWorker } = await import('./mocks/browser');
    await createWorker({ api, collector }).start({ onUnhandledRequest: 'bypass', quiet: true });
}

function render() {
    // файлы анкет, на которые не ссылается ни один черновик (отправленные, брошенные), — из IndexedDB
    void import('./pages/WizardPage/draft').then((m) => m.collectOrphanFiles());
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
