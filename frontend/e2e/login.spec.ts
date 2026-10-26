import { expect, test } from '@playwright/test';

import { expectPath, loginAs, seedToken, USERS } from './helpers';

test.describe('Вход под тестовым пользователем', () => {
    test('показывает клиентов из /auth/test-users и выбирает первого', async ({ page }) => {
        await page.goto('/login');
        const personas = page.getByRole('radiogroup', { name: 'Тестовый пользователь' }).getByRole('radio');
        // список — от data-collector: клиенты на вкладке «Клиент» (сколько их, берём у него же)
        const clients = await page.evaluate(async () => {
            const { items } = (await (await fetch('/collector/api/v1/auth/test-users')).json()) as { items: { roles: string[] }[] };
            return items.filter((u) => u.roles.includes('CLIENT')).length;
        });
        expect(clients).toBeGreaterThan(4);
        await expect(personas).toHaveCount(clients);
        await expect(personas.filter({ hasText: 'Ковалёв Игорь Сергеевич' })).toContainText('Сценарий A');
        await expect(personas.first()).toHaveAttribute('aria-checked', 'true');
        await expect(page.getByRole('button', { name: 'Войти как Петров П. П.' })).toBeEnabled();
    });

    test('вкладка «Сотрудник банка» показывает только андеррайтеров', async ({ page }) => {
        await page.goto('/login');
        await page.getByText('Сотрудник банка', { exact: true }).click();
        const personas = page.getByRole('radio');
        await expect(personas).toHaveCount(2);
        await expect(personas.first()).toContainText(USERS.underwriter.name);
        await expect(personas.nth(1)).toContainText(USERS.orlov.name);
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
        // по кругу — на последнего клиента в списке data-collector, кнопка входа — под него
        const last = await page.evaluate(async () => {
            const { items } = (await (await fetch('/collector/api/v1/auth/test-users')).json()) as { items: { full_name: string; roles: string[] }[] };
            return items.filter((u) => u.roles.includes('CLIENT')).at(-1)?.full_name ?? '';
        });
        const [lastName, first, middle] = last.split(' ');
        await expect(page.getByRole('button', { name: `Войти как ${lastName} ${first?.[0]}. ${middle?.[0]}.` })).toBeVisible();
    });

    test('клиент после входа попадает на главную', async ({ page }) => {
        await loginAs(page, 'director');
        await expectPath(page, '/');
        await expect(page.getByRole('heading', { level: 1 })).toHaveText('Добрый день, Пётр Петрович');
    });

    test('представитель компании сценария A: в шапке его компания и роль', async ({ page }) => {
        await page.goto('/login');
        await page.getByRole('radio', { name: /Литвинова Марина Олеговна/ }).click();
        await page.getByRole('button', { name: 'Войти как Литвинова М. О.' }).click();
        await expectPath(page, '/');
        // у живого data-collector — название из ЕГРЮЛ, если по компании уже собирали данные
        await expect(page.getByRole('banner')).toContainText(/ООО «Северный ветер»|ООО "СЕВЕРНЫЙ ВЕТЕР"/);
        await expect(page.getByRole('banner')).toContainText('Бухгалтер');
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
