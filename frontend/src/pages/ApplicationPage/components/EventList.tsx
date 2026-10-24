import { Typography } from '@alfalab/core-components/typography';

import type { TimelineEvent } from '../../../api/types';
import { formatUpdated, shortName } from '../../../shared/lib/format';
import './EventList.css';

type Props = {
    events: TimelineEvent[];
    /** Первое событие — то, что идёт сейчас (тёмная точка, как в макете 07). */
    activeFirst?: boolean;
};

/** Список событий истории, новые сверху. */
export function EventList({ events, activeFirst }: Props) {
    return (
        <ol className='event-list'>
            {events.map((e, i) => (
                <li key={e.id} className='event-list__item'>
                    <span
                        className={`event-list__dot${activeFirst && i === 0 ? ' event-list__dot_active' : ''}`}
                        aria-hidden='true'
                    />
                    <span className='event-list__text'>
                        <Typography.Text view='primary-medium' weight={activeFirst && i === 0 ? 'bold' : 'regular'}>
                            {e.message}
                        </Typography.Text>
                        <Typography.Text view='primary-small' color='secondary'>
                            <time dateTime={e.at}>{formatUpdated(e.at)}</time>
                            {e.actor_kind === 'CLIENT' && e.actor && ` · ${shortName(e.actor.full_name)}`}
                        </Typography.Text>
                    </span>
                </li>
            ))}
        </ol>
    );
}
