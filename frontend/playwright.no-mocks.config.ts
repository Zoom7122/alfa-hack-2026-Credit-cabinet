import { defineConfig } from '@playwright/test';

import config from './playwright.config';

const externalBaseURL = process.env.E2E_BASE_URL;
const port = 5180;

/** Отдельный сервер: оба набора MSW выключены, ответы data-collector задаёт тест. */
export default defineConfig({
    ...config,
    testMatch: '**/no-mocks.spec.ts',
    testIgnore: [],
    use: { ...config.use, baseURL: externalBaseURL ?? `http://localhost:${port}` },
    webServer: externalBaseURL
        ? undefined
        : {
              command: `npm run dev -- --port ${port} --strictPort`,
              url: `http://localhost:${port}/login`,
              env: { VITE_API_MOCK: 'false', VITE_COLLECTOR_MOCK: 'false' },
              reuseExistingServer: false,
              timeout: 60_000,
          },
});
