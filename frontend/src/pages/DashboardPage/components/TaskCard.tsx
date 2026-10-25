import { Button } from '@alfalab/core-components/button';
import { Typography } from '@alfalab/core-components/typography';
import { DocumentCheckmarkMIcon } from '@alfalab/icons-glyph/DocumentCheckmarkMIcon';
import { PenNibMIcon } from '@alfalab/icons-glyph/PenNibMIcon';
import { QuestionMIcon } from '@alfalab/icons-glyph/QuestionMIcon';
import { Link } from 'react-router-dom';

import './TaskCard.css';

export type Task = {
    id: string;
    kind: 'sign' | 'info' | 'offer';
    title: string;
    subtitle: string;
    action: string;
};

const ICONS = { sign: PenNibMIcon, info: QuestionMIcon, offer: DocumentCheckmarkMIcon };

export function TaskCard({ task }: { task: Task }) {
    const Icon = ICONS[task.kind];
    return (
        <article className='task-card'>
            <span className={`task-card__icon task-card__icon_kind_${task.kind}`} aria-hidden='true'>
                <Icon />
            </span>
            <div className='task-card__text'>
                <Typography.Text tag='div' view='primary-large' weight='bold'>
                    {task.title}
                </Typography.Text>
                <Typography.Text tag='div' view='primary-small' color='secondary'>
                    {task.subtitle}
                </Typography.Text>
            </div>
            <Button
                Component={Link}
                href={`/applications/${task.id}`}
                view={task.kind === 'sign' ? 'primary' : 'secondary'}
                size={48}
                className='task-card__action'
            >
                {task.action}
            </Button>
        </article>
    );
}
