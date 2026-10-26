import { expect, type Page, test } from '@playwright/test';

import { COLLECTOR_LIVE, MOCK_ONLY, seedToken, type UserKey } from './helpers';

/* Адаптив: на телефоне (375 px) страницы не прокручиваются вбок, шапка помещается в экран. */

test.use({ viewport: { width: 375, height: 812 }, hasTouch: true });

async function expectNoHorizontalScroll(page: Page) {
    await expect(page.getByRole('heading', { level: 1 }).or(page.getByRole('heading', { level: 2 })).first()).toBeVisible();
    const width = await page.evaluate(() => document.documentElement.scrollWidth);
    expect(width).toBeLessThanOrEqual(375);
}

const PAGES: { name: string; user?: UserKey; path: string }[] = [
    { name: 'вход', path: '/login' },
    { name: 'главная', user: 'director', path: '/' },
    { name: 'подбор продукта', user: 'director', path: '/products' },
    { name: 'анкета: компания', user: 'northDirector', path: '/applications/new?product=WORKING_CAPITAL&amount=5000000&term=24&purpose=WORKING_CAPITAL' },
    { name: 'нужно уточнить (08)', user: 'director', path: '/applications/app_000131' },
    { name: 'предложения (09)', user: 'vector', path: '/applications/app_000155' },
    { name: 'подписание (11)', user: 'director', path: '/applications/app_000123' },
    { name: 'отказ банка (14)', user: 'vector', path: '/applications/app_000152' },
    { name: 'документы (15)', user: 'director', path: '/applications/app_000131?tab=documents' },
    { name: 'очередь банка (16)', user: 'underwriter', path: '/backoffice' },
];

for (const p of PAGES) {
    test(`без горизонтальной прокрутки: ${p.name}`, async ({ page }) => {
        // демо-заявки 07–15 есть только в моках
        test.skip(COLLECTOR_LIVE && p.path.includes('app_000'), MOCK_ONLY);
        if (p.user) await seedToken(page, p.user);
        await page.goto(p.path);
        await expectNoHorizontalScroll(page);
    });
}

test('шапка на телефоне: разделы второй строкой, выход виден', async ({ page }) => {
    await seedToken(page, 'director');
    await page.goto('/');
    const header = page.getByRole('banner');
    await expect(header.getByRole('navigation', { name: 'Разделы' })).toBeVisible();
    await expect(header.getByRole('button', { name: 'Выйти' })).toBeInViewport();
});

test('этапы заявки на телефоне: подписан только текущий', async ({ page }) => {
    test.skip(COLLECTOR_LIVE, MOCK_ONLY);
    await seedToken(page, 'director');
    await page.goto('/applications/app_000123');
    const stages = page.getByRole('list', { name: 'Этапы заявки' });
    // остальные названия скрыты визуально (1×1 px), но остаются для экранного диктора
    const width = async (name: string) => (await stages.getByText(name).boundingBox())?.width ?? 0;
    expect(await width('Подписание')).toBeGreaterThan(40);
    expect(await width('Рассмотрение')).toBeLessThanOrEqual(1);
});
