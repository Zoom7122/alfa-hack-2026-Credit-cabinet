import { useEffect, useState } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';
import { Textarea } from '@alfalab/core-components/textarea';
import { Link } from 'react-router-dom';

import type { CollectorPurpose } from '../../../api/collector/types';
import { useCalculation } from '../../../api/queries';
import { PURPOSE_LABELS, PURPOSES } from '../../../shared/lib/dictionaries';
import { fitTerms, type TermsValue, validTerms } from '../../../shared/lib/terms';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { PaymentEstimate } from '../../../shared/ui/PaymentEstimate/PaymentEstimate';
import { TermsFields } from '../../../shared/ui/TermsFields/TermsFields';
import { StepLayout } from '../components/StepLayout';
import { type StepProps, useStepGate } from '../stepProps';

export function TermsStep({ product, draft, hrefFor, errors, serverErrors }: StepProps) {
    const d = draft.value;
    const [terms, setTerms] = useState<TermsValue>(() => {
        const stored: TermsValue = { amount: d.requested?.amount ?? '', term: d.requested?.term_months ?? '' };
        return product ? fitTerms(product, stored) : stored;
    });
    const valid = validTerms(product, terms);
    const calculation = useCalculation(product?.code, valid);
    const gate = useStepGate(errors, serverErrors, hrefFor('borrower'));

    // слайдеры показывают сумму и срок подогнанными под продукт — их и сохраняем (один раз при открытии)
    const [fitOnOpen] = useState(() => {
        const fitted = validTerms(product, terms);
        return fitted && (fitted.amount !== d.requested?.amount || fitted.term_months !== d.requested?.term_months) ? fitted : undefined;
    });
    const { update } = draft;
    useEffect(() => {
        if (fitOnOpen) update({ requested: fitOnOpen });
    }, [fitOnOpen, update]);

    const changeTerms = (next: TermsValue) => {
        setTerms(next);
        draft.update({ requested: validTerms(product, next) });
    };

    // каталог продуктов — пока из моков; цели data-collector совпадают со справочником кабинета
    const purposes = (product?.purposes?.length ? product.purposes : PURPOSES) as CollectorPurpose[];

    // каталог не загрузился или такого продукта больше нет — без диапазонов суммы и срока шаг не заполнить
    if (!product) {
        return (
            <StepLayout title='Сумма, срок и цель' backHref={hrefFor('company')}>
                <Plate view='negative' title='Не нашли продукт этой заявки'>
                    <Button Component={Link} href='/products' view='secondary' size={40}>
                        Выбрать продукт
                    </Button>
                </Plate>
            </StepLayout>
        );
    }

    return (
        <StepLayout
            title='Сумма, срок и цель'
            subtitle='Можно поменять до отправки заявки'
            backHref={hrefFor('company')}
            onNext={gate.onNext}
            hint={gate.hint}
        >
            <FormCard>
                <TermsFields product={product} value={terms} onChange={changeTerms} />
                <PaymentEstimate payment={valid && calculation.data?.monthly_payment_from} rate={product.rate_annual_from} loading={calculation.isFetching} />
            </FormCard>

            <FormCard title='На что нужны деньги'>
                <Select
                    label='Цель кредита'
                    size={56}
                    block={true}
                    options={purposes.map((code) => ({ key: code, content: PURPOSE_LABELS[code] }))}
                    selected={d.purpose?.code ?? null}
                    onChange={({ selected }) => selected && draft.update((prev) => ({ purpose: { ...prev.purpose, code: selected.key as CollectorPurpose } }))}
                    error={gate.errorOf('purpose.code')}
                />
                <Textarea
                    label='Расскажите подробнее'
                    // подпись кита не связана с полем — даём имя явно
                    aria-label='Расскажите подробнее'
                    hint='Например: «Закупка товара под сезон». Необязательно, но помогает быстрее принять решение'
                    size={56}
                    block={true}
                    minRows={2}
                    maxLength={2000}
                    value={d.purpose?.description ?? ''}
                    error={gate.errorOf('purpose.description')}
                    onChange={(e) => {
                        const description = e.target.value;
                        draft.update((prev) => ({ purpose: { ...prev.purpose, description } }));
                    }}
                />
            </FormCard>
        </StepLayout>
    );
}
