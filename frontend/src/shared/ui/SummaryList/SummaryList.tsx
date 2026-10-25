import type { ReactNode } from 'react';
import { Typography } from '@alfalab/core-components/typography';

import './SummaryList.css';

export type SummaryRow = { label: string; value: ReactNode; bold?: boolean };

/** Список «подпись — значение»: серой панелью (экраны 12, 13) или без фона — внутри карточки. */
export function SummaryList({ rows, plain }: { rows: SummaryRow[]; plain?: boolean }) {
    return (
        <dl className={`summary-list${plain ? ' summary-list_plain' : ''}`}>
            {rows.map((row) => (
                <div key={row.label} className='summary-list__row'>
                    <dt className='summary-list__label'>
                        <Typography.Text view='primary-medium' color='secondary'>
                            {row.label}
                        </Typography.Text>
                    </dt>
                    <dd className='summary-list__value'>
                        <Typography.Text view='primary-medium' weight={row.bold ? 'bold' : 'regular'}>
                            {row.value}
                        </Typography.Text>
                    </dd>
                </div>
            ))}
        </dl>
    );
}
