import { type Browser, expect, type Page, test } from '@playwright/test';

import { expectPath, seedToken, setMockFailures, type UserKey } from './helpers';

const rows = (page: Page) => page.getByRole('region', { name: 'Заявки' }).getByRole('table').getByRole('row');
const row = (page: Page, text: string) => rows(page).filter({ hasText: text });
const panel = (page: Page) => page.getByRole('dialog', { name: 'Решение по заявке' });
/** Select кита — не кнопка, а его подпись перекрыта полем: кликаем по месту подписи, клик получает само поле. */
async function openSelect(page: Page, label: string) {
    const caption = panel(page).getByText(label, { exact: true }).first();
    await caption.scrollIntoViewIfNeeded();
    await caption.click({ force: true });
}
const stat = (page: Page, label: string) => page.getByRole('group', { name: label });
// в панели две «Отказать»: сегмент переключателя и кнопка отправки в подвале — она последняя
const rejectSubmit = (page: Page) => panel(page).getByRole('button', { name: 'Отказать', exact: true }).last();

async function openDossier(page: Page, id = 'app_000147') {
    await page.goto(`/backoffice/applications/${id}`);
    await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
}

/** Клиент в отдельном браузере с тем же состоянием моков, что у андеррайтера. */
async function asClient(page: Page, browser: Browser, user: UserKey, path: string) {
    const db = await page.evaluate(() => {
        const key = Object.keys(localStorage).find((k) => k.startsWith('credit-cabinet:mock-db:'));
        return key ? ([key, localStorage.getItem(key) ?? ''] as const) : null;
    });
    const client = await (await browser.newContext()).newPage();
    await client.addInitScript((entry) => {
        if (entry) localStorage.setItem(entry[0], entry[1]);
    }, db);
    await seedToken(client, user);
    await client.goto(path);
    return client;
}

test.describe('Очередь (16)', () => {
    test.beforeEach(async ({ page }) => {
        await seedToken(page, 'underwriter');
        await page.goto('/backoffice');
        await expect(page.getByRole('heading', { name: 'Очередь заявок' })).toBeVisible();
    });

    test('ручное рассмотрение по сроку SLA, статистика и действия', async ({ page }) => {
        await expect(page.getByRole('button', { name: 'Ручное рассмотрение · 5' })).toHaveAttribute('aria-pressed', 'true');
        await expect(stat(page, 'Ждут ручного рассмотрения')).toContainText('5');
        await expect(stat(page, 'В работе у меня')).toContainText('2');
        await expect(stat(page, 'Ждут ответа клиента')).toContainText('1');
        await expect(rows(page)).toHaveCount(6); // заголовок + 5 заявок
        // самый срочный SLA — сверху
        await expect(rows(page).nth(1)).toContainText('A-2026-000147');
        await expect(row(page, 'A-2026-000147')).toContainText('Серая зона скоринга');
        await expect(row(page, 'A-2026-000145')).toContainText('БКИ — ошибка');
        await expect(row(page, 'A-2026-000145').getByRole('button', { name: 'Взять в работу A-2026-000145' })).toBeVisible();
        await expect(row(page, 'A-2026-000142')).toContainText('Соколова М. И.');
        await expect(row(page, 'A-2026-000142').getByRole('link', { name: 'Открыть A-2026-000142' })).toBeVisible();
    });

    test('фильтры по статусу, поиск и исполнитель', async ({ page }) => {
        await page.getByRole('button', { name: /^Ждут клиента/ }).click();
        await expect(page).toHaveURL(/group=waiting/);
        await expect(row(page, 'A-2026-000131')).toHaveCount(1);
        await page.getByRole('button', { name: /^В обработке/ }).click();
        await expect(row(page, 'A-2026-000150')).toContainText('ООО «Бета Логистик»');

        await page.getByRole('button', { name: /^Ручное рассмотрение/ }).click();
        await page.getByLabel('Поиск по ИНН, номеру').fill('2901122334');
        await expect(rows(page)).toHaveCount(2);
        await expect(rows(page).nth(1)).toContainText('ООО «Север Агро»');
    });

    test('«Мои заявки» — только назначенные на меня', async ({ page }) => {
        await page.getByRole('navigation', { name: 'Разделы' }).getByRole('link', { name: 'Мои заявки' }).click();
        await expect(page.getByRole('heading', { name: 'Мои заявки' })).toBeVisible();
        await expect(rows(page)).toHaveCount(3);
        await expect(row(page, 'A-2026-000142')).toHaveCount(1);
        await expect(row(page, 'A-2026-000136')).toHaveCount(1);
    });

    test('«Взять в работу» открывает досье, заявка становится моей', async ({ page }) => {
        await row(page, 'A-2026-000147').getByRole('button', { name: 'Взять в работу A-2026-000147' }).click();
        await expectPath(page, '/backoffice/applications/app_000147');
        await expect(page.getByText(/в работе у вас/)).toBeVisible();
        await expect(page.getByRole('button', { name: 'Взять в работу' })).toHaveCount(0);
    });

    test('заявку успели взять другие — 409', async ({ page }) => {
        await setMockFailures(page, [{ method: 'POST', path: '/assign$', status: 409, code: 'CONFLICT', times: 1 }]);
        await row(page, 'A-2026-000145').getByRole('button', { name: 'Взять в работу A-2026-000145' }).click();
        await expect(page.getByText('Не получилось взять в работу')).toBeVisible();
        await expectPath(page, '/backoffice');
    });
});

test.describe('Досье (17)', () => {
    test.beforeEach(async ({ page }) => {
        await seedToken(page, 'underwriter');
    });

    test('обзор: почему на ручном, расхождения, скоринг, источники', async ({ page }) => {
        await openDossier(page);
        await expect(page.getByRole('heading', { level: 1 })).toHaveText('ИП Смирнов К. А.');
        await expect(page.getByText('Грейд C')).toBeVisible();
        await expect(page.getByText('Серая зона скоринга: 412 баллов (порог автоодобрения — 450)')).toBeVisible();
        const findings = page.getByRole('region', { name: 'Расхождения «заявлено ↔ найдено»' });
        await expect(findings.getByRole('row').filter({ hasText: 'Выручка за год' })).toContainText(/12\s900\s000/);
        await expect(findings).toContainText('Скоринг посчитан на консервативном значении выручки');
        const score = page.getByRole('region', { name: 'Скоринг · scorecard-sme-v1' });
        await expect(score).toContainText('412');
        await expect(score.getByRole('listitem')).toHaveCount(7);
        // полоски скоринга скрыты от скринридера — баллы есть текстом
        await expect(score.getByRole('progressbar')).toHaveCount(0);
        const engine = page.getByRole('region', { name: 'Предложения движка' });
        await expect(engine.getByRole('listitem')).toHaveCount(2);
        await expect(engine).toContainText('Обороты по счёту в Альфа-Банке — от 30% выручки');
    });

    test('упавший источник перезапрашивается и становится «получено»', async ({ page }) => {
        await openDossier(page);
        const sources = page.getByRole('region', { name: 'Источники данных' });
        await sources.getByRole('button', { name: 'Перезапросить КАД' }).click();
        const kad = sources.getByRole('listitem').filter({ hasText: 'КАД' });
        await expect(kad).toContainText('собираем…');
        await expect(kad).toContainText(/сегодня в/, { timeout: 10_000 });
    });

    test('вкладки: источники, скоринг, история, данные компании', async ({ page }) => {
        await openDossier(page);
        await page.getByRole('tab', { name: /^Источники/ }).click();
        await expect(page.getByRole('tabpanel')).toContainText('ГИР БО');
        await page.getByRole('tab', { name: 'Скоринг' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('Обороты в Альфа-Банке');
        await page.getByRole('tab', { name: 'История' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('Заявка отправлена');
        await page.getByRole('tab', { name: 'Данные компании' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('ИНН 771234567890');
        await expect(page.getByRole('tabpanel')).toContainText('Бухгалтерская отчётность');
    });

    test('чужая заявка: ни взять, ни решить', async ({ page }) => {
        await openDossier(page, 'app_000139');
        await expect(page.getByText('Заявка в работе у Орлов Д. А.')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Взять в работу' })).toHaveCount(0);
        await expect(page.getByRole('button', { name: 'Одобрить', exact: true })).toHaveCount(0);
        await expect(page.getByRole('button', { name: 'Отказать', exact: true })).toHaveCount(0);
    });
});

test.describe('Решение (18)', () => {
    test.beforeEach(async ({ page }) => {
        await seedToken(page, 'underwriter');
        await openDossier(page);
    });

    test('одобрить: заявка берётся в работу, клиент видит предложения без внутреннего комментария', async ({ page, browser }) => {
        await page.getByRole('button', { name: 'Одобрить', exact: true }).click();
        await expect(panel(page)).toContainText('Предложения клиенту');
        await expect(panel(page)).toContainText('Предложение 1 · Альтернатива');
        await expect(panel(page)).toContainText('Предложение 2 · Как запрошено');
        await expect(panel(page)).toContainText('Условия движка: Обороты по счёту в Альфа-Банке — от 30% выручки');
        await expect(page.getByText(/в работе у вас/)).toBeVisible();
        await panel(page).getByLabel('Внутренний комментарий (клиент не увидит)').fill('Договор с «Метро» на 6 млн подтверждён');
        await panel(page).getByRole('button', { name: 'Одобрить и отправить клиенту' }).click();
        await expect(panel(page)).toHaveCount(0);
        await expect(page.getByText('Предложения у клиента')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Одобрить', exact: true })).toHaveCount(0);
        const issued = page.getByRole('region', { name: 'Предложения клиенту' });
        await expect(issued.getByRole('listitem')).toHaveCount(2);
        await expect(issued).toContainText('Ждёт клиента');
        await page.getByRole('tab', { name: 'История' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('Внутренний комментарий: Договор с «Метро» на 6 млн подтверждён');

        const client = await asClient(page, browser, 'soleProprietor', '/applications/app_000147');
        await expect(client.getByRole('heading', { level: 2, name: 'Банк готов выдать кредит' })).toBeVisible();
        await expect(client.getByRole('region', { name: 'Предложения банка' }).getByRole('article')).toHaveCount(2);
        await expect(client.getByText('Обороты по счёту в Альфа-Банке — от 30% выручки')).toBeVisible();
        await client.getByRole('tab', { name: 'История' }).click();
        await expect(client.getByText(/Метро/)).toHaveCount(0);
    });

    test('поправка к скорингу: баллы проверяются до отправки', async ({ page }) => {
        await page.getByRole('button', { name: 'Одобрить', exact: true }).click();
        await openSelect(page, 'Фактор');
        await page.getByRole('option', { name: 'Крупный контракт' }).click();
        await panel(page).getByRole('textbox', { name: 'Баллы' }).fill('-');
        await panel(page).getByRole('button', { name: 'Одобрить и отправить клиенту' }).click();
        await expect(panel(page)).toContainText('Укажите баллы целым числом');
        await panel(page).getByRole('textbox', { name: 'Баллы' }).fill('+30');
        await panel(page).getByRole('button', { name: 'Одобрить и отправить клиенту' }).click();
        await expect(panel(page)).toHaveCount(0);
        await page.getByRole('tab', { name: 'История' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('поправка CONFIRMED_LARGE_CONTRACT +30');
    });

    test('запросить данные: вопрос по расхождению → клиент видит вопрос', async ({ page, browser }) => {
        await page.getByRole('button', { name: 'Запросить данные' }).click();
        await expect(panel(page).getByRole('textbox', { name: 'Вопрос' })).toHaveValue('Уточните выручку за 2025 год');
        await panel(page).getByRole('button', { name: 'Отправить вопросы клиенту' }).click();
        await expect(page.getByText('Ждём клиента')).toBeVisible();
        await page.getByRole('tab', { name: 'Уточнения' }).click();
        await expect(page.getByRole('tabpanel')).toContainText('Уточните выручку за 2025 год');
        await expect(page.getByRole('tabpanel')).toContainText('Ждём ответа');

        const client = await asClient(page, browser, 'soleProprietor', '/applications/app_000147');
        await expect(client.getByText('Уточните выручку за 2025 год')).toBeVisible();
    });

    test('вопрос «только документ» без документа не отправляется', async ({ page }) => {
        await page.getByRole('button', { name: 'Запросить данные' }).click();
        await openSelect(page, 'Обновит поле');
        await page.getByRole('option', { name: 'Не связано с анкетой' }).click();
        await openSelect(page, 'Ответ');
        await page.getByRole('option', { name: 'Только документ' }).click();
        await openSelect(page, 'Документ');
        await page.getByRole('option', { name: 'Документ не нужен' }).click();
        await panel(page).getByRole('button', { name: 'Отправить вопросы клиенту' }).click();
        await expect(panel(page)).toContainText('Для ответа документом выберите документ');
        await expect(page.getByText('Ждём клиента')).toHaveCount(0);
    });

    test('отказать: без причины — подсказка; с причиной — клиент видит причину', async ({ page, browser }) => {
        await page.getByRole('button', { name: 'Отказать', exact: true }).click();
        await rejectSubmit(page).click();
        await expect(panel(page)).toContainText('Выберите причину или опишите свою');
        await panel(page).getByRole('group', { name: 'Что увидит клиент' }).getByText('Выручка не подтверждается отчётностью').click();
        await rejectSubmit(page).click();
        await expect(panel(page)).toHaveCount(0);
        await expect(page.getByRole('heading', { level: 1 }).locator('xpath=..')).toContainText('Отказ');

        const client = await asClient(page, browser, 'soleProprietor', '/applications/app_000147');
        await expect(client.getByText('Выручка не подтверждается отчётностью')).toBeVisible();
    });

    test('412 — досье перечитано, черновик можно перезаполнить', async ({ page }) => {
        await setMockFailures(page, [{ method: 'POST', path: '/decision$', status: 412, code: 'VERSION_CONFLICT', times: 1 }]);
        await page.getByRole('button', { name: 'Одобрить', exact: true }).click();
        await panel(page).getByRole('button', { name: 'Одобрить и отправить клиенту' }).click();
        await expect(panel(page)).toContainText('Заявку изменили — досье обновлено');
        await panel(page).getByRole('button', { name: 'Заполнить по новым данным' }).click();
        await expect(panel(page)).not.toContainText('Заявку изменили');
        await panel(page).getByRole('button', { name: 'Одобрить и отправить клиенту' }).click();
        await expect(page.getByText('Предложения у клиента')).toBeVisible();
    });

    test('409 на решение — ошибка видна, панель остаётся', async ({ page }) => {
        await setMockFailures(page, [{ method: 'POST', path: '/decision$', status: 409, code: 'INVALID_TRANSITION', times: 1 }]);
        await page.getByRole('button', { name: 'Отказать', exact: true }).click();
        await panel(page).getByRole('group', { name: 'Что увидит клиент' }).getByText('Негативная кредитная история').click();
        await rejectSubmit(page).click();
        await expect(panel(page)).toContainText('Не получилось сохранить решение');
    });

    test('Esc закрывает панель', async ({ page }) => {
        await page.getByRole('button', { name: 'Запросить данные' }).click();
        await expect(panel(page)).toBeVisible();
        await page.keyboard.press('Escape');
        await expect(panel(page)).toHaveCount(0);
    });
});

test('клиент в рабочее место банка не попадает', async ({ page }) => {
    await seedToken(page, 'director');
    await page.goto('/backoffice');
    await expectPath(page, '/');
});
