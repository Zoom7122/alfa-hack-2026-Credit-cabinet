import type { CollectorSystem } from '../../api/collector/types';

/** Тестовые субъекты mock-api (docs/mock-api-guide.md): что вернёт ЕГРЮЛ и чем закончится сбор. */
export type Scenario = {
    inn: string;
    egrulName: string;
    ogrn: string;
    director: { inn: string; last_name: string; first_name: string; middle_name: string; birth_date: string };
    /** Системы, которые в этом сценарии ответили не полностью. */
    partial?: CollectorSystem[];
};

export const SCENARIOS: Scenario[] = [
    {
        inn: '7799000012',
        egrulName: 'ООО "СЕВЕРНЫЙ ВЕТЕР"',
        ogrn: '1217700412345',
        director: { inn: '779900010139', last_name: 'Ковалёв', first_name: 'Игорь', middle_name: 'Сергеевич', birth_date: '1984-07-19' },
    },
    {
        inn: '7799000020',
        egrulName: 'ООО "СТРОЙРЕСУРС"',
        ogrn: '1197700230011',
        director: { inn: '779900020136', last_name: 'Орлов', first_name: 'Денис', middle_name: 'Павлович', birth_date: '1979-11-02' },
    },
    {
        inn: '7799000037',
        egrulName: 'ООО "АЛЬТАИР"',
        ogrn: '1207700370022',
        director: { inn: '779900030147', last_name: 'Белов', first_name: 'Артём', middle_name: 'Николаевич', birth_date: '1988-03-14' },
        partial: ['KAD'],
    },
    {
        inn: '779900010202',
        egrulName: 'ИП СМИРНОВА АННА ВИКТОРОВНА',
        ogrn: '321774600123456',
        director: { inn: '779900010202', last_name: 'Смирнова', first_name: 'Анна', middle_name: 'Викторовна', birth_date: '1991-05-27' },
    },
    {
        inn: '7799000044',
        egrulName: 'ООО "ТЕХНО-СЕРВИС"',
        ogrn: '1187700440033',
        director: { inn: '779900040158', last_name: 'Зайцева', first_name: 'Ольга', middle_name: 'Михайловна', birth_date: '1982-09-08' },
        partial: ['FNS'],
    },
];

export const scenarioByInn = (inn: string) => SCENARIOS.find((s) => s.inn === inn);

/** Для e2e: у этой компании сбор падает — задача FAILED, заявка навсегда остаётся в ENRICHING (как у data-collector). */
export const FAILING_COLLECTION_INN = '7799000077';
