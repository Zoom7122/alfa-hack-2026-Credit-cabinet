import { Button } from '@alfalab/core-components/button';
import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Typography } from '@alfalab/core-components/typography';
import { CrossMIcon } from '@alfalab/icons-glyph/CrossMIcon';
import { Link } from 'react-router-dom';

import { ApiError } from '../../../api/client';
import { useDecision } from '../../../api/queries';
import type { Application } from '../../../api/types';
import { formatCalendarDate, formatDate } from '../../../shared/lib/format';
import './RejectedStatus.css';

type Props = { app: Application };

/** Экран 14: банк отказал — понятные причины без баллов и источников, что можно сделать, когда подать снова. */
export function RejectedStatus({ app }: Props) {
    const decision = useDecision(app.id);
    const data = decision.data;
    // 404 по контракту — «решения ещё нет» (статус пришёл раньше объяснения), это не ошибка
    const pending = decision.error instanceof ApiError && decision.error.status === 404;

    return (
        <section className='rejected-status' aria-labelledby='rejected-status-title'>
            <div className='rejected-status__head'>
                <span className='rejected-status__icon' aria-hidden='true'>
                    <CrossMIcon />
                </span>
                <div className='rejected-status__titles'>
                    <Typography.TitleResponsive id='rejected-status-title' tag='h2' view='medium' font='system'>
                        Сейчас не можем выдать кредит
                    </Typography.TitleResponsive>
                    {data && (
                        <Typography.Text view='primary-medium' color='secondary'>
                            Решение от {formatDate(data.decided_at)}
                        </Typography.Text>
                    )}
                </div>
            </div>

            {decision.isLoading && <Skeleton visible={true} className='rejected-status__skeleton' />}
            {pending && (
                <Typography.Text view='primary-medium' color='secondary' role='status'>
                    Готовим объяснение решения — оно появится здесь через несколько минут.
                </Typography.Text>
            )}
            {decision.isError && !pending && <Plate view='negative' title='Не удалось загрузить объяснение решения' />}

            {data?.reasons && data.reasons.length > 0 && (
                <div className='rejected-status__section'>
                    <Typography.Title tag='h3' view='xsmall' font='system'>
                        Что повлияло на решение
                    </Typography.Title>
                    <ol className='rejected-status__reasons' role='list'>
                        {data.reasons.map((reason, i) => (
                            <li key={reason.code} className='rejected-status__reason'>
                                <Typography.Text view='primary-medium' weight='bold' color='accent' className='rejected-status__number'>
                                    {i + 1}
                                </Typography.Text>
                                <span className='rejected-status__reason-text'>
                                    <Typography.Text view='primary-medium' weight='bold'>
                                        {reason.title}
                                    </Typography.Text>
                                    {reason.description && (
                                        <Typography.Text view='primary-small' color='secondary'>
                                            {reason.description}
                                        </Typography.Text>
                                    )}
                                </span>
                            </li>
                        ))}
                    </ol>
                </div>
            )}

            {data?.recommendations && data.recommendations.length > 0 && (
                <div className='rejected-status__section'>
                    <Typography.Title tag='h3' view='xsmall' font='system'>
                        Что можно сделать
                    </Typography.Title>
                    <ul className='rejected-status__tips' role='list'>
                        {data.recommendations.map((tip) => (
                            <li key={tip}>
                                <Typography.Text view='primary-medium'>{tip}</Typography.Text>
                            </li>
                        ))}
                    </ul>
                </div>
            )}

            {data?.reapply_after && (
                <Plate view='common'>
                    Подать новую заявку можно после{' '}
                    <Typography.Text view='primary-small' weight='bold'>
                        {formatCalendarDate(data.reapply_after)}
                    </Typography.Text>
                    . Данные этой заявки сохранятся — заново заполнять не придётся.
                </Plate>
            )}

            <div className='rejected-status__actions'>
                <Button Component={Link} href='/' view='primary' size={56}>
                    На главную
                </Button>
                <Button Component={Link} href='/products' view='secondary' size={56}>
                    Подобрать другой кредит
                </Button>
            </div>
        </section>
    );
}
