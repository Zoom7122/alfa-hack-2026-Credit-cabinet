import { expect, test } from '@playwright/test';

import { expectPath, loginAs, seedToken, USERS } from './helpers';

test.describe('Вход под тестовым пользователем', () => {
    test('показывает клиентов из /auth/test-users и выбирает первого', async ({ page }) => {
        await page.goto('/login');
        const personas = page.getByRole('radiogroup', { name: 'Тестовый пользователь' }).getByRole('radio');
        await expect(personas).toHaveCount(4);
        await expect(personas.first()).toHaveAttribute('aria-checked', 'true');
        await expect(page.getByRole('button', { name: 'Войти как Петров П. П.' })).toBeEnabled();
    });

    test('вкладка «Сотрудник банка» показывает только андеррайтера', async ({ page }) => {
        await page.goto('/login');
        await page.getByText('Сотрудник банка', { exact: true }).click();
        const personas = page.getByRole('radio');
        await expect(personas).toHaveCount(1);
        await expect(personas.first()).toContainText(USERS.underwriter.name);
    });

    test('стрелки переключают выбор и фокус (radiogroup)', async ({ page }) => {
        await page.goto('/login');
        const personas = page.getByRole('radio');
        await personas.first().focus();
        await page.keyboard.press('ArrowDown');
        await expect(personas.nth(1)).toHaveAttribute('aria-checked', 'true');
        await expect(personas.nth(1)).toBeFocused();
        await page.keyboard.press('ArrowUp');
        await page.keyboard.press('ArrowUp');
        await expect(personas.last()).toHaveAttribute('aria-checked', 'true');
        await expect(page.getByRole('button', { name: 'Войти как Лебедева В. О.' })).toBeVisible();
    });

    test('клиент после входа попадает на главную', async ({ page }) => {
        await loginAs(page, 'director');
        await expectPath(page, '/');
        await expect(page.getByRole('heading', { level: 1 })).toHaveText('Добрый день, Пётр Петрович');
    });

    test('залогиненного с /login уводит на главную', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/login');
        await expectPath(page, '/');
    });

    test('сессия переживает перезагрузку', async ({ page }) => {
        await loginAs(page, 'director');
        await expectPath(page, '/');
        await page.reload();
        await expectPath(page, '/');
        await expect(page.getByRole('heading', { name: 'Мои заявки' })).toBeVisible();
    });
});
