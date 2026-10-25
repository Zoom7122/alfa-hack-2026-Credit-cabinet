import { Plate } from '@alfalab/core-components/plate';

import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { ListField } from '../components/ListField';
import { StepLayout } from '../components/StepLayout';
import { MAX_COLLATERAL, type QuestionnaireDraft } from '../draft';
import { type StepProps, useStepGate } from '../stepProps';

type Kind = keyof QuestionnaireDraft['collateral'];

const KINDS: { id: Kind; title: string; label: string; add: string; hint: string; normalize: (v: string) => string }[] = [
    {
        id: 'cadastral_numbers',
        title: 'Недвижимость',
        label: 'Кадастровый номер',
        add: 'Добавить объект',
        hint: 'Вида 50:21:0110203:845 — выписку из ЕГРН запросим сами',
        normalize: (v) => v.replace(/[^\d:]/g, ''),
    },
    {
        id: 'vins',
        title: 'Транспорт',
        label: 'VIN',
        add: 'Добавить транспорт',
        hint: '17 символов. Проверим, не заложен ли он уже',
        normalize: (v) => v.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 17),
    },
    {
        id: 'serials',
        title: 'Оборудование и спецтехника',
        label: 'Заводской номер',
        add: 'Добавить технику',
        hint: 'Как в паспорте техники',
        normalize: (v) => v.slice(0, 64),
    },
];

/** Шаг «Залог»: что можно предложить в залог. Необязательно; до 20 объектов. */
export function CollateralStep({ draft, hrefFor, errors, serverErrors }: StepProps) {
    const c = draft.value.collateral;
    const gate = useStepGate(errors, serverErrors, hrefFor('documents'));
    const total = c.cadastral_numbers.length + c.vins.length + c.serials.length;

    return (
        <StepLayout title='Залог' subtitle='Необязательно: с залогом сумма может быть больше' backHref={hrefFor('sources')} onNext={gate.onNext} hint={gate.hint}>
            {KINDS.map((k) => (
                <FormCard key={k.id} title={k.title}>
                    <ListField
                        label={k.label}
                        values={c[k.id]}
                        onChange={(values) => draft.update((prev) => ({ collateral: { ...prev.collateral, [k.id]: values } }))}
                        errorOf={(i) => gate.errorOf(`collateral.${k.id}[${i}]`)}
                        addLabel={k.add}
                        max={c[k.id].length + Math.max(0, MAX_COLLATERAL - total)}
                        hint={k.hint}
                        normalize={k.normalize}
                    />
                </FormCard>
            ))}
            {gate.errorOf('collateral') && <Plate view='negative' title={gate.errorOf('collateral')} />}
        </StepLayout>
    );
}
