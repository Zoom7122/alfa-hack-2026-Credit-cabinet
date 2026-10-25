import { Typography } from '@alfalab/core-components/typography';

import type { Application } from '../../../api/types';
import { PURPOSE_LABELS } from '../../../shared/lib/dictionaries';
import { formatMoney, formatMonths, shortName } from '../../../shared/lib/format';
import { InfoCard } from '../../../shared/ui/InfoCard/InfoCard';
import './ParamsCard.css';

export function ParamsCard({ app }: { app: Application }) {
    const rows: [string, string | undefined][] = [
        ['Сумма', app.requested && formatMoney(app.requested.amount)],
        ['Срок', app.requested && formatMonths(app.requested.term_months)],
        ['Цель', app.purpose?.description || (app.purpose?.code && PURPOSE_LABELS[app.purpose.code])],
        ['Подал', app.created_by && shortName(app.created_by.full_name)],
    ];
    return (
        <InfoCard title='Параметры заявки'>
            <dl className='params-card'>
                {rows
                    .filter(([, value]) => value)
                    .map(([label, value]) => (
                        <div key={label} className='params-card__row'>
                            <dt className='params-card__label'>
                                <Typography.Text view='primary-medium' color='secondary'>
                                    {label}
                                </Typography.Text>
                            </dt>
                            <dd className='params-card__value'>
                                <Typography.Text view='primary-medium'>{value}</Typography.Text>
                            </dd>
                        </div>
                    ))}
            </dl>
        </InfoCard>
    );
}
