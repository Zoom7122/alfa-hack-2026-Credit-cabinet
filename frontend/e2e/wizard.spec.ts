import { expect, type Page, test } from '@playwright/test';

import { clearMockFailures, expectPath, seedToken, setMockFailures } from './helpers';

const DRAFT = '/applications/app_000140/edit';
const NEW_CREDIT_LINE = '/applications/new?product=CREDIT_LINE&amount=5000000&term=24&purpose=WORKING_CAPITAL';

const step = (page: Page, title: string) => page.getByRole('navigation', { name: 'Шаги анкеты' }).getByText(title, { exact: true });
// то, что услышит скринридер: живой регион в шапке объявляет только итог сохранения
const headerStatus = (page: Page) => page.getByRole('banner').getByRole('status');
const saved = (page: Page) => expect(headerStatus(page)).toHaveText(/^✓ Черновик сохранён/);
const saveFailed = (page: Page) => expect(headerStatus(page)).toHaveText('Не удалось сохранить черновик');
const files = (page: Page) => page.getByRole('list', { name: 'Загруженные файлы' }).getByRole('listitem');
const pdf = (name: string) => ({ name, mimeType: 'application/pdf', buffer: Buffer.from('%PDF-1.4 test') });

async function chooseFiles(page: Page, list: ReturnType<typeof pdf>[]) {
    const chooser = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Выбрать на компьютере' }).click();
    await (await chooser).setFiles(list);
}

async function fillExpenses(page: Page, value = '900000') {
    await page.getByLabel('Регулярные расходы в месяц').pressSequentially(value);
}

test.describe('Анкета — новая заявка', () => {
    test('директор: компания уже привязана → черновик с предзаполнением', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto(NEW_CREDIT_LINE);
        await expect(page.getByText('Нашли компанию в реестре')).toBeVisible();
        await expect(page.getByLabel('Название')).toHaveValue('ООО «Ромашка»');
        await expect(page.getByRole('radio', { name: 'Директор', exact: true })).toBeChecked();

        await page.getByRole('button', { name: 'Продолжить' }).click();
        await expect(page).toHaveURL(/\/applications\/app_\d+\/edit\/terms$/);
        await expect(page.getByText(/^Заявка A-2026-\d{6}$/)).toBeVisible();
        await expect(page.getByLabel('Сумма кредита')).toHaveValue(/5\s000\s000/);

        // финансы и долги подставились из прошлой заявки компании
        await step(page, 'Финансы').click();
        // берём последнюю отправленную заявку компании — A-2026-000123 от 20.10.2026
        await expect(page.getByText('Подставили данные с прошлой заявки от 20.10.2026')).toBeVisible();
        await expect(page.getByLabel('Выручка за 2025 год')).toHaveValue(/48\s200\s000/);
    });

    test('новая компания по ИНН: нужна роль, после — привязка и черновик', async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto('/applications/new?product=WORKING_CAPITAL&amount=2000000&term=12');
        await page.getByLabel('ИНН компании или ИП').fill('7707083893');
        await expect(page.getByText('Нашли компанию в реестре')).toBeVisible();
        await expect(page.getByLabel('Название')).toHaveValue('ООО «Северный ветер»');
        await expect(page.getByRole('button', { name: 'Продолжить' })).toBeDisabled();
        await expect(page.getByText('Укажите вашу роль в компании')).toBeVisible();

        await page.getByText('Бухгалтер', { exact: true }).click();
        await page.getByRole('button', { name: 'Продолжить' }).click();
        await expect(page).toHaveURL(/\/edit\/terms$/);
        // новой компании нечего предзаполнять
        await step(page, 'Финансы').click();
        await expect(page.getByText(/Подставили данные/)).toHaveCount(0);
    });

    test('реестр не ответил (504) — можно продолжить без реквизитов', async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto(NEW_CREDIT_LINE);
        await page.getByLabel('ИНН компании или ИП').fill('7700000000');
        await expect(page.getByText('Реестр сейчас не отвечает')).toBeVisible({ timeout: 10_000 });
        await page.getByText('Собственник', { exact: true }).click();
        await page.getByRole('button', { name: 'Продолжить' }).click();
        await expect(page).toHaveURL(/\/edit\/terms$/);
    });

    test('ИНН не найден и ликвидируемая компания — дальше нельзя', async ({ page }) => {
        await seedToken(page, 'vector');
        await page.goto(NEW_CREDIT_LINE);
        const inn = page.getByLabel('ИНН компании или ИП');
        await inn.fill('7701111111');
        await expect(page.getByText('Не нашли компанию с таким ИНН')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Продолжить' })).toBeDisabled();

        await inn.fill('7709876543');
        await expect(page.getByText('Компания ликвидируется или реорганизуется')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Продолжить' })).toBeDisabled();
    });

    test('вторая активная заявка на тот же продукт — 409 со ссылкой на неё', async ({ page }) => {
        await seedToken(page, 'director');
        await page.goto('/applications/new?product=WORKING_CAPITAL&amount=2000000&term=12');
        await expect(page.getByText('Нашли компанию в реестре')).toBeVisible();
        await page.getByRole('button', { name: 'Продолжить' }).click();
        await expect(page.getByText('У компании уже есть заявка на этот продукт')).toBeVisible();
        await page.getByRole('link', { name: 'Перейти к заявке A-2026-000123' }).click();
        await expectPath(page, '/applications/app_000123');
    });
});

test.describe('Анкета — черновик', () => {
    test.beforeEach(async ({ page }) => {
        await seedToken(page, 'director');
    });

    test('открывается на первом незаполненном шаге и подсвечивает пропуск', async ({ page }) => {
        await page.goto(DRAFT);
        await expectPath(page, `${DRAFT}/finance`);
        await expect(page.getByText('Нужно для расчёта платежа, который комфортен бизнесу')).toBeVisible();
        await expect(page.getByText('Изменения сохраняются автоматически')).toBeVisible();
    });

    test('автосохранение переживает перезагрузку, шаг отмечается заполненным', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await fillExpenses(page);
        await saved(page);
        await page.reload();
        await expect(page.getByLabel('Регулярные расходы в месяц')).toHaveValue(/900\s000/);
        await page.getByRole('link', { name: /Кредиты и долги/ }).click();
        await expect(page.getByRole('link', { name: 'Финансы, заполнено' })).toBeVisible();
    });

    test('«Сохранить и выйти» дожидается сохранения', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await fillExpenses(page, '700000');
        // сразу, не дожидаясь автосохранения
        await page.getByRole('button', { name: 'Сохранить и выйти' }).click();
        await expectPath(page, '/');
        await page.goto(`${DRAFT}/finance`);
        await expect(page.getByLabel('Регулярные расходы в месяц')).toHaveValue(/700\s000/);
    });

    test('сбой сохранения: выйти не даёт, «Повторить» досохраняет', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await setMockFailures(page, [{ method: 'PATCH', path: '/applications/', status: 500 }]);
        await fillExpenses(page);
        await saveFailed(page);
        await page.getByRole('button', { name: 'Сохранить и выйти' }).click();
        // выход повторяет сохранение — дожидаемся, что оно снова не удалось, и мы остались в анкете
        await expect(page.getByRole('button', { name: 'Повторить' })).toBeVisible();
        await expectPath(page, `${DRAFT}/finance`);

        await clearMockFailures(page);
        await page.getByRole('button', { name: 'Повторить' }).click();
        await saved(page);
        await page.reload();
        await expect(page.getByLabel('Регулярные расходы в месяц')).toHaveValue(/900\s000/);
    });

    test('несохранённое при сбое докидывается после перезагрузки', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await setMockFailures(page, [{ method: 'PATCH', path: '/applications/', status: 503 }]);
        await fillExpenses(page, '650000');
        await saveFailed(page);
        await clearMockFailures(page);
        await page.reload();
        await saved(page);
        await expect(page.getByLabel('Регулярные расходы в месяц')).toHaveValue(/650\s000/);
    });

    test('422 на поле: его не сохранили, остальное сохраняется дальше', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await setMockFailures(page, [
            { method: 'PATCH', path: '/applications/', status: 422, field: 'financials.avg_monthly_expenses', times: 1 },
        ]);
        await fillExpenses(page);
        await expect(page.getByText('Эти данные не сохранились')).toBeVisible();
        await expect(page.getByText(/регулярные расходы: Отклонено сервером/)).toBeVisible();
        await page.getByLabel('Сотрудников').fill('30');
        await saved(page);
    });

    test('412: правки ложатся поверх свежей версии с предупреждением', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await setMockFailures(page, [{ method: 'PATCH', path: '/applications/', status: 412, code: 'VERSION_CONFLICT', times: 1 }]);
        await fillExpenses(page);
        await saved(page);
        await expect(page.getByText('Заявку меняли в другой вкладке')).toBeVisible();
    });

    test('сумма, срок и цель сохраняются', async ({ page }) => {
        await page.goto(`${DRAFT}/terms`);
        const term = page.getByLabel('Срок');
        await term.fill('');
        await term.pressSequentially('48');
        await term.blur();
        await page.getByLabel('Расскажите подробнее').fill('Витрины и кассы');
        await saved(page);
        await page.reload();
        await expect(page.getByLabel('Срок')).toHaveValue('48');
        await expect(page.getByLabel('Расскажите подробнее')).toHaveValue('Витрины и кассы');
    });

    test('«Нет долгов» убирает список и сохраняется', async ({ page }) => {
        await page.goto(`${DRAFT}/debts`);
        await expect(page.getByText('Кредит · Сбербанк')).toBeVisible();
        await page.getByText('У компании нет действующих кредитов, лизинга и гарантий').click();
        await expect(page.getByText('Кредит · Сбербанк')).toHaveCount(0);
        await saved(page);
        await page.reload();
        await expect(page.getByRole('checkbox', { name: /нет действующих кредитов/ })).toBeChecked();
    });

    test('поручитель с неверным ИНН не уходит в черновик, но не теряется', async ({ page }) => {
        await page.goto(`${DRAFT}/collateral`);
        await page.getByRole('button', { name: 'Добавить поручителя' }).click();
        await page.getByLabel('ФИО').last().fill('Сидоров Иван Петрович');
        await page.getByLabel('ИНН').last().fill('770112345678');
        await expect(page.getByText('Проверьте ИНН — не сходится контрольная цифра или длина')).toBeVisible();
        // неполная запись живёт во вкладке — после перехода по шагам на месте
        await step(page, 'Документы').click();
        await step(page, 'Залог и поручители').click();
        await expect(page.getByLabel('ФИО').last()).toHaveValue('Сидоров Иван Петрович');
        await step(page, 'Проверка и отправка').click();
        await expect(page.getByText('Петров П. П., учредитель')).toBeVisible();
        await expect(page.getByText(/Сидоров/)).toHaveCount(0);
    });

    test('загрузка нескольких файлов, распознавание и удаление', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await chooseFiles(page, [pdf('Баланс.pdf'), pdf('ОФР.pdf'), pdf('Отчётность_2025.pdf')]);
        await expect(files(page)).toHaveCount(3);
        await expect(files(page).filter({ hasText: 'загружаем…' })).toHaveCount(0);
        const item = files(page).filter({ hasText: 'Отчётность_2025.pdf' });
        await expect(item).toContainText('распознали', { timeout: 10_000 });
        await item.getByRole('button', { name: /удалить/i }).click();
        await expect(files(page)).toHaveCount(2);
    });

    test('документ выбранного типа отмечается в списке «Что пригодится»', async ({ page }) => {
        await page.goto(`${DRAFT}/documents`);
        const checklist = page.getByRole('list').filter({ hasText: 'Выписка по счёту из другого банка' });
        await expect(checklist.getByRole('listitem').filter({ hasText: 'Выписка по счёту' })).toContainText('не загружено');
        await page.getByText('Бухгалтерская отчётность', { exact: true }).last().click();
        await page.getByRole('option', { name: 'Выписка по счёту из другого банка' }).click();
        await chooseFiles(page, [pdf('Выписка.pdf')]);
        await expect(files(page).filter({ hasText: 'Выписка.pdf' })).toHaveCount(1);
        await expect(checklist.getByRole('listitem').filter({ hasText: 'Выписка по счёту' })).toContainText('загружено');
        await expect(checklist.getByRole('listitem').filter({ hasText: 'Выписка по счёту' })).not.toContainText('не загружено');
    });

    test('отправка: без согласий нельзя, после — уходит на рассмотрение', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await fillExpenses(page);
        await saved(page);
        await page.getByRole('link', { name: /Проверка и отправка/ }).click();

        const submit = page.getByRole('button', { name: 'Отправить заявку' });
        await expect(page.getByText('Не хватает данных')).toHaveCount(0);
        await expect(submit).toBeDisabled();
        await expect(page.getByText('Дайте оба согласия, чтобы отправить')).toBeVisible();
        await expect(page.getByText('Можно отправить без отчётности')).toBeVisible();

        await page.getByText('Согласен на обработку персональных данных').click();
        await page.getByText('Согласен на запрос кредитной истории в бюро кредитных историй').click();
        await expect(submit).toBeEnabled();
        await submit.click();
        await expectPath(page, '/applications/app_000140');

        // отправленную заявку больше не редактируют
        await page.goto(`${DRAFT}/review`);
        await expectPath(page, '/applications/app_000140');
        await page.goto('/');
        await expect(page.getByRole('link', { name: /Кредит на оборудование/ })).toContainText('Рассматриваем');
    });

    test('сервер не видит согласий (CONSENT_REQUIRED) — снимаем галочку и просим ещё раз', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await fillExpenses(page);
        await saved(page);
        await step(page, 'Проверка и отправка').click();
        await page.getByText('Согласен на обработку персональных данных').click();
        await page.getByText('Согласен на запрос кредитной истории в бюро кредитных историй').click();
        await saved(page);
        await setMockFailures(page, [
            { method: 'POST', path: '/submit$', status: 422, code: 'CONSENT_REQUIRED', field: 'consents.credit_bureau', times: 1 },
        ]);
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        await expect(page.getByText('Банк не получил согласия')).toBeVisible();
        const bureau = page.getByRole('checkbox', { name: /бюро кредитных историй/ });
        await expect(bureau).not.toBeChecked();
        await page.getByText('Согласен на запрос кредитной истории в бюро кредитных историй').click();
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        await expectPath(page, '/applications/app_000140');
    });

    test('сервер нашёл пропуск (DRAFT_INCOMPLETE) — ссылка на шаг', async ({ page }) => {
        await page.goto(`${DRAFT}/finance`);
        await fillExpenses(page);
        await saved(page);
        await step(page, 'Проверка и отправка').click();
        await page.getByText('Согласен на обработку персональных данных').click();
        await page.getByText('Согласен на запрос кредитной истории в бюро кредитных историй').click();
        await saved(page);
        await setMockFailures(page, [{ method: 'POST', path: '/submit$', status: 422, code: 'DRAFT_INCOMPLETE', field: 'collateral', times: 1 }]);
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        await expect(page.getByText('Не хватает данных')).toBeVisible();
        await page.getByRole('link', { name: 'залог', exact: true }).click();
        await expectPath(page, `${DRAFT}/collateral`);
    });

    test('недостающее поле видно на проверке со ссылкой на шаг', async ({ page }) => {
        await page.goto(`${DRAFT}/review`);
        await expect(page.getByText('Не хватает данных')).toBeVisible();
        await page.getByRole('link', { name: 'регулярные расходы' }).click();
        await expectPath(page, `${DRAFT}/finance`);
    });
});
