import type { ReactNode } from 'react';
import { Typography } from '@alfalab/core-components/typography';

import './FormCard.css';

type Props = {
    title?: ReactNode;
    subtitle?: ReactNode;
    /** Справа от заголовка: «Изменить», счётчик и т. п. */
    addon?: ReactNode;
    children: ReactNode;
    className?: string;
};

/** Белая карточка-секция формы. */
export function FormCard({ title, subtitle, addon, children, className }: Props) {
    return (
        <section className={['form-card', className].filter(Boolean).join(' ')}>
            {(title || addon) && (
                <div className='form-card__head'>
                    <div className='form-card__titles'>
                        {title && (
                            <Typography.Title tag='h2' view='xsmall' font='system'>
                                {title}
                            </Typography.Title>
                        )}
                        {subtitle && (
                            <Typography.Text view='primary-small' color='secondary'>
                                {subtitle}
                            </Typography.Text>
                        )}
                    </div>
                    {addon}
                </div>
            )}
            {children}
        </section>
    );
}
