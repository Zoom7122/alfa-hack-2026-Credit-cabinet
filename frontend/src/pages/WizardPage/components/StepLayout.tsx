import { type ReactNode, useEffect, useRef } from 'react';
import { Button } from '@alfalab/core-components/button';
import { Typography } from '@alfalab/core-components/typography';
import { Link } from 'react-router-dom';

import './StepLayout.css';

type Props = {
    title: string;
    subtitle?: ReactNode;
    children: ReactNode;
    backHref?: string;
    nextLabel?: string;
    nextHref?: string;
    onNext?: () => void;
    nextDisabled?: boolean;
    nextLoading?: boolean;
    /** Подсказка справа от кнопок — почему нельзя дальше. */
    hint?: ReactNode;
};

/** Заголовок шага, карточки формы и кнопки «Назад / Продолжить». */
export function StepLayout({ title, subtitle, children, backHref, nextLabel = 'Продолжить', nextHref, onNext, nextDisabled, nextLoading, hint }: Props) {
    const headRef = useRef<HTMLDivElement>(null);
    // новый шаг: наверх страницы и фокус на заголовок, чтобы скринридер прочитал, куда попали
    useEffect(() => {
        window.scrollTo(0, 0);
        headRef.current?.focus({ preventScroll: true });
    }, []);

    return (
        <div className='step-layout'>
            <div ref={headRef} tabIndex={-1} className='step-layout__head'>
                <Typography.TitleResponsive tag='h1' view='medium' font='system'>
                    {title}
                </Typography.TitleResponsive>
                {subtitle && (
                    <Typography.Text view='primary-large' color='secondary'>
                        {subtitle}
                    </Typography.Text>
                )}
            </div>
            {children}
            <div className='step-layout__actions'>
                {backHref && (
                    <Button Component={Link} href={backHref} view='secondary' size={56}>
                        Назад
                    </Button>
                )}
                {nextHref && !nextDisabled ? (
                    <Button Component={Link} href={nextHref} view='primary' size={56} onClick={onNext}>
                        {nextLabel}
                    </Button>
                ) : (
                    <Button view='primary' size={56} onClick={onNext} disabled={nextDisabled} loading={nextLoading}>
                        {nextLabel}
                    </Button>
                )}
                {hint && (
                    <Typography.Text view='primary-small' color='secondary'>
                        {hint}
                    </Typography.Text>
                )}
            </div>
        </div>
    );
}
