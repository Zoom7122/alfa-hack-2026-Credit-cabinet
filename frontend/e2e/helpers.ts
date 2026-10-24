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

type UserKey = keyof typeof USERS;

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
