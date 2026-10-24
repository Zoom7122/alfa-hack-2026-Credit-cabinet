import type { KeyboardEvent } from 'react';
import { Status } from '@alfalab/core-components/status';
import { Typography } from '@alfalab/core-components/typography';

import type { Product } from '../../../api/types';
import { formatCompactMoney, formatRate } from '../../../shared/lib/format';
import './ProductCard.css';

type Props = {
    product: Product;
    checked: boolean;
    recommended?: boolean;
    hasActiveApplication?: boolean;
    onSelect: () => void;
    onKeyDown?: (e: KeyboardEvent<HTMLElement>) => void;
};

function collateralText(product: Product) {
    const threshold = product.collateral?.required_above_amount;
    if (threshold === null || threshold === undefined) return 'не нужен';
    if (threshold === 0) return 'обязателен';
    return `от ${formatCompactMoney(threshold)} ₽`;
}

/**
 * Продукт — один вариант в radiogroup каталога. Содержимое role=radio скринридер читает как подпись,
 * поэтому название — не заголовок, а текст, на который ссылается aria-labelledby.
 */
export function ProductCard({ product, checked, recommended, hasActiveApplication, onSelect, onKeyDown }: Props) {
    const facts = [
        ['Сумма', `до ${formatCompactMoney(product.amount.max)} ₽`],
        ['Срок', `${product.term_months.min}–${product.term_months.max} мес.`],
        ['Залог', collateralText(product)],
    ];

    return (
        <div
            role='radio'
            aria-checked={checked}
            aria-labelledby={`product-${product.code}`}
            tabIndex={checked ? 0 : -1}
            className={`product-card${checked ? ' product-card_checked' : ''}`}
            onClick={onSelect}
            onKeyDown={(e) => {
                if (e.key === ' ' || e.key === 'Enter') {
                    e.preventDefault();
                    onSelect();
                }
                onKeyDown?.(e);
            }}
        >
            <div className='product-card__main'>
                <div className='product-card__title'>
                    <Typography.Title id={`product-${product.code}`} tag='div' view='small' font='system'>
                        {product.name}
                    </Typography.Title>
                    {hasActiveApplication ? (
                        <Status view='muted-alt' color='orange' size={24} uppercase={false}>
                            Уже есть заявка
                        </Status>
                    ) : (
                        recommended && (
                            <Status view='muted-alt' color='green' size={24} uppercase={false}>
                                Подходит лучше всего
                            </Status>
                        )
                    )}
                </div>
                {product.description && (
                    <Typography.Text view='primary-small' color='secondary'>
                        {product.description}
                    </Typography.Text>
                )}
                <div className='product-card__facts'>
                    {facts.map(([label, value]) => (
                        <div key={label} className='product-card__fact'>
                            <Typography.Text view='primary-small' color='secondary'>
                                {label}
                            </Typography.Text>
                            <Typography.Text view='primary-medium' weight='bold'>
                                {value}
                            </Typography.Text>
                        </div>
                    ))}
                </div>
            </div>
            <div className='product-card__rate'>
                <Typography.Text view='primary-small' color='secondary'>
                    ставка
                </Typography.Text>
                <Typography.Title tag='div' view='small' font='system'>
                    от {formatRate(product.rate_annual_from)}
                </Typography.Title>
            </div>
        </div>
    );
}
