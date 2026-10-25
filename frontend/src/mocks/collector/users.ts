import type { components } from '../../api/collector/schema';

type TestUser = components['schemas']['TestUser'];

const access = (applicant_id: string, short_name: string, inn: string, role: TestUser['companies'][number]['role'], can_sign: boolean) => [
    { applicant_id, short_name, inn, role, can_sign },
];

/**
 * Тестовые пользователи data-collector — копия service.TestUsers (data-collector/internal/service/testusers.go).
 * Первые пять — те же, что у моков остальных экранов (src/mocks/data.ts), остальные — компании сценариев A–E mock-api.
 */
export const COLLECTOR_TEST_USERS: TestUser[] = [
    {
        id: 'usr_director_romashka',
        full_name: 'Петров Пётр Петрович',
        roles: ['CLIENT'],
        persona: 'Директор ООО «Ромашка»: хорошая история, авто-одобрение, сам подписывает договор',
        companies: access('apl_romashka', 'ООО «Ромашка»', '7701234567', 'DIRECTOR', true),
    },
    {
        id: 'usr_accountant_romashka',
        full_name: 'Иванова Анна Сергеевна',
        roles: ['CLIENT'],
        persona: 'Бухгалтер ООО «Ромашка»: подаёт заявку, банк запросит доверенность, подписывает директор',
        companies: access('apl_romashka', 'ООО «Ромашка»', '7701234567', 'ACCOUNTANT', false),
    },
    {
        id: 'usr_ip_smirnov',
        full_name: 'Смирнов Кирилл Андреевич',
        roles: ['CLIENT'],
        persona: 'ИП, кофейня 1,5 года: расхождение по выручке, ручное рассмотрение',
        companies: access('apl_smirnov', 'ИП Смирнов К. А.', '771234567890', 'DIRECTOR', true),
    },
    {
        id: 'usr_director_vector',
        full_name: 'Лебедева Вера Олеговна',
        roles: ['CLIENT'],
        persona: 'Директор ООО «Вектор Строй»: отказ по оборудованию с объяснением, готовое предложение по транспорту',
        companies: access('apl_vector', 'ООО «Вектор Строй»', '7724567890', 'DIRECTOR', true),
    },
    {
        id: 'usr_underwriter_sokolova',
        full_name: 'Соколова Мария Игоревна',
        roles: ['UNDERWRITER'],
        persona: 'Андеррайтер: очередь ручного рассмотрения, досье и решение по заявке',
        companies: [],
    },
    {
        id: 'usr_director_north',
        full_name: 'Ковалёв Игорь Сергеевич',
        roles: ['CLIENT'],
        persona: 'Сценарий A: директор ООО «Северный ветер», опт продуктов — живой сбор данных, ожидается одобрение',
        companies: access('apl_north_wind', 'ООО «Северный ветер»', '7799000012', 'DIRECTOR', true),
    },
    {
        id: 'usr_accountant_north',
        full_name: 'Литвинова Марина Олеговна',
        roles: ['CLIENT'],
        persona: 'Сценарий A: бухгалтер ООО «Северный ветер» — подаёт по доверенности (МЧД), подписывает директор',
        companies: access('apl_north_wind', 'ООО «Северный ветер»', '7799000012', 'ACCOUNTANT', false),
    },
    {
        id: 'usr_director_stroyresurs',
        full_name: 'Орлов Денис Павлович',
        roles: ['CLIENT'],
        persona: 'Сценарий B: директор ООО «Стройресурс» — одобрение с меньшей суммой или дольше срок',
        companies: access('apl_stroyresurs', 'ООО «Стройресурс»', '7799000020', 'DIRECTOR', true),
    },
    {
        id: 'usr_director_altair',
        full_name: 'Белов Артём Николаевич',
        roles: ['CLIENT'],
        persona: 'Сценарий C: директор ООО «Альтаир» — ожидается отказ',
        companies: access('apl_altair', 'ООО «Альтаир»', '7799000037', 'DIRECTOR', true),
    },
    {
        id: 'usr_ip_smirnova',
        full_name: 'Смирнова Анна Викторовна',
        roles: ['CLIENT'],
        persona: 'Сценарий D: ИП, интернет-торговля — банк спросит про маркетплейсы и доход НПД',
        companies: access('apl_ip_smirnova', 'ИП Смирнова А. В.', '779900010202', 'OWNER', true),
    },
    {
        id: 'usr_finance_techno',
        full_name: 'Кузнецов Павел Андреевич',
        roles: ['CLIENT'],
        persona: 'Сценарий E: отвечает за финансирование в ООО «Техно-Сервис» — ручное рассмотрение',
        companies: access('apl_techno_service', 'ООО «Техно-Сервис»', '7799000044', 'FINANCE_EMPLOYEE', false),
    },
];

export const collectorUser = (id: string) => COLLECTOR_TEST_USERS.find((u) => u.id === id);
