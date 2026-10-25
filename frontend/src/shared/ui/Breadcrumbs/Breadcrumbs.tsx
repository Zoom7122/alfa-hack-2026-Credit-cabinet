import { Typography } from '@alfalab/core-components/typography';
import { Link } from 'react-router-dom';

import './Breadcrumbs.css';

type Props = { items: { label: string; to?: string }[] };

/** «Кредиты → Новая заявка»: последний пункт — текущая страница. */
export function Breadcrumbs({ items }: Props) {
    return (
        <nav aria-label='Вы здесь' className='breadcrumbs'>
            {items.map((item, i) => (
                <span key={item.label} className='breadcrumbs__item'>
                    {i > 0 && (
                        <Typography.Text view='primary-small' color='secondary' aria-hidden='true'>
                            →
                        </Typography.Text>
                    )}
                    {item.to ? (
                        <Link to={item.to} className='breadcrumbs__link'>
                            <Typography.Text view='primary-small'>{item.label}</Typography.Text>
                        </Link>
                    ) : (
                        <Typography.Text view='primary-small' color='secondary' aria-current='page'>
                            {item.label}
                        </Typography.Text>
                    )}
                </span>
            ))}
        </nav>
    );
}
