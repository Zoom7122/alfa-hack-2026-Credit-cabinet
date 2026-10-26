import { expect, type Page, test } from '@playwright/test';

import { loginAs, type UserKey, USERS } from './helpers';

/** Проверяем именно выключенные флаги MSW; API data-collector перехватываем на границе сети. */
async function stubCollector(page: Page, role: 'director' | 'underwriter') {
    const company = {
        applicant_id: 'apl_live_company',
        short_name: 'ООО «Живая компания»',
        inn: '7701234567',
        role: 'DIRECTOR',
        can_sign: true,
    };
    const user = {
        id: USERS[role].id,
        full_name: USERS[role].name,
        roles: [role === 'underwriter' ? 'UNDERWRITER' : 'CLIENT'],
        persona: 'Пользователь data-collector',
        companies: role === 'underwriter' ? [] : [company],
        pending_signatures: [],
    };
    const legacyRequests: string[] = [];
    const sockets: string[] = [];
    const collectorRequests: string[] = [];
    page.on('request', (request) => {
        const path = new URL(request.url()).pathname;
        if (path.startsWith('/api/v1/') || path.endsWith('/mockServiceWorker.js')) legacyRequests.push(path);
        if (path.startsWith('/collector/api/v1/')) collectorRequests.push(path);
    });
    page.on('websocket', (socket) => sockets.push(socket.url()));

    await page.route('**/api/v1/**', (route) => {
        const path = new URL(route.request().url()).pathname;
        switch (path) {
            case '/collector/api/v1/auth/test-users':
                return route.fulfill({ json: { items: [user] } });
            case '/collector/api/v1/auth/mock-login':
                return route.fulfill({ json: { token: `mock.${user.id}`, user } });
            case '/collector/api/v1/me':
                return route.fulfill({ json: user });
            case '/collector/api/v1/applicants':
                return route.fulfill({
                    json: { items: [{ id: company.applicant_id, inn: company.inn, ogrn: '1207700123456', registration_date: '2020-01-01' }] },
                });
            case '/collector/api/v1/applications':
            case '/collector/api/v1/products':
            case '/collector/api/v1/backoffice/applications':
                return route.fulfill({ json: { items: [] } });
            default:
                return route.fulfill({ status: 502, json: { detail: 'Этот API не подключён' } });
        }
    });
    return { legacyRequests, sockets, collectorRequests };
}

test('без моков кабинет использует только data-collector, пустые списки и старые ссылки работают', async ({ page, context }) => {
    const network = await stubCollector(page, 'director');
    await loginAs(page, 'director');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Добрый день, Пётр Петрович');
    await expect(page.getByText('Активных заявок нет', { exact: true })).toBeVisible();
    await expect(page.getByRole('region', { name: 'ООО «Живая компания»' })).toContainText('1207700123456');
    await expect(page.getByText('Ждут вашего действия', { exact: true })).toHaveCount(0);

    await page.reload();
    await expect(page.getByText('Активных заявок нет', { exact: true })).toBeVisible();
    await page.getByRole('button', { name: /Завершённые · 0/ }).click();
    await expect(page.getByText('Завершённых заявок пока нет', { exact: true })).toBeVisible();

    await page.getByRole('link', { name: 'Новая заявка', exact: true }).click();
    await expect(page.getByText('Под эту задачу пока нет кредитов', { exact: true })).toBeVisible();

    await page.goto('/applications/app_000131');
    await expect(page.getByRole('heading', { name: 'Экран в разработке' })).toBeVisible();
    expect(network.collectorRequests).toEqual(expect.arrayContaining([
        '/collector/api/v1/me',
        '/collector/api/v1/applicants',
        '/collector/api/v1/applications',
        '/collector/api/v1/products',
    ]));
    expect(network.legacyRequests).toEqual([]);
    expect(network.sockets).toEqual([]);
    expect(context.serviceWorkers()).toHaveLength(0);
});

test('без моков рабочее место банка загружает очередь без демо-профиля и WebSocket', async ({ page }) => {
    const network = await stubCollector(page, 'underwriter');
    await loginAs(page, 'underwriter' satisfies UserKey);
    await expect(page.getByRole('heading', { name: 'Очередь заявок', exact: true })).toBeVisible();
    await expect(page.getByRole('group', { name: 'Ручное рассмотрение', exact: true })).toContainText('0');
    expect(network.collectorRequests).toContain('/collector/api/v1/backoffice/applications');
    expect(network.legacyRequests).toEqual([]);
    expect(network.sockets).toEqual([]);
});
