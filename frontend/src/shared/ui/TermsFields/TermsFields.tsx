import { AmountInput } from '@alfalab/core-components/amount-input';
import { Input } from '@alfalab/core-components/input';
import { SliderInput } from '@alfalab/core-components/slider-input';
import { withSuffix } from '@alfalab/core-components/with-suffix';

import type { Product } from '../../../api/types';
import { clamp, type TermsValue } from '../../lib/terms';
import { formatCompactMoney, formatMoney, pluralize } from '../../lib/format';
import './TermsFields.css';

const MonthsInput = withSuffix(Input);


type Props = {
    product: Pick<Product, 'amount' | 'term_months'>;
    value: TermsValue;
    onChange: (value: TermsValue) => void;
    size?: 56 | 64;
    errors?: { amount?: string; term?: string };
};

/** Пара «сумма + срок» со слайдерами в пределах продукта. */
export function TermsFields({ product, value, onChange, size = 56, errors }: Props) {
    const { amount, term_months: term } = product;
    const amountStep = amount.max - amount.min > 5_000_000 ? 100_000 : 10_000;
    const amountError =
        value.amount !== '' && (value.amount < amount.min || value.amount > amount.max)
            ? `От ${formatMoney(amount.min)} до ${formatMoney(amount.max)}`
            : undefined;

    return (
        <div className='terms-fields'>
            <SliderInput
                block={true}
                size={size}
                label='Сумма кредита'
                Input={AmountInput}
                customInputProps={{ minority: 1, integersOnly: true }}
                value={value.amount}
                sliderValue={value.amount === '' ? amount.min : value.amount}
                min={amount.min}
                max={amount.max}
                step={amountStep}
                steps={[formatCompactMoney(amount.min), formatCompactMoney(amount.max)]}
                error={errors?.amount ?? amountError}
                onInputChange={(_, { value: v }) => onChange({ ...value, amount: v })}
                onSliderChange={({ value: v }) => onChange({ ...value, amount: v })}
                onBlur={() => value.amount !== '' && onChange({ ...value, amount: clamp(value.amount, amount.min, amount.max) })}
            />
            <SliderInput
                block={true}
                size={size}
                label='Срок'
                Input={MonthsInput}
                customInputProps={{
                    suffix: value.term === '' ? '' : ` ${pluralize(value.term, 'месяц', 'месяца', 'месяцев')}`,
                }}
                value={value.term}
                sliderValue={value.term === '' ? term.min : value.term}
                min={term.min}
                max={term.max}
                step={1}
                steps={[`${term.min} мес.`, `${term.max} мес.`]}
                error={errors?.term}
                onInputChange={(_, { value: v }) => onChange({ ...value, term: v })}
                onSliderChange={({ value: v }) => onChange({ ...value, term: v })}
                onBlur={() => value.term !== '' && onChange({ ...value, term: clamp(value.term, term.min, term.max) })}
            />
        </div>
    );
}
