import { expect, type Page } from '@playwright/test';

/** Ключ токена в localStorage — как в src/api/session.ts */
export const TOKEN_KEY = 'credit-cabinet:token';

export const USERS = {
    director: { id: 'usr_director_romashka', name: 'Петров Пётр Петрович' },
    accountant: { id: 'usr_accountant_romashka', name: 'Иванова Анна Сергеевна' },
    soleProprietor: { id: 'usr_ip_smirnov', name: 'Смирнов Кирилл Андреевич' },
    vector: { id: 'usr_director_vector', name: 'Лебедева Вера Олеговна' },
    underwriter: { id: 'usr_underwriter_sokolova', name: 'Соколова Мария Игоревна' },
} as const;

export type UserKey = keyof typeof USERS;

export async function loginAs(page: Page, user: UserKey) {
    const { name } = USERS[user];
    await page.goto('/login');
    if (user === 'underwriter') {
        await page.getByText('Сотрудник банка', { exact: true }).click();
    }
    await page.getByRole('radio', { name: new RegExp(name) }).click();
    await page.getByRole('button', { name: /^Войти как/ }).click();
}

/** Сразу положить токен мока, минуя экран входа. */
export async function seedToken(page: Page, user: UserKey) {
    await page.addInitScript(
        ([key, token]) => window.localStorage.setItem(key, token),
        [TOKEN_KEY, `mock.${USERS[user].id}`] as const,
    );
}

export async function expectPath(page: Page, path: string) {
    await expect(page).toHaveURL((url) => url.pathname === path);
}

/** Сбой в моке по флагу (см. mockFailure в src/mocks/handlers.ts). */
export type MockFailure = { method: string; path: string; status: number; code?: string; field?: string; times?: number };

export async function setMockFailures(page: Page, rules: MockFailure[]) {
    await page.evaluate((r) => localStorage.setItem('credit-cabinet:mock-fail', JSON.stringify(r)), rules);
}

export async function clearMockFailures(page: Page) {
    await page.evaluate(() => localStorage.removeItem('credit-cabinet:mock-fail'));
}

/** Шаг конвейера рассмотрения в моке (по умолчанию 2,5 с) — тесты его ускоряют. */
export async function setPipelineStep(page: Page, ms: number) {
    await page.addInitScript((value) => localStorage.setItem('credit-cabinet:mock-pipeline-ms', String(value)), ms);
}
