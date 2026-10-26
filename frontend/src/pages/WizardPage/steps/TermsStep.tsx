import { Input } from '@alfalab/core-components/input';
import { Select } from '@alfalab/core-components/select';
import { Textarea } from '@alfalab/core-components/textarea';

import type { CollectorPurpose } from '../../../api/collector/types';
import { useCalculation } from '../../../api/queries';
import { PURPOSE_LABELS, PURPOSES } from '../../../shared/lib/dictionaries';
import { type TermsValue, validTerms } from '../../../shared/lib/terms';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import { PaymentEstimate } from '../../../shared/ui/PaymentEstimate/PaymentEstimate';
import { TermsFields } from '../../../shared/ui/TermsFields/TermsFields';
import { StepLayout } from '../components/StepLayout';
import { type StepProps, useStepGate } from '../stepProps';

export function TermsStep({ product, products, draft, hrefFor, errors, serverErrors }: StepProps) {
    const d = draft.value;
    const terms: TermsValue = { amount: d.requested?.amount ?? '', term: d.requested?.term_months ?? '' };
    const valid = validTerms(product, terms);
    const calculation = useCalculation(product?.code, valid);
    const gate = useStepGate(errors, serverErrors, hrefFor('borrower'));

    const changeTerms = (next: TermsValue) => {
        draft.update({ requested: {
            ...(next.amount !== '' && { amount: next.amount }),
            ...(next.term !== '' && { term_months: next.term }),
        } });
    };

    const purposes = (product?.purposes?.length ? product.purposes : PURPOSES) as CollectorPurpose[];

    return (
        <StepLayout
            title='Сумма, срок и цель'
            subtitle='Можно поменять до отправки заявки'
            backHref={hrefFor('company')}
            onNext={gate.onNext}
            hint={gate.hint}
        >
            <FormCard>
                <Select label='Кредитный продукт' block={true} size={56}
                    options={products.map((p) => ({ key: p.code, content: p.name }))} selected={product?.code ?? null}
                    onChange={({ selected }) => selected && draft.update((prev) => ({ product_code: selected.key,
                        purpose: { ...prev.purpose, code: undefined },
                    }))} error={gate.errorOf('product_code')} />
                {product ? <TermsFields product={product} value={terms} onChange={changeTerms}
                    errors={{ amount: gate.errorOf('requested.amount'), term: gate.errorOf('requested.term_months') }} /> : <>
                    <Input label='Сумма кредита' value='' disabled={true} block={true} size={56} hint='Сначала выберите кредитный продукт' />
                    <Input label='Срок' value='' disabled={true} block={true} size={56} />
                </>}
                {product && <PaymentEstimate payment={valid && calculation.data?.monthly_payment_from} rate={product.rate_annual_from} loading={calculation.isFetching} />}
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
