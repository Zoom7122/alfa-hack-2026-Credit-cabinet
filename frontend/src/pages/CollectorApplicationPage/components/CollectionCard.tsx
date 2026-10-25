import { Plate } from '@alfalab/core-components/plate';
import { Skeleton } from '@alfalab/core-components/skeleton';
import { Status } from '@alfalab/core-components/status';
import { Typography } from '@alfalab/core-components/typography';

import { useCollection } from '../../../api/collector/queries';
import { SYSTEM_LABELS } from '../../../api/collector/types';
import { SOURCE_STATE, sourceNote } from '../../../shared/lib/collector';
import { pluralize } from '../../../shared/lib/format';
import { FormCard } from '../../../shared/ui/FormCard/FormCard';
import './CollectionCard.css';

/** Итог сбора по системам (GET /collections/{task}): что получили, что пропустили и почему. */
export function CollectionCard({ taskId }: { taskId?: string }) {
    const collection = useCollection(taskId);
    const task = collection.data;

    if (!taskId) return null;
    if (collection.isError && !task) return <Plate view='negative' title='Не удалось узнать, как идёт сбор данных' />;
    if (!task || task.status === 'PENDING' || task.status === 'RUNNING') {
        return (
            <FormCard title='Сбор данных'>
                <Typography.Text view='primary-small' color='secondary' role='status'>
                    {task?.status === 'RUNNING' ? 'Опрашиваем реестры, бюро кредитных историй и банки…' : 'Задача на сбор в очереди…'}
                </Typography.Text>
                <Skeleton visible={true} className='collection-card__skeleton' />
            </FormCard>
        );
    }
    if (task.status === 'FAILED') {
        // FAILED у data-collector — конечное состояние: попытки (WORKER_MAX_ATTEMPTS) исчерпаны, повтора не будет
        return (
            <Plate view='negative' title='Сбор данных не удался'>
                {task.error ? `Причина: ${task.error}. ` : ''}Попыток: {task.attempts ?? 0}. Повторить сбор из кабинета пока нельзя.
            </Plate>
        );
    }

    const sources = task.sources ?? [];
    const received = sources.filter((s) => s.state === 'DONE' || s.state === 'PARTIAL').length;
    return (
        <FormCard title='Сбор данных'>
            <Typography.Text view='primary-small' color='secondary'>
                Получили данные из {received} {pluralize(received, 'системы', 'систем', 'систем')} из {sources.length}
            </Typography.Text>
            <ul className='collection-card__list' aria-label='Системы'>
                {sources.map((s, i) => {
                    const state = s.state ? SOURCE_STATE[s.state] : undefined;
                    const note = sourceNote(s);
                    return (
                        <li key={s.source} className={`collection-card__item${i > 0 ? ' collection-card__item_divided' : ''}`}>
                            <span className='collection-card__name'>
                                <Typography.Text view='primary-medium'>{s.source ? SYSTEM_LABELS[s.source] : '—'}</Typography.Text>
                                {note && (
                                    <Typography.Text view='primary-small' color='secondary'>
                                        {note}
                                    </Typography.Text>
                                )}
                            </span>
                            {state && (
                                <Status view='muted-alt' color={state.color} size={24} uppercase={false}>
                                    {state.label}
                                </Status>
                            )}
                        </li>
                    );
                })}
            </ul>
        </FormCard>
    );
}
