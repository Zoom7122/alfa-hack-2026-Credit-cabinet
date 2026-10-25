import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';

import { formatMoney, formatRate } from '../../lib/format';
import './PaymentEstimate.css';

type Props = {
    payment?: number;
    rate?: number;
    loading?: boolean;
};

/** Плашка «Ежемесячный платёж от …». */
export function PaymentEstimate({ payment, rate, loading }: Props) {
    return (
        <div className='payment-estimate' role='status' aria-label='Ежемесячный платёж'>
            <Typography.Text view='primary-small' color='secondary'>
                Ежемесячный платёж
            </Typography.Text>
            <Skeleton visible={loading && payment === undefined} className='payment-estimate__skeleton'>
                <Typography.TitleResponsive tag='div' view='medium' font='system' className='payment-estimate__value'>
                    {payment === undefined ? '—' : `от ${formatMoney(payment)}`}
                </Typography.TitleResponsive>
            </Skeleton>
            {rate !== undefined && (
                <Typography.Text view='primary-small' color='secondary'>
                    Ставка от {formatRate(rate)} годовых
                </Typography.Text>
            )}
        </div>
    );
}
