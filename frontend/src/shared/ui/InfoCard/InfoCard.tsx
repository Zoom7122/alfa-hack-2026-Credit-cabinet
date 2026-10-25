import { type ReactNode, useId } from 'react';
import { Typography } from '@alfalab/core-components/typography';

import './InfoCard.css';

/** Карточка в правой колонке: заголовок капсом и содержимое. */
export function InfoCard({ title, children }: { title: string; children: ReactNode }) {
    const titleId = useId();
    return (
        <section className='info-card' aria-labelledby={titleId}>
            <h2 id={titleId} className='info-card__title'>
                <Typography.Text view='caps' color='tertiary' weight='bold'>
                    {title}
                </Typography.Text>
            </h2>
            {children}
        </section>
    );
}
