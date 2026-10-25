import { expect, test } from '@playwright/test';

import { expectPath, loginAs, seedToken, TOKEN_KEY } from './helpers';

test.describe('Доступ по ролям', () => {
    test('гостя с защищённых страниц уводит на вход', async ({ page }) => {
        await page.goto('/');
        await expectPath(page, '/login');
        await page.goto('/backoffice');
        await expectPath(page, '/login');
    });

    test('андеррайтер попадает в рабочее место банка и может выйти', async ({ page }) => {
        await loginAs(page, 'underwriter');
        await expectPath(page, '/backoffice');
        // кабинет клиента андеррайтеру недоступен
        await page.goto('/');
        await expectPath(page, '/backoffice');

        await page.getByRole('button', { name: 'Выйти' }).click();
        await expectPath(page, '/login');
        expect(await page.evaluate((key) => localStorage.getItem(key), TOKEN_KEY)).toBeNull();
    });

    test('клиента с /backoffice уводит на главную', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/backoffice');
        await expectPath(page, '/');
    });
});

/* Ошибки /me подставляем через page.route, поэтому Service Worker MSW выключаем:
   заодно проверяем, что приложение стартует и без моков. */
test.describe('Ошибки профиля /me', () => {
    test.use({ serviceWorkers: 'block' });

    const problem = (status: number, code: string) => ({
        status,
        contentType: 'application/problem+json',
        body: JSON.stringify({ type: `https://errors.credit-cabinet.local/${code}`, title: code, status, code }),
    });

    test('500 — плашка с «Повторить», без петли редиректов', async ({ page }) => {
        let calls = 0;
        await page.route('**/api/v1/me', (route) => {
            calls += 1;
            return route.fulfill(problem(500, 'INTERNAL'));
        });
        await seedToken(page, 'director');
        await page.goto('/');

        await expect(page.getByText('Не удалось загрузить профиль')).toBeVisible();
        await expectPath(page, '/');
        await page.waitForTimeout(500);
        expect(calls).toBe(1);

        // бэкенд ожил — «Повторить» показывает кабинет
        await page.unroute('**/api/v1/me');
        await page.route('**/api/v1/me', (route) =>
            route.fulfill({
                json: { id: 'usr_director_romashka', full_name: 'Петров Пётр Петрович', roles: ['CLIENT'], companies: [] },
            }),
        );
        await page.getByRole('button', { name: 'Повторить' }).click();
        await expect(page.getByRole('heading', { level: 1 })).toHaveText('Добрый день, Пётр Петрович');
    });

    test('401 — сброс токена и переход на вход', async ({ page }) => {
        await page.route('**/api/v1/me', (route) => route.fulfill(problem(401, 'UNAUTHORIZED')));
        await page.route('**/api/v1/auth/test-users', (route) => route.fulfill({ json: { items: [] } }));
        await seedToken(page, 'director');
        await page.goto('/');

        await expectPath(page, '/login');
        expect(await page.evaluate((key) => localStorage.getItem(key), TOKEN_KEY)).toBeNull();
    });
});
