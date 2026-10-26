import { useEffect } from 'react';
import { Checkbox } from '@alfalab/core-components/checkbox';
import { DateInput } from '@alfalab/core-components/date-input';
import { Input } from '@alfalab/core-components/input';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';

import { MARITAL_LABELS, type MaritalStatus } from '../../../api/collector/types';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { StepLayout } from '../components/StepLayout';
import type { DirectorDraft } from '../draft';
import { type StepProps, useStepGate } from '../stepProps';
import './BorrowerStep.css';

const MARITAL_OPTIONS = (Object.entries(MARITAL_LABELS) as [MaritalStatus, string][]).map(([key, content]) => ({ key, content }));

/** DateInput кита работает с «ДД.ММ.ГГГГ», в анкету уходит ГГГГ-ММ-ДД. Недописанную дату храним как есть. */
const toIso = (value: string) => value.replace(/^(\d{2})\.(\d{2})\.(\d{4})$/, '$3-$2-$1');
const fromIso = (value: string) => value.replace(/^(\d{4})-(\d{2})-(\d{2})$/, '$3.$2.$1');
const digits = (value: string, max: number) => value.replace(/\D/g, '').slice(0, max);
const toNumber = (value: string) => (value === '' ? undefined : Number(value));

/** Шаг «Руководитель и анкета»: поля анкеты заёмщика и данные руководителя для проверок физлица. */
export function BorrowerStep({ app, draft, hrefFor, errors, serverErrors, soleProprietor }: StepProps) {
    const d = draft.value;
    const gate = useStepGate(errors, serverErrors, hrefFor('scoring'));
    const setDirector = (patch: Partial<DirectorDraft>) => draft.update((prev) => ({ director: { ...prev.director, ...patch } }));

    // у ИП руководитель — сам предприниматель: ИНН совпадает с ИНН ИП
    const { update } = draft;
    const companyInn = app.applicant?.inn ?? '';
    useEffect(() => {
        if (soleProprietor && !d.director.inn) update((prev) => ({ director: { ...prev.director, inn: companyInn } }));
    }, [soleProprietor, companyInn, d.director.inn, update]);

    return (
        <StepLayout
            title='Руководитель и анкета'
            subtitle={soleProprietor ? 'Данные предпринимателя' : 'Данные генерального директора'}
            backHref={hrefFor('terms')}
            onNext={gate.onNext}
            hint={gate.hint}
        >
            <FormCard title='Анкета заёмщика'>
                <div className='borrower-step__grid'>
                    <Input
                        label='Возраст, лет'
                        value={d.age === undefined ? '' : String(d.age)}
                        inputMode='numeric'
                        onChange={(_, { value }) => draft.update({ age: toNumber(digits(value, 3)) })}
                        error={gate.errorOf('age')}
                        block={true}
                        size={56}
                    />
                    <Select
                        label='Семейное положение'
                        size={56}
                        block={true}
                        options={MARITAL_OPTIONS}
                        selected={d.marital_status ?? null}
                        onChange={({ selected }) => selected && draft.update({ marital_status: selected.key as MaritalStatus })}
                        error={gate.errorOf('marital_status')}
                    />
                    <Input
                        label='Дети и иждивенцы'
                        value={d.children_count === undefined ? '' : String(d.children_count)}
                        inputMode='numeric'
                        onChange={(_, { value }) => draft.update({ children_count: toNumber(digits(value, 2)) })}
                        error={gate.errorOf('children_count')}
                        hint='0, если нет'
                        block={true}
                        size={56}
                    />
                </div>
            </FormCard>

            <FormCard title={soleProprietor ? 'Предприниматель' : 'Руководитель'}>
                <Checkbox
                    label='Не указывать руководителя'
                    checked={!d.with_director}
                    // без руководителя его согласие на БКИ теряет смысл — снимаем вместе с ним
                    onChange={(_, { checked }) =>
                        draft.update((prev) => ({ with_director: !checked, consents: checked ? { ...prev.consents, bki_person: undefined } : prev.consents }))
                    }
                    size={24}
                />
                {d.with_director ? (
                    <div className='borrower-step__grid'>
                        <Input label='Фамилия' value={d.director.last_name} onChange={(_, { value }) => setDirector({ last_name: value })} error={gate.errorOf('director.last_name')} block={true} size={56} />
                        <Input label='Имя' value={d.director.first_name} onChange={(_, { value }) => setDirector({ first_name: value })} error={gate.errorOf('director.first_name')} block={true} size={56} />
                        <Input label='Отчество' value={d.director.middle_name} onChange={(_, { value }) => setDirector({ middle_name: value })} hint='Если есть' block={true} size={56} />
                        <Input
                            label='ИНН физлица'
                            value={d.director.inn}
                            inputMode='numeric'
                            onChange={(_, { value }) => setDirector({ inn: digits(value, 12) })}
                            error={gate.errorOf('director.inn')}
                            hint='12 цифр'
                            block={true}
                            size={56}
                        />
                        <DateInput
                            label='Дата рождения'
                            value={fromIso(d.director.birth_date)}
                            onChange={(_, { value }) => setDirector({ birth_date: toIso(value) })}
                            error={gate.errorOf('director.birth_date')}
                            block={true}
                            size={56}
                        />
                    </div>
                ) : (
                    <Plate view='attention' title='Без руководителя проверим только компанию'>
                        Не запросим его кредитную историю, исполнительные производства, банкротство, «Госуслуги», дисквалификацию и
                        санкционные списки — решение может занять больше времени.
                    </Plate>
                )}
            </FormCard>
        </StepLayout>
    );
}
