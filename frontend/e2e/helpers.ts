import { expect, type Page } from '@playwright/test';

/** Ключ токена в localStorage — как в src/api/session.ts */
export const TOKEN_KEY = 'credit-cabinet:token';

export const USERS = {
    director: { id: 'usr_director_romashka', name: 'Петров Пётр Петрович' },
    accountant: { id: 'usr_accountant_romashka', name: 'Иванова Анна Сергеевна' },
    soleProprietor: { id: 'usr_ip_smirnov', name: 'Смирнов Кирилл Андреевич' },
    vector: { id: 'usr_director_vector', name: 'Лебедева Вера Олеговна' },
    underwriter: { id: 'usr_underwriter_sokolova', name: 'Соколова Мария Игоревна' },
    // тестовые пользователи data-collector — представители компаний сценариев mock-api
    northDirector: { id: 'usr_director_north', name: 'Ковалёв Игорь Сергеевич' },
    northAccountant: { id: 'usr_accountant_north', name: 'Литвинова Марина Олеговна' },
    ipSmirnova: { id: 'usr_ip_smirnova', name: 'Смирнова Анна Викторовна' },
    stroyresurs: { id: 'usr_director_stroyresurs', name: 'Орлов Денис Павлович' },
    orlov: { id: 'usr_underwriter_orlov', name: 'Орлов Дмитрий Андреевич' },
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
/** Прогон против живого data-collector (E2E_COLLECTOR_LIVE=1): тесты на сбои и данные из моков пропускаются. */
export const COLLECTOR_LIVE = process.env.E2E_COLLECTOR_LIVE === '1';
export const MOCK_ONLY = 'только в моке data-collector';

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

/** Анкета сценария B mock-api (ООО «Стройресурс», оборотный кредит 8 млн): у движка — «много кредитных обращений». */
export const SCENARIO_B = {
    age: 47,
    marital_status: 'MARRIED',
    children_count: 3,
    product_code: 'WORKING_CAPITAL',
    requested: { amount: 8_000_000, term_months: 36 },
    purpose: { code: 'WORKING_CAPITAL' },
    director: { inn: '779900020136', last_name: 'Орлов', first_name: 'Денис', middle_name: 'Павлович', birth_date: '1979-02-11' },
    consents: {
        bki_company: { id: 'CNS-2026-000801', signed_at: '2026-10-21T14:02:11Z', valid_until: '2027-04-21' },
        bki_person: { id: 'CNS-2026-000802', signed_at: '2026-10-21T14:02:40Z', valid_until: '2027-04-21' },
        tax: { id: 'TAX-CNS-2026-000801', signed_at: '2026-10-21T14:03:02Z', valid_until: '2027-04-21', scope: ['DECLARATIONS', 'ENS_BALANCE'] },
    },
    other_bank_biks: ['044525777', '044525650'],
    scoring: {
        max_annual_rate: 28,
        min_acceptable_amount: 2_400_000,
        max_acceptable_term_months: 48,
        need_cost: 8_800_000,
        own_contribution: 800_000,
        free_cash: 50_000,
        minimum_cash: 20_000,
        need_evidence: 'VERIFIED_ESTIMATE',
        experience_years: 8,
    },
};
