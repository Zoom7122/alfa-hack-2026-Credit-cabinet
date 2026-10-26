import { expect, type Page, test } from '@playwright/test';

import { COLLECTOR_LIVE, seedToken, setPipelineStep, type UserKey } from './helpers';

async function offered(page: Page, user: UserKey = 'northDirector') {
    await seedToken(page, user);
    await setPipelineStep(page, 100);
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    if (COLLECTOR_LIVE) {
        test.skip(!process.env.E2E_SIGNING_APP_ID, 'нужна отдельная тестовая заявка OFFERED: E2E_SIGNING_APP_ID');
        return process.env.E2E_SIGNING_APP_ID ?? '';
    }
    return page.evaluate(async () => {
        const headers = { Authorization: `Bearer ${localStorage.getItem('credit-cabinet:token')}`, 'Content-Type': 'application/json' };
        const registered = await fetch('/collector/api/v1/applicants', { method: 'POST', headers, body: JSON.stringify({ representative: { full_name: 'Представитель', role: 'DIRECTOR' }, company: { inn: '7799000012' } }) });
        const app = (await registered.json()) as { id: string };
        const submitted = await fetch(`/collector/api/v1/applications/${app.id}/questionnaire`, { method: 'POST', headers, body: JSON.stringify({ age: 42, marital_status: 'MARRIED', children_count: 2, product_code: 'WORKING_CAPITAL', requested: { amount: 5_000_000, term_months: 24 }, purpose: { code: 'WORKING_CAPITAL' } }) });
        if (submitted.status !== 202) throw new Error(`submit: ${submitted.status} ${await submitted.text()}`);
        return app.id;
    });
}

async function choose(page: Page, id: string) {
    await page.goto(`/applications/${id}`);
    await expect(page.getByRole('heading', { name: 'Банк одобрил кредит', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Выбрать', exact: true }).first().click();
}

test('договор подписывается кодом через API, сохраняется после перезагрузки и завершается один раз', async ({ page }) => {
    const id = await offered(page);
    await choose(page, id);
    const demo = page.getByTestId('collector-demo-code');
    await expect(demo).toHaveText(/^\d{6}$/);
    const code = (await demo.textContent()) ?? '';
    const download = page.waitForEvent('download');
    await page.getByRole('button', { name: 'Скачать кредитный договор' }).click();
    expect((await download).suggestedFilename()).toMatch(/Кредитный договор.*\.txt$/);
    await page.reload();
    await expect(demo).toHaveText(code);
    const input = page.getByLabel('Код подтверждения');
    const submit = page.getByRole('button', { name: 'Подписать договор', exact: true });
    await input.fill(code === '000000' ? '111111' : '000000');
    await expect(submit).toBeDisabled();
    await page.getByText('Я ознакомился с договором и согласен с условиями', { exact: true }).click();
    await submit.click();
    await expect(page.getByText('Неверный код. Осталось попыток: 2', { exact: true })).toBeVisible();
    await input.fill(code);
    await submit.click();
    await expect(page.getByRole('heading', { name: 'Договор подписан', exact: true })).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Кредит оформлен', exact: true })).toBeVisible();
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Договор подписан', exact: true })).toBeVisible();
    await expect(demo).toHaveCount(0);
    const replay = await page.evaluate(async ({ id, code }) => {
        const response = await fetch(`/collector/api/v1/applications/${id}/signing/confirm`, { method: 'POST', headers: { Authorization: `Bearer ${localStorage.getItem('credit-cabinet:token')}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ code }) });
        return { status: response.status, body: (await response.json()) as { state: string; demo_code?: string } };
    }, { id, code });
    expect(replay.status).toBe(200);
    expect(replay.body.state).toBe('SIGNED');
    expect(replay.body.demo_code).toBeUndefined();
    await page.getByRole('tab', { name: /^Документы/ }).click();
    await expect(page.getByText('Подписан демо-кодом', { exact: true })).toBeVisible();
    await page.getByRole('tab', { name: 'История' }).click();
    await expect(page.getByText('Договор подписан демонстрационным кодом', { exact: true })).toHaveCount(1);
});

test('три неверных кода возвращают к предложению и позволяют начать новое подписание', async ({ page }) => {
    test.skip(COLLECTOR_LIVE, 'ошибки и сроки на живом backend проверяются Go integration-тестами');
    const id = await offered(page);
    await choose(page, id);
    const demo = page.getByTestId('collector-demo-code');
    await expect(demo).toHaveText(/^\d{6}$/);
    const code = (await demo.textContent()) ?? '';
    await page.getByText('Я ознакомился с договором и согласен с условиями', { exact: true }).click();
    for (const left of [2, 1, 0]) {
        await page.getByLabel('Код подтверждения').fill(code === '000000' ? '111111' : '000000');
        await page.getByRole('button', { name: 'Подписать договор', exact: true }).click();
        if (left) await expect(page.getByText(`Неверный код. Осталось попыток: ${left}`, { exact: true })).toBeVisible();
    }
    await expect(page.getByRole('heading', { name: 'Банк одобрил кредит', exact: true })).toBeVisible();
    await page.getByRole('button', { name: 'Выбрать', exact: true }).first().click();
    await expect(demo).toHaveText(/^\d{6}$/);
    await expect(page.getByText(/осталось попыток: 3/)).toBeVisible();
});

test('бухгалтер может подготовить договор, код доступен только директору', async ({ page }) => {
    test.skip(COLLECTOR_LIVE, 'права на живом backend проверяются Go integration-тестами');
    const id = await offered(page, 'northAccountant');
    await choose(page, id);
    await expect(page.getByText('Ожидаем подпись', { exact: true })).toBeVisible();
    await expect(page.getByTestId('collector-demo-code')).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Подписать договор', exact: true })).toHaveCount(0);
    const denied = await page.evaluate(async (id) => {
        const response = await fetch(`/collector/api/v1/applications/${id}/signing/confirm`, { method: 'POST', headers: { Authorization: `Bearer ${localStorage.getItem('credit-cabinet:token')}`, 'Content-Type': 'application/json' }, body: JSON.stringify({ code: '123456' }) });
        return response.status;
    }, id);
    expect(denied).toBe(403);
});
