import { expect, test } from '@playwright/test';

import { expectPath, loginAs, seedToken } from './helpers';

test.describe('Главная клиента', () => {
    test('директор видит задачи «Ждут вашего действия»', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/');
        const tasks = page.getByRole('region', { name: 'Ждут вашего действия' }).getByRole('article');
        await expect(tasks).toHaveCount(2);
        await expect(tasks.nth(0)).toContainText('Подпишите кредитный договор');
        await expect(tasks.nth(0).getByRole('link', { name: 'Подписать' })).toHaveAttribute('href', '/applications/app_000123');
        await expect(tasks.nth(1)).toContainText('Банк задал 2 вопроса');
    });

    test('бухгалтер не получает задачу на подпись — подписывает директор', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto('/');
        await expect(page.getByRole('link', { name: /Оборотный кредит · 5 000 000/ })).toContainText('Ждём подписи директора');
        await expect(page.getByText('Подпишите кредитный договор')).toHaveCount(0);
    });

    test('у Лебедевой есть готовое решение', async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto('/');
        await expect(page.getByText('Решение готово — выберите предложение')).toBeVisible();
        await expect(page.getByRole('link', { name: /Кредит на транспорт/ })).toContainText('Решение готово');
    });

    test('фильтр «Активные / Завершённые»', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/');
        const list = page.getByRole('region', { name: 'Мои заявки' }).getByRole('listitem');
        await expect(list).toHaveCount(3);
        await expect(page.getByRole('button', { name: 'Активные · 3' })).toHaveAttribute('aria-pressed', 'true');

        await page.getByRole('button', { name: 'Завершённые · 2' }).click();
        await expect(list).toHaveCount(2);
        await expect(list.nth(0)).toContainText('Кредит выдан');
        await expect(list.nth(1)).toContainText('Вы отказались от предложения');
    });

    test('статусы и подсказки строк соответствуют этапам FSM', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/');
        await expect(page.getByRole('link', { name: /Кредит на оборудование/ })).toContainText('Продолжить заполнение');
        await expect(page.getByRole('link', { name: /Кредит на транспорт/ })).toContainText('Нужно уточнить');
        await expect(page.getByRole('link', { name: /Кредит на транспорт/ })).toContainText('2 вопроса от банка');
        await expect(page.getByRole('link', { name: /Оборотный кредит · 5 000 000/ })).toContainText('Ждёт вашей подписи');
    });

    test('карточка компании из /applicants', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/');
        const card = page.getByRole('region', { name: 'ООО «Ромашка»' });
        await expect(card).toContainText('7701234567');
        await expect(card).toContainText('1027700000000');
        await expect(card).toContainText('2019 года');
    });

    test('нет вложенных интерактивных элементов, у каждого — один Tab', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/');
        await expect(page.getByRole('region', { name: 'Мои заявки' }).getByRole('listitem')).toHaveCount(3);
        await expect(page.locator('a button, button a, a a, button button')).toHaveCount(0);
    });

    test('переход по строке заявки и «На главную» из заглушки', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/');
        await page.getByRole('link', { name: /Кредит на оборудование/ }).click();
        await expectPath(page, '/applications/app_000140');
        await expect(page.getByRole('heading', { name: 'Экран в разработке' })).toBeVisible();
        await page.getByRole('link', { name: 'На главную', exact: true }).click();
        await expectPath(page, '/');
    });

    test('«Выйти» в шапке возвращает на вход', async ({ page }) => {
        await loginAs(page, 'director');
        await expectPath(page, '/');
        await page.getByRole('button', { name: 'Выйти' }).click();
        await expectPath(page, '/login');
        await page.goto('/');
        await expectPath(page, '/login');
    });
});
