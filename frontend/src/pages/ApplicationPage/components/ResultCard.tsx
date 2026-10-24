import type { ReactNode } from 'react';
import { Typography } from '@alfalab/core-components/typography';

import './ResultCard.css';

type Props = {
    icon: ReactNode;
    tone: 'neutral' | 'positive';
    title: string;
    text?: ReactNode;
    children?: ReactNode;
    actions?: ReactNode;
};

/** Центрированная карточка итога этапа: «ждём директора», «кредит выдан». */
export function ResultCard({ icon, tone, title, text, children, actions }: Props) {
    return (
        <section className='result-card' aria-labelledby='result-card-title'>
            <span className={`result-card__icon result-card__icon_tone_${tone}`} aria-hidden='true'>
                {icon}
            </span>
            <div className='result-card__text'>
                <Typography.TitleResponsive id='result-card-title' tag='h2' view='medium' font='system'>
                    {title}
                </Typography.TitleResponsive>
                {text && (
                    <Typography.Text view='primary-large' color='secondary'>
                        {text}
                    </Typography.Text>
                )}
            </div>
            <div className='result-card__body'>{children}</div>
            {actions && <div className='result-card__actions'>{actions}</div>}
        </section>
    );
}
