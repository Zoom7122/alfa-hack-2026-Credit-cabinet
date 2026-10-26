import type { InfoRequest } from '../../../api/types';
import { fieldLabel } from '../../../shared/lib/dictionaries';
import type { TestValues } from '../../../shared/ui/TestField/TestField';
import type { AnswerDraft } from './answers';

const FACTS: Record<string, TestValues<string>> = {
    business_age_months: { good: '72', neutral: '24', bad: '6' },
    overdue_taxes_rub: { good: '0', neutral: '10000', bad: '500000' },
    bank_relationship_months: { good: '36', neutral: '12', bad: '0' },
    material_register_inaccuracy: { good: 'Недостоверных сведений нет', neutral: 'Требуется сверка реквизитов', bad: 'Обнаружены недостоверные сведения' },
    operating_nonfinancial_company: { good: 'Основная деятельность ведётся', neutral: 'Деятельность сезонная', bad: 'Деятельность приостановлена' },
    liquidated_or_excluded: { good: 'Компания не ликвидирована', neutral: 'Статус требует уточнения', bad: 'Принято решение о ликвидации' },
    active: { good: 'Компания действует', neutral: 'Есть изменения регистрационных данных', bad: 'Компания прекратила деятельность' },
};

/** Синтетические ответы уходят через обычную ручку ответа, без подмены проверок банка. */
export function testAnswers(request: InfoRequest): TestValues<NonNullable<AnswerDraft['value']>> | undefined {
    const field = request.target_field ?? '';
    switch (request.input.type) {
        case 'MONEY': return { good: 1_000_000, neutral: 100_000, bad: 0 };
        case 'NUMBER': return field.includes('rate') ? { good: '40', neutral: '26.5', bad: '5' } : { good: '72', neutral: '24', bad: '0' };
        case 'BOOLEAN': return { good: true, neutral: false, bad: false };
        case 'DATE': return { good: '26.10.2026', neutral: '01.01.2026', bad: '01.01.2000' };
        case 'CHOICE': {
            const options = request.input.options ?? [];
            if (!options.length) return undefined;
            return { good: options[0]!.value, neutral: options[Math.floor(options.length / 2)]!.value, bad: options.at(-1)!.value };
        }
        case 'TEXT': {
            const fields = field.split(',').map((key) => key.trim());
            if (fields.some((key) => FACTS[key.split('.').at(-1) ?? ''])) {
                const text = (variant: keyof TestValues<string>) => fields.map((key) => {
                    const values = FACTS[key.split('.').at(-1) ?? ''];
                    return `${fieldLabel(key)}: ${values?.[variant] ?? 'требуется проверка'}`;
                }).join('; ');
                return { good: text('good'), neutral: text('neutral'), bad: text('bad') };
            }
            return { good: 'Тестовые сведения подтверждены документами. Существенных изменений нет.',
                neutral: 'Тестовые сведения требуют дополнительной проверки.',
                bad: 'Тестовые сведения не подтверждены. Деятельность приостановлена, основной покупатель потерян.' };
        }
        default: return undefined;
    }
}
