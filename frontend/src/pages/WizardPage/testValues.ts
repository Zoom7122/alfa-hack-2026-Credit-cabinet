import type { DirectorDraft } from './draftTypes';

/** Синтетические профили тех же компаний, которые используются mock-api. */
export function testDirector(inn: string): DirectorDraft {
    const profiles: Record<string, [string, string, string, string, string]> = {
        '7799000012': ['779900010139', 'Ковалёв', 'Игорь', 'Сергеевич', '1984-07-19'],
        '7799000020': ['779900020136', 'Орлов', 'Денис', 'Павлович', '1979-02-11'],
        '7799000037': ['779900030147', 'Белов', 'Артём', 'Николаевич', '1988-11-03'],
        '7799000044': ['779900040158', 'Зайцева', 'Ольга', 'Михайловна', '1981-09-14'],
        '779900010202': ['779900010202', 'Смирнова', 'Анна', 'Викторовна', '1993-05-27'],
        '7701234567': ['770112345695', 'Петров', 'Пётр', 'Петрович', '1984-07-19'],
    };
    const [personINN, last_name, first_name, middle_name, birth_date] = profiles[inn]
        ?? [inn.length === 12 ? inn : '770112345695', 'Петров', 'Пётр', 'Петрович', '1984-07-19'] as const;
    return { inn: personINN, last_name, first_name, middle_name, birth_date };
}

export const TEST_POA = {
    good: '0f5a2c1e-7b3d-4e8a-9c21-6d4b8e0a1f37',
    neutral: '3c9e7d10-55a2-4f61-8b0e-2a7c4f9d6b12',
    bad: 'a71b4e02-9d3c-4c88-b5f0-81e6c2d3a945',
};
