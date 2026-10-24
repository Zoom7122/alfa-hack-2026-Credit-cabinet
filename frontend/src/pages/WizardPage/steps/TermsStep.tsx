import { useEffect, useState } from 'react';
import { Plate } from '@alfalab/core-components/plate';
import { Select } from '@alfalab/core-components/select';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Textarea } from '@alfalab/core-components/textarea';

import { useCalculation } from '../../../api/queries';
import type { PurposeCode } from '../../../api/types';
import { PURPOSE_LABELS, PURPOSES } from '../../../shared/lib/dictionaries';
import { formatMoney } from '../../../shared/lib/format';
import { useSessionState } from '../../../shared/lib/sessionState';
import { fitTerms, type TermsValue, validTerms } from '../../../shared/lib/terms';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { PaymentEstimate } from '../../../shared/ui/PaymentEstimate/PaymentEstimate';
import { TermsFields } from '../../../shared/ui/TermsFields/TermsFields';
import { StepLayout } from '../components/StepLayout';
import type { StepProps } from '../stepProps';
import './TermsStep.css';

export function TermsStep({ app, product, draft, hrefFor }: StepProps) {
    const [terms, setTerms] = useState<TermsValue>(() =>
        product
            ? fitTerms(product, { amount: app.requested?.amount ?? '', term: app.requested?.term_months ?? '' })
            : { amount: app.requested?.amount ?? '', term: app.requested?.term_months ?? '' },
    );
    // описание без выбранной цели контракт не примет — до выбора цели держим его во вкладке
    const [description, setDescription] = useSessionState(`credit-cabinet:wizard:${app.id}:purpose-description`, () => app.purpose?.description ?? '');
    const valid = validTerms(product, terms);
    const calculation = useCalculation(product?.code, valid);

    // сумму и срок без значения или вне диапазона продукта слайдеры показывают подогнанными — их и сохраняем (один раз при открытии)
    const [fitOnOpen] = useState(() => {
        const fitted = validTerms(product, terms);
        const stored = app.requested;
        return fitted && (stored?.amount !== fitted.amount || stored?.term_months !== fitted.term_months) ? fitted : undefined;
    });
    const { update } = draft;
    useEffect(() => {
        if (fitOnOpen) update({ requested: fitOnOpen });
    }, [fitOnOpen, update]);

    const changeTerms = (next: TermsValue) => {
        setTerms(next);
        // в черновик — только то, что в пределах продукта
        const ok = validTerms(product, next);
        if (ok) draft.update({ requested: ok });
    };

    const purposes = product?.purposes?.length ? product.purposes : PURPOSES;
    const threshold = product?.collateral?.required_above_amount;
    const needsCollateral = valid && threshold !== null && threshold !== undefined && threshold > 0 && valid.amount > threshold;

    if (!product) return <Skeleton visible={true} className='terms-step__skeleton' />;

    return (
        <StepLayout
            title='Сумма, срок и цель'
            subtitle='Можно поменять позже — до отправки заявки'
            backHref={hrefFor('company')}
            nextHref={hrefFor('finance')}
        >
            <FormCard>
                <TermsFields product={product} value={terms} onChange={changeTerms} />
                <PaymentEstimate payment={valid && calculation.data?.monthly_payment_from} rate={product.rate_annual_from} loading={calculation.isFetching} />
                {needsCollateral && (
                    <Plate view='attention' title={`Для суммы больше ${formatMoney(threshold)} нужен залог`}>
                        Укажете его на шаге «Залог и поручители».
                    </Plate>
                )}
            </FormCard>

            <FormCard title='На что нужны деньги'>
                <Select
                    label='Цель кредита'
                    size={56}
                    block={true}
                    options={purposes.map((code) => ({ key: code, content: PURPOSE_LABELS[code] }))}
                    selected={app.purpose?.code ?? null}
                    onChange={({ selected }) =>
                        selected && draft.update({ purpose: { code: selected.key as PurposeCode, description: description || null } })
                    }
                    error={!app.purpose?.code && valid ? 'Выберите цель — от неё зависят условия' : undefined}
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
                    value={description}
                    onChange={(e) => {
                        setDescription(e.target.value);
                        // Purpose без code контракт не примет — описание уйдёт вместе с выбранной целью
                        const code = app.purpose?.code;
                        if (code) draft.update({ purpose: { code, description: e.target.value || null } });
                    }}
                />
            </FormCard>
        </StepLayout>
    );
}
