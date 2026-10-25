import { ProgressBar } from '@alfalab/core-components/progress-bar';
import { Typography } from '@alfalab/core-components/typography';
import { CheckmarkCompactSIcon } from '@alfalab/icons-glyph/CheckmarkCompactSIcon';
import { ExclamationCompactSIcon } from '@alfalab/icons-glyph/ExclamationCompactSIcon';
import { Link } from 'react-router-dom';

import { STEPS, type StepId, type StepState } from '../steps';
import './WizardNav.css';

type Props = {
    productName: string;
    subtitle: string;
    percent: number;
    current: StepId;
    /** Ссылка на шаг; без неё шаги недоступны (черновика ещё нет). */
    hrefFor?: (step: StepId) => string;
    stateOf: (step: StepId) => StepState;
};

const STATE_LABELS: Record<StepState, string> = {
    done: 'заполнено',
    warning: 'есть что добавить',
    todo: 'не заполнено',
};

/** Карточка слева: продукт, прогресс и вертикальные шаги (Steps кита только горизонтальный). */
export function WizardNav({ productName, subtitle, percent, current, hrefFor, stateOf }: Props) {
    return (
        <aside className='wizard-nav' aria-labelledby='wizard-nav-title'>
            <div className='wizard-nav__head'>
                <Typography.Title id='wizard-nav-title' tag='h2' view='xsmall' font='system'>
                    {productName}
                </Typography.Title>
                <Typography.Text view='primary-small' color='secondary'>
                    {subtitle}
                </Typography.Text>
            </div>
            <div className='wizard-nav__progress'>
                <div className='wizard-nav__progress-text'>
                    <Typography.Text view='primary-small' color='secondary'>
                        Заполнено
                    </Typography.Text>
                    <Typography.Text view='primary-small' weight='bold'>
                        {percent}%
                    </Typography.Text>
                </div>
                <ProgressBar value={percent} view='positive' size={8} />
            </div>
            <nav aria-label='Шаги анкеты'>
                <ol className='wizard-nav__steps'>
                    {STEPS.map((step, i) => {
                        const isCurrent = step.id === current;
                        const state = isCurrent ? 'current' : stateOf(step.id);
                        const marker = (
                            <span className={`wizard-nav__marker wizard-nav__marker_state_${state}`} aria-hidden='true'>
                                {state === 'done' ? (
                                    <CheckmarkCompactSIcon />
                                ) : state === 'warning' ? (
                                    <ExclamationCompactSIcon />
                                ) : (
                                    <Typography.Text view='primary-small' weight='bold'>
                                        {i + 1}
                                    </Typography.Text>
                                )}
                            </span>
                        );
                        const label = (
                            <>
                                {marker}
                                <Typography.Text view='primary-medium' weight={isCurrent ? 'bold' : 'regular'}>
                                    {step.title}
                                </Typography.Text>
                            </>
                        );
                        const className = `wizard-nav__step${isCurrent ? ' wizard-nav__step_current' : ''}`;
                        return (
                            <li key={step.id}>
                                {hrefFor && !isCurrent ? (
                                    <Link to={hrefFor(step.id)} className={`${className} wizard-nav__step_link`} aria-label={`${step.title}, ${STATE_LABELS[stateOf(step.id)]}`}>
                                        {label}
                                    </Link>
                                ) : (
                                    <span className={className} aria-current={isCurrent ? 'step' : undefined}>
                                        {label}
                                    </span>
                                )}
                            </li>
                        );
                    })}
                </ol>
            </nav>
        </aside>
    );
}
