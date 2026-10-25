import { Button } from '@alfalab/core-components/button';
import { Divider } from '@alfalab/core-components/divider';
import { Status } from '@alfalab/core-components/status';
import { Typography } from '@alfalab/core-components/typography';

import type { Offer } from '../../../api/types';
import { formatMoney, formatMonthsLong, formatRate } from '../../../shared/lib/format';
import './OfferCard.css';

type Props = {
    offer: Offer;
    /** Предложение «как запросили» — с ним сравниваем альтернативы для бейджа. */
    base?: Offer;
    guarantorNames: string[];
    onChoose: () => void;
    loading?: boolean;
    disabled?: boolean;
};

/** Короткое отличие от запрошенного — по цифрам, а не по тексту бэка. */
function badgeFor(offer: Offer, base: Offer | undefined) {
    if (offer.kind === 'REQUESTED') return 'Как вы запросили';
    if (!base) return undefined;
    if (offer.term_months > base.term_months && offer.monthly_payment < base.monthly_payment) return 'Дольше и дешевле в месяц';
    if (base.guarantee_required && !offer.guarantee_required) return 'Без поручителя';
    if (offer.amount < base.amount) return 'Меньше сумма';
    if (offer.rate_annual < base.rate_annual) return 'Ниже ставка';
    return undefined;
}

/** Экран 09: одно кредитное предложение. */
export function OfferCard({ offer, base, guarantorNames, onChoose, loading, disabled }: Props) {
    const badge = badgeFor(offer, base);
    const overpayment = offer.total_payment !== undefined ? offer.total_payment - offer.amount : undefined;
    const titleId = `offer-${offer.id}`;
    const rows: [string, string, boolean?][] = [
        ['Ставка', `${formatRate(offer.rate_annual)} годовых`, true],
        ['Платёж в месяц', formatMoney(offer.monthly_payment), true],
        ...(overpayment !== undefined ? ([['Переплата', formatMoney(overpayment)]] as [string, string][]) : []),
        ...(offer.full_cost_rate !== undefined ? ([['ПСК', formatRate(offer.full_cost_rate)]] as [string, string][]) : []),
        ['Поручительство', offer.guarantee_required ? guarantorNames.join(', ') || 'Нужно' : 'Не нужно'],
    ];

    return (
        <article className={`offer-card${offer.recommended ? ' offer-card_recommended' : ''}`} aria-labelledby={titleId}>
            <div className='offer-card__badges'>
                {offer.recommended && (
                    <Status view='contrast' color='grey' size={24} uppercase={false}>
                        Рекомендуем
                    </Status>
                )}
                {badge && (
                    <Status view='muted-alt' color='grey' size={24} uppercase={false}>
                        {badge}
                    </Status>
                )}
            </div>
            <div>
                <Typography.TitleResponsive id={titleId} tag='h3' view='medium' font='system'>
                    {formatMoney(offer.amount)}
                </Typography.TitleResponsive>
                <Typography.Text view='primary-large' color='secondary'>
                    на {formatMonthsLong(offer.term_months)}
                </Typography.Text>
            </div>
            <Divider />
            <dl className='offer-card__rows'>
                {rows.map(([label, value, bold]) => (
                    <div key={label} className='offer-card__row'>
                        <dt className='offer-card__label'>
                            <Typography.Text view='primary-medium' color='secondary'>
                                {label}
                            </Typography.Text>
                        </dt>
                        <dd className='offer-card__value'>
                            <Typography.Text view='primary-medium' weight={bold ? 'bold' : 'regular'}>
                                {value}
                            </Typography.Text>
                        </dd>
                    </div>
                ))}
            </dl>
            {(offer.conditions?.length || offer.difference_from_requested) && (
                <ul className='offer-card__notes'>
                    {offer.conditions?.map((c) => (
                        <li key={c}>
                            <Typography.Text view='primary-small' color='secondary'>
                                {c}
                            </Typography.Text>
                        </li>
                    ))}
                    {offer.difference_from_requested && (
                        <li>
                            <Typography.Text view='primary-small' color='attention'>
                                {offer.difference_from_requested}
                            </Typography.Text>
                        </li>
                    )}
                </ul>
            )}
            <Button
                view={offer.recommended ? 'primary' : 'secondary'}
                size={56}
                block={true}
                className='offer-card__choose'
                onClick={onChoose}
                loading={loading}
                disabled={disabled}
                aria-describedby={titleId}
            >
                Выбрать
            </Button>
        </article>
    );
}
