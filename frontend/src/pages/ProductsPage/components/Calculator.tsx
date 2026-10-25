import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Typography } from '@alfalab/core-components/typography';
import { Link } from 'react-router-dom';

import { useCalculation } from '../../../api/queries';
import type { ApplicationSummary, Product } from '../../../api/types';
import { applicationPath } from '../../../shared/lib/routes';
import { type TermsValue, validTerms } from '../../../shared/lib/terms';
import { PaymentEstimate } from '../../../shared/ui/PaymentEstimate/PaymentEstimate';
import { TermsFields } from '../../../shared/ui/TermsFields/TermsFields';
import './Calculator.css';

type Props = {
    product: Product;
    value: TermsValue;
    onChange: (value: TermsValue) => void;
    /** Активная заявка компании на этот продукт — вторую банк не примет (409 ACTIVE_APPLICATION_EXISTS). */
    existing?: ApplicationSummary;
    onApply: () => void;
};

export function Calculator({ product, value, onChange, existing, onApply }: Props) {
    const terms = validTerms(product, value);
    const calculation = useCalculation(product.code, terms);

    return (
        <aside className='calculator' aria-labelledby='calculator-title'>
            <Typography.Title id='calculator-title' tag='h2' view='small' font='system'>
                Рассчитайте платёж
            </Typography.Title>
            <TermsFields product={product} value={value} onChange={onChange} />
            <PaymentEstimate
                payment={terms ? calculation.data?.monthly_payment_from : undefined}
                rate={calculation.data?.rate_annual_from ?? product.rate_annual_from}
                loading={calculation.isFetching}
            />
            <Typography.Text view='primary-small' color='secondary'>
                {calculation.data?.disclaimer ?? 'Предварительный расчёт по условиям продукта. Точные условия — после рассмотрения заявки.'}
            </Typography.Text>
            {existing ? (
                <Plate view='attention' title='У компании уже есть заявка на этот продукт'>
                    <Button Component={Link} href={applicationPath(existing)} view='secondary' size={40}>
                        Перейти к заявке {existing.number}
                    </Button>
                </Plate>
            ) : (
                <Button view='accent' size={56} block={true} disabled={!terms} onClick={onApply}>
                    Оформить заявку
                </Button>
            )}
        </aside>
    );
}
