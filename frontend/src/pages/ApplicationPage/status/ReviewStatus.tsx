import { Plate } from '@alfalab/core-components/plate';
import { Typography } from '@alfalab/core-components/typography';
import { CheckmarkCompactSIcon } from '@alfalab/icons-glyph/CheckmarkCompactSIcon';
import { HourglassMIcon } from '@alfalab/icons-glyph/HourglassMIcon';

import type { ApplicationStatus, ApplicationStatusView } from '../../../api/types';
import { StageBar } from '../components/StageBar';
import { VisuallyHidden } from '../../../shared/ui/VisuallyHidden/VisuallyHidden';
import './ReviewStatus.css';

const ORDER: ApplicationStatus[] = ['SUBMITTED', 'ENRICHING', 'VERIFYING', 'SCORING', 'DECIDING'];
// ручное рассмотрение — тот же шаг «решение», только принимает человек
const rank = (s: ApplicationStatus) => ORDER.indexOf(s === 'MANUAL_REVIEW' ? 'DECIDING' : s);

type Step = { title: string; from: ApplicationStatus; until?: ApplicationStatus };

/** Что сейчас делает банк — без названий источников, только понятные шаги. */
function steps(status: ApplicationStatus): Step[] {
    return [
        { title: 'Заявка принята', from: 'SUBMITTED', until: 'ENRICHING' },
        { title: 'Проверяем данные о компании', from: 'ENRICHING', until: 'SCORING' },
        { title: 'Оцениваем финансовое положение', from: 'SCORING', until: 'DECIDING' },
        { title: status === 'MANUAL_REVIEW' ? 'Решение принимает специалист банка' : 'Готовим решение', from: 'DECIDING' },
    ];
}

type State = 'done' | 'current' | 'upcoming';

function stateOf(step: Step, status: ApplicationStatus): State {
    const r = rank(status);
    if (step.until && r >= rank(step.until)) return 'done';
    return r >= rank(step.from) ? 'current' : 'upcoming';
}

const STATE_LABELS: Record<State, string> = { done: 'готово', current: 'идёт сейчас', upcoming: 'впереди' };

/** Экран 07: банк рассматривает заявку. */
export function ReviewStatus({ status }: { status: ApplicationStatusView }) {
    return (
        <div className='review-status'>
            <section className='review-status__card'>
                <StageBar stages={status.stages} clientStage={status.client_stage} />
                <div className='review-status__head'>
                    <span className='review-status__icon' aria-hidden='true'>
                        <HourglassMIcon />
                    </span>
                    <div className='review-status__titles'>
                        <Typography.Title tag='h2' view='small' font='system'>
                            {status.title}
                        </Typography.Title>
                        {status.subtitle && (
                            <Typography.Text view='primary-large' color='secondary'>
                                {status.subtitle}
                            </Typography.Text>
                        )}
                    </div>
                </div>
                <ul className='review-status__steps' aria-label='Что делает банк'>
                    {steps(status.status).map((step) => {
                        const state = stateOf(step, status.status);
                        return (
                            <li key={step.title} className={`review-status__step review-status__step_state_${state}`}>
                                <span className='review-status__mark' aria-hidden='true'>
                                    {state === 'done' && <CheckmarkCompactSIcon />}
                                </span>
                                <Typography.Text
                                    view='primary-medium'
                                    weight={state === 'current' ? 'bold' : 'regular'}
                                    color={state === 'upcoming' ? 'tertiary' : 'primary'}
                                >
                                    {step.title}
                                    <VisuallyHidden>, {STATE_LABELS[state]}</VisuallyHidden>
                                </Typography.Text>
                            </li>
                        );
                    })}
                </ul>
            </section>
            <Plate view='common'>
                Если банку что-то понадобится, мы спросим здесь и отметим заявку в списке. Писать на почту и звонить не будем.
            </Plate>
        </div>
    );
}
