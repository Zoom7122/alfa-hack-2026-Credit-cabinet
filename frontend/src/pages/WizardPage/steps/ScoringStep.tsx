import { AmountInput } from '@alfalab/core-components/amount-input';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';

import type { ScoringAnswers } from '../../../api/collector/types';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { StepLayout } from '../components/StepLayout';
import { type StepProps, useStepGate } from '../stepProps';
import './ScoringStep.css';

type ChoiceField = 'need_evidence' | 'primary_income_source' | 'qualification' | 'car_class' | 'education_level' | 'real_estate_status' | 'business_location' | 'buyer_contracts';
type Choice<K extends ChoiceField> = { key: NonNullable<ScoringAnswers[K]>; content: string }[];

const NEED_EVIDENCE: Choice<'need_evidence'> = [
    { key: 'CONTRACT_AND_COST', content: 'Договор или заказ и документы о стоимости' },
    { key: 'VERIFIED_ESTIMATE', content: 'Коммерческие предложения или смета' },
    { key: 'ORAL_ONLY', content: 'Пока только описание' },
];
const INCOME_SOURCE: Choice<'primary_income_source'> = [
    { key: 'BUSINESS', content: 'Предпринимательская деятельность' },
    { key: 'PUBLIC_EMPLOYMENT', content: 'Работа в бюджетной организации' },
    { key: 'PRIVATE_EMPLOYMENT', content: 'Работа в коммерческой организации' },
    { key: 'PENSION', content: 'Пенсия' },
    { key: 'SELF_EMPLOYED_OR_GPH', content: 'Самозанятость или договоры ГПХ' },
    { key: 'NONE', content: 'Регулярного дохода нет' },
];
const QUALIFICATION: Choice<'qualification'> = [
    { key: 'MANAGER_OR_HIGHLY_QUALIFIED', content: 'Руководитель или специалист высшей квалификации' },
    { key: 'SPECIALIST', content: 'Специалист или служащий' },
    { key: 'SUPPORT', content: 'Обслуживающий персонал' },
    { key: 'NONE', content: 'Подтверждённой квалификации нет' },
];
const CAR: Choice<'car_class'> = [
    { key: 'NONE', content: 'Нет автомобиля' },
    { key: 'ECONOMY', content: 'Эконом-класс' },
    { key: 'COMFORT', content: 'Комфорт-класс' },
    { key: 'PREMIUM', content: 'Премиум-класс' },
];
const EDUCATION: Choice<'education_level'> = [
    { key: 'ACADEMIC_DEGREE', content: 'Учёная степень' },
    { key: 'HIGHER', content: 'Высшее' },
    { key: 'VOCATIONAL', content: 'Среднее профессиональное' },
    { key: 'GENERAL', content: 'Среднее общее' },
    { key: 'BELOW_GENERAL', content: 'Не завершено среднее' },
];
const REAL_ESTATE: Choice<'real_estate_status'> = [
    { key: 'UNENCUMBERED_WHOLE', content: 'Есть объект без обременений' },
    { key: 'UNENCUMBERED_SHARE', content: 'Есть только доля без обременений' },
    { key: 'ENCUMBERED_ONLY', content: 'Есть только с обременением' },
    { key: 'NONE', content: 'Нет' },
];
const LOCATION: Choice<'business_location'> = [
    { key: 'VERIFIED_PHYSICAL', content: 'Своё или арендованное помещение' },
    { key: 'VERIFIED_ONLINE_OR_MOBILE', content: 'Онлайн или выездная работа' },
    { key: 'UNVERIFIED', content: 'Постоянного места нет' },
];
const BUYERS: Choice<'buyer_contracts'> = [
    { key: 'ACTIVE_AND_PERFORMED', content: 'Действующие договоры, они исполняются' },
    { key: 'REPEAT_ORDERS', content: 'Повторные разовые заказы' },
    { key: 'PLANS_ONLY', content: 'Пока только планы продаж' },
];

const digits = (value: string, max: number) => value.replace(/\D/g, '').slice(0, max);
const toNumber = (value: string) => (value === '' ? undefined : Number(value));

/** Шаг «Для оценки»: ответы для скоринга credit-engine, которых нет в собранных данных. Необязателен. */
export function ScoringStep({ draft, hrefFor, errors, serverErrors, soleProprietor }: StepProps) {
    const s = draft.value.scoring ?? {};
    const gate = useStepGate(errors, serverErrors, hrefFor('sources'));
    const set = (patch: Partial<ScoringAnswers>) => draft.update((prev) => ({ scoring: { ...prev.scoring, ...patch } }));

    const amount = (field: 'min_acceptable_amount' | 'need_cost' | 'own_contribution' | 'free_cash' | 'minimum_cash' | 'net_monthly_income', label: string, hint?: string) => (
        <AmountInput
            label={label}
            value={s[field] ?? null}
            minority={1}
            integersOnly={true}
            onChange={(_, { value }) => set({ [field]: value ?? undefined })}
            error={gate.errorOf(`scoring.${field}`)}
            hint={hint}
            block={true}
            size={56}
        />
    );
    const count = (field: 'max_acceptable_term_months' | 'experience_years' | 'total_experience_months' | 'location_stability_months', label: string, hint?: string) => (
        <Input
            label={label}
            value={s[field] === undefined ? '' : String(s[field])}
            inputMode='numeric'
            onChange={(_, { value }) => set({ [field]: toNumber(digits(value, 3)) })}
            error={gate.errorOf(`scoring.${field}`)}
            hint={hint}
            block={true}
            size={56}
        />
    );
    const choice = <K extends ChoiceField>(field: K, label: string, options: Choice<K>) => (
        <Select
            label={label}
            size={56}
            block={true}
            options={options}
            selected={s[field] ?? null}
            onChange={({ selected }) => set({ [field]: selected?.key } as Partial<ScoringAnswers>)}
            error={gate.errorOf(`scoring.${field}`)}
        />
    );

    return (
        <StepLayout
            title='Для оценки'
            subtitle='Этого нет в реестрах и выписках. Можно пропустить — тогда банк спросит после оценки'
            backHref={hrefFor('borrower')}
            onNext={gate.onNext}
            hint={gate.hint}
        >
            <FormCard title='Какие условия вам подойдут'>
                <div className='scoring-step__grid'>
                    <Input
                        label='Максимальная ставка, % годовых'
                        value={s.max_annual_rate === undefined ? '' : String(s.max_annual_rate)}
                        inputMode='decimal'
                        onChange={(_, { value }) => {
                            const v = value.replace(',', '.').replace(/[^\d.]/g, '');
                            set({ max_annual_rate: v === '' || Number.isNaN(Number(v)) ? undefined : Number(v) });
                        }}
                        error={gate.errorOf('scoring.max_annual_rate')}
                        block={true}
                        size={56}
                    />
                    {amount('min_acceptable_amount', 'Минимальная сумма', 'Если всю сумму дать нельзя')}
                    {count('max_acceptable_term_months', 'Максимальный срок, мес.', 'Можно дольше запрошенного')}
                </div>
            </FormCard>

            <FormCard title='На что нужен кредит'>
                <div className='scoring-step__grid'>
                    {amount('need_cost', 'Сколько это стоит')}
                    {amount('own_contribution', 'Ваш собственный вклад', '0, если нет')}
                    {choice('need_evidence', 'Чем подтверждена стоимость', NEED_EVIDENCE)}
                </div>
            </FormCard>

            <FormCard title='Деньги и опыт'>
                <div className='scoring-step__grid'>
                    {amount('free_cash', 'Свободные деньги после вклада', 'Сверим с остатком на счёте')}
                    {amount('minimum_cash', 'Неснижаемый остаток на счетах')}
                    {count('experience_years', soleProprietor ? 'Ваш опыт в этом бизнесе, лет' : 'Опыт руководителя в этом бизнесе, лет')}
                </div>
            </FormCard>

            {soleProprietor && (
                <FormCard title='О предпринимателе'>
                    <div className='scoring-step__grid'>
                        {choice('primary_income_source', 'Основной источник личного дохода', INCOME_SOURCE)}
                        {amount('net_monthly_income', 'Личный доход после налогов, в месяц')}
                        {choice('qualification', 'Квалификация', QUALIFICATION)}
                        {count('total_experience_months', 'Общий трудовой стаж, мес.')}
                        {choice('education_level', 'Образование', EDUCATION)}
                        {choice('car_class', 'Автомобиль', CAR)}
                        {choice('real_estate_status', 'Недвижимость в собственности', REAL_ESTATE)}
                        {choice('buyer_contracts', 'Отношения с покупателями', BUYERS)}
                        {choice('business_location', 'Где вы ведёте бизнес', LOCATION)}
                        {s.business_location === 'VERIFIED_PHYSICAL' && count('location_stability_months', 'Сколько месяцев на этом месте')}
                    </div>
                </FormCard>
            )}

            <Plate view='common' title='Остальное банк узнает сам'>
                Возраст бизнеса, кредитную историю, налоги, суды и обороты по счетам возьмём из реестров, бюро и выписок.
            </Plate>
        </StepLayout>
    );
}
