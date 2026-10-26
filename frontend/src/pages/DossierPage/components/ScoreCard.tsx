import { ProgressBar } from '@alfalab/core-components/progress-bar';
import { Typography } from '@alfalab/core-components/typography';

import type { ScoreResult } from '../../../api/types';
import { LIMITED_BY_LABELS } from '../../../shared/lib/backoffice';
import { formatMoney } from '../../../shared/lib/format';
import './ScoreCard.css';

/** Скоринг: вклад каждого признака полоской (масштаб — самый весомый признак) и итог. */
export function ScoreCard({ score }: { score: ScoreResult }) {
    const scale = Math.max(...score.factors.map((f) => Math.abs(f.points)), 1);
    const max = score.max_amount;
    return (
        <section className='score-card' aria-labelledby='score-card-title'>
            <div className='score-card__head'>
                <Typography.Title id='score-card-title' tag='h2' view='xsmall' font='system'>
                    Скоринг · {score.model_version}
                </Typography.Title>
                <Typography.Title tag='div' view='small' font='system'>
                    {score.total}
                </Typography.Title>
            </div>
            {score.factors.length === 0 && (
                <Typography.Text view='primary-small' color='secondary'>
                    Разбивки по критериям нет: заявку оценили до того, как движок стал её присылать
                </Typography.Text>
            )}
            <ul className='score-card__factors' aria-label='Признаки скоринга'>
                {score.factors.map((f) => (
                    <li key={f.code} className='score-card__factor'>
                        <Typography.Text view='primary-small'>
                            {f.title}
                            {f.bucket && ` · ${f.bucket}`}
                            {f.degraded && ' (по умолчанию)'}
                        </Typography.Text>
                        {/* полоска — только для глаз: баллы есть текстом рядом */}
                        <span className='score-card__bar' aria-hidden='true'>
                            <ProgressBar
                                value={(Math.abs(f.points) / scale) * 100}
                                view={f.points < 0 ? 'negative' : f.points >= scale * 0.6 ? 'positive' : 'attention'}
                                size={4}
                            />
                        </span>
                        <Typography.Text view='primary-small' weight='bold' color={f.points < 0 ? 'negative' : 'primary'} className='score-card__points'>
                            {f.points > 0 ? `+${f.points}` : f.points}
                        </Typography.Text>
                    </li>
                ))}
            </ul>
            {max && (
                <div className='score-card__max'>
                    <Typography.Text view='primary-small' color='secondary'>
                        Максимальная сумма
                    </Typography.Text>
                    <Typography.Text view='primary-small' weight='bold'>
                        {max.value !== undefined && formatMoney(max.value)}
                        {max.limited_by && ` · ограничена ${LIMITED_BY_LABELS[max.limited_by]}`}
                    </Typography.Text>
                </div>
            )}
        </section>
    );
}
