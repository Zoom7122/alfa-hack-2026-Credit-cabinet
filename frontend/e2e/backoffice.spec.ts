import { type Browser, expect, type Page, test } from '@playwright/test';

import { COLLECTOR_LIVE, expectPath, MOCK_ONLY, seedToken, setMockFailures, setPipelineStep, USERS, type UserKey } from './helpers';

/* Рабочее место банка (16–18) на API data-collector. Заявку на ручное рассмотрение каждый тест создаёт сам:
   сценарий B mock-api (ООО «Стройресурс») — у движка «много кредитных обращений», и в моке, и в живом стеке. */

const rows = (page: Page) => page.getByRole('region', { name: 'Заявки' }).getByRole('table').getByRole('row');
const panel = (page: Page) => page.getByRole('dialog', { name: 'Решение по заявке' });
const rejectSubmit = (page: Page) => panel(page).getByRole('button', { name: 'Отказать', exact: true }).last();
const stat = (page: Page, label: string) => page.getByRole('group', { name: label });

const SCENARIO_B = {
    age: 47,
    marital_status: 'MARRIED',
    children_count: 3,
    product_code: 'WORKING_CAPITAL',
    requested: { amount: 8_000_000, term_months: 36 },
    purpose: { code: 'WORKING_CAPITAL' },
    director: { inn: '779900020136', last_name: 'Орлов', first_name: 'Денис', middle_name: 'Павлович', birth_date: '1979-02-11' },
    consents: {
        bki_company: { id: 'CNS-2026-000801', signed_at: '2026-10-21T14:02:11Z', valid_until: '2027-04-21' },
        bki_person: { id: 'CNS-2026-000802', signed_at: '2026-10-21T14:02:40Z', valid_until: '2027-04-21' },
        tax: { id: 'TAX-CNS-2026-000801', signed_at: '2026-10-21T14:03:02Z', valid_until: '2027-04-21', scope: ['DECLARATIONS', 'ENS_BALANCE'] },
    },
    other_bank_biks: ['044525777', '044525650'],
    scoring: {
        max_annual_rate: 28,
        min_acceptable_amount: 2_400_000,
        max_acceptable_term_months: 48,
        need_cost: 8_800_000,
        own_contribution: 800_000,
        free_cash: 50_000,
        minimum_cash: 20_000,
        need_evidence: 'VERIFIED_ESTIMATE',
        experience_years: 8,
    },
};

type Created = { id: string; number: string };

/** Заявка сценария B от директора «Стройресурса» — прямо в API data-collector; ждём ручного рассмотрения. */
async function createReview(page: Page): Promise<Created> {
    // запросы — только после старта моков (иначе прокси dev-сервера отправит их в живой data-collector)
    await seedToken(page, 'stroyresurs');
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await setPipelineStep(page, 300);
    const created = await page.evaluate(
        async ({ questionnaire, token }) => {
            const base = '/collector/api/v1';
            const auth = { Authorization: `Bearer ${token}` };
            const registered = await fetch(`${base}/applicants`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', ...auth },
                body: JSON.stringify({ representative: { full_name: 'Орлов Денис Павлович', role: 'DIRECTOR' }, company: { inn: '7799000020' } }),
            });
            const app = (await registered.json()) as { id: string; number: string };
            const form = new FormData();
            form.append('questionnaire', JSON.stringify(questionnaire));
            const sent = await fetch(`${base}/applications/${app.id}/questionnaire`, { method: 'POST', body: form, headers: auth });
            if (sent.status !== 202) throw new Error(`questionnaire: ${sent.status} ${await sent.text()}`);
            for (let i = 0; i < 200; i += 1) {
                const cur = (await (await fetch(`${base}/applications/${app.id}`, { headers: auth })).json()) as { status: string };
                if (cur.status === 'MANUAL_REVIEW') return { id: app.id, number: app.number };
                if (!['SUBMITTED', 'ENRICHING', 'VERIFYING', 'SCORING'].includes(cur.status)) throw new Error(`status ${cur.status}`);
                await new Promise((r) => setTimeout(r, 300));
            }
            throw new Error('application is not in manual review');
        },
        { questionnaire: SCENARIO_B, token: `mock.${USERS.stroyresurs.id}` },
    );
    return created;
}

/** Действие другого андеррайтера — запросом с его токеном. */
async function assignAs(page: Page, user: UserKey, id: string) {
    return page.evaluate(
        async ({ id, token }) => (await fetch(`/collector/api/v1/backoffice/applications/${id}/assign`, { method: 'POST', headers: { Authorization: `Bearer ${token}` } })).status,
        { id, token: `mock.${USERS[user].id}` },
    );
}

/** Клиент в отдельном браузере с тем же состоянием мока data-collector. */
async function asClient(page: Page, browser: Browser, user: UserKey, path: string) {
    const db = await page.evaluate(() => {
        const key = 'credit-cabinet:collector-mock:v1';
        return [key, localStorage.getItem(key) ?? ''] as const;
    });
    const client = await (await browser.newContext()).newPage();
    await client.addInitScript((entry) => {
        if (entry[1]) localStorage.setItem(entry[0], entry[1]);
    }, db);
    await seedToken(client, user);
    await client.goto(path);
    return client;
}

async function openDossier(page: Page, id: string) {
    await page.goto(`/backoffice/applications/${id}`);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
}

let app: Created;

test.beforeEach(async ({ page }) => {
    app = await createReview(page);
    await seedToken(page, 'underwriter');
});

test.describe('Очередь (16)', () => {
    test('заявка на ручном рассмотрении — с причиной движка и сроком', async ({ page }) => {
        await page.goto('/backoffice');
        await expect(page.getByRole('heading', { name: 'Очередь заявок' })).toBeVisible();
        await expect(page.getByRole('button', { name: /^Ручное рассмотрение/ })).toHaveAttribute('aria-pressed', 'true');
        const row = rows(page).filter({ hasText: app.number });
        await expect(row).toContainText('СТРОЙРЕСУРС');
        await expect(row).toContainText('кредитных обращений');
        await expect(row.getByRole('button', { name: `Взять в работу ${app.number}` })).toBeVisible();
        await expect(stat(page, 'Ждут ручного рассмотрения')).not.toContainText(/^0$/);
    });

    test('«Взять в работу» открывает досье, заявка — в «Моих заявках»', async ({ page }) => {
        await page.goto('/backoffice');
        await rows(page).filter({ hasText: app.number }).getByRole('button', { name: `Взять в работу ${app.number}` }).click();
        await expectPath(page, `/backoffice/applications/${app.id}`);
        await expect(page.getByText(/в работе у вас/)).toBeVisible();
        await page.goto('/backoffice?assignee=me');
        await expect(rows(page).filter({ hasText: app.number })).toHaveCount(1);
    });

    test('заявку уже взял другой андеррайтер — ни взять, ни решить', async ({ page }) => {
        expect(await assignAs(page, 'orlov', app.id)).toBe(200);
        expect(await assignAs(page, 'underwriter', app.id)).toBe(409);
        await openDossier(page, app.id);
        await expect(page.getByText('Заявка в работе у Орлов Д. А.')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Взять в работу' })).toHaveCount(0);
        await expect(page.getByRole('button', { name: 'Одобрить', exact: true })).toHaveCount(0);
    });
});

test.describe('Досье (17)', () => {
    test('обзор и вкладки: причина, скоринг по критериям, системы, компания, журнал', async ({ page }) => {
        await openDossier(page, app.id);
        await expect(page.getByRole('heading', { level: 1 })).toContainText('СТРОЙРЕСУРС');
        await expect(page.getByText('Грейд B')).toBeVisible();
        await expect(page.getByRole('region', { name: 'Почему заявка на ручном рассмотрении' })).toContainText('кредитных обращений');
        await expect(page.getByRole('region', { name: 'Источники данных' })).toContainText('Счета в Альфа-Банке');
        await expect(page.getByRole('button', { name: /Перезапросить/ })).toHaveCount(0);

        await page.getByRole('tab', { name: 'Скоринг' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('Сколько месяцев работает действующий бизнес организации');
        await page.getByRole('tab', { name: /^Источники/ }).click();
        await expect(page.getByRole('tabpanel')).toContainText('Кредитная история (БКИ)');
        await page.getByRole('tab', { name: 'Данные компании' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('ИНН 7799000020');
        await expect(page.getByRole('tabpanel')).toContainText('Бухгалтерская отчётность');
        await page.getByRole('tab', { name: 'История' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('Заявка отправлена');
    });
});

test.describe('Решение (18)', () => {
    test.beforeEach(async ({ page }) => {
        await openDossier(page, app.id);
    });

    test('одобрить: условия андеррайтера, внутренний комментарий клиенту не виден', async ({ page, browser }) => {
        await page.getByRole('button', { name: 'Одобрить', exact: true }).click();
        await expect(page.getByText(/в работе у вас/)).toBeVisible();
        await panel(page).getByRole('textbox', { name: 'Ставка, %' }).fill('23,5');
        await panel(page).getByLabel('Внутренний комментарий (клиент не увидит)').fill('Обращения — рефинансирование лизинга');
        await panel(page).getByRole('button', { name: 'Одобрить и отправить клиенту' }).click();
        await expect(panel(page)).toHaveCount(0);
        await expect(page.getByRole('button', { name: 'Одобрить', exact: true })).toHaveCount(0);
        await expect(page.getByRole('region', { name: 'Предложения клиенту' })).toContainText(/8\s000\s000/);
        await page.getByRole('tab', { name: 'История' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('Внутренний комментарий: Обращения — рефинансирование лизинга');

        test.skip(COLLECTOR_LIVE, 'клиент на живом стеке — тот же браузер, проверено выше'); // мок — в localStorage этой вкладки
        const client = await asClient(page, browser, 'stroyresurs', `/applications/${app.id}`);
        await expect(client.getByRole('heading', { level: 2, name: 'Банк одобрил кредит' })).toBeVisible();
        await expect(client.getByRole('region', { name: 'Предложение банка' })).toContainText('23,5');
        await client.goto(`/applications/${app.id}?tab=history`);
        await expect(client.getByRole('tabpanel')).not.toContainText('рефинансирование');
    });

    test('запросить данные → клиент отвечает → заявка снова у андеррайтера', async ({ page, browser }) => {
        await page.getByRole('button', { name: 'Запросить данные' }).click();
        await panel(page).getByRole('textbox', { name: 'Вопрос' }).fill('Для чего брали кредиты за последние 90 дней?');
        await panel(page).getByRole('button', { name: 'Отправить вопросы клиенту' }).click();
        await expect(panel(page)).toHaveCount(0);
        await page.getByRole('tab', { name: 'Уточнения' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('Для чего брали кредиты за последние 90 дней?');

        const client = COLLECTOR_LIVE ? page : await asClient(page, browser, 'stroyresurs', `/applications/${app.id}`);
        if (COLLECTOR_LIVE) {
            await seedToken(client, 'stroyresurs');
            await client.goto(`/applications/${app.id}`);
        }
        const question = client.getByRole('region', { name: /Для чего брали кредиты/ });
        await question.getByLabel('Ответ').fill('Рефинансировали лизинг техники под меньшую ставку');
        await question.getByRole('button', { name: 'Отправить ответ' }).click();
        await expect(client.getByRole('heading', { level: 2, name: 'Заявку рассматривает кредитный специалист' })).toBeVisible({ timeout: 10_000 });
    });

    test('отказать: без причины — подсказка; с причиной — клиент видит причину', async ({ page, browser }) => {
        await page.getByRole('button', { name: 'Отказать', exact: true }).click();
        await rejectSubmit(page).click();
        await expect(panel(page)).toContainText('Выберите причину или опишите свою');
        await panel(page).getByRole('group', { name: 'Что увидит клиент' }).getByText('Негативная кредитная история').click();
        await rejectSubmit(page).click();
        await expect(panel(page)).toHaveCount(0);
        await expect(page.getByRole('heading', { level: 1 }).locator('xpath=..')).toContainText('Отказ');

        test.skip(COLLECTOR_LIVE, MOCK_ONLY);
        const client = await asClient(page, browser, 'stroyresurs', `/applications/${app.id}`);
        await expect(client.getByRole('region', { name: 'Почему банк отказал' })).toContainText('Негативная кредитная история');
    });

    test('412 — досье перечитано, черновик можно перезаполнить', async ({ page }) => {
        test.skip(COLLECTOR_LIVE, MOCK_ONLY);
        await setMockFailures(page, [{ method: 'POST', path: '/decision$', status: 412, code: 'VERSION_CONFLICT', times: 1 }]);
        await page.getByRole('button', { name: 'Отказать', exact: true }).click();
        await panel(page).getByRole('group', { name: 'Что увидит клиент' }).getByText('Негативная кредитная история').click();
        await rejectSubmit(page).click();
        await expect(panel(page)).toContainText('Заявку изменили — досье обновлено');
    });

    test('409 на решение — ошибка видна, панель остаётся', async ({ page }) => {
        test.skip(COLLECTOR_LIVE, MOCK_ONLY);
        await setMockFailures(page, [{ method: 'POST', path: '/decision$', status: 409, code: 'INVALID_TRANSITION', times: 1 }]);
        await page.getByRole('button', { name: 'Отказать', exact: true }).click();
        await panel(page).getByRole('group', { name: 'Что увидит клиент' }).getByText('Негативная кредитная история').click();
        await rejectSubmit(page).click();
        await expect(panel(page)).toContainText('Не получилось сохранить решение');
    });

    test('Esc закрывает панель', async ({ page }) => {
        await page.getByRole('button', { name: 'Запросить данные' }).click();
        await expect(panel(page)).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(panel(page)).toHaveCount(0);
    });
});

test('клиент в рабочее место банка не попадает', async ({ page }) => {
    await seedToken(page, 'stroyresurs');
    await page.goto('/backoffice');
    await expectPath(page, '/');
});
