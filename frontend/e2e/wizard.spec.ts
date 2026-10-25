import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { expect, type Page, test } from '@playwright/test';

import { expectPath, seedToken, setMockFailures } from './helpers';

/* Анкета на API data-collector (в тестах — его MSW-моки, src/mocks/collector). */

const NEW = '/applications/new?product=WORKING_CAPITAL&amount=5000000&term=24&purpose=WORKING_CAPITAL';
const EXAMPLES = resolve(import.meta.dirname, '../../data-collector/examples/files');
const balance = () => ({ name: 'balance.pdf', mimeType: 'application/pdf', buffer: readFileSync(`${EXAMPLES}/balance.pdf`) });
const balanceSig = () => ({ name: 'balance.pdf.sig', mimeType: 'application/pkcs7-signature', buffer: readFileSync(`${EXAMPLES}/balance.pdf.sig`) });

const step = (page: Page, title: string) => page.getByRole('navigation', { name: 'Шаги анкеты' }).getByText(title, { exact: true });
const files = (page: Page) => page.getByRole('list', { name: 'Файлы заявки' }).getByRole('listitem');
const next = (page: Page) => page.getByRole('button', { name: 'Продолжить' }).click();
const editPath = (page: Page) => new URL(page.url()).pathname.replace(/\/edit\/[a-z]+$/, '');

async function openSelect(page: Page, label: string, option: string) {
    const caption = page.getByText(label, { exact: true }).first();
    await caption.scrollIntoViewIfNeeded();
    // подпись Select кита перекрыта полем: кликаем по месту подписи, клик получает само поле
    await caption.click({ force: true });
    await page.getByRole('option', { name: option, exact: true }).click();
}

async function chooseFiles(page: Page, list: ReturnType<typeof balance>[]) {
    const chooser = page.waitForEvent('filechooser');
    await page.getByRole('button', { name: 'Выбрать на компьютере' }).click();
    await (await chooser).setFiles(list);
}

// input радио кита скрыт визуально — выбираем кликом по подписи
/** Роль в своей компании задана профилем и заблокирована — тогда только проверяем её. */
async function chooseRole(page: Page, role: string) {
    const radio = page.getByRole('radio', { name: role, exact: true });
    if (await radio.isDisabled()) await expect(radio).toBeChecked();
    else await page.getByText(role, { exact: true }).click();
}

/** Шаг «Компания» для сценария A mock-api. */
async function register(page: Page, inn = '7799000012', role = 'Генеральный директор') {
    await page.goto(NEW);
    await page.getByLabel('ИНН компании или ИП').fill(inn);
    await expect(page.getByText('Нашли компанию в реестре')).toBeVisible();
    await chooseRole(page, role);
    await next(page);
    await expect(page).toHaveURL(/\/applications\/app_[A-Z0-9]+\/edit\/terms$/);
}

async function fillBorrower(page: Page) {
    await page.getByLabel('Возраст, лет').fill('42');
    await openSelect(page, 'Семейное положение', 'В браке');
    await page.getByLabel('Дети и иждивенцы').fill('2');
    await page.getByLabel('Фамилия').fill('Ковалёв');
    await page.getByLabel('Имя', { exact: true }).fill('Игорь');
    await page.getByLabel('Отчество').fill('Сергеевич');
    await page.getByLabel('ИНН физлица').fill('779900010139');
    await page.getByLabel('Дата рождения').fill('19.07.1984');
}

/** Через все шаги до «Согласия и отправка». */
async function toReview(page: Page) {
    await next(page); // сумма и цель пришли из калькулятора
    await fillBorrower(page);
    await next(page);
    await next(page); // счета и площадки
    await next(page); // залог
    await next(page); // документы
    await expect(page.getByRole('heading', { level: 1, name: 'Согласия и отправка' })).toBeVisible();
}

type CollectorAppView = { status: string; documents: { file_name: string; type: string; signed_document_id?: string }[] };
const collectorApp = (page: Page, id: string) =>
    page.evaluate(async (appId) => (await (await fetch(`/collector/api/v1/applications/${appId}`)).json()) as CollectorAppView, id);

test.beforeEach(async ({ page }) => {
    // представитель компании сценария A: «Северный ветер» уже в его профиле директором — так же и у живого сервиса,
    // сколько бы раз ни гоняли тесты (у других пользователей профиль копит компании из прошлых прогонов)
    await seedToken(page, 'northDirector');
});

test.describe('Анкета — компания', () => {
    test('сценарий A: реквизиты из подсказки, роль, черновик в data-collector', async ({ page }) => {
        await seedToken(page, 'northAccountant');
        await page.goto(NEW);
        await page.getByLabel('ИНН компании или ИП').fill('7799000012');
        await expect(page.getByText('Нашли компанию в реестре')).toBeVisible();
        await expect(page.getByLabel('Название')).toHaveValue('ООО «Северный ветер»');
        await expect(page.getByLabel('Руководитель')).toHaveValue('Ковалёв Игорь Сергеевич');
        await expect(page.getByLabel('ФИО')).toHaveValue('Литвинова Марина Олеговна');
        // роль в своей компании — из профиля, сменить нельзя
        await expect(page.getByRole('radio', { name: 'Генеральный директор' })).toBeDisabled();
        await chooseRole(page, 'Бухгалтер');
        await next(page);
        await expect(page).toHaveURL(/\/edit\/terms$/);
        await step(page, 'Компания').click();
        // для уже известной компании data-collector сразу отдаёт название из ЕГРЮЛ
        await expect(page.getByLabel('Название')).toHaveValue(/Северный ветер|СЕВЕРНЫЙ ВЕТЕР/);
        await expect(page.getByRole('radio', { name: 'Бухгалтер' })).toBeChecked();
    });

    test('ИНН вне подсказок — можно продолжить, реквизиты возьмём из ЕГРЮЛ', async ({ page }) => {
        await page.goto(NEW);
        // каждый раз новый ИНН: у живого сервиса компания из прошлого прогона уже была бы в профиле
        await page.getByLabel('ИНН компании или ИП').fill(`77${String(Date.now()).slice(-8)}`);
        await expect(page.getByText('В подсказках реестра такой компании нет')).toBeVisible();
        await chooseRole(page, 'Генеральный директор');
        await next(page);
        await expect(page).toHaveURL(/\/edit\/terms$/);
    });

    test('ликвидируемая компания — дальше нельзя', async ({ page }) => {
        await page.goto(NEW);
        await page.getByLabel('ИНН компании или ИП').fill('7709876543');
        await expect(page.getByText('Компания ликвидируется или реорганизуется')).toBeVisible();
        await expect(page.getByRole('button', { name: 'Продолжить' })).toBeDisabled();
    });

    test('422 от data-collector — ошибка у поля', async ({ page }) => {
        await page.goto(NEW);
        await setMockFailures(page, [{ method: 'POST', path: '/collector/api/v1/applicants$', status: 422, field: 'company.inn', times: 1 }]);
        await page.getByLabel('ИНН компании или ИП').fill('7799000012');
        await chooseRole(page, 'Генеральный директор');
        await next(page);
        await expect(page.getByText('Отклонено сервером')).toBeVisible();
        await expectPath(page, '/applications/new');
    });
});

test.describe('Анкета — шаги', () => {
    test.beforeEach(async ({ page }) => {
        await register(page);
    });

    test('обязательные поля заёмщика проверяются перед переходом', async ({ page }) => {
        await next(page);
        await next(page);
        await expect(page.getByText('Укажите возраст')).toBeVisible();
        await expect(page.getByText('ИНН физлица — 12 цифр')).toBeVisible();
        await expect(page).toHaveURL(/\/edit\/borrower$/);
        await page.getByText('Не указывать руководителя', { exact: true }).click();
        await expect(page.getByText('Без руководителя проверим только компанию')).toBeVisible();
        await page.getByLabel('Возраст, лет').fill('17');
        await openSelect(page, 'Семейное положение', 'Не в браке');
        await page.getByLabel('Дети и иждивенцы').fill('0');
        await next(page);
        await expect(page.getByText('Возраст — от 18 до 100 лет')).toBeVisible();
        await page.getByLabel('Возраст, лет').fill('30');
        await next(page);
        await expect(page).toHaveURL(/\/edit\/sources$/);
    });

    test('БИК, VIN и кадастровый номер — проверка формата', async ({ page }) => {
        await next(page);
        await fillBorrower(page);
        await next(page);
        await page.getByRole('button', { name: 'Добавить банк' }).click();
        await page.getByRole('textbox', { name: 'БИК банка 1' }).fill('1234');
        await next(page);
        await expect(page.getByText('БИК — 9 цифр')).toBeVisible();
        await page.getByRole('textbox', { name: 'БИК банка 1' }).fill('044525225');
        await next(page);
        await page.getByRole('button', { name: 'Добавить транспорт' }).click();
        await page.getByRole('textbox', { name: 'VIN 1' }).fill('XTC652005N149821O');
        await page.getByRole('button', { name: 'Добавить объект' }).click();
        await page.getByRole('textbox', { name: 'Кадастровый номер 1' }).fill('50:21:0110203:845');
        await next(page);
        await expect(page.getByText('VIN — 17 символов, без I, O, Q')).toBeVisible();
        await page.getByRole('textbox', { name: 'VIN 1' }).fill('XTC652005N1498213');
        await next(page);
        await expect(page).toHaveURL(/\/edit\/documents$/);
    });

    test('черновик и файлы переживают перезагрузку', async ({ page }) => {
        await next(page);
        await fillBorrower(page);
        await page.reload();
        await expect(page.getByLabel('Возраст, лет')).toHaveValue('42');
        await expect(page.getByLabel('Дата рождения')).toHaveValue('19.07.1984');
        await page.goto(`${editPath(page)}/edit/documents`);
        await chooseFiles(page, [balance(), balanceSig()]);
        await expect(files(page)).toHaveCount(2);
        await page.reload();
        await expect(files(page)).toHaveCount(2);
        await expect(files(page).filter({ hasText: 'balance.pdf.sig' })).toContainText('подпись к balance.pdf');
    });

    test('две подписи к одному документу — ошибка у второй', async ({ page }) => {
        await page.goto(`${editPath(page)}/edit/documents`);
        const sig = balanceSig();
        await chooseFiles(page, [balance(), sig, { ...sig, name: 'balance.sig' }]);
        await next(page);
        await expect(files(page).filter({ hasText: 'balance.sig' }).last()).toContainText('уже есть подпись');
    });
});

test.describe('Анкета — отправка', () => {
    test('сценарий A целиком: анкета и файлы уходят в data-collector', async ({ page }) => {
        await register(page);
        const appPath = editPath(page);
        const appId = appPath.split('/').pop() ?? '';
        await next(page);
        await fillBorrower(page);
        await next(page);
        await page.getByRole('button', { name: 'Добавить банк' }).click();
        await page.getByRole('textbox', { name: 'БИК банка 1' }).fill('044525225');
        await page.getByLabel('Токен оператора фискальных данных (ОФД)').fill('demo-ofd-token');
        await next(page);
        await next(page);
        await chooseFiles(page, [balance(), balanceSig()]);
        await openSelect(page, 'Что за документ: balance.pdf', 'Бухгалтерская отчётность');
        await next(page);

        const skipped = page.getByText('Не сможем проверить').locator('..');
        await expect(skipped).toContainText('кредитную историю');
        await page.getByText('Согласие компании на запрос кредитной истории в БКИ', { exact: true }).click();
        await page.getByText('Согласие руководителя на запрос его кредитной истории', { exact: true }).click();
        await page.getByText('Согласие на получение налоговой отчётности', { exact: true }).click();
        await expect(skipped).not.toContainText('кредитную историю');
        // директор подписывает сам — МЧД не нужна
        await expect(page.getByLabel('Номер МЧД')).toHaveCount(0);
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        await expectPath(page, appPath);
        await expect(page.getByRole('heading', { level: 2, name: /Заявка принята|Собираем данные о компании|Данные о компании собраны/ })).toBeVisible();

        const app = await collectorApp(page, appId);
        expect(['SUBMITTED', 'ENRICHING', 'VERIFYING']).toContain(app.status);
        expect(app.documents.map((d) => [d.file_name, d.type])).toEqual([
            ['balance.pdf', 'FINANCIAL_STATEMENTS'],
            ['balance.pdf.sig', 'SIGNATURE'],
        ]);
        expect(app.documents[1]?.signed_document_id).toBeTruthy();
        // черновик после отправки в браузере не храним
        expect(await page.evaluate((id) => localStorage.getItem(`credit-cabinet:questionnaire:${id}`), appId)).toBeNull();
    });

    test('бухгалтер: МЧД проверяется по формату', async ({ page }) => {
        await seedToken(page, 'northAccountant');
        await register(page, '7799000012', 'Бухгалтер');
        await toReview(page);
        await page.getByLabel('Номер МЧД').fill('12345');
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        await expect(page.getByText(/Номер МЧД — UUID/)).toBeVisible();
        await page.getByLabel('Номер МЧД').fill('0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37');
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        await expect(page).not.toHaveURL(/\/edit\//);
    });

    test('незаполненный шаг — ссылка на него, отправки нет', async ({ page }) => {
        await register(page);
        await page.goto(`${editPath(page)}/edit/review`);
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        const plate = page.getByText('Анкету пока не отправить').locator('..');
        await plate.getByRole('link', { name: 'Руководитель и анкета' }).click();
        await expect(page).toHaveURL(/\/edit\/borrower$/);
    });

    test('422 data-collector — ошибка разложена по шагам', async ({ page }) => {
        await register(page);
        await toReview(page);
        await setMockFailures(page, [{ method: 'POST', path: '/questionnaire$', status: 422, field: 'director.inn', times: 1 }]);
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        const plate = page.getByText('Банк не принял анкету').locator('..');
        await expect(plate).toContainText('Отклонено сервером');
        await plate.getByRole('link', { name: 'Руководитель и анкета' }).click();
        await expect(page.getByText('Отклонено сервером')).toBeVisible();
    });

    test('ИП (сценарий D): руководитель — сам предприниматель, без МЧД', async ({ page }) => {
        await seedToken(page, 'ipSmirnova');
        await register(page, '779900010202', 'Собственник бизнеса');
        await next(page);
        await expect(page.getByRole('heading', { level: 1, name: 'Руководитель и анкета' })).toBeVisible();
        await expect(page.getByText('Данные предпринимателя')).toBeVisible();
        await expect(page.getByLabel('ИНН физлица')).toHaveValue('779900010202');
        await page.goto(`${editPath(page)}/edit/review`);
        await expect(page.getByLabel('Номер МЧД')).toHaveCount(0);
    });
});

test.describe('Анкета — надёжность', () => {
    test('повторный вход по той же компании открывает тот же черновик и берёт новый продукт', async ({ page }) => {
        await register(page);
        const first = editPath(page);
        await page.goto('/applications/new?product=EXPRESS&amount=1000000&term=12&purpose=WORKING_CAPITAL');
        await page.getByLabel('ИНН компании или ИП').fill('7799000012');
        await expect(page.getByText('Нашли компанию в реестре')).toBeVisible();
        // компания уже в профиле после первой регистрации — роль из профиля, менять нельзя
        await expect(page.getByRole('radio', { name: 'Генеральный директор' })).toBeChecked();
        await expect(page.getByRole('radio', { name: 'Бухгалтер' })).toBeDisabled();
        await next(page);
        await expect(page).toHaveURL(/\/edit\/terms$/);
        expect(editPath(page)).toBe(first);
        await expect(page.getByRole('heading', { level: 2, name: 'Экспресс' })).toBeVisible();
    });

    test('ошибка сервера у поля пропадает, когда поле исправили', async ({ page }) => {
        await register(page);
        await toReview(page);
        await setMockFailures(page, [{ method: 'POST', path: '/questionnaire$', status: 422, field: 'director.inn', times: 1 }]);
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        await page.getByText('Банк не принял анкету').locator('..').getByRole('link', { name: 'Руководитель и анкета' }).click();
        await expect(page.getByText('Отклонено сервером')).toBeVisible();
        await page.getByLabel('ИНН физлица').fill('779900010140');
        await expect(page.getByText('Отклонено сервером')).toHaveCount(0);
    });

    test('токены — только во вкладке и удаляются после отправки; двойной клик — одна отправка', async ({ page }) => {
        await register(page);
        const appId = editPath(page).split('/').pop() ?? '';
        await next(page);
        await fillBorrower(page);
        await next(page);
        await page.getByLabel('Токен оператора фискальных данных (ОФД)').fill('secret-ofd-token');
        const stored = () => page.evaluate(() => ({ local: JSON.stringify(localStorage), session: JSON.stringify(sessionStorage) }));
        expect((await stored()).local).not.toContain('secret-ofd-token');
        expect((await stored()).session).toContain('secret-ofd-token');

        await page.goto(`${editPath(page)}/edit/review`);
        let posts = 0;
        page.on('request', (r) => {
            if (r.method() === 'POST' && r.url().endsWith('/questionnaire')) posts += 1;
        });
        const send = page.getByRole('button', { name: 'Отправить заявку' });
        await send.dblclick();
        await expectPath(page, `/applications/${appId}`);
        expect(posts).toBe(1);
        expect((await stored()).session).not.toContain('secret-ofd-token');
    });

    test('кавычки в имени файла заменяются — data-collector их не раскодирует', async ({ page }) => {
        await register(page);
        await page.goto(`${editPath(page)}/edit/documents`);
        await chooseFiles(page, [{ ...balance(), name: 'отчёт "2025".pdf' }]);
        await expect(files(page)).toContainText('отчёт _2025_.pdf');
    });

    test('согласие руководителя снимается вместе с руководителем — отправить можно', async ({ page }) => {
        await register(page);
        await toReview(page);
        await page.getByText('Согласие руководителя на запрос его кредитной истории', { exact: true }).click();
        await page.getByRole('link', { name: /Руководитель и анкета/ }).first().click();
        await page.getByText('Не указывать руководителя', { exact: true }).click();
        await page.goto(`${editPath(page)}/edit/review`);
        await expect(page.getByRole('checkbox', { name: 'Согласие руководителя на запрос его кредитной истории' })).not.toBeChecked();
        await page.getByRole('button', { name: 'Отправить заявку' }).click();
        await expect(page).not.toHaveURL(/\/edit\//);
    });
});

test.describe('Анкета — пользователь data-collector', () => {
    test('своя компания из профиля: роль подставлена, черновик привязан к пользователю', async ({ page }) => {
        await seedToken(page, 'northAccountant');
        await page.goto(NEW);
        await page.getByLabel('ИНН компании или ИП').fill('7799000012');
        await expect(page.getByText('Нашли компанию в реестре')).toBeVisible();
        await expect(page.getByRole('radio', { name: 'Бухгалтер' })).toBeChecked();
        await expect(page.getByLabel('ФИО')).toHaveValue('Литвинова Марина Олеговна');
        await next(page);
        await expect(page).toHaveURL(/\/edit\/terms$/);
        const id = editPath(page).split('/').pop() ?? '';
        const app = await page.evaluate(
            async (appId) =>
                (await (await fetch(`/collector/api/v1/applications/${appId}`, { headers: { Authorization: 'Bearer mock.usr_accountant_north' } })).json()) as {
                    representative: { id: string };
                },
            id,
        );
        expect(app.representative.id).toBe('usr_accountant_north');
    });
});
