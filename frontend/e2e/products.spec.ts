import { expect, test } from '@playwright/test';

import { expectPath, SCENARIO_B, seedToken, USERS } from './helpers';

test.describe('Подбор продукта и калькулятор', () => {
    test.beforeEach(async ({ page }) => {
        await seedToken(page, 'director');
    });

    test('под «Пополнить оборот» три продукта, у оборотного — уже есть заявка', async ({ page }) => {
        // заявку подаём прямо в API data-collector — после старта моков, иначе прокси dev-сервера уведёт запрос в живой стек
        await seedToken(page, 'stroyresurs');
        await page.goto('/');
        await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
        const number = await page.evaluate(
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
                return app.number;
            },
            { questionnaire: SCENARIO_B, token: `mock.${USERS.stroyresurs.id}` },
        );

        await page.goto('/products');
        const products = page.getByRole('radiogroup', { name: 'Кредитный продукт' }).getByRole('radio');
        await expect(products).toHaveCount(3);
        await expect(products.first()).toHaveAttribute('aria-checked', 'true');
        await expect(products.first()).toContainText('Уже есть заявка');
        // вторую заявку на тот же продукт не оформляем — ведём к существующей
        await expect(page.getByRole('link', { name: `Перейти к заявке ${number}` })).toBeVisible();
        await expect(page.getByRole('button', { name: 'Оформить заявку' })).toHaveCount(0);
    });

    test('расчёт вне диапазона продукта — 422 по сумме и сроку', async ({ page }) => {
        await page.goto('/');
        await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
        const res = await page.evaluate(async () => {
            const r = await fetch('/collector/api/v1/products/EXPRESS/calculate', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ amount: 8_000_000, term_months: 36 }),
            });
            return { status: r.status, fields: ((await r.json()) as { errors?: { field: string }[] }).errors?.map((e) => e.field) };
        });
        expect(res).toEqual({ status: 422, fields: ['requested.amount', 'requested.term_months'] });
    });

    test('смена задачи фильтрует каталог и попадает в URL', async ({ page }) => {
        await page.goto('/products');
        await page.getByRole('button', { name: 'Купить транспорт' }).click();
        await expect(page).toHaveURL(/purpose=VEHICLE/);
        const products = page.getByRole('radio');
        await expect(products).toHaveCount(1);
        await expect(products.first()).toHaveAccessibleName('Кредит на транспорт');
        await expect(page.getByRole('button', { name: 'Купить транспорт' })).toHaveAttribute('aria-pressed', 'true');
    });

    test('платёж пересчитывается, «Оформить» уводит в анкету с параметрами', async ({ page }) => {
        await page.goto('/products');
        await page.getByRole('radio', { name: 'Кредитная линия' }).click();
        const estimate = page.getByRole('status', { name: 'Ежемесячный платёж' });
        await expect(estimate).toContainText(/от \d/);
        const before = await estimate.textContent();

        // стрелки двигают выбор внутри radiogroup
        await page.getByRole('radio', { name: 'Кредитная линия' }).press('ArrowDown');
        await expect(page.getByRole('radio', { name: 'Экспресс-кредит без залога' })).toHaveAttribute('aria-checked', 'true');
        await expect(page.getByRole('radio', { name: 'Экспресс-кредит без залога' })).toBeFocused();
        await expect(estimate).not.toHaveText(before ?? '');

        await page.getByRole('button', { name: 'Оформить заявку' }).click();
        await expectPath(page, '/applications/new');
        const url = new URL(page.url());
        expect(url.searchParams.get('product')).toBe('EXPRESS');
        // 5 млн — максимум экспресс-кредита, срок прижат к его 24 мес.
        expect(url.searchParams.get('amount')).toBe('5000000');
        expect(url.searchParams.get('term')).toBe('24');
        expect(url.searchParams.get('purpose')).toBe('WORKING_CAPITAL');
    });

    test('сумма вне диапазона — ошибка и кнопка недоступна', async ({ page }) => {
        await page.goto('/products?purpose=WORKING_CAPITAL');
        await page.getByRole('radio', { name: 'Кредитная линия' }).click();
        const amount = page.getByLabel('Сумма кредита');
        await amount.fill('');
        await amount.pressSequentially('500');
        await expect(page.getByText('От 1 000 000 ₽ до 50 000 000 ₽')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Оформить заявку' })).toBeDisabled();
    });
});
