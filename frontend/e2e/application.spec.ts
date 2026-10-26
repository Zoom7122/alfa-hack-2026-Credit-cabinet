import { expect, type Page, test } from '@playwright/test';

import { expectPath, seedToken, setMockFailures, setPipelineStep } from './helpers';

const statusHeading = (page: Page, name: string | RegExp) => page.getByRole('heading', { level: 2, name });
const offers = (page: Page) => page.getByRole('region', { name: 'Предложения банка' }).getByRole('article');

type FileSpec = { name: string; mimeType: string };

async function chooseFile(page: Page, scope: ReturnType<Page['getByRole']>, file: FileSpec | FileSpec[]) {
    const chooser = page.waitForEvent('filechooser');
    await scope.getByRole('button', { name: 'Выбрать на компьютере' }).click();
    await (await chooser).setFiles([file].flat().map((f) => ({ ...f, buffer: Buffer.from('test') })));
}

const POA = [
    { name: 'doverennost_ivanova.xml', mimeType: 'application/xml' },
    { name: 'doverennost_ivanova.xml.sig', mimeType: 'application/pkcs7-signature' },
];

/**
 * Отправить демо-черновик A-2026-000140 через мок-API (экраны 07+ пока на моках по openapi.yaml).
 * Анкета кабинета теперь работает на data-collector, поэтому черновик дозаполняем запросами, а не через UI.
 */
async function submitDraft(page: Page) {
    await page.goto('/');
    // моки MSW стартуют до отрисовки: главная на экране — запросы уже перехватываются
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    await page.evaluate(async () => {
        const headers = { Authorization: `Bearer ${localStorage.getItem('credit-cabinet:token')}`, 'Content-Type': 'application/merge-patch+json' };
        const base = '/api/v1/applications/app_000140';
        const app = (await (await fetch(base, { headers })).json()) as { version: number };
        const patched = await fetch(base, {
            method: 'PATCH',
            headers: { ...headers, 'If-Match': String(app.version) },
            body: JSON.stringify({ financials: { avg_monthly_expenses: 900000 }, consents: { personal_data: true, credit_bureau: true } }),
        });
        const { version } = (await patched.json()) as { version: number };
        const submitted = await fetch(`${base}/submit`, {
            method: 'POST',
            headers: { ...headers, 'Content-Type': 'application/json', 'If-Match': String(version), 'Idempotency-Key': crypto.randomUUID() },
        });
        if (!submitted.ok) throw new Error(`submit: ${submitted.status} ${await submitted.text()}`);
    });
    await page.goto('/applications/app_000140');
    await expectPath(page, '/applications/app_000140');
}

test.describe('Рассмотрение (07)', () => {
    test('после отправки статус сам доходит до предложений — без перезагрузки', async ({ page }) => {
        await seedToken(page, 'director');
        await setPipelineStep(page, 300);
        await submitDraft(page);
        await expect(statusHeading(page, 'Рассматриваем заявку')).toBeVisible();
        await expect(page.getByRole('list', { name: 'Что делает банк' })).toBeVisible();
        // события приходят по WebSocket — страница перечитывает статус сама
        await expect(statusHeading(page, 'Банк готов выдать кредит')).toBeVisible({ timeout: 15_000 });
        await expect(offers(page)).toHaveCount(3);
    });

    test('параметры, последние события и история', async ({ page }) => {
        await seedToken(page, 'soleProprietor');
        await page.goto('/applications/app_000147');
        await expect(page.getByRole('heading', { level: 1 })).toHaveText('Оборотный кредит · 2 500 000 ₽');
        await expect(page.getByText('Решение принимает специалист банка')).toBeVisible();
        await expect(page.getByRole('region', { name: 'Параметры заявки' })).toContainText('2 500 000 ₽');
        await page.getByRole('tab', { name: 'История' }).click();
        await expect(page).toHaveURL(/tab=history/);
        await expect(page.getByText('Заявка отправлена')).toBeVisible();
    });

    test('отзыв заявки', async ({ page }) => {
        await seedToken(page, 'soleProprietor');
        await page.goto('/applications/app_000147');
        await page.getByRole('button', { name: 'Отозвать заявку' }).click();
        const dialog = page.getByRole('dialog');
        await dialog.getByLabel('Почему отзываете').fill('Нашли деньги сами');
        await dialog.getByRole('button', { name: 'Отозвать', exact: true }).click();
        await expect(statusHeading(page, 'Заявка закрыта')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Отозвать заявку' })).toHaveCount(0);
        await page.getByRole('tab', { name: 'История' }).click();
        await expect(page.getByText('Заявка отозвана: Нашли деньги сами')).toBeVisible();
    });
});

test.describe('Нужно уточнить (08)', () => {
    test('ответы на вопросы возвращают заявку на рассмотрение', async ({ page }) => {
        await seedToken(page, 'accountant');
        await setPipelineStep(page, 300);
        await page.goto('/applications/app_000131');
        await expect(page.getByRole('tab', { name: /Статус/ })).toContainText('2');
        const send = page.getByRole('button', { name: 'Отправить ответы' });
        await expect(send).toBeDisabled();
        await expect(page.getByText('Ответили на 0 из 2 вопросов')).toBeVisible();

        const revenue = page.getByRole('region', { name: /Уточните выручку/ });
        await revenue.getByLabel('Выручка за год').pressSequentially('41600000');
        await expect(page.getByText('Ответили на 1 из 2 вопросов')).toBeVisible();

        // МЧД и откреплённая подпись: .sig уходит отдельным документом SIGNATURE со ссылкой на XML
        const poa = page.getByRole('region', { name: /Загрузите доверенность/ });
        await chooseFile(page, poa, POA);
        await expect(poa.getByRole('listitem').filter({ hasText: 'подпись к doverennost_ivanova.xml' })).toHaveCount(1);
        await expect(poa.getByRole('listitem').first()).toContainText('подпись верна', { timeout: 10_000 });

        await expect(send).toBeEnabled();
        await send.click();
        await expect(statusHeading(page, 'Рассматриваем заявку')).toBeVisible();
        await expect(statusHeading(page, 'Банк готов выдать кредит')).toBeVisible({ timeout: 15_000 });
    });

    test('распознанную выручку можно подставить в ответ', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto('/applications/app_000131');
        const revenue = page.getByRole('region', { name: /Уточните выручку/ });
        const recognized = page.waitForResponse((response) => response.url().includes('/financial-result') && response.status() === 200);
        await chooseFile(page, revenue, { name: 'Баланс_2025.pdf', mimeType: 'application/pdf' });
        await recognized;
        await revenue.getByRole('button', { name: 'Подставить' }).click({ timeout: 10_000 });
        await expect(revenue.getByLabel('Выручка за год')).toHaveValue(/41\s600\s000/);
    });

    test('отчёт и повторная проверка доверенности доступны в вопросе банка', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto('/applications/app_000131');
        const poa = page.getByRole('region', { name: /Загрузите доверенность/ });
        await chooseFile(page, poa, POA);
        const original = poa.getByRole('listitem').first();
        await expect(original).toContainText('подпись верна', { timeout: 10_000 });
        const report = page.waitForResponse((response) => response.url().endsWith('/signature-report') && response.status() === 200);
        await original.getByRole('button', { name: 'Отчёт о подписи' }).click();
        await report;
        await expect(original.getByRole('region', { name: /Проверка подписи/ })).toContainText('Подпись верна');
        const retry = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/signature-verification'));
        await original.getByRole('button', { name: 'Проверить подпись повторно' }).click();
        await retry;
        await expect(original).toContainText('проверяем подпись');
        await expect(original.getByRole('button', { name: 'Проверить подпись повторно' })).toBeEnabled({ timeout: 10_000 });
        // Вложения остаются привязаны к доверенности после обновления страницы.
        await page.reload();
        await expect(poa.getByRole('listitem')).toHaveCount(2);
        await expect(page.getByRole('region', { name: /Уточните выручку/ }).getByRole('listitem')).toHaveCount(0);
    });
});

test.describe('Кредитное предложение (09) и отказ (10)', () => {
    test.beforeEach(async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto('/applications/app_000155');
        await expect(statusHeading(page, 'Банк готов выдать кредит')).toBeVisible();
    });

    test('три предложения, рекомендуемое первым; выбор ведёт к подписанию', async ({ page }) => {
        await expect(offers(page)).toHaveCount(3);
        await expect(offers(page).first()).toContainText('Рекомендуем');
        await expect(offers(page).first()).toContainText('Как вы запросили');
        await expect(offers(page).nth(1)).toContainText('Дольше и дешевле в месяц');
        await offers(page).first().getByRole('button', { name: 'Выбрать' }).click();
        await expect(page.getByRole('heading', { name: 'Документы на подпись' })).toBeVisible();
    });

    test('отказ → удерживающее предложение → всё равно отказаться', async ({ page }) => {
        await page.getByRole('button', { name: 'Ни одно не подходит' }).click();
        const dialog = page.getByRole('dialog');
        const submit = dialog.getByRole('button', { name: 'Отправить' });
        await expect(submit).toBeDisabled();
        await dialog.getByText('Высокая ставка').click();
        await submit.click();
        await expect(dialog.getByRole('heading', { name: 'Можем снизить ставку' })).toBeVisible();
        await expect(dialog).toContainText('Обороты в банк от 50% выручки');
        await dialog.getByRole('button', { name: 'Всё равно отказаться' }).click();
        await expect(dialog).toHaveCount(0);
        await expect(statusHeading(page, 'Заявка закрыта')).toBeVisible();
    });

    test('удерживающее предложение можно принять', async ({ page }) => {
        await page.getByRole('button', { name: 'Ни одно не подходит' }).click();
        const dialog = page.getByRole('dialog');
        await dialog.getByText('Нашёл кредит в другом банке').click();
        await dialog.getByRole('button', { name: 'Отправить' }).click();
        await dialog.getByRole('button', { name: 'Принять предложение' }).click();
        await expect(page.getByRole('heading', { name: 'Документы на подпись' })).toBeVisible();
    });

    test('«Кредит больше не нужен» — сразу закрываем, без удержания', async ({ page }) => {
        await page.getByRole('button', { name: 'Ни одно не подходит' }).click();
        const dialog = page.getByRole('dialog');
        await dialog.getByText('Кредит больше не нужен').click();
        await dialog.getByRole('button', { name: 'Отправить' }).click();
        await expect(statusHeading(page, 'Заявка закрыта')).toBeVisible();
    });

    test('закрыли окно удержания — предложение остаётся на странице', async ({ page }) => {
        await page.getByRole('button', { name: 'Ни одно не подходит' }).click();
        const dialog = page.getByRole('dialog');
        await dialog.getByText('Не подходит срок').click();
        await dialog.getByRole('button', { name: 'Отправить' }).click();
        await expect(dialog.getByRole('heading', { name: 'Можем снизить ставку' })).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(page.getByRole('region', { name: 'Специальное предложение' })).toBeVisible();
        await page.getByRole('region', { name: 'Специальное предложение' }).getByRole('button', { name: 'Принять предложение' }).click();
        await expect(page.getByRole('heading', { name: 'Документы на подпись' })).toBeVisible();
    });
});

test.describe('Ошибки и редкие сценарии', () => {
    test('412 при отказе: перечитываем статус, повтор проходит', async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto('/applications/app_000155');
        await setMockFailures(page, [{ method: 'POST', path: '/offers/decline$', status: 412, code: 'VERSION_CONFLICT', times: 1 }]);
        await page.getByRole('button', { name: 'Ни одно не подходит' }).click();
        const dialog = page.getByRole('dialog', { name: 'Почему предложения не подошли?' });
        await dialog.getByText('Высокая ставка').click();
        const refreshed = page.waitForRequest((r) => r.url().endsWith('/applications/app_000155/status'));
        await dialog.getByRole('button', { name: 'Отправить' }).click();
        await expect(dialog.getByRole('alert')).toContainText('только что изменили');
        await refreshed;
        await dialog.getByRole('button', { name: 'Отправить' }).click();
        await expect(page.getByRole('dialog', { name: 'Можем снизить ставку' })).toBeVisible();
    });

    test('410 при выборе: предложение больше не действует', async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto('/applications/app_000155');
        await setMockFailures(page, [{ method: 'POST', path: '/accept$', status: 410, code: 'OFFER_EXPIRED', times: 1 }]);
        await offers(page).first().getByRole('button', { name: 'Выбрать' }).click();
        await expect(page.getByRole('alert')).toContainText('Предложение больше не действует');
    });

    test('ответ отклонён (422) — заявка ждёт, после исправления уходит дальше', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto('/applications/app_000131');
        await setMockFailures(page, [{ method: 'POST', path: '/info-requests/ir_app_000131_1/answer$', status: 422, field: 'value', times: 1 }]);
        const revenue = page.getByRole('region', { name: /Уточните выручку/ });
        await revenue.getByLabel('Выручка за год').pressSequentially('41600000');
        const poa = page.getByRole('region', { name: /Загрузите доверенность/ });
        await chooseFile(page, poa, POA);
        await expect(poa.getByRole('listitem').first()).toContainText('подпись верна', { timeout: 10_000 });
        await page.getByRole('button', { name: 'Отправить ответы' }).click();
        await expect(revenue.getByRole('alert')).toContainText('Отклонено сервером');
        // доверенность принята, выручку — ещё раз
        await expect(page.getByRole('region', { name: /Загрузите доверенность/ })).toHaveCount(0);
        await page.getByRole('button', { name: 'Отправить ответы' }).click();
        await expect(statusHeading(page, 'Рассматриваем заявку')).toBeVisible();
    });

    test('подпись без документа не загружаем', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto('/applications/app_000131');
        const poa = page.getByRole('region', { name: /Загрузите доверенность/ });
        await chooseFile(page, poa, { name: 'lonely.sig', mimeType: 'application/pkcs7-signature' });
        await expect(poa).toContainText('Сначала загрузите документ, к которому относится подпись');
    });

    test('отзыв из «Нужно уточнить»', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto('/applications/app_000131');
        await page.getByRole('button', { name: 'Отозвать заявку' }).click();
        await page.getByRole('dialog', { name: 'Отозвать заявку?' }).getByRole('button', { name: 'Отозвать', exact: true }).click();
        await expect(statusHeading(page, 'Заявка закрыта')).toBeVisible();
        await expect(page.getByRole('tab', { name: 'Статус' })).not.toContainText(/\d/);
    });

    test('главная обновляется по событиям без перезагрузки', async ({ page }) => {
        await seedToken(page, 'director');
        await setPipelineStep(page, 300);
        await submitDraft(page);
        await page.getByRole('navigation', { name: 'Вы здесь' }).getByRole('link', { name: 'Кредиты' }).click();
        const row = page.getByRole('link', { name: /Кредит на оборудование/ });
        await expect(row).toContainText('Решение готово', { timeout: 15_000 });
    });
});
