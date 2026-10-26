import { expect, type Page, test } from '@playwright/test';

import { loginAs, type UserKey, USERS } from './helpers';
import type { SavedDraft } from '../src/pages/WizardPage/draftTypes';

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

const restoredDraftCases = [
    {
        name: 'черновик из БД открывается без локальных данных',
        stored: undefined,
        productName: 'Кредит на оборудование', amount: 1_800_000, term: 30,
        purpose: 'Купить оборудование', description: 'Закупка оборудования из заявки',
    },
    {
        name: 'устаревший локальный выбор продукта не заменяет данные БД',
        stored: { product_code: 'VEHICLE', requested: { amount: 3_200_000, term_months: 36 }, purpose: { code: 'VEHICLE', description: 'Мой транспорт' } },
        productName: 'Кредит на оборудование', amount: 1_800_000, term: 30,
        purpose: 'Купить оборудование', description: 'Закупка оборудования из заявки',
    },
    {
        name: 'неполный локальный черновик не подставляет отсутствующие в БД значения',
        stored: { requested: { amount: 2_100_000, term_months: 42 }, purpose: { description: '' } },
        productName: 'Кредит на оборудование', amount: 1_800_000, term: 30,
        purpose: 'Купить оборудование', description: 'Закупка оборудования из заявки',
    },
];

for (const scenario of restoredDraftCases) {
    test(`без моков ${scenario.name}`, async ({ page }) => {
        const network = await stubCollector(page, 'director');
        const id = `app_${'140'.padStart(26, '0')}`;
        const application = {
            id, number: 'A-2026-000007', status: 'DRAFT', product_code: 'EQUIPMENT',
            applicant: { id: 'apl_live_company', name: 'ООО «Живая компания»', inn: '7701234567', type: 'LEGAL_ENTITY' },
            representative: { id: USERS.director.id, full_name: USERS.director.name, role: 'DIRECTOR', can_sign: true },
            requested: { amount: 1_800_000, term_months: 30 },
            purpose: { code: 'EQUIPMENT', description: 'Закупка оборудования из заявки' },
            created_at: '2026-10-26T08:00:00Z', updated_at: '2026-10-26T08:00:00Z',
        };
        const products = [
            { code: 'EQUIPMENT', name: 'Кредит на оборудование' },
            { code: 'VEHICLE', name: 'Кредит на транспорт' },
        ].map((product) => ({
            ...product,
            applicant_types: ['LEGAL_ENTITY'], purposes: [product.code],
            amount: { min: 500_000, max: 50_000_000 }, term_months: { min: 12, max: 60 }, rate_annual_from: 18.9,
        }));
        await page.route('**/collector/api/v1/products', (route) => route.fulfill({ json: { items: products } }));
        await page.route('**/collector/api/v1/products/*/calculate', (route) => route.fulfill({ json: { monthly_payment_from: 80_000 } }));
        await page.route('**/collector/api/v1/applications', (route) => route.fulfill({ json: { items: [application] } }));
        await page.route(`**/collector/api/v1/applications/${id}`, (route) => route.fulfill({ json: application }));
        await stubSavedDraft(page, id, {
            product_code: application.product_code, requested: application.requested,
            purpose: { code: 'EQUIPMENT', description: application.purpose.description },
            age: 42, marital_status: 'MARRIED', children_count: 0,
            director: { inn: '770112345695', last_name: 'Петров', first_name: 'Пётр', middle_name: 'Петрович', birth_date: '1984-07-19' },
            scoring: { free_cash: 123_456 }, other_bank_biks: ['044525225'],
        });
        await loginAs(page, 'director');
        await expect(page.getByRole('heading', { level: 1 })).toHaveText('Добрый день, Пётр Петрович');
        if (scenario.stored) {
            await page.evaluate(({ key, value }) => localStorage.setItem(key, JSON.stringify(value)), {
                key: `credit-cabinet:questionnaire:${USERS.director.id}:${id}`, value: scenario.stored,
            });
        }
        await page.goto(`/applications/${id}/edit/company`);
        await page.getByRole('link', { name: 'Продолжить', exact: true }).click();

        const expectTerms = async () => {
            await expect(page.getByRole('heading', { name: 'Сумма, срок и цель', exact: true })).toBeVisible();
            await expect(page.getByText('Не нашли продукт этой заявки', { exact: true })).toHaveCount(0);
            await expect(page.getByText(scenario.productName, { exact: true }).first()).toBeVisible();
            await expect(page.getByLabel('Сумма кредита')).toBeVisible();
            await expect.poll(async () => (await page.getByLabel('Сумма кредита').inputValue()).replace(/\D/g, '')).toBe(String(scenario.amount));
            await expect(page.getByLabel('Срок', { exact: true })).toHaveValue(String(scenario.term));
            await expect(page.getByText(scenario.purpose, { exact: true })).toBeVisible();
            await expect(page.getByLabel('Расскажите подробнее')).toHaveValue(scenario.description);
        };
        await expectTerms();
        await page.reload();
        await expectTerms();
        await page.getByRole('button', { name: 'Продолжить', exact: true }).click();
        await expect(page).toHaveURL(new RegExp(`/applications/${id}/edit/borrower$`));
        await expect(page.getByLabel('Возраст, лет')).toHaveValue('42');
        await expect(page.getByLabel('Дети и иждивенцы')).toHaveValue('0');
        await expect(page.getByLabel('Фамилия', { exact: true })).toHaveValue('Петров');
        await expect(page.getByLabel('Дата рождения')).toHaveValue('19.07.1984');
        expect(network.legacyRequests).toEqual([]);
    });
}

async function stubSavedDraft(page: Page, id: string, data: SavedDraft['data']) {
    const state = { data, version: 1, updated_at: '2026-10-26T08:00:00Z' };
    await page.route(`**/collector/api/v1/applications/${id}/draft`, async (route) => {
        if (route.request().method() === 'PUT') {
            const body = route.request().postDataJSON() as { data: SavedDraft['data']; version: number };
            if (body.version !== state.version) return route.fulfill({ status: 409, json: { code: 'CONFLICT' } });
            state.data = body.data;
            state.version++;
            state.updated_at = new Date().toISOString();
        }
        return route.fulfill({ json: state });
    });
    return state;
}

test('без данных БД форма пустая, выбор и частично заполненная анкета сохраняются на backend', async ({ page }) => {
    await stubCollector(page, 'director');
    const id = `app_${'141'.padStart(26, '0')}`;
    await page.route(`**/collector/api/v1/applications/${id}`, (route) => route.fulfill({ json: {
        id, number: 'A-EMPTY', status: 'DRAFT', applicant: { id: 'apl_live_company', inn: '7701234567', type: 'LEGAL_ENTITY' },
        representative: { id: USERS.director.id, role: 'DIRECTOR', can_sign: true },
    } }));
    await page.route('**/collector/api/v1/products', (route) => route.fulfill({ json: { items: [{
        code: 'EQUIPMENT', name: 'Кредит на оборудование', purposes: ['EQUIPMENT'],
        amount: { min: 500_000, max: 50_000_000 }, term_months: { min: 12, max: 60 }, rate_annual_from: 18.9,
    }] } }));
    await page.route('**/collector/api/v1/products/*/calculate', (route) => route.fulfill({ json: { monthly_payment_from: 80_000 } }));
    const state = await stubSavedDraft(page, id, {});
    await loginAs(page, 'director');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Добрый день, Пётр Петрович');
    await page.evaluate(({ key }) => localStorage.setItem(key, JSON.stringify({ product_code: 'EQUIPMENT', requested: { amount: 9_000_000, term_months: 60 }, age: 77 })),
        { key: `credit-cabinet:questionnaire:${USERS.director.id}:${id}` });
    await page.goto(`/applications/${id}/edit/terms`);
    await expect(page.getByLabel('Сумма кредита')).toHaveValue('');
    await expect(page.getByLabel('Срок', { exact: true })).toHaveValue('');
    await expect(page.getByLabel('Расскажите подробнее')).toHaveValue('');
    await expect(page.getByText('Не нашли продукт этой заявки', { exact: true })).toHaveCount(0);
    await page.getByText('Кредитный продукт', { exact: true }).first().click({ force: true });
    await page.getByRole('option', { name: 'Кредит на оборудование', exact: true }).click();
    await expect(page.getByLabel('Сумма кредита')).toHaveValue('');
    await expect(page.getByLabel('Срок', { exact: true })).toHaveValue('');
    await page.getByLabel('Сумма кредита').fill('2500000');
    await expect.poll(() => state.data.requested?.amount).toBe(2_500_000);
    expect(state.data.requested?.term_months).toBeUndefined();
    await page.reload();
    await expect.poll(async () => (await page.getByLabel('Сумма кредита').inputValue()).replace(/\D/g, '')).toBe('2500000');
    await expect(page.getByLabel('Срок', { exact: true })).toHaveValue('');
    await page.goto(`/applications/${id}/edit/borrower`);
    await expect(page.getByLabel('Возраст, лет')).toHaveValue('');
    await expect(page.getByLabel('Дети и иждивенцы')).toHaveValue('');
    await expect(page.getByLabel('Фамилия', { exact: true })).toHaveValue('');
    await page.getByLabel('Возраст, лет').fill('35');
    await expect.poll(() => state.data.age).toBe(35);
    await page.evaluate(() => { for (const key of Object.keys(localStorage)) if (key.startsWith('credit-cabinet:questionnaire:')) localStorage.removeItem(key); });
    await page.reload();
    await expect(page.getByLabel('Возраст, лет')).toHaveValue('35');
    await expect(page.getByLabel('Фамилия', { exact: true })).toHaveValue('');
});

test('частичная анкета из БД и ошибка сохранения: правки остаются до успешной записи', async ({ page }) => {
    await stubCollector(page, 'director');
    const id = `app_${'143'.padStart(26, '0')}`;
    await page.route(`**/collector/api/v1/applications/${id}`, (route) => route.fulfill({ json: {
        id, number: 'A-PARTIAL', status: 'DRAFT', applicant: { id: 'apl_live_company', inn: '7701234567', type: 'LEGAL_ENTITY' },
        representative: { id: USERS.director.id, role: 'DIRECTOR', can_sign: true }, documents: [], version: 1,
    } }));
    const state = await stubSavedDraft(page, id, { age: 42, director: { last_name: 'Сидоров' }, collateral: { serials: ['SN-1'] } });
    let failing = true;
    await page.route(`**/collector/api/v1/applications/${id}/draft`, (route) =>
        route.request().method() === 'PUT' && failing
            ? route.fulfill({ status: 500, json: { title: 'Ошибка записи', code: 'INTERNAL_ERROR' } })
            : route.fallback());
    await loginAs(page, 'director');
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Добрый день, Пётр Петрович');
    await page.goto(`/applications/${id}/edit/borrower`);
    await expect(page.getByLabel('Возраст, лет')).toHaveValue('42');
    await expect(page.getByLabel('Фамилия', { exact: true })).toHaveValue('Сидоров');
    await expect(page.getByLabel('Имя', { exact: true })).toHaveValue('');
    await page.getByLabel('Возраст, лет').fill('43');
    await expect(page.getByText('Не удалось сохранить черновик', { exact: true })).toBeVisible();
    expect(state.data.age).toBe(42);
    await page.getByRole('button', { name: 'Сохранить и выйти' }).click();
    await expect(page).toHaveURL(/\/edit\/borrower$/);
    await expect(page.getByLabel('Возраст, лет')).toHaveValue('43');
    failing = false;
    await page.getByRole('button', { name: 'Повторить сохранение' }).click();
    await expect.poll(() => state.data.age).toBe(43);
    await expect(page.getByText('Не удалось сохранить черновик', { exact: true })).toHaveCount(0);
    await page.reload();
    await expect(page.getByLabel('Возраст, лет')).toHaveValue('43');
    await expect(page.getByLabel('Фамилия', { exact: true })).toHaveValue('Сидоров');
    await expect(page.getByLabel('Имя', { exact: true })).toHaveValue('');
});
