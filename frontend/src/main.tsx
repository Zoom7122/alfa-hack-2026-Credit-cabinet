import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';

import { App } from './app/App';
import { API_MOCK_ENABLED, COLLECTOR_MOCK_ENABLED } from './config';
import './styles/base.css';

/**
 * Моки MSW: VITE_API_MOCK — экраны без своего бэкенда (по api/openapi.yaml), VITE_COLLECTOR_MOCK — ответы data-collector.
 * При выключенных демо-моках остаются экраны data-collector (его мок или живой сервис через прокси).
 */
async function enableMocks() {
    const api = API_MOCK_ENABLED;
    const collector = COLLECTOR_MOCK_ENABLED;
    if (!api && !collector) return;
    const { createWorker } = await import('./mocks/browser');
    await createWorker({ api, collector }).start({ onUnhandledRequest: 'bypass', quiet: true });
}

/** Ключи прежних версий фронта: список заявок теперь отдаёт data-collector. */
function dropLegacyKeys() {
    try {
        for (const key of Object.keys(localStorage)) {
            if (key.startsWith('credit-cabinet:collector-apps:')) localStorage.removeItem(key);
        }
    } catch {
        // хранилище недоступно — чистить нечего
    }
}

function render() {
    dropLegacyKeys();
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
