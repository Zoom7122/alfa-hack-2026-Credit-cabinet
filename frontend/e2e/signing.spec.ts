import { expect, type Page, test } from '@playwright/test';

import { clearMockFailures, expectPath, seedToken, setMockFailures } from './helpers';

const SIGNING = '/applications/app_000123';
const heading = (page: Page, name: string | RegExp) => page.getByRole('heading', { level: 2, name });

/** Демо-код из подсказки (в мок-режиме его показывают подписанту — SMS не отправляем). */
async function demoCode(page: Page) {
    const plate = page.getByText(/Демо-код:/);
    await expect(plate).toBeVisible();
    const match = /Демо-код:\s*(\d{3})\s*(\d{3})/.exec((await plate.textContent()) ?? '');
    expect(match).not.toBeNull();
    return `${match?.[1]}${match?.[2]}`;
}

async function typeCode(page: Page, code: string) {
    await page.getByLabel('Код 1 из 6').click();
    await page.keyboard.type(code);
}

const agree = (page: Page) => page.getByText('Ознакомился с условиями договора и графиком платежей').click();
const signButton = (page: Page) => page.getByRole('button', { name: 'Подписать и получить кредит' });

test.describe('Подписание директором (11)', () => {
    test.beforeEach(async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto(SIGNING);
        await expect(page.getByRole('heading', { name: 'Документы на подпись' })).toBeVisible();
    });

    test('документы, условия и кто готовил', async ({ page }) => {
        const docs = page.getByRole('region', { name: 'Документы на подпись' }).getByRole('listitem');
        await expect(docs).toHaveCount(3);
        await expect(docs.first()).toContainText('Кредитный договор № КД-2026-000123');
        await expect(docs.nth(1)).toContainText('24 платежа по');
        await expect(page.getByRole('region', { name: 'Условия' })).toContainText('5 000 000 ₽');
        await expect(page.getByRole('region', { name: 'Подготовил(а)' })).toContainText('Иванова А. С.');

        const download = page.waitForEvent('download');
        await page.getByRole('button', { name: 'Открыть: Кредитный договор № КД-2026-000123' }).click();
        expect((await download).suggestedFilename()).toBe('Кредитный договор № КД-2026-000123.pdf');
    });

    test('кодом: без согласия нельзя, с верным кодом — кредит выдан', async ({ page }) => {
        const code = await demoCode(page);
        await typeCode(page, code);
        await expect(signButton(page)).toBeDisabled();
        await agree(page);
        await signButton(page).click();
        await expect(heading(page, 'Кредит выдан')).toBeVisible();
        await expect(page.getByText(/5\s000\s000\s₽ зачислены на счёт 40702/)).toBeVisible();
        await expect(page.getByText('Петров П. П., код подтверждения')).toBeVisible();
        // задача «подписать» на главной пропала
        await page.getByRole('link', { name: 'На главную', exact: true }).click();
        await expectPath(page, '/');
        await expect(page.getByText('Подпишите кредитный договор')).toHaveCount(0);
        await expect(page.getByRole('link', { name: /Оборотный кредит · 5 000 000/ })).toHaveCount(0);
    });

    test('неверный код уменьшает попытки, после трёх — назад к предложениям', async ({ page }) => {
        await agree(page);
        for (const left of [2, 1]) {
            await typeCode(page, '000000');
            await signButton(page).click();
            await expect(page.getByText(`Неверный код. Осталось попыток: ${left}`)).toBeVisible();
            await expect(page.getByText(new RegExp(`осталось попыток: ${left}`))).toBeVisible();
        }
        await typeCode(page, '000000');
        await signButton(page).click();
        await expect(heading(page, 'Банк готов выдать кредит')).toBeVisible();
        // объясняем, почему вернулись, и задача «подписать» в колокольчике пропала
        await expect(page.getByText('Подписать договор не удалось')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Уведомления', exact: true })).toBeVisible();
    });

    test('сбой выдачи кода — сообщение и «Повторить»', async ({ browser }) => {
        const page = await (await browser.newContext()).newPage();
        await seedToken(page, 'director');
        await page.goto('/');
        await setMockFailures(page, [{ method: 'POST', path: '/signing/start$', status: 503, times: 1 }]);
        await page.goto(SIGNING);
        await expect(page.getByText('Не удалось получить код')).toBeVisible();
        await expect(page.getByText(/Демо-код:/)).toHaveCount(0);
        await page.getByRole('button', { name: 'Повторить' }).click();
        await expect(page.getByText(/Демо-код:/)).toBeVisible();
    });

    test('сбой сети при подписи — повтор с тем же кодом проходит', async ({ page }) => {
        const code = await demoCode(page);
        await typeCode(page, code);
        await agree(page);
        await setMockFailures(page, [{ method: 'POST', path: '/signing/confirm$', status: 503, times: 1 }]);
        await signButton(page).click();
        await expect(page.getByText('Не получилось подписать')).toBeVisible();
        await clearMockFailures(page);
        await signButton(page).click();
        await expect(heading(page, 'Кредит выдан')).toBeVisible();
    });

    test('подпись к прошлому договору не подходит к новому', async ({ page }) => {
        const uploadSig = async (name: string) => {
            await page.getByText('Электронная подпись (КЭП)').click();
            const chooser = page.waitForEvent('filechooser');
            await page.getByRole('button', { name: 'Загрузить подпись' }).click();
            await (await chooser).setFiles({ name, mimeType: 'application/pkcs7-signature', buffer: Buffer.from('sig') });
        };
        await uploadSig('old.sig');
        await expect(page.getByText(/old\.sig · подпись верна/)).toBeVisible({ timeout: 10_000 });
        // три неверных кода → назад к предложениям → выбираем снова → договор новый
        await page.getByText('Код подтверждения', { exact: true }).click();
        await agree(page);
        for (let i = 0; i < 3; i++) {
            await typeCode(page, '000000');
            await signButton(page).click();
            if (i < 2) await expect(page.getByText(`Неверный код. Осталось попыток: ${2 - i}`)).toBeVisible();
        }
        await expect(heading(page, 'Банк готов выдать кредит')).toBeVisible();
        await page.getByRole('region', { name: 'Предложения банка' }).getByRole('article').first().getByRole('button', { name: 'Выбрать' }).click();
        await page.getByText('Электронная подпись (КЭП)').click();
        await expect(page.getByText(/old\.sig/)).toHaveCount(0);
        await agree(page);
        await expect(signButton(page)).toBeDisabled();
    });

    test('КЭП: подпись к договору проверяется, после — кредит выдан', async ({ page }) => {
        await page.getByText('Электронная подпись (КЭП)').click();
        const chooser = page.waitForEvent('filechooser');
        await page.getByRole('button', { name: 'Загрузить подпись' }).click();
        await (await chooser).setFiles({ name: 'dogovor.pdf.sig', mimeType: 'application/pkcs7-signature', buffer: Buffer.from('sig') });
        await expect(page.getByText(/dogovor\.pdf\.sig · подпись верна/)).toBeVisible({ timeout: 10_000 });
        await agree(page);
        await signButton(page).click();
        await expect(heading(page, 'Кредит выдан')).toBeVisible();
        await expect(page.getByText('Петров П. П., электронная подпись (КЭП)')).toBeVisible();
    });
});

test.describe('Ждём директора (12)', () => {
    test('бухгалтер видит, кто подписывает, и может напомнить', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto(SIGNING);
        await expect(heading(page, 'Договор ждёт подписи директора')).toBeVisible();
        await expect(page.getByText('Петров П. П., директор')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Подписать и получить кредит' })).toHaveCount(0);
        await expect(page.getByText(/Демо-код/)).toHaveCount(0);

        await page.getByRole('button', { name: 'Напомнить директору' }).click();
        await expect(page.getByText('Напомнили директору — задача снова в его кабинете')).toBeVisible();
        await page.getByRole('tab', { name: 'История' }).click();
        await expect(page.getByText('Напомнили директору о подписании')).toBeVisible();
    });

    test('договор можно скачать', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto(SIGNING);
        const download = page.waitForEvent('download');
        await page.getByRole('button', { name: 'Скачать договор' }).click();
        expect((await download).suggestedFilename()).toMatch(/^Кредитный договор/);
    });
});

test.describe('Кредит выдан (13)', () => {
    test('выбор предложения директором сразу ведёт к подписи, после — выдача', async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto('/applications/app_000155');
        await page.getByRole('region', { name: 'Предложения банка' }).getByRole('article').first().getByRole('button', { name: 'Выбрать' }).click();
        await expect(page.getByRole('heading', { name: 'Документы на подпись' })).toBeVisible();
        await typeCode(page, await demoCode(page));
        await agree(page);
        await signButton(page).click();
        await expect(heading(page, 'Кредит выдан')).toBeVisible();
        await expect(page.getByRole('list', { name: 'Этапы заявки' }).locator('[aria-current]')).toHaveCount(0);
    });

    test('выданный кредит: договор, первый платёж, документы', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/applications/app_000088');
        await expect(heading(page, 'Кредит выдан')).toBeVisible();
        await expect(page.getByText(/КД-2026-000088 от/)).toBeVisible();
        await expect(page.getByText(/Первый платёж/)).toBeVisible();
        await expect(page.getByText(/Первый платёж/)).toBeVisible();
        const download = page.waitForEvent('download');
        await page.getByRole('button', { name: 'Скачать: График платежей' }).click();
        expect((await download).suggestedFilename()).toBe('График платежей.pdf');
    });
});
