import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { COLLECTOR_LIVE, expectPath, MOCK_ONLY, seedToken, setPipelineStep, USERS } from './helpers';

/* Заявка data-collector после отправки анкеты: статус сбора, итог по системам, документы, главная. */

const EXAMPLES = resolve(import.meta.dirname, '../../data-collector/examples/files');
const QUESTIONNAIRE = {
    age: 42,
    marital_status: 'MARRIED',
    children_count: 2,
    product_code: 'WORKING_CAPITAL',
    requested: { amount: 5_000_000, term_months: 24 },
    purpose: { code: 'WORKING_CAPITAL' },
    documents: [{ file_name: 'balance.pdf', type: 'FINANCIAL_STATEMENTS' }],
    director: { inn: '779900010139', last_name: 'Ковалёв', first_name: 'Игорь', middle_name: 'Сергеевич', birth_date: '1984-07-19' },
    consents: { bki_company: { id: 'CNS-1', signed_at: '2026-10-21T14:02:11Z', valid_until: '2027-04-21' } },
};

/** Регистрация и анкета — прямо в API data-collector, с токеном вошедшего, как это делает анкета кабинета. */
async function createApplication(page: Page, { submit = true, inn = '7799000012', as = '' } = {}) {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    const files = submit
        ? [
              { name: 'balance.pdf', type: 'application/pdf', data: [...readFileSync(`${EXAMPLES}/balance.pdf`)] },
              { name: 'balance.pdf.sig', type: 'application/pkcs7-signature', data: [...readFileSync(`${EXAMPLES}/balance.pdf.sig`)] },
          ]
        : [];
    return page.evaluate(
        async ({ questionnaire, files, submit, inn, as }) => {
            const base = '/collector/api/v1';
            const auth = { Authorization: `Bearer ${as ? `mock.${as}` : localStorage.getItem('credit-cabinet:token')}` };
            const registered = await fetch(`${base}/applicants`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', ...auth },
                body: JSON.stringify({ representative: { full_name: 'Петров Пётр Петрович', role: 'DIRECTOR' }, company: { inn, name: 'ООО «Северный ветер»' } }),
            });
            const app = (await registered.json()) as { id: string };
            if (submit) {
                const form = new FormData();
                form.append('questionnaire', JSON.stringify(questionnaire));
                for (const f of files) form.append('files', new File([new Uint8Array(f.data)], f.name, { type: f.type }), f.name);
                const sent = await fetch(`${base}/applications/${app.id}/questionnaire`, { method: 'POST', body: form, headers: auth });
                if (sent.status !== 202) throw new Error(`questionnaire: ${sent.status} ${await sent.text()}`);
            }
            return app.id;
        },
        { questionnaire: QUESTIONNAIRE, files, submit, inn, as },
    );
}

test.beforeEach(async ({ page }) => {
    // директор «Северного ветра»: компания сценария A уже в профиле — так же и у живого сервиса
    await seedToken(page, 'northDirector');
});

test('статус сбора доходит до «данные собраны» сам, без перезагрузки', async ({ page }) => {
    await setPipelineStep(page, 1500);
    const id = await createApplication(page);
    await page.goto(`/applications/${id}`);
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Оборотный кредит · 5 000 000 ₽');
    const stages = page.getByRole('list', { name: 'Этапы заявки' });
    await expect(page.getByRole('heading', { level: 2, name: 'Данные о компании собраны' })).toBeVisible({ timeout: 10_000 });
    await expect(stages.getByRole('listitem').filter({ hasText: 'Проверка банком' })).toHaveAttribute('aria-current', 'step');

    // название и ОГРН — из ЕГРЮЛ после сбора
    const company = page.getByRole('region', { name: 'Компания' });
    await expect(company).toContainText('ООО "СЕВЕРНЫЙ ВЕТЕР"');
    await expect(company).toContainText('1217700412345');
});

test('итог сбора по системам: получено и пропущено с причиной', async ({ page }) => {
    await setPipelineStep(page, 300);
    const id = await createApplication(page);
    await page.goto(`/applications/${id}`);
    const systems = page.getByRole('list', { name: 'Системы' }).getByRole('listitem');
    await expect(systems).toHaveCount(19, { timeout: 10_000 });
    await expect(systems.filter({ hasText: 'Счета в Альфа-Банке' })).toContainText('получено');
    await expect(systems.filter({ hasText: 'ЕГРН' })).toContainText('пропущен');
    await expect(systems.filter({ hasText: 'ЕГРН' })).toContainText('нет недвижимости в залог');
    await expect(systems.filter({ hasText: 'Налоговая отчётность' })).toContainText('нет согласия на налоговую отчётность');
    await expect(page.getByText(/Получили данные из \d+ систем из 19/)).toBeVisible();
});

test('главная: заявка и черновик data-collector в «Моих заявках»', async ({ page }) => {
    await setPipelineStep(page, 300);
    const submitted = await createApplication(page);
    const draft = await createApplication(page, { submit: false });
    await page.goto('/');
    const rows = page.getByRole('region', { name: 'Мои заявки' }).getByRole('link');
    // строки — по ссылкам: у живого сервиса в списке есть и заявки коллег по компании
    const submittedRow = page.locator(`a[href="/applications/${submitted}"]`);
    await expect(submittedRow).toContainText('Данные собраны', { timeout: 10_000 });
    await expect(submittedRow).toContainText('Оборотный кредит · 5 000 000 ₽');
    await expect(rows.first()).toBeVisible();
    // список — с сервера (заявки компании, в том числе коллег), строка черновика ведёт в анкету
    const draftRow = page.locator(`a[href^="/applications/${draft}"]`);
    await expect(draftRow).toContainText('Продолжить заполнение');
    // карточка компании — реквизиты из ЕГРЮЛ после сбора
    await expect(page.getByRole('region', { name: /СЕВЕРНЫЙ ВЕТЕР|Северный ветер/ })).toContainText('1217700412345');

    await page.locator(`a[href="/applications/${submitted}"]`).click();
    await expectPath(page, `/applications/${submitted}`);
    await page.goto('/');
    await draftRow.click();
    await expect(page).toHaveURL(new RegExp(`/applications/${draft}/edit/`));
});

test('черновик по ссылке статуса — в анкету; неизвестная заявка — 404', async ({ page }) => {
    const draft = await createApplication(page, { submit: false });
    await page.goto(`/applications/${draft}`);
    await expect(page).toHaveURL(new RegExp(`/applications/${draft}/edit/`));
    await page.goto('/applications/app_01ZZZZZZZZZZZZZZZZZZZZZZZZ');
    await expect(page.getByText('Заявка не найдена')).toBeVisible();
});

test('сбор не удался: честный статус, опрос останавливается', async ({ page }) => {
    test.skip(COLLECTOR_LIVE, MOCK_ONLY);
    await setPipelineStep(page, 300);
    // у этой компании в моке задача сбора уходит в FAILED, а заявка остаётся в ENRICHING — как у data-collector
    const id = await createApplication(page, { inn: '7799000077' });
    let polls = 0;
    page.on('request', (r) => {
        if (r.url().endsWith(`/collector/api/v1/applications/${id}`)) polls += 1;
    });
    await page.goto(`/applications/${id}`);
    await expect(page.getByRole('heading', { level: 2, name: 'Не получилось собрать данные о компании' })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Сбор не удался', { exact: true })).toBeVisible();
    await expect(page.getByText(/Повторить сбор из кабинета пока нельзя/)).toBeVisible();
    const before = polls;
    await page.waitForTimeout(5_000);
    expect(polls - before).toBeLessThanOrEqual(1);

    await page.goto('/');
    await expect(page.getByRole('region', { name: 'Мои заявки' }).getByRole('link').filter({ hasText: 'Сбор не удался' })).toHaveCount(1);
});

test('вкладка «Документы»: файлы, скачивание, отчёт о подписи (экран 15)', async ({ page }) => {
    await setPipelineStep(page, 300);
    const id = await createApplication(page);
    await page.goto(`/applications/${id}?tab=documents`);
    const rows = page.getByRole('table').getByRole('row');
    await expect(rows.filter({ hasText: 'balance.pdf.sig' })).toContainText('подпись к «balance.pdf»');
    const doc = rows.filter({ hasText: 'Бухгалтерская отчётность' });
    await expect(doc).toContainText('Верна, есть замечания', { timeout: 10_000 });

    const download = page.waitForEvent('download');
    await doc.getByRole('button', { name: 'Скачать: balance.pdf' }).click();
    expect((await download).suggestedFilename()).toBe('balance.pdf');

    // отчёт — у открепленной подписи: подписантов у документа может быть несколько
    await expect(doc.getByRole('button', { name: /Отчёт о подписи/ })).toHaveCount(0);
    await page.getByRole('button', { name: 'Отчёт о подписи: balance.pdf.sig' }).click();
    const panel = page.getByRole('complementary', { name: 'Проверка подписи' });
    await expect(panel).toContainText('Подпись верна, с замечаниями');
    await expect(panel.getByRole('list', { name: 'Этапы проверки' }).getByRole('listitem')).toHaveCount(5);
    await expect(panel).toContainText('Тестовый профиль проверки');
});

test('вкладка «История»: журнал заявки', async ({ page }) => {
    await setPipelineStep(page, 300);
    const id = await createApplication(page);
    await page.goto(`/applications/${id}?tab=history`);
    const history = page.getByRole('tabpanel');
    await expect(history).toContainText('Данные о компании собраны', { timeout: 10_000 });
    await expect(history).toContainText('Начали собирать данные о компании');
    await expect(history).toContainText(/Заявка отправлена/);
});

test('черновик коллеги не виден и не отправляется', async ({ page }) => {
    const colleagueDraft = await createApplication(page, { submit: false, as: USERS.northAccountant.id });
    const list = page.waitForResponse((r) => new URL(r.url()).pathname === '/collector/api/v1/applications');
    await page.goto('/');
    const items = ((await (await list).json()) as { items: { id: string }[] }).items;
    expect(items.map((a) => a.id)).not.toContain(colleagueDraft);
    await expect(page.locator(`a[href^="/applications/${colleagueDraft}"]`)).toHaveCount(0);
    const status = await page.evaluate(async (id) => {
        const form = new FormData();
        form.append('questionnaire', JSON.stringify({ age: 40, marital_status: 'SINGLE', children_count: 0 }));
        const r = await fetch(`/collector/api/v1/applications/${id}/questionnaire`, {
            method: 'POST',
            body: form,
            headers: { Authorization: `Bearer ${localStorage.getItem('credit-cabinet:token')}` },
        });
        return r.status;
    }, colleagueDraft);
    expect(status).toBe(403);
});
