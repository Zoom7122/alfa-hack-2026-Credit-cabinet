import { Typography } from '@alfalab/core-components/typography';
import { PercentMIcon } from '@alfalab/icons-glyph/PercentMIcon';

import type { Offer } from '../../../api/types';
import { formatDay, formatMoney, formatMonths, formatRate } from '../../../shared/lib/format';
import './RetentionOffer.css';

type Props = { offer: Offer; base?: Offer; companyName: string; titleId?: string };

/** Экран 10: удерживающее предложение после отказа — крупная ставка и чем оно лучше прежнего. */
export function RetentionOffer({ offer, base, companyName, titleId }: Props) {
    const saving = base ? base.monthly_payment - offer.monthly_payment : 0;
    const condition = offer.conditions?.[0];
    const rows: [string, string][] = [
        ['Сумма и срок', `${formatMoney(offer.amount)} на ${formatMonths(offer.term_months)}`],
        ['Платёж в месяц', `${formatMoney(offer.monthly_payment)}${saving > 0 ? ` (−${formatMoney(saving)})` : ''}`],
        ...(condition ? ([['Условие', condition]] as [string, string][]) : []),
        ['Действует до', formatDay(offer.valid_until)],
    ];
    return (
        <div className='retention-offer'>
            <span className='retention-offer__icon' aria-hidden='true'>
                <PercentMIcon />
            </span>
            <div className='retention-offer__head'>
                <Typography.Title id={titleId} tag='h2' view='small' font='system'>
                    Можем снизить ставку
                </Typography.Title>
                <Typography.Text view='primary-medium' color='secondary'>
                    Специально для {companyName}
                    {condition ? ` — ${condition.charAt(0).toLowerCase()}${condition.slice(1)}` : ''}
                </Typography.Text>
            </div>
            <div className='retention-offer__panel'>
                <div className='retention-offer__rate'>
                    <Typography.TitleResponsive tag='div' view='medium' font='system'>
                        {formatRate(offer.rate_annual)}
                    </Typography.TitleResponsive>
                    {base && (
                        <Typography.Text view='primary-medium' color='tertiary'>
                            <s aria-label={`было ${formatRate(base.rate_annual)}`}>{formatRate(base.rate_annual)}</s>
                        </Typography.Text>
                    )}
                </div>
                <dl className='retention-offer__rows'>
                    {rows.map(([label, value]) => (
                        <div key={label} className='retention-offer__row'>
                            <dt className='retention-offer__label'>
                                <Typography.Text view='primary-small' color='secondary'>
                                    {label}
                                </Typography.Text>
                            </dt>
                            <dd className='retention-offer__value'>
                                <Typography.Text view='primary-small' weight='bold'>
                                    {value}
                                </Typography.Text>
                            </dd>
                        </div>
                    ))}
                </dl>
            </div>
        </div>
    );
}
