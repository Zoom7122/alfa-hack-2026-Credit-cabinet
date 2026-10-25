import { defineConfig, devices } from '@playwright/test';

/**
 * E2E-тесты кабинета (e2e/). По умолчанию сами поднимают dev-сервер на моках MSW.
 * E2E_BASE_URL — прогнать против уже запущенного стенда, например контейнера: E2E_BASE_URL=http://localhost:3000
 */
const externalBaseURL = process.env.E2E_BASE_URL;
// свой порт, а не 5173: иначе reuseExistingServer подхватит обычный `npm run dev` — возможно, с живым data-collector
const port = 5179;

export default defineConfig({
    testDir: './e2e',
    fullyParallel: true,
    forbidOnly: Boolean(process.env.CI),
    retries: process.env.CI ? 1 : 0,
    reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
    use: {
        baseURL: externalBaseURL ?? `http://localhost:${port}`,
        viewport: { width: 1440, height: 900 },
        locale: 'ru-RU',
        trace: 'retain-on-failure',
        screenshot: 'only-on-failure',
    },
    projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } }],
    webServer: externalBaseURL
        ? undefined
        : {
              command: `npm run dev -- --port ${port} --strictPort`,
              url: `http://localhost:${port}/login`,
              // тесты — всегда на моках MSW; против живого сервиса — через E2E_BASE_URL
              env: { VITE_API_MOCK: 'true', VITE_COLLECTOR_MOCK: 'true' },
              reuseExistingServer: !process.env.CI,
              timeout: 60_000,
          },
});
