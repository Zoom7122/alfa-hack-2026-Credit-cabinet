import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { expectPath, seedToken, setPipelineStep, USERS } from './helpers';

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

/**
 * Регистрация и анкета — прямо в API data-collector (его моки), как это делает анкета кабинета;
 * id — в локальный список заявок пользователя (ручки списка у сервиса нет).
 */
async function createApplication(page: Page, { submit = true, inn = '7799000012' } = {}) {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
    const files = submit
        ? [
              { name: 'balance.pdf', type: 'application/pdf', data: [...readFileSync(`${EXAMPLES}/balance.pdf`)] },
              { name: 'balance.pdf.sig', type: 'application/pkcs7-signature', data: [...readFileSync(`${EXAMPLES}/balance.pdf.sig`)] },
          ]
        : [];
    return page.evaluate(
        async ({ questionnaire, files, submit, userId, inn }) => {
            const base = '/collector/api/v1';
            const registered = await fetch(`${base}/applicants`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ representative: { full_name: 'Петров Пётр Петрович', role: 'DIRECTOR' }, company: { inn, name: 'ООО «Северный ветер»' } }),
            });
            const app = (await registered.json()) as { id: string; number: string; created_at: string };
            const key = `credit-cabinet:collector-apps:${userId}`;
            const list = JSON.parse(localStorage.getItem(key) ?? '[]') as unknown[];
            localStorage.setItem(key, JSON.stringify([{ id: app.id, number: app.number, company_inn: inn, created_at: app.created_at }, ...list]));
            if (submit) {
                const form = new FormData();
                form.append('questionnaire', JSON.stringify(questionnaire));
                for (const f of files) form.append('files', new File([new Uint8Array(f.data)], f.name, { type: f.type }), f.name);
                const sent = await fetch(`${base}/applications/${app.id}/questionnaire`, { method: 'POST', body: form });
                if (sent.status !== 202) throw new Error(`questionnaire: ${sent.status} ${await sent.text()}`);
            }
            return app.id;
        },
        { questionnaire: QUESTIONNAIRE, files, submit, userId: USERS.director.id, inn },
    );
}

test.beforeEach(async ({ page }) => {
    await seedToken(page, 'director');
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

test('документы заявки: тип, подпись к документу', async ({ page }) => {
    const id = await createApplication(page);
    await page.goto(`/applications/${id}`);
    const docs = page.getByRole('list', { name: 'Документы заявки' }).getByRole('listitem');
    await expect(docs).toHaveCount(2);
    await expect(docs.filter({ hasText: 'balance.pdf.sig' })).toContainText('подпись к balance.pdf');
    await expect(docs.first()).toContainText('Бухгалтерская отчётность');
});

test('главная: заявка и черновик data-collector в «Моих заявках»', async ({ page }) => {
    await setPipelineStep(page, 300);
    const submitted = await createApplication(page);
    const draft = await createApplication(page, { submit: false });
    await page.goto('/');
    const rows = page.getByRole('region', { name: 'Мои заявки' }).getByRole('link');
    // после сбора у компании — название из ЕГРЮЛ (у всех её заявок, включая черновик)
    const submittedRow = rows.filter({ hasText: 'ООО "СЕВЕРНЫЙ ВЕТЕР"' }).filter({ hasText: 'Данные собраны' });
    await expect(submittedRow).toHaveCount(1, { timeout: 10_000 });
    await expect(submittedRow).toContainText('Оборотный кредит · 5 000 000 ₽');
    await expect(rows.filter({ hasText: 'Продолжить заполнение' })).toHaveCount(1);
    // демо-черновик моков больше не показываем: черновики живут в data-collector
    await expect(rows.filter({ hasText: 'A-2026-000140' })).toHaveCount(0);

    await submittedRow.click();
    await expectPath(page, `/applications/${submitted}`);
    await page.goto('/');
    await rows.filter({ hasText: 'Продолжить заполнение' }).click();
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
