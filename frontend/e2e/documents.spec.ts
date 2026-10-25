import { expect, type Page, test } from '@playwright/test';

import { expectPath, seedToken, setMockFailures } from './helpers';

const table = (page: Page) => page.getByRole('region', { name: 'Документы заявки' }).getByRole('table');
const row = (page: Page, name: string | RegExp) => table(page).getByRole('row').filter({ hasText: name });
/** Строка самого документа, а не его откреплённой подписи «… .sig». */
const docRow = (page: Page, name: string) => table(page).getByRole('row').filter({ hasText: name, hasNotText: `${name}.sig` });
const panel = (page: Page) => page.getByRole('region', { name: 'Проверка подписи' });

async function upload(page: Page, name: string, mimeType = 'application/pdf') {
    await page.getByRole('button', { name: 'Загрузить' }).click();
    const chooser = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Выбрать на компьютере' }).click();
    await (await chooser).setFiles({ name, mimeType, buffer: Buffer.from('%PDF-1.4') });
}

async function confirmDelete(page: Page, name: string) {
    await row(page, name).getByRole('button', { name: `Удалить: ${name}` }).click();
    await page.getByRole('dialog', { name: 'Удалить документ?' }).getByRole('button', { name: 'Удалить' }).click();
}

test.describe('Отказ банка (14)', () => {
    test('причины, что делать и когда подать снова', async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto('/applications/app_000152');
        await expect(page.getByRole('heading', { level: 2, name: 'Сейчас не можем выдать кредит' })).toBeVisible();
        const card = page.getByRole('region', { name: 'Сейчас не можем выдать кредит' });
        await expect(card).toContainText('Решение от 22.10.2026');
        await expect(card.getByRole('listitem').filter({ hasText: 'Высокая текущая долговая нагрузка' })).toContainText(
            'больше половины поступлений',
        );
        await expect(card).toContainText('Погасите часть действующих кредитов');
        await expect(card).toContainText(/Подать новую заявку можно после \d+ \S+ \d{4}/);
        await card.getByRole('link', { name: 'Подобрать другой кредит' }).click();
        await expectPath(page, '/products');
    });
});

test.describe('Документы (15)', () => {
    test('таблица: распознавание, подпись, кто загрузил; отчёт о подписи', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/applications/app_000131?tab=documents');
        await expect(table(page).getByRole('row')).toHaveCount(6); // заголовок + 5 документов
        await expect(row(page, 'Отчётность_2025.pdf')).toContainText(/Выручка 41,6\s*млн/);
        await expect(row(page, 'Отчётность_2025.pdf')).toContainText('Без подписи');
        await expect(row(page, 'Устав_Ромашка.pdf.sig')).toContainText('подпись к «Устав_Ромашка.pdf»');
        await expect(row(page, 'Договор_поставки_Метро.pdf')).toContainText('Не распознан');
        await expect(row(page, 'Договор_поставки_Метро.pdf')).toContainText('Иванова А. С.');

        // по умолчанию — первый подписанный: устав, подпись директора
        await expect(panel(page)).toContainText('Действительна');
        await expect(panel(page)).toContainText('Петров П. П.');
        await expect(panel(page).getByRole('list', { name: 'Что проверили' })).toContainText('Сертификат не отозван');

        await page.getByRole('button', { name: 'Подробнее о подписи: Договор_поставки_Метро.pdf' }).click();
        await expect(panel(page)).toContainText('Есть замечания');
        await expect(panel(page)).toContainText('Нет метки доверенного времени');
        await expect(page.getByRole('button', { name: 'Подробнее о подписи: Договор_поставки_Метро.pdf' })).toHaveAttribute(
            'aria-current',
            'true',
        );
        // у откреплённой .sig своего «Подробнее» нет — это тот же отчёт, что у устава
        await expect(page.getByRole('button', { name: 'Подробнее о подписи: Устав_Ромашка.pdf.sig' })).toHaveCount(0);
    });

    test('скачать документ', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/applications/app_000131?tab=documents');
        const download = page.waitForEvent('download');
        await page.getByRole('button', { name: 'Скачать: Устав_Ромашка.pdf', exact: true }).click();
        expect((await download).suggestedFilename()).toBe('Устав_Ромашка.pdf');
    });

    test('при уточнении: загрузить и удалить', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto('/applications/app_000131?tab=documents');
        await upload(page, 'Устав_новый.pdf');
        await expect(row(page, 'Устав_новый.pdf')).toHaveCount(1);
        await confirmDelete(page, 'Устав_новый.pdf');
        await expect(row(page, 'Устав_новый.pdf')).toHaveCount(0);
    });

    test('на рассмотрении: догрузить можно, удалить — нет', async ({ page }) => {
        await seedToken(page, 'soleProprietor');
        await page.goto('/applications/app_000147?tab=documents');
        await expect(panel(page)).toContainText('У документов этой заявки пока нет электронных подписей');
        await upload(page, 'Выписка.pdf');
        await expect(row(page, 'Выписка.pdf')).toHaveCount(1);
        await expect(row(page, 'Выписка.pdf').getByRole('button', { name: /Удалить/ })).toHaveCount(0);
    });

    test('подпись к уже загруженному документу: бухгалтер — не хватает подписи руководителя', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto('/applications/app_000131?tab=documents');
        // подпись привязывается к уже загруженному — ждём, пока список документов придёт
        await expect(docRow(page, 'Отчётность_2025.pdf')).toHaveCount(1);
        await upload(page, 'Отчётность_2025.pdf.sig', 'application/pkcs7-signature');
        await expect(row(page, 'Отчётность_2025.pdf.sig')).toContainText('подпись к «Отчётность_2025.pdf»');
        const statements = docRow(page, 'Отчётность_2025.pdf');
        await expect(statements).toContainText('Не хватает подписей', { timeout: 10_000 });
        await statements.getByRole('button', { name: 'Подробнее о подписи: Отчётность_2025.pdf' }).click();
        await expect(panel(page)).toContainText('Подпись руководителя: нет');
        await expect(panel(page)).toContainText('Нет подписи руководителя ООО «Ромашка»');
    });

    test('удаление подписанного документа убирает и подпись; выбор откатывается', async ({ page }) => {
        await seedToken(page, 'accountant');
        await page.goto('/applications/app_000131?tab=documents');
        await page.getByRole('button', { name: 'Подробнее о подписи: Договор_поставки_Метро.pdf' }).click();
        await confirmDelete(page, 'Договор_поставки_Метро.pdf');
        await expect(row(page, 'Договор_поставки_Метро.pdf')).toHaveCount(0);
        // панель вернулась к первому подписанному
        await expect(panel(page)).toContainText('Устав_Ромашка.pdf');

        await docRow(page, 'Устав_Ромашка.pdf').getByRole('button', { name: 'Удалить: Устав_Ромашка.pdf' }).click();
        await expect(page.getByRole('dialog', { name: 'Удалить документ?' })).toContainText('вместе с подписью к нему');
        await page.getByRole('dialog', { name: 'Удалить документ?' }).getByRole('button', { name: 'Удалить' }).click();
        await expect(row(page, 'Устав_Ромашка.pdf')).toHaveCount(0);
        await expect(panel(page)).toContainText('У документов этой заявки пока нет электронных подписей');
    });

    test('ошибка скачивания видна', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/applications/app_000131?tab=documents');
        await setMockFailures(page, [{ method: 'GET', path: '/content$', status: 500, times: 1 }]);
        await page.getByRole('button', { name: 'Скачать: Устав_Ромашка.pdf', exact: true }).click();
        await expect(page.getByText('Не удалось скачать: Устав_Ромашка.pdf')).toBeVisible();
    });
});

test.describe('Отказ банка — без объяснения', () => {
    test('решения ещё нет (404) — не ошибка, а «готовим»', async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto('/');
        await setMockFailures(page, [{ method: 'GET', path: '/decision$', status: 404, code: 'NOT_FOUND' }]);
        await page.goto('/applications/app_000152');
        await expect(page.getByText('Готовим объяснение решения')).toBeVisible();
        await expect(page.getByText('Не удалось загрузить объяснение решения')).toHaveCount(0);
    });
});
